// Package tools – browser automation tool powered by go-rod / Chrome DevTools Protocol.
package tools

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/launcher"
	"github.com/go-rod/rod/lib/proto"
)

func init() {
	Register(&browserTool{})
}

// ---------------------------------------------------------------------------
// Singleton browser state
// ---------------------------------------------------------------------------

var (
	browserMu     sync.Mutex
	sharedBrowser *rod.Browser
	sharedPage    *rod.Page
)

func acquirePage(ctx context.Context) (*rod.Page, error) {
	browserMu.Lock()
	defer browserMu.Unlock()

	// Launch browser lazily on first use.
	if sharedBrowser == nil {
		headless := true
		if v := os.Getenv("PAN_AGENT_BROWSER_HEADLESS"); strings.ToLower(v) == "false" {
			headless = false
		}

		u, err := launcher.New().
			Headless(headless).
			// go-rod auto-downloads Chromium when not present.
			Launch()
		if err != nil {
			return nil, fmt.Errorf("browser: launch chromium: %w", err)
		}

		b := rod.New().ControlURL(u).MustConnect()

		// Shut the browser down when the provided context is cancelled.
		go func() {
			<-ctx.Done()
			browserMu.Lock()
			defer browserMu.Unlock()
			if sharedBrowser != nil {
				_ = sharedBrowser.Close()
				sharedBrowser = nil
				sharedPage = nil
			}
		}()

		sharedBrowser = b
	}

	// Reuse or open a single page.
	if sharedPage == nil {
		p, err := sharedBrowser.Page(proto.TargetCreateTarget{URL: "about:blank"})
		if err != nil {
			return nil, fmt.Errorf("browser: open page: %w", err)
		}
		sharedPage = p
	}

	return sharedPage, nil
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

	page, err := acquirePage(ctx)
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

func (b *browserTool) navigate(ctx context.Context, page *rod.Page, url string) (*Result, error) {
	if url == "" {
		return &Result{Error: "navigate: url is required"}, nil
	}

	if err := page.Context(ctx).Navigate(url); err != nil {
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
