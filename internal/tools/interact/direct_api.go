package interact

import (
	"context"
	"fmt"
	"os/exec"
	"regexp"
	"runtime"
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
		_, err := exec.LookPath("dbus-send")
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
