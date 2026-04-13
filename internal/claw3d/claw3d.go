// Package claw3d manages the lifecycle of the hermes-office (Claw3D) Node.js
// application: cloning the repo, installing dependencies, and running the dev
// server and adapter processes.
package claw3d

import (
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/euraika-labs/pan-agent/internal/paths"
)

const (
	repoURL        = "https://github.com/fathah/hermes-office"
	defaultPort    = 3000
	defaultWsURL   = "ws://localhost:18789"
	portCheckTimeout = 300 * time.Millisecond
)

// file name constants — stored inside AgentHome, not the repo dir.
const (
	devPIDFile     = "claw3d-dev.pid"
	adapterPIDFile = "claw3d-adapter.pid"
	portFile       = "claw3d-port"
	wsURLFile      = "claw3d-ws-url"
)

// Claw3dStatus describes the current state of the Claw3D installation.
type Claw3dStatus struct {
	Cloned           bool   `json:"cloned"`
	Installed        bool   `json:"installed"`
	DevServerRunning bool   `json:"dev_server_running"`
	AdapterRunning   bool   `json:"adapter_running"`
	Port             int    `json:"port"`
	WsURL            string `json:"ws_url"`
}

// mu guards the in-process handles.
var mu sync.Mutex

var (
	devCmd     *exec.Cmd
	adapterCmd *exec.Cmd
)

// ---------------------------------------------------------------------------
// File helpers
// ---------------------------------------------------------------------------

func pidPath(name string) string {
	return filepath.Join(paths.AgentHome(), name)
}

func readPID(name string) (int, bool) {
	data, err := os.ReadFile(pidPath(name))
	if err != nil {
		return 0, false
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil || pid <= 0 {
		return 0, false
	}
	return pid, true
}

func writePID(name string, pid int) {
	_ = os.WriteFile(pidPath(name), []byte(strconv.Itoa(pid)), 0o600)
}

func removePID(name string) {
	_ = os.Remove(pidPath(name))
}

// isAlive returns true when a PID corresponds to a running process.
func isAlive(pid int) bool {
	p, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	// On Windows FindProcess always succeeds; use a zero-signal to probe.
	if runtime.GOOS == "windows" {
		// os.FindProcess on Windows returns success for any pid; we need to
		// actually check. Use a handle-based check via Signal(0) — not
		// available on Windows via the stdlib. Instead we check whether the
		// process handle is still valid by calling Wait with a no-hang flag
		// via a side-channel: just try to open the process.
		err = signalZero(p)
		return err == nil
	}
	// POSIX: Signal(0) returns nil iff the process exists.
	err = signalZero(p)
	return err == nil
}

// ---------------------------------------------------------------------------
// Port helpers
// ---------------------------------------------------------------------------

func configPath(name string) string {
	return filepath.Join(paths.AgentHome(), name)
}

// GetPort reads the saved port from disk. Returns defaultPort if unset.
func GetPort() int {
	data, err := os.ReadFile(configPath(portFile))
	if err != nil {
		return defaultPort
	}
	p, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil || p <= 0 {
		return defaultPort
	}
	return p
}

// SetPort saves the port to disk.
func SetPort(port int) error {
	return os.WriteFile(configPath(portFile), []byte(strconv.Itoa(port)), 0o600)
}

// GetWsURL reads the saved WebSocket URL. Returns defaultWsURL if unset.
func GetWsURL() string {
	data, err := os.ReadFile(configPath(wsURLFile))
	if err != nil {
		return defaultWsURL
	}
	s := strings.TrimSpace(string(data))
	if s == "" {
		return defaultWsURL
	}
	return s
}

// SetWsURL saves the WebSocket URL to disk.
func SetWsURL(url string) error {
	return os.WriteFile(configPath(wsURLFile), []byte(url), 0o600)
}

// ---------------------------------------------------------------------------
// Process state
// ---------------------------------------------------------------------------

func isDevServerRunning() bool {
	mu.Lock()
	cmd := devCmd
	mu.Unlock()
	if cmd != nil && cmd.Process != nil && isAlive(cmd.Process.Pid) {
		return true
	}
	pid, ok := readPID(devPIDFile)
	if ok && isAlive(pid) {
		return true
	}
	removePID(devPIDFile)
	return false
}

func isAdapterRunning() bool {
	mu.Lock()
	cmd := adapterCmd
	mu.Unlock()
	if cmd != nil && cmd.Process != nil && isAlive(cmd.Process.Pid) {
		return true
	}
	pid, ok := readPID(adapterPIDFile)
	if ok && isAlive(pid) {
		return true
	}
	removePID(adapterPIDFile)
	return false
}

// checkPortInUse probes whether a TCP port is already bound.
func checkPortInUse(port int) bool {
	conn, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", port), portCheckTimeout)
	if err != nil {
		return false
	}
	conn.Close()
	return true
}

// ---------------------------------------------------------------------------
// Status
// ---------------------------------------------------------------------------

// Status returns the current state of the Claw3D installation and processes.
func Status() *Claw3dStatus {
	repoDir := paths.Claw3dDir()
	cloned := fileExists(filepath.Join(repoDir, "package.json"))
	installed := fileExists(filepath.Join(repoDir, "node_modules"))
	port := GetPort()
	return &Claw3dStatus{
		Cloned:           cloned,
		Installed:        installed,
		DevServerRunning: isDevServerRunning(),
		AdapterRunning:   isAdapterRunning(),
		Port:             port,
		WsURL:            GetWsURL(),
	}
}

// ---------------------------------------------------------------------------
// Setup
// ---------------------------------------------------------------------------

// Setup clones (or pulls) the hermes-office repository and runs npm install.
// progress is called with human-readable status lines as they occur.
func Setup(progress func(string)) error {
	repoDir := paths.Claw3dDir()
	cloned := fileExists(filepath.Join(repoDir, "package.json"))

	npm, err := findNpm()
	if err != nil {
		return fmt.Errorf("claw3d: npm not found: %w", err)
	}

	if !cloned {
		progress("Cloning hermes-office from GitHub...\n")
		if err := runStreaming(progress, paths.AgentHome(),
			"git", "clone", repoURL, repoDir); err != nil {
			return fmt.Errorf("claw3d: git clone: %w", err)
		}
		progress("Clone complete.\n")
	} else {
		progress("Repository already exists, pulling latest...\n")
		// Non-fatal — pull failures should not block setup.
		_ = runStreaming(progress, repoDir, "git", "pull", "--ff-only")
		progress("Pull complete.\n")
	}

	progress("Running npm install...\n")
	if err := runStreaming(progress, repoDir, npm, "install"); err != nil {
		return fmt.Errorf("claw3d: npm install: %w", err)
	}
	progress("Dependencies installed successfully.\n")

	// Write .env so Claw3D skips onboarding.
	writeEnv(repoDir)
	return nil
}

// writeEnv writes a .env file into the repo directory with the current
// port and WebSocket URL configuration.
func writeEnv(repoDir string) {
	port := GetPort()
	wsURL := GetWsURL()
	lines := []string{
		"# Auto-configured by pan-agent",
		fmt.Sprintf("PORT=%d", port),
		"HOST=127.0.0.1",
		fmt.Sprintf("NEXT_PUBLIC_GATEWAY_URL=%s", wsURL),
		fmt.Sprintf("CLAW3D_GATEWAY_URL=%s", wsURL),
		"CLAW3D_GATEWAY_TOKEN=",
		"HERMES_ADAPTER_PORT=18789",
		"HERMES_MODEL=hermes",
		"HERMES_AGENT_NAME=Hermes",
		"",
	}
	content := strings.Join(lines, "\n")
	_ = os.WriteFile(filepath.Join(repoDir, ".env"), []byte(content), 0o600)
}

// ---------------------------------------------------------------------------
// Dev server
// ---------------------------------------------------------------------------

// StartDevServer spawns `npm run dev` in the repo directory.
// It is a no-op if the server is already running.
func StartDevServer() error {
	if isDevServerRunning() {
		return nil
	}
	repoDir := paths.Claw3dDir()
	if !fileExists(filepath.Join(repoDir, "node_modules")) {
		return fmt.Errorf("claw3d: not installed — run Setup first")
	}

	npm, err := findNpm()
	if err != nil {
		return fmt.Errorf("claw3d: npm not found: %w", err)
	}

	port := GetPort()
	env := append(os.Environ(),
		fmt.Sprintf("PORT=%d", port),
		"NEXT_TELEMETRY_DISABLED=1",
		"TERM=dumb",
	)

	cmd := exec.Command(npm, "run", "dev")
	cmd.Dir = repoDir
	cmd.Env = env

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("claw3d: start dev server: %w", err)
	}

	mu.Lock()
	devCmd = cmd
	mu.Unlock()

	writePID(devPIDFile, cmd.Process.Pid)

	// Reap in background and clean up PID file when the process exits.
	go func() {
		_ = cmd.Wait()
		mu.Lock()
		if devCmd == cmd {
			devCmd = nil
		}
		mu.Unlock()
		removePID(devPIDFile)
	}()

	return nil
}

// StopDevServer kills the dev server process.
func StopDevServer() error {
	mu.Lock()
	cmd := devCmd
	devCmd = nil
	mu.Unlock()

	var firstErr error
	if cmd != nil && cmd.Process != nil {
		firstErr = killProcess(cmd.Process)
	}

	if pid, ok := readPID(devPIDFile); ok {
		p, err := os.FindProcess(pid)
		if err == nil {
			if kerr := killProcess(p); kerr != nil && firstErr == nil {
				firstErr = kerr
			}
		}
	}
	removePID(devPIDFile)
	return firstErr
}

// ---------------------------------------------------------------------------
// Adapter
// ---------------------------------------------------------------------------

// StartAdapter spawns `npm run hermes-adapter` in the repo directory.
// It is a no-op if the adapter is already running.
func StartAdapter() error {
	if isAdapterRunning() {
		return nil
	}
	repoDir := paths.Claw3dDir()
	if !fileExists(filepath.Join(repoDir, "node_modules")) {
		return fmt.Errorf("claw3d: not installed — run Setup first")
	}

	npm, err := findNpm()
	if err != nil {
		return fmt.Errorf("claw3d: npm not found: %w", err)
	}

	cmd := exec.Command(npm, "run", "hermes-adapter")
	cmd.Dir = repoDir
	cmd.Env = append(os.Environ(), "TERM=dumb")

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("claw3d: start adapter: %w", err)
	}

	mu.Lock()
	adapterCmd = cmd
	mu.Unlock()

	writePID(adapterPIDFile, cmd.Process.Pid)

	go func() {
		_ = cmd.Wait()
		mu.Lock()
		if adapterCmd == cmd {
			adapterCmd = nil
		}
		mu.Unlock()
		removePID(adapterPIDFile)
	}()

	return nil
}

// StopAdapter kills the adapter process.
func StopAdapter() error {
	mu.Lock()
	cmd := adapterCmd
	adapterCmd = nil
	mu.Unlock()

	var firstErr error
	if cmd != nil && cmd.Process != nil {
		firstErr = killProcess(cmd.Process)
	}

	if pid, ok := readPID(adapterPIDFile); ok {
		p, err := os.FindProcess(pid)
		if err == nil {
			if kerr := killProcess(p); kerr != nil && firstErr == nil {
				firstErr = kerr
			}
		}
	}
	removePID(adapterPIDFile)
	return firstErr
}

// ---------------------------------------------------------------------------
// Config marshalling helpers (for HTTP handlers)
// ---------------------------------------------------------------------------

// PortConfig is the JSON body for SetPort.
type PortConfig struct {
	Port int `json:"port"`
}

// WsURLConfig is the JSON body for SetWsURL.
type WsURLConfig struct {
	WsURL string `json:"ws_url"`
}

// MarshalStatus serialises a Claw3dStatus to JSON bytes.
func MarshalStatus(s *Claw3dStatus) ([]byte, error) {
	return json.Marshal(s)
}

// ---------------------------------------------------------------------------
// Internal helpers
// ---------------------------------------------------------------------------

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// findNpm resolves the npm binary. On Windows, npm ships as npm.cmd.
func findNpm() (string, error) {
	// On Windows the shim is npm.cmd; exec.LookPath finds .cmd only when
	// PATHEXT includes .CMD, which is the Windows default but may not be set
	// in all environments. Try both names.
	candidates := []string{"npm"}
	if runtime.GOOS == "windows" {
		candidates = []string{"npm.cmd", "npm"}
	}
	for _, name := range candidates {
		if p, err := exec.LookPath(name); err == nil {
			return p, nil
		}
	}
	return "", fmt.Errorf("npm not found in PATH")
}

// runStreaming runs a command, streaming its combined stdout+stderr through the
// progress callback line-by-line. Returns an error if the process exits with a
// non-zero code.
func runStreaming(progress func(string), dir, name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.Dir = dir

	out, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	cmd.Stderr = cmd.Stdout // merge stderr into the same pipe is not possible; use a buffer
	// Use a combined approach: write both to progress via CombinedOutput for
	// simplicity — the repo is small so the output fits in memory.
	cmd.Stdout = nil
	cmd.Stderr = nil

	combined, err := cmd.CombinedOutput()
	if progress != nil && len(combined) > 0 {
		progress(string(combined))
	}
	_ = out // not used after switching to CombinedOutput
	if err != nil {
		return err
	}
	return nil
}
