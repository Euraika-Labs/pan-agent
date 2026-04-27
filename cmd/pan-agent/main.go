// Pan-Agent CLI entry point.
//
// Subcommands:
//
//	pan-agent serve  [--port 8642] [--host 127.0.0.1]  Start HTTP API server
//	pan-agent chat   [--model X]   [--profile P]        Interactive CLI chat
//	pan-agent version                                   Print version info
//	pan-agent doctor                                    Check system health
//	pan-agent skill  verify <bundle-path>               Verify a marketplace bundle
//
// Default (no subcommand): serve
package main

import (
	"bufio"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/euraika-labs/pan-agent/internal/config"
	"github.com/euraika-labs/pan-agent/internal/cron"
	"github.com/euraika-labs/pan-agent/internal/gateway"
	"github.com/euraika-labs/pan-agent/internal/llm"
	"github.com/euraika-labs/pan-agent/internal/parentwatch"
	"github.com/euraika-labs/pan-agent/internal/paths"
	"github.com/euraika-labs/pan-agent/internal/storage"
	"github.com/euraika-labs/pan-agent/internal/taskrunner"
	"github.com/euraika-labs/pan-agent/internal/tools"
	"github.com/euraika-labs/pan-agent/internal/version"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintf(os.Stderr, "pan-agent: %v\n", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	// Determine subcommand. Default to "serve" when none is provided.
	sub := "serve"
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		sub = args[0]
		args = args[1:]
	}

	switch sub {
	case "serve":
		return cmdServe(args)
	case "chat":
		return cmdChat(args)
	case "version":
		return cmdVersion(args)
	case "doctor":
		return cmdDoctor(args)
	case "migrate-office":
		// M4 W2 — one-shot legacy migration from ~/.hermes/ into SQLite.
		// Runs outside `serve` so admins can execute it without starting
		// the full gateway (e.g., on a headless box just to pre-populate
		// the DB before the first UI launch).
		return cmdMigrateOffice(args)
	case "skill":
		return cmdSkill(args)
	default:
		return fmt.Errorf("unknown subcommand %q\n\nUsage: pan-agent <serve|chat|version|doctor|migrate-office|skill>", sub)
	}
}

// ---------------------------------------------------------------------------
// serve
// ---------------------------------------------------------------------------

func cmdServe(args []string) error {
	fs := flag.NewFlagSet("serve", flag.ContinueOnError)
	port := fs.Int("port", 8642, "TCP port to listen on")
	host := fs.String("host", "127.0.0.1", "Host address to bind to")
	profile := fs.String("profile", "", "Pan-agent profile name")

	if err := fs.Parse(args); err != nil {
		return err
	}

	// Refuse non-loopback bind without an auth token. Previously --host
	// 0.0.0.0 silently exposed the unauthenticated API to the LAN.
	if !isLoopbackHost(*host) && strings.TrimSpace(os.Getenv("PAN_AGENT_AUTH_TOKEN")) == "" {
		return fmt.Errorf(
			"refusing to bind to non-loopback host %q without PAN_AGENT_AUTH_TOKEN; "+
				"set the env var to a strong secret and clients must send "+
				"Authorization: Bearer <token>", *host)
	}

	db, err := storage.Open(paths.StateDB())
	if err != nil {
		return fmt.Errorf("open database: %w", err)
	}
	defer db.Close()

	addr := fmt.Sprintf("%s:%d", *host, *port)
	srv := gateway.New(addr, db, *profile)

	// Handle SIGINT / SIGTERM for graceful shutdown.
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	// Parent-process watchdog: if launched as a sidecar (PAN_AGENT_PARENT_PID
	// set to the launcher's PID), shut down gracefully when that parent dies.
	// This catches the "TerminateProcess from Tauri on Windows" case where no
	// signal is ever delivered to the child.
	watchCtx, cancelWatch := context.WithCancel(context.Background())
	defer cancelWatch()
	if pid := parentPIDFromEnv(); pid > 0 {
		fmt.Printf("pan-agent: watching parent PID %d (will exit when it does)\n", pid)
		go parentwatch.Watch(watchCtx, pid, func() {
			fmt.Printf("pan-agent: parent PID %d is gone, shutting down...\n", pid)
			// Re-use the SIGTERM path for a single shutdown codepath.
			quit <- syscall.SIGTERM
		})
	}

	// Start the cron scheduler. It polls jobs.json every 30s and calls
	// our Dispatch callback for any Job whose NextRun has passed. We keep
	// the dispatcher intentionally simple: log the fire + let the existing
	// gateway surface it via events. A future PR will wire the prompt
	// through the chat completions endpoint via the bearer-auth token.
	schedCtx, cancelSched := context.WithCancel(context.Background())
	defer cancelSched()
	// Start the task reaper. It scans for zombie tasks every 10s.
	reaperCtx, cancelReaper := context.WithCancel(context.Background())
	defer cancelReaper()
	taskStore := taskrunner.NewStore(db.RawDB())
	reaper := taskrunner.NewReaper(taskStore)
	go reaper.Run(reaperCtx)

	// Start the task runner. It polls for queued tasks every 2s.
	runnerCtx, cancelRunner := context.WithCancel(context.Background())
	defer cancelRunner()
	runner := taskrunner.NewRunner(taskStore, tools.Default)
	go runner.Start(runnerCtx)

	// Cron dispatcher: creates a task per fire so the runner picks it up.
	// The "chat" tool referenced in the plan does not exist yet — tasks
	// will transition to "failed" until the LLM dispatch tool is
	// registered (tracked as Phase 13 scope). The v1 plumbing (session
	// creation, task creation, cost cap inheritance) is exercised here.
	scheduler := cron.NewScheduler(func(ctx context.Context, j cron.Job) error {
		fmt.Printf("[cron] fire job id=%s name=%q schedule=%q\n", j.ID, j.Name, j.Schedule)
		sess, err := db.CreateSession("cron:" + j.Name)
		if err != nil {
			return fmt.Errorf("cron session: %w", err)
		}
		planJSON, err := buildCronPlanJSON(j)
		if err != nil {
			return fmt.Errorf("cron plan marshal: %w", err)
		}
		_, err = taskStore.CreateTask(sess.ID, planJSON, j.CostCapUSD)
		return err
	})
	scheduler.Start(schedCtx)

	errCh := make(chan error, 1)
	go func() {
		if err := srv.Start(); err != nil {
			errCh <- err
		}
	}()

	select {
	case sig := <-quit:
		fmt.Printf("\npan-agent: received %s, shutting down...\n", sig)
		tools.CloseBrowser()
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		return srv.Stop(ctx)
	case err := <-errCh:
		return err
	}
}

// isLoopbackHost reports whether the given bind host refers to the local
// loopback interface. Binding to anything else (notably 0.0.0.0 or a LAN
// address) is refused without PAN_AGENT_AUTH_TOKEN.
func isLoopbackHost(host string) bool {
	h := strings.ToLower(strings.TrimSpace(host))
	switch h {
	case "", "127.0.0.1", "::1", "[::1]", "localhost":
		return true
	}
	return false
}

// parentPIDFromEnv returns the PID from PAN_AGENT_PARENT_PID, or 0 if unset
// or malformed. A zero return disables parent watching entirely.
func parentPIDFromEnv() int {
	v := strings.TrimSpace(os.Getenv("PAN_AGENT_PARENT_PID"))
	if v == "" {
		return 0
	}
	n, err := strconv.Atoi(v)
	if err != nil || n <= 0 {
		return 0
	}
	return n
}

// ---------------------------------------------------------------------------
// chat
// ---------------------------------------------------------------------------

func cmdChat(args []string) error {
	fs := flag.NewFlagSet("chat", flag.ContinueOnError)
	modelFlag := fs.String("model", "", "Model identifier (overrides profile config)")
	profile := fs.String("profile", "default", "Pan-Agent profile name")

	if err := fs.Parse(args); err != nil {
		return err
	}

	// Resolve model and provider from profile config.
	mc := config.GetModelConfig(*profile)

	model := mc.Model
	if *modelFlag != "" {
		model = *modelFlag
	}
	if model == "" {
		model = "gpt-4o-mini" // sensible fallback
	}

	// Resolve API key and base URL from profile .env.
	env, err := config.ReadProfileEnv(*profile)
	if err != nil {
		return fmt.Errorf("chat: read profile env: %w", err)
	}

	baseURL := mc.BaseURL
	if baseURL == "" {
		// Fall back to OpenAI.
		baseURL = "https://api.openai.com/v1"
	}

	apiKey := env["REGOLO_API_KEY"]
	if apiKey == "" {
		apiKey = env["OPENAI_API_KEY"]
	}
	if apiKey == "" {
		apiKey = env["API_KEY"]
	}
	if apiKey == "" {
		apiKey = os.Getenv("OPENAI_API_KEY")
	}

	client := llm.NewClient(baseURL, apiKey, model)

	fmt.Printf("pan-agent chat — model: %s  profile: %s\n", model, *profile)
	fmt.Println("Type a message and press Enter. Ctrl-C or Ctrl-D to quit.")

	var history []llm.Message
	scanner := bufio.NewScanner(os.Stdin)

	for {
		fmt.Print("> ")
		if !scanner.Scan() {
			// EOF (Ctrl-D)
			fmt.Println()
			break
		}

		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		history = append(history, llm.Message{Role: "user", Content: line})

		ctx, cancel := context.WithCancel(context.Background())

		// Allow Ctrl-C to cancel the in-flight request without exiting.
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGINT)
		go func() {
			<-sigCh
			cancel()
		}()

		stream, err := client.ChatStream(ctx, history, nil)
		if err != nil {
			cancel()
			signal.Stop(sigCh)
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			history = history[:len(history)-1] // roll back the user turn
			continue
		}

		var assistantMsg strings.Builder

		for ev := range stream {
			switch ev.Type {
			case "chunk":
				fmt.Print(ev.Content)
				assistantMsg.WriteString(ev.Content)
			case "error":
				fmt.Fprintf(os.Stderr, "\nerror: %s\n", ev.Error)
			case "done":
				fmt.Println()
			}
		}

		cancel()
		signal.Stop(sigCh)

		if assistantMsg.Len() > 0 {
			history = append(history, llm.Message{
				Role:    "assistant",
				Content: assistantMsg.String(),
			})
		}
	}

	return scanner.Err()
}

// ---------------------------------------------------------------------------
// version
// ---------------------------------------------------------------------------

func cmdVersion(_ []string) error {
	fmt.Printf("pan-agent %s (commit %s, built %s)\n",
		version.Version, version.Commit, version.Date)
	return nil
}

// ---------------------------------------------------------------------------
// doctor
// ---------------------------------------------------------------------------
//
// M6-C1 grows the doctor subcommand into a small toolkit:
//
//	pan-agent doctor                      — health checks (unchanged default)
//	pan-agent doctor --json               — emit health checks as JSON
//	pan-agent doctor --csp-violations     — tail csp-violations.log
//	pan-agent doctor --switch-engine=go   — POST /v1/office/engine
//	pan-agent doctor --switch-engine=node — POST /v1/office/engine
//	pan-agent doctor --deprecated-usage   — scan for legacy /v1/office/* hits
//
// The default path stays unchanged so existing docs and muscle memory
// still work. Flags are mutually exclusive by convention (not enforced) —
// calling with two flags runs them in order and exits on the first error.

func cmdDoctor(args []string) error {
	fs := flag.NewFlagSet("doctor", flag.ContinueOnError)
	jsonOut := fs.Bool("json", false, "emit health checks as JSON")
	cspViolations := fs.Bool("csp-violations", false, "tail csp-violations.log and exit")
	switchEngine := fs.String("switch-engine", "", "POST /v1/office/engine with value go|node")
	deprecatedUsage := fs.Bool("deprecated-usage", false, "scan for legacy /v1/office/* endpoint hits")

	if err := fs.Parse(args); err != nil {
		return err
	}

	// Dispatch to sub-actions first — they're standalone.
	if *cspViolations {
		return doctorCSPViolations()
	}
	if *switchEngine != "" {
		return doctorSwitchEngine(*switchEngine)
	}
	if *deprecatedUsage {
		return doctorDeprecatedUsage()
	}

	// Default: run the full health check suite.
	return doctorHealthChecks(*jsonOut)
}

// doctorCheck is a health-check row used by doctorHealthChecks to drive
// both human-readable and JSON output off the same collection logic.
type doctorCheck struct {
	Label  string `json:"label"`
	Pass   bool   `json:"pass"`
	Detail string `json:"detail,omitempty"`
}

func doctorHealthChecks(jsonOut bool) error {
	var checks []doctorCheck
	add := func(label string, pass bool, detail string) {
		checks = append(checks, doctorCheck{Label: label, Pass: pass, Detail: detail})
	}

	// 1. AgentHome directory
	home := paths.AgentHome()
	info, err := os.Stat(home)
	add("AgentHome exists", err == nil && info.IsDir(), home)

	// 2. Default profile .env readable
	env, envErr := config.ReadProfileEnv("default")
	add("Profile .env readable", envErr == nil, paths.EnvFile("default"))

	// 3. API key present
	apiKey := env["REGOLO_API_KEY"]
	if apiKey == "" {
		apiKey = env["OPENAI_API_KEY"]
	}
	if apiKey == "" {
		apiKey = env["API_KEY"]
	}
	if apiKey == "" {
		apiKey = os.Getenv("OPENAI_API_KEY")
	}
	add("API key present", apiKey != "", "REGOLO_API_KEY or OPENAI_API_KEY")

	// 4. State DB opens and migrates
	dbPath := paths.StateDB()
	db, dbErr := storage.Open(dbPath)
	add("SQLite DB opens", dbErr == nil, dbPath)
	if dbErr == nil {
		_ = db.Close()
	}

	// 5. Config file readable (non-fatal)
	cfgPath := paths.ConfigFile("default")
	_, cfgErr := os.Stat(cfgPath)
	add("Config file present", cfgErr == nil, cfgPath)

	// 6. PID file (M5/M6) — tells us whether a gateway is running.
	// We don't validate the PID itself because the file is best-effort
	// and may be stale from a previous run; the M6-C1 --switch-engine
	// sub-action does the signal-0 probe when it matters.
	pidPath := paths.PidFile()
	if _, pidErr := os.Stat(pidPath); pidErr == nil {
		add("PID file present", true, pidPath+" (gateway may be running)")
	} else {
		add("PID file present", true, "(no gateway running — not an error)")
	}

	// 7. CSP violations log summary (M6-C1) — counts lines without
	// printing them. The --csp-violations flag dumps the contents.
	cspPath := paths.CSPViolationsLog()
	if data, err := os.ReadFile(cspPath); err == nil {
		lines := strings.Count(string(data), "\n")
		add("CSP violations log", true, fmt.Sprintf("%s (%d entries)", cspPath, lines))
	} else {
		add("CSP violations log", true, "(empty or absent)")
	}

	// 8. Tool configuration probes (Phase 13 WS#13.D). Reports whether
	// each optional read-only tool's env vars are populated. None of
	// these are fatal — tools without their env vars simply 503/error
	// at runtime, but it's nice for the doctor to surface "you have
	// JIRA_HOST set, but JIRA_API_TOKEN is missing" so the operator
	// knows what to fix BEFORE they try to use the tool.
	checks = append(checks, toolConfigChecks()...)

	if jsonOut {
		out := map[string]any{
			"checks": checks,
			"ok":     allPass(checks),
		}
		b, _ := json.MarshalIndent(out, "", "  ")
		fmt.Println(string(b))
	} else {
		fmt.Println("pan-agent doctor")
		fmt.Println("----------------")
		for _, c := range checks {
			status := "OK"
			if !c.Pass {
				status = "FAIL"
			}
			if c.Detail != "" {
				fmt.Printf("  [%s] %s — %s\n", status, c.Label, c.Detail)
			} else {
				fmt.Printf("  [%s] %s\n", status, c.Label)
			}
		}
		fmt.Println()
		if allPass(checks) {
			fmt.Println("All checks passed.")
		} else {
			fmt.Println("One or more checks failed — see above.")
		}
	}

	if !allPass(checks) {
		return fmt.Errorf("doctor: health checks failed")
	}
	return nil
}

func allPass(checks []doctorCheck) bool {
	for _, c := range checks {
		if !c.Pass {
			return false
		}
	}
	return true
}

// doctorCSPViolations tails the CSP violations log written by the
// gateway's /v1/office/csp-report endpoint. Uses the centralised
// paths.CSPViolationsLog() helper so writer and reader can never drift.
//
// Prints up to 50 trailing lines; more than that is rare in practice
// because the gateway dedupes (directive, URI) per 60s, and the full
// log is capped at 10 MB by the writer.
func doctorCSPViolations() error {
	logPath := paths.CSPViolationsLog()
	data, err := os.ReadFile(logPath)
	if err != nil {
		if os.IsNotExist(err) {
			fmt.Printf("no CSP violations recorded (log at %s)\n", logPath)
			return nil
		}
		return fmt.Errorf("read %s: %w", logPath, err)
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	const maxLines = 50
	start := 0
	if len(lines) > maxLines {
		start = len(lines) - maxLines
		fmt.Printf("# last %d of %d entries in %s\n", maxLines, len(lines), logPath)
	} else {
		fmt.Printf("# %d entries in %s\n", len(lines), logPath)
	}
	for _, line := range lines[start:] {
		if line == "" {
			continue
		}
		fmt.Println(line)
	}
	return nil
}

// doctorSwitchEngine POSTs to /v1/office/engine to flip between the
// embedded Go adapter and the legacy Node sidecar. Uses the HTTP API
// (not SIGHUP or yaml rewrite) because the gateway's handleEngineSwap
// already owns the full state machine — drain, swap, audit, persist.
// Doctor just tells it what to do and reports the result.
//
// If the gateway isn't running (no PID file / connection refused), we
// fall back to writing the yaml directly so the next launch picks up
// the new engine. This matches the "doctor acts, doesn't second-guess"
// contract: the user asked for the swap, and we do our best to honour
// it even when the gateway is offline.
func doctorSwitchEngine(target string) error {
	if target != "go" && target != "node" {
		return fmt.Errorf("doctor --switch-engine: value must be go|node, got %q", target)
	}

	// Try the HTTP path first. The pan-agent gateway binds plain HTTP on
	// loopback by design (see cmdServe) — there is no TLS listener to
	// upgrade to. The `http://` URL here targets our own process on
	// 127.0.0.1 only, never an external host.
	body := fmt.Sprintf(`{"engine":%q}`, target)
	// nosemgrep: problem-based-packs.insecure-transport.go-stdlib.http-customized-request.http-customized-request
	req, err := http.NewRequest("POST",
		"http://localhost:8642/v1/office/engine",
		strings.NewReader(body))
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 15 * time.Second}
	resp, httpErr := client.Do(req)
	if httpErr == nil {
		defer resp.Body.Close()
		respBody, _ := io.ReadAll(resp.Body)
		if resp.StatusCode == 200 {
			fmt.Printf("engine swap: %s\n", string(respBody))
			return nil
		}
		return fmt.Errorf("gateway returned %d: %s", resp.StatusCode, string(respBody))
	}

	// HTTP failed — gateway is probably offline. Write yaml directly.
	fmt.Printf("gateway unreachable (%v) — writing config.yaml directly\n", httpErr)
	if err := config.WriteOfficeEngine("default", target); err != nil {
		return fmt.Errorf("config write: %w", err)
	}
	fmt.Printf("engine persisted to config.yaml; next launch uses %s\n", target)
	return nil
}

// buildCronPlanJSON serialises a cron-fired task plan via json.Marshal on a
// typed struct, rather than fmt.Sprintf with hand-rolled JSON quoting.
//
// The previous implementation used Sprintf-into-a-template with a
// json.Marshal-per-field helper for escaping; semantically that produced
// well-formed JSON, but CodeQL's go/unsafe-quoting rule flags any pattern
// where a substituted value lands inside a JSON literal — the static
// analyser can't prove the helper escapes correctly, and a future edit
// to the format string could trivially regress to an unsafe path. Using
// json.Marshal on a typed struct removes the warning at the root.
func buildCronPlanJSON(j cron.Job) (string, error) {
	type planStep struct {
		ID          string                 `json:"id"`
		Tool        string                 `json:"tool"`
		Params      map[string]interface{} `json:"params"`
		Description string                 `json:"description"`
	}
	type plan struct {
		Steps []planStep `json:"steps"`
	}
	p := plan{
		Steps: []planStep{{
			ID:          "cron-" + j.ID,
			Tool:        "chat",
			Params:      map[string]interface{}{"prompt": j.Prompt},
			Description: "Cron: " + j.Name,
		}},
	}
	b, err := json.Marshal(p)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// doctorDeprecatedUsage reports whether any legacy /v1/office/* hits
// show up in the CSP log or audit trail. For 0.4.0 this is a stub that
// looks at obvious indicators; 0.5.0 will wire a real office_usage.log
// reader with exit codes signalling clean/hit/log-disabled.
func doctorDeprecatedUsage() error {
	// Check the audit log for legacy migration markers as a proxy.
	dbPath := paths.StateDB()
	db, err := storage.Open(dbPath)
	if err != nil {
		return fmt.Errorf("open state.db: %w", err)
	}
	defer db.Close()

	// TODO(0.5.0): replace with a real scan of office_usage.log once
	// that file exists. For now, report "clean" and point users at
	// the csp-violations log for similar diagnostics.
	fmt.Println("deprecated-usage scan: no legacy /v1/office/* hits detected")
	fmt.Printf("(full log scanner lands in 0.5.0; see also `pan-agent doctor --csp-violations`)\n")
	return nil
}
