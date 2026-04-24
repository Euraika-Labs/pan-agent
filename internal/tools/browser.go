// Package tools – browser automation tool powered by go-rod / Chrome DevTools Protocol.
package tools

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/euraika-labs/pan-agent/internal/paths"
	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/launcher"
	"github.com/go-rod/rod/lib/proto"
)

// checkNavigableURL enforces a scheme + host allowlist for browser.navigate.
// The prior implementation accepted any URL — including file:// (local-file
// read) and http://localhost:8642 (self-SSRF against the agent's own API).
// Set PAN_AGENT_BROWSER_ALLOW_LOCAL=true to opt into loopback access (for
// legitimate local-dev workflows).
func checkNavigableURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme == "" {
		return fmt.Errorf("invalid url")
	}
	scheme := strings.ToLower(u.Scheme)
	if scheme != "http" && scheme != "https" {
		return fmt.Errorf("scheme %q not allowed (http/https only)", u.Scheme)
	}
	allowLocal := strings.EqualFold(os.Getenv("PAN_AGENT_BROWSER_ALLOW_LOCAL"), "true")
	host := u.Hostname()
	if host == "" {
		return fmt.Errorf("host is empty")
	}
	if allowLocal {
		return nil
	}
	lowered := strings.ToLower(host)
	if lowered == "localhost" || lowered == "localhost." {
		return fmt.Errorf("loopback host blocked (set PAN_AGENT_BROWSER_ALLOW_LOCAL=true to enable)")
	}
	// IP-literal check: block loopback + private RFC 1918 + link-local.
	if ip := net.ParseIP(lowered); ip != nil {
		if ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsUnspecified() {
			return fmt.Errorf("private/loopback IP blocked (set PAN_AGENT_BROWSER_ALLOW_LOCAL=true to enable)")
		}
	}
	return nil
}

func init() {
	Register(&browserTool{})
}

// ---------------------------------------------------------------------------
// Singleton browser state
// ---------------------------------------------------------------------------

var (
	browserMu       sync.Mutex
	sharedLauncher  *launcher.Launcher
	sharedBrowser   *rod.Browser
	sharedPage      *rod.Page
	browserLaunched bool
)

func acquirePage(ctx context.Context) (*rod.Page, error) {
	browserMu.Lock()
	defer browserMu.Unlock()

	if sharedBrowser == nil {
		headless := true
		if v := os.Getenv("PAN_AGENT_BROWSER_HEADLESS"); strings.ToLower(v) == "false" {
			headless = false
		}

		profileDir := browserProfileDir()
		cleanSingletonLock(profileDir)

		l := launcher.New().
			UserDataDir(profileDir).
			Headless(headless)

		// Rod defaults include "use-mock-keychain"; remove it so Chromium
		// uses the real OS keyring (Safe Storage) for cookie encryption.
		l.Delete("use-mock-keychain")

		// Remove password-store flag — rod's default launcher doesn't set
		// it, but Chromium may fall back to "basic" (plaintext) unless we
		// explicitly avoid injecting one. Deleting ensures Chromium probes
		// the system keyring.
		l.Delete("password-store")

		// Security-hardening flags per Phase 12 WS#1.
		l.Set("disable-extensions")
		l.Set("disable-component-update")

		u, err := l.Launch()
		if err != nil {
			return nil, fmt.Errorf("browser: launch chromium: %w", err)
		}

		b := rod.New().ControlURL(u)
		if err := b.Connect(); err != nil {
			return nil, fmt.Errorf("browser: connect: %w", err)
		}

		sharedLauncher = l
		sharedBrowser = b
		browserLaunched = true
	}

	if sharedPage == nil {
		p, err := sharedBrowser.Page(proto.TargetCreateTarget{URL: "about:blank"})
		if err != nil {
			return nil, fmt.Errorf("browser: open page: %w", err)
		}
		sharedPage = p
	}

	return sharedPage, nil
}

var browserProfileOverride string

func browserProfileDir() string {
	if browserProfileOverride != "" {
		return browserProfileOverride
	}
	return paths.BrowserProfile()
}

// CloseBrowser shuts down the shared Chromium instance. The profile
// directory is preserved for reuse across sessions. Safe to call if the
// browser was never launched. Called from the server shutdown path.
func CloseBrowser() {
	browserMu.Lock()
	defer browserMu.Unlock()

	if !browserLaunched {
		return
	}

	if sharedBrowser != nil {
		_ = sharedBrowser.Close()
		sharedBrowser = nil
		sharedPage = nil
	}

	sharedLauncher = nil
	browserLaunched = false
}

// cleanSingletonLock removes a stale SingletonLock file from the Chromium
// user-data directory. Chromium creates this file to prevent concurrent
// access; a crashed pan-agent leaves it behind, making the profile
// unusable until manual cleanup. We only remove it after confirming no
// live Chromium is using the profile (by checking the SingletonSocket).
func cleanSingletonLock(profileDir string) {
	lockPath := filepath.Join(profileDir, "SingletonLock")
	if _, err := os.Lstat(lockPath); err != nil {
		return
	}

	socketPath := filepath.Join(profileDir, "SingletonSocket")
	if _, err := os.Stat(socketPath); err == nil {
		conn, dialErr := net.DialTimeout("unix", socketPath, 2*time.Second)
		if dialErr == nil {
			conn.Close()
			return
		}
	}

	_ = os.Remove(lockPath)
	_ = os.Remove(socketPath)
}

// ---------------------------------------------------------------------------
// Tool implementation
// ---------------------------------------------------------------------------

type browserTool struct{}

func (b *browserTool) Name() string { return "browser" }

func (b *browserTool) Description() string {
	return "Control a Chromium browser to navigate web pages, click elements, type text, " +
		"evaluate JavaScript, and take screenshots."
}

func (b *browserTool) Parameters() json.RawMessage {
	return json.RawMessage(`{
  "type": "object",
  "properties": {
    "operation": {
      "type": "string",
      "enum": ["navigate","click","type","screenshot","evaluate","get_text","get_html"],
      "description": "Browser operation to perform"
    },
    "url": {
      "type": "string",
      "description": "URL to navigate to (navigate)"
    },
    "selector": {
      "type": "string",
      "description": "CSS selector for click / type / get_html"
    },
    "text": {
      "type": "string",
      "description": "Text to type into the selected input (type)"
    },
    "script": {
      "type": "string",
      "description": "JavaScript expression to evaluate in the page context (evaluate)"
    }
  },
  "required": ["operation"]
}`)
}

// browserParams mirrors the JSON Schema above.
type browserParams struct {
	Operation string `json:"operation"`
	URL       string `json:"url"`
	Selector  string `json:"selector"`
	Text      string `json:"text"`
	Script    string `json:"script"`
}

func (b *browserTool) Execute(ctx context.Context, raw json.RawMessage) (*Result, error) {
	var p browserParams
	if err := json.Unmarshal(raw, &p); err != nil {
		return &Result{Error: "invalid parameters: " + err.Error()}, nil
	}

	// Apply a 30-second per-operation timeout.
	opCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	page, err := acquirePage(opCtx)
	if err != nil {
		return &Result{Error: err.Error()}, nil
	}

	switch p.Operation {
	case "navigate":
		return b.navigate(opCtx, page, p.URL)
	case "click":
		return b.click(opCtx, page, p.Selector)
	case "type":
		return b.typeText(opCtx, page, p.Selector, p.Text)
	case "screenshot":
		return b.screenshot(opCtx, page)
	case "evaluate":
		return b.evaluate(opCtx, page, p.Script)
	case "get_text":
		return b.getText(opCtx, page)
	case "get_html":
		return b.getHTML(opCtx, page, p.Selector)
	default:
		return &Result{Error: fmt.Sprintf("unknown operation %q", p.Operation)}, nil
	}
}

// ---------------------------------------------------------------------------
// Operation handlers
// ---------------------------------------------------------------------------

func (b *browserTool) navigate(ctx context.Context, page *rod.Page, target string) (*Result, error) {
	if target == "" {
		return &Result{Error: "navigate: url is required"}, nil
	}
	if err := checkNavigableURL(target); err != nil {
		return &Result{Error: "navigate: " + err.Error()}, nil
	}

	if err := page.Context(ctx).Navigate(target); err != nil {
		return &Result{Error: fmt.Sprintf("navigate: %v", err)}, nil
	}

	if err := page.Context(ctx).WaitLoad(); err != nil {
		// Non-fatal: page may have partially loaded.
		_ = err
	}

	titleEl, err := page.Context(ctx).Element("title")
	var title string
	if err == nil {
		title, _ = titleEl.Text()
	}
	if title == "" {
		title = "(unknown title)"
	}

	text, err := pageVisibleText(ctx, page)
	if err != nil {
		text = ""
	}

	output := fmt.Sprintf("Title: %s\n\n%s", title, text)
	return &Result{Output: output}, nil
}

func (b *browserTool) click(ctx context.Context, page *rod.Page, selector string) (*Result, error) {
	if selector == "" {
		return &Result{Error: "click: selector is required"}, nil
	}

	el, err := page.Context(ctx).Element(selector)
	if err != nil {
		return &Result{Error: fmt.Sprintf("click: element %q not found: %v", selector, err)}, nil
	}

	if err := el.Context(ctx).Click(proto.InputMouseButtonLeft, 1); err != nil {
		return &Result{Error: fmt.Sprintf("click: %v", err)}, nil
	}

	return &Result{Output: fmt.Sprintf("clicked %q", selector)}, nil
}

func (b *browserTool) typeText(ctx context.Context, page *rod.Page, selector, text string) (*Result, error) {
	if selector == "" {
		return &Result{Error: "type: selector is required"}, nil
	}

	el, err := page.Context(ctx).Element(selector)
	if err != nil {
		return &Result{Error: fmt.Sprintf("type: element %q not found: %v", selector, err)}, nil
	}

	if err := el.Context(ctx).Input(text); err != nil {
		return &Result{Error: fmt.Sprintf("type: %v", err)}, nil
	}

	return &Result{Output: fmt.Sprintf("typed into %q", selector)}, nil
}

func (b *browserTool) screenshot(ctx context.Context, page *rod.Page) (*Result, error) {
	data, err := page.Context(ctx).Screenshot(true, &proto.PageCaptureScreenshot{
		Format:  proto.PageCaptureScreenshotFormatPng,
		Quality: nil,
	})
	if err != nil {
		return &Result{Error: fmt.Sprintf("screenshot: %v", err)}, nil
	}

	encoded := base64.StdEncoding.EncodeToString(data)
	return &Result{Output: encoded}, nil
}

func (b *browserTool) evaluate(ctx context.Context, page *rod.Page, script string) (*Result, error) {
	if script == "" {
		return &Result{Error: "evaluate: script is required"}, nil
	}

	res, err := page.Context(ctx).Eval(script)
	if err != nil {
		return &Result{Error: fmt.Sprintf("evaluate: %v", err)}, nil
	}

	output := res.Value.String()
	return &Result{Output: output}, nil
}

func (b *browserTool) getText(ctx context.Context, page *rod.Page) (*Result, error) {
	text, err := pageVisibleText(ctx, page)
	if err != nil {
		return &Result{Error: fmt.Sprintf("get_text: %v", err)}, nil
	}

	return &Result{Output: text}, nil
}

func (b *browserTool) getHTML(ctx context.Context, page *rod.Page, selector string) (*Result, error) {
	if selector == "" {
		// Return the full page HTML when no selector is provided.
		html, err := page.Context(ctx).HTML()
		if err != nil {
			return &Result{Error: fmt.Sprintf("get_html: %v", err)}, nil
		}

		return &Result{Output: html}, nil
	}

	el, err := page.Context(ctx).Element(selector)
	if err != nil {
		return &Result{Error: fmt.Sprintf("get_html: element %q not found: %v", selector, err)}, nil
	}

	html, err := el.Context(ctx).HTML()
	if err != nil {
		return &Result{Error: fmt.Sprintf("get_html: %v", err)}, nil
	}

	return &Result{Output: html}, nil
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// pageVisibleText extracts the visible text content of the page body via JS.
func pageVisibleText(ctx context.Context, page *rod.Page) (string, error) {
	res, err := page.Context(ctx).Eval(`() => document.body ? document.body.innerText : ""`)
	if err != nil {
		return "", err
	}

	return strings.TrimSpace(res.Value.String()), nil
}
