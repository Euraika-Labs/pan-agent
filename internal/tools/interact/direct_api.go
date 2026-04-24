package interact

import (
	"context"
	"fmt"
	"os/exec"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"time"
)

// DirectAPI executes platform-native automation commands: osascript on
// macOS, PowerShell+UIAutomation on Windows, dbus-send on Linux.
type DirectAPI struct {
	platform string
}

// NewDirectAPI creates a DirectAPI for the current platform.
func NewDirectAPI() *DirectAPI {
	return &DirectAPI{platform: runtime.GOOS}
}

// safeAppName matches app names that are safe filesystem/process names:
// letters, digits, spaces, dots, hyphens. Rejects shell metacharacters,
// path separators, and control characters.
var safeAppName = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9 .\-]{0,127}$`)

// safeKeyCombo matches key combinations that are safe for xdotool key:
// letters, digits, plus signs, underscores, and spaces only.
var safeKeyCombo = regexp.MustCompile(`^[a-zA-Z0-9+_ ]+$`)

// maxTextLen is the maximum number of characters accepted by linuxType.
const maxTextLen = 10000

// Available reports whether direct API automation is possible on this platform.
func (d *DirectAPI) Available() bool {
	switch d.platform {
	case "darwin":
		_, err := exec.LookPath("osascript")
		return err == nil
	case "windows":
		_, err := exec.LookPath("powershell")
		return err == nil
	case "linux":
		_, err := exec.LookPath("xdotool")
		if err == nil {
			return true
		}
		_, err = exec.LookPath("dbus-send")
		return err == nil
	default:
		return false
	}
}

// Execute runs a direct-API command for the given intent. Returns the
// output or an error. Only handles well-known intents; returns an error
// for unrecognised ones so the router can fall through to vision.
func (d *DirectAPI) Execute(ctx context.Context, req Request) (string, error) {
	intent := strings.ToLower(req.Intent)

	// Linux xdotool intents — checked first so they take priority over
	// the generic "open" / "frontmost" cases on Linux.
	if d.platform == "linux" {
		switch {
		case strings.Contains(intent, "type") && req.Text != "":
			return d.linuxType(ctx, req.Text)
		case strings.Contains(intent, "right_click") && req.X != nil && req.Y != nil:
			return d.linuxRightClick(ctx, *req.X, *req.Y)
		case strings.Contains(intent, "click") && req.X != nil && req.Y != nil:
			return d.linuxClick(ctx, *req.X, *req.Y)
		case strings.Contains(intent, "focus") && req.App != "":
			return d.linuxFocus(ctx, req.App)
		case strings.Contains(intent, "minimize"):
			return d.linuxMinimize(ctx)
		case strings.Contains(intent, "maximize"):
			return d.linuxMaximize(ctx)
		case intent == "key" && req.Key != "":
			return d.linuxKey(ctx, req.Key)
		case intent == "list_windows":
			return d.linuxListWindows(ctx)
		}
	}

	switch {
	case strings.Contains(intent, "screenshot"):
		return d.screenshot(ctx)
	case strings.Contains(intent, "open") && req.App != "":
		return d.openApp(ctx, req.App)
	case strings.Contains(intent, "frontmost") || strings.Contains(intent, "active window"):
		return d.frontmostApp(ctx)
	default:
		return "", fmt.Errorf("unhandled intent for direct API: %q", req.Intent)
	}
}

func (d *DirectAPI) screenshot(ctx context.Context) (string, error) {
	switch d.platform {
	case "darwin":
		return d.runStaticCmd(ctx, "osascript", "-e", `tell application "System Events" to get name of first process whose frontmost is true`)
	default:
		return "", fmt.Errorf("screenshot via direct API not supported on %s", d.platform)
	}
}

func (d *DirectAPI) openApp(ctx context.Context, app string) (string, error) {
	if !safeAppName.MatchString(app) {
		return "", fmt.Errorf("rejected app name %q: must be alphanumeric with spaces/dots/hyphens only", app)
	}

	switch d.platform {
	case "darwin":
		// Use `open -a` which takes the app name as a separate argv
		// element — no shell interpretation, no script injection.
		return d.runStaticCmd(ctx, "open", "-a", app) // nosemgrep: problem-based-packs.insecure-transport.go-stdlib.cmd-injection
	case "windows":
		// Pass app name as a separate argument to Start-Process via
		// -ArgumentList, not interpolated into -Command.
		return d.runStaticCmd(ctx, "powershell", "-NoProfile", "-NonInteractive", "-Command", // nosemgrep: problem-based-packs.insecure-transport.go-stdlib.cmd-injection
			"Start-Process -FilePath $args[0]", "-args", app)
	case "linux":
		return d.runStaticCmd(ctx, "xdg-open", app) // nosemgrep: problem-based-packs.insecure-transport.go-stdlib.cmd-injection
	default:
		return "", fmt.Errorf("open app not supported on %s", d.platform)
	}
}

func (d *DirectAPI) frontmostApp(ctx context.Context) (string, error) {
	switch d.platform {
	case "darwin":
		return d.runStaticCmd(ctx, "osascript", "-e", `tell application "System Events" to get name of first process whose frontmost is true`)
	case "linux":
		return d.runStaticCmd(ctx, "xdotool", "getactivewindow", "getwindowname")
	default:
		return "", fmt.Errorf("frontmost app not supported on %s", d.platform)
	}
}

// runStaticCmd executes a command with a timeout. All arguments are
// passed as separate argv entries (no shell interpretation). Callers
// must validate any dynamic arguments before calling.
func (d *DirectAPI) runStaticCmd(ctx context.Context, name string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	// #nosec G204 — all callers validate dynamic inputs via safeAppName
	// before reaching here; static callers use hardcoded arguments only.
	cmd := exec.CommandContext(ctx, name, args...) // nosemgrep: go.lang.security.audit.dangerous-exec-command.dangerous-exec-command
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("%s: %w: %s", name, err, string(out))
	}
	return strings.TrimSpace(string(out)), nil
}

// ── Linux xdotool methods ──────────────────────────────────────────

func (d *DirectAPI) linuxType(ctx context.Context, text string) (string, error) {
	if len(text) > maxTextLen {
		return "", fmt.Errorf("text exceeds %d character limit", maxTextLen)
	}
	for _, r := range text {
		if r < 0x20 && r != '\n' && r != '\t' {
			return "", fmt.Errorf("text contains disallowed control character U+%04X", r)
		}
	}
	return d.runStaticCmd(ctx, "xdotool", "type", "--delay", "50", text)
}

func (d *DirectAPI) linuxClick(ctx context.Context, x, y int) (string, error) {
	if x < 0 || x > 65535 || y < 0 || y > 65535 {
		return "", fmt.Errorf("coordinates out of range: x=%d y=%d (must be 0-65535)", x, y)
	}
	xs, ys := strconv.Itoa(x), strconv.Itoa(y)
	if _, err := d.runStaticCmd(ctx, "xdotool", "mousemove", xs, ys); err != nil {
		return "", err
	}
	return d.runStaticCmd(ctx, "xdotool", "click", "1")
}

func (d *DirectAPI) linuxRightClick(ctx context.Context, x, y int) (string, error) {
	if x < 0 || x > 65535 || y < 0 || y > 65535 {
		return "", fmt.Errorf("coordinates out of range: x=%d y=%d (must be 0-65535)", x, y)
	}
	xs, ys := strconv.Itoa(x), strconv.Itoa(y)
	if _, err := d.runStaticCmd(ctx, "xdotool", "mousemove", xs, ys); err != nil {
		return "", err
	}
	return d.runStaticCmd(ctx, "xdotool", "click", "3")
}

func (d *DirectAPI) linuxFocus(ctx context.Context, window string) (string, error) {
	if !safeAppName.MatchString(window) {
		return "", fmt.Errorf("rejected window name %q: must match safe pattern", window)
	}
	return d.runStaticCmd(ctx, "xdotool", "search", "--name", window, "windowactivate", "--sync")
}

func (d *DirectAPI) linuxMinimize(ctx context.Context) (string, error) {
	return d.runStaticCmd(ctx, "xdotool", "getactivewindow", "windowminimize")
}

func (d *DirectAPI) linuxMaximize(ctx context.Context) (string, error) {
	return d.runStaticCmd(ctx, "xdotool", "getactivewindow", "windowsize", "--usehints", "100%", "100%")
}

func (d *DirectAPI) linuxKey(ctx context.Context, combo string) (string, error) {
	if !safeKeyCombo.MatchString(combo) {
		return "", fmt.Errorf("rejected key combo %q: must match pattern [a-zA-Z0-9+_ ]", combo)
	}
	return d.runStaticCmd(ctx, "xdotool", "key", combo)
}

func (d *DirectAPI) linuxListWindows(ctx context.Context) (string, error) {
	ids, err := d.runStaticCmd(ctx, "xdotool", "search", "--onlyvisible", "--name", "")
	if err != nil {
		return "", err
	}
	if ids == "" {
		return "", nil
	}
	var names []string
	for _, id := range strings.Split(ids, "\n") {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		name, err := d.runStaticCmd(ctx, "xdotool", "getwindowname", id)
		if err != nil {
			continue
		}
		names = append(names, name)
	}
	return strings.Join(names, "\n"), nil
}
