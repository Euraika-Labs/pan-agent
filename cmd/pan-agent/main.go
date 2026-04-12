// Pan-Agent CLI entry point.
//
// Subcommands:
//
//	pan-agent serve  [--port 8642] [--host 127.0.0.1]  Start HTTP API server
//	pan-agent chat   [--model X]   [--profile P]        Interactive CLI chat
//	pan-agent version                                   Print version info
//	pan-agent doctor                                    Check system health
//
// Default (no subcommand): serve
package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/euraika-labs/pan-agent/internal/config"
	"github.com/euraika-labs/pan-agent/internal/gateway"
	"github.com/euraika-labs/pan-agent/internal/llm"
	"github.com/euraika-labs/pan-agent/internal/paths"
	"github.com/euraika-labs/pan-agent/internal/storage"
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
	default:
		return fmt.Errorf("unknown subcommand %q\n\nUsage: pan-agent <serve|chat|version|doctor>", sub)
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

	errCh := make(chan error, 1)
	go func() {
		if err := srv.Start(); err != nil {
			errCh <- err
		}
	}()

	select {
	case sig := <-quit:
		fmt.Printf("\npan-agent: received %s, shutting down...\n", sig)
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		return srv.Stop(ctx)
	case err := <-errCh:
		return err
	}
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

	apiKey := env["OPENAI_API_KEY"]
	if apiKey == "" {
		apiKey = env["API_KEY"]
	}
	if apiKey == "" {
		apiKey = os.Getenv("OPENAI_API_KEY")
	}

	client := llm.NewClient(baseURL, apiKey, model)

	fmt.Printf("pan-agent chat — model: %s  profile: %s\n", model, *profile)
	fmt.Println("Type a message and press Enter. Ctrl-C or Ctrl-D to quit.\n")

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

func cmdDoctor(_ []string) error {
	ok := true
	check := func(label string, pass bool, detail string) {
		status := "OK"
		if !pass {
			status = "FAIL"
			ok = false
		}
		if detail != "" {
			fmt.Printf("  [%s] %s — %s\n", status, label, detail)
		} else {
			fmt.Printf("  [%s] %s\n", status, label)
		}
	}

	fmt.Println("pan-agent doctor")
	fmt.Println("----------------")

	// 1. AgentHome directory
	home := paths.AgentHome()
	info, err := os.Stat(home)
	check("AgentHome exists",
		err == nil && info.IsDir(),
		home)

	// 2. Default profile .env readable
	env, err := config.ReadProfileEnv("default")
	check("Profile .env readable",
		err == nil,
		paths.EnvFile("default"))

	// 3. API key present
	apiKey := env["OPENAI_API_KEY"]
	if apiKey == "" {
		apiKey = env["API_KEY"]
	}
	if apiKey == "" {
		apiKey = os.Getenv("OPENAI_API_KEY")
	}
	check("API key present",
		apiKey != "",
		"OPENAI_API_KEY or API_KEY")

	// 4. State DB opens and migrates
	dbPath := paths.StateDB()
	db, dbErr := storage.Open(dbPath)
	check("SQLite DB opens",
		dbErr == nil,
		dbPath)
	if dbErr == nil {
		_ = db.Close()
	}

	// 5. Config file readable (non-fatal)
	cfgPath := paths.ConfigFile("default")
	_, cfgErr := os.Stat(cfgPath)
	check("Config file present",
		cfgErr == nil,
		cfgPath)

	fmt.Println()
	if ok {
		fmt.Println("All checks passed.")
	} else {
		fmt.Println("One or more checks failed — see above.")
		return fmt.Errorf("doctor: health checks failed")
	}
	return nil
}
