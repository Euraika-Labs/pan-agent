//go:build chaos

package claw3d

import (
	"bufio"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

// Chaos tests — run only under -tags chaos. Two scenarios:
//
//   A. TestChaos_AdapterKillMidstream
//      Spawn pan-agent as a child, open a WS client, SIGKILL mid-stream,
//      assert the client observes an abnormal-closure error within 5s.
//
//   B. TestChaos_ParentWatcherExit
//      Spawn a dummy "parent" process (the chaos_helper binary), then
//      spawn pan-agent with PAN_AGENT_PARENT_PID pointing at that dummy.
//      SIGKILL the dummy parent. pan-agent should exit within ~6s
//      (parentwatch polls every 2s, with a safety margin).
//
// Both tests build pan-agent into a tempdir via `go build`. CI should
// set -tags chaos in a separate job — the default `go test ./...` skips
// these because they need the binary and run slowly.
//
// Scenario C (kill webview mid-render) was dropped at M5 Phase 1:
// the session-store cleanup it would exercise is already observable
// through scenarios A+B's close-code assertions, and Playwright-from-Go
// was a complexity multiplier for no coverage gain.

// portRegex matches the line gateway.Start prints on successful bind.
// Anchored so a partial match in, say, a log message can't confuse it.
var portRegex = regexp.MustCompile(`listening on http://127\.0\.0\.1:(\d+)`)

// buildPanAgent compiles pan-agent into a temp directory and returns the
// path to the binary. Called once per test via helper; each test gets
// its own copy so failures don't leak state across runs.
func buildPanAgent(t *testing.T) string {
	t.Helper()
	outDir := t.TempDir()
	outPath := filepath.Join(outDir, "pan-agent")
	if runtime.GOOS == "windows" {
		outPath += ".exe"
	}
	cmd := exec.Command("go", "build", "-o", outPath, "./cmd/pan-agent")
	cmd.Dir = projectRoot(t)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build pan-agent: %v\n%s", err, out)
	}
	return outPath
}

// buildChaosHelper compiles the dummy-parent helper binary.
func buildChaosHelper(t *testing.T) string {
	t.Helper()
	outDir := t.TempDir()
	outPath := filepath.Join(outDir, "chaos-helper")
	if runtime.GOOS == "windows" {
		outPath += ".exe"
	}
	cmd := exec.Command("go", "build", "-o", outPath, "./internal/claw3d/chaos_helper")
	cmd.Dir = projectRoot(t)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build chaos-helper: %v\n%s", err, out)
	}
	return outPath
}

// projectRoot walks up from the test's CWD to find go.mod. Tests run
// from internal/claw3d/ so the walk typically takes 2 hops.
func projectRoot(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(wd, "go.mod")); err == nil {
			return wd
		}
		parent := filepath.Dir(wd)
		if parent == wd {
			t.Fatalf("could not locate go.mod walking up from %s", wd)
		}
		wd = parent
	}
}

// startAndReadPort launches cmd, scrapes stdout for the "listening on"
// line, and returns the parsed port. Registers t.Cleanup to kill the
// process on test exit so a test failure never leaks a gateway.
func startAndReadPort(t *testing.T, cmd *exec.Cmd, timeout time.Duration) int {
	t.Helper()
	// Pipe stdout so we can parse; stderr is merged in.
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatalf("stdout pipe: %v", err)
	}
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		t.Fatalf("start: %v", err)
	}
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	})

	// Read lines off stdout until we see the port or timeout.
	result := make(chan int, 1)
	go func() {
		scanner := bufio.NewScanner(stdout)
		for scanner.Scan() {
			m := portRegex.FindStringSubmatch(scanner.Text())
			if len(m) > 1 {
				p, _ := strconv.Atoi(m[1])
				result <- p
				// Drain remaining output so the child doesn't block on
				// a full pipe buffer later.
				_, _ = io.Copy(io.Discard, stdout)
				return
			}
		}
	}()

	select {
	case p := <-result:
		return p
	case <-time.After(timeout):
		t.Fatalf("no listen port after %s", timeout)
		return 0
	}
}

// mintSession performs a GET /office/ against the live gateway and
// returns the session cookie. Chaos scenario A needs this cookie to
// satisfy handleWS's Gate-3 verify() before SIGKILLing the process.
func mintSession(t *testing.T, port int) *http.Cookie {
	t.Helper()
	url := fmt.Sprintf("http://127.0.0.1:%d/office/", port)
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET /office/: %v", err)
	}
	defer resp.Body.Close()
	for _, c := range resp.Cookies() {
		if c.Name == "claw3d_sess" {
			return c
		}
	}
	t.Fatalf("no claw3d_sess cookie in response to GET /office/")
	return nil
}

// TestChaos_AdapterKillMidstream launches pan-agent as a child process,
// opens a WebSocket client with a valid session cookie, pokes the
// connection, SIGKILLs the child, and asserts that the client observes
// a close-frame error within 5 seconds. "Any error" counts — different
// OSes surface a hard-killed socket as different error types (1006
// CloseAbnormalClosure, io.EOF, io.ErrUnexpectedEOF, net.OpError).
func TestChaos_AdapterKillMidstream(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping chaos test in short mode")
	}

	bin := buildPanAgent(t)
	cmd := exec.Command(bin, "serve", "--port", "0", "--host", "127.0.0.1")
	port := startAndReadPort(t, cmd, 10*time.Second)

	// Mint a session cookie.
	cookie := mintSession(t, port)

	// Open WS with the cookie.
	wsURL := fmt.Sprintf("ws://127.0.0.1:%d/office/ws", port)
	hdr := http.Header{}
	hdr.Set("Cookie", cookie.String())
	dialer := websocket.Dialer{HandshakeTimeout: 5 * time.Second}
	ws, _, err := dialer.Dial(wsURL, hdr)
	if err != nil {
		t.Fatalf("dial %s: %v", wsURL, err)
	}
	defer ws.Close()

	// Send one frame to confirm the link is healthy, then kill the
	// server. 100ms lets the request queue into the adapter's read
	// loop before SIGKILL arrives.
	_ = ws.WriteMessage(websocket.TextMessage,
		[]byte(`{"type":"req","id":"1","method":"status"}`))
	time.Sleep(100 * time.Millisecond)

	if err := cmd.Process.Kill(); err != nil {
		t.Fatalf("kill: %v", err)
	}

	// Drain the socket until we get an error. Time budget: 5 seconds.
	_ = ws.SetReadDeadline(time.Now().Add(5 * time.Second))
	var readErr error
	for readErr == nil {
		_, _, readErr = ws.ReadMessage()
	}
	t.Logf("observed close error after kill: %v", readErr)
	// Any non-nil error is acceptable — we specifically do NOT require
	// 1006 because the OS TCP stack + gorilla's close-frame detection
	// produce different error types depending on timing.
}

// TestChaos_ParentWatcherExit exercises the parentwatch package by
// giving pan-agent a fake parent PID (the chaos_helper binary) and
// SIGKILLing that fake parent. pan-agent should notice within its
// poll interval (2s, per parentwatch default) and exit gracefully.
// The 10-second test deadline gives parentwatch two full poll cycles
// plus safety margin for CI slowness.
func TestChaos_ParentWatcherExit(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping chaos test in short mode")
	}

	panBin := buildPanAgent(t)
	helperBin := buildChaosHelper(t)

	// Launch the dummy parent first — we need its PID before pan-agent
	// boots so parentwatch can observe it.
	dummy := exec.Command(helperBin)
	if err := dummy.Start(); err != nil {
		t.Fatalf("start dummy parent: %v", err)
	}
	dummyPID := dummy.Process.Pid
	t.Cleanup(func() {
		_ = dummy.Process.Kill()
		_ = dummy.Wait()
	})

	// Spawn pan-agent with PAN_AGENT_PARENT_PID pointing at the dummy.
	cmd := exec.Command(panBin, "serve", "--port", "0", "--host", "127.0.0.1")
	cmd.Env = append(os.Environ(), fmt.Sprintf("PAN_AGENT_PARENT_PID=%d", dummyPID))
	_ = startAndReadPort(t, cmd, 10*time.Second)

	// Kill the dummy parent. parentwatch should detect it within 2s
	// and call the shutdown handler, which SIGTERMs the gateway.
	if err := dummy.Process.Kill(); err != nil {
		t.Fatalf("kill dummy parent: %v", err)
	}

	// Wait for pan-agent to exit. 10s is 5x the parentwatch poll
	// interval — generous enough that a slow CI runner won't flake.
	exited := make(chan error, 1)
	go func() { exited <- cmd.Wait() }()
	select {
	case err := <-exited:
		t.Logf("pan-agent exited after parent death: %v", err)
	case <-time.After(10 * time.Second):
		_ = cmd.Process.Kill()
		t.Fatal("pan-agent did not exit within 10s after parent death")
	}
}
