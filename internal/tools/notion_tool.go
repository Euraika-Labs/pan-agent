package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/euraika-labs/pan-agent/internal/saaslinks"
)

// Phase 13 WS#13.D — Notion read-only tool.
//
// Surfaces three actions over the Notion API:
//
//   search       Search pages + databases by query string.
//   get_page     Fetch one page's metadata + properties.
//   get_block    Fetch one block's content (used for page bodies).
//
// Auth: NOTION_API_KEY env var. Format: secret_xxx (legacy) or
// ntn_xxx (the post-Aug-2024 internal-integration prefix). Either
// is accepted as a Bearer token.
//
// Output JSON includes a saaslinks.Notion URL for the looked-up
// id so the receipt UI surfaces a click-through to notion.so.
//
// Read-only by design — no create/update/append actions in this
// slice. Those land behind the existing approval gate when added.

const (
	notionAPIBase = "https://api.notion.com/v1"
	// notionAPIVersion pins the Notion-Version header. Notion is
	// strict about this — omit it and every call returns 400.
	notionAPIVersion = "2022-06-28"
)

var notionAPIBaseFn = func() string { return notionAPIBase }

var notionHTTPClient = &http.Client{Timeout: 15 * time.Second}

// notionUUIDRe matches the two id shapes Notion accepts: bare 32
// hex chars, or canonical UUID with dashes (8-4-4-4-12).
var notionUUIDRe = regexp.MustCompile(
	`^[0-9a-fA-F]{32}$|^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

// NotionTool is the read-only Notion API tool.
type NotionTool struct{}

func (NotionTool) Name() string { return "notion" }

func (NotionTool) Description() string {
	return "Read-only Notion API client. " +
		"Actions: search (pages + databases by query), " +
		"get_page (metadata + properties of one page), " +
		"get_block (one block's content; used for page bodies). " +
		"Returns JSON with the Notion data + a notion.so URL " +
		"for human review. Requires NOTION_API_KEY env var."
}

func (NotionTool) Parameters() json.RawMessage {
	return json.RawMessage(`{
  "type": "object",
  "required": ["action"],
  "properties": {
    "action": {
      "type": "string",
      "enum": ["search", "get_page", "get_block"]
    },
    "query": {
      "type": "string",
      "description": "search-only: substring query; matches page + database titles."
    },
    "id": {
      "type": "string",
      "description": "get_page / get_block-only: 32-hex or UUID-form id."
    },
    "filter": {
      "type": "string",
      "enum": ["page", "database"],
      "description": "search-only: restrict to pages or databases. Default: both."
    },
    "limit": {
      "type": "integer",
      "minimum": 1,
      "maximum": 100,
      "description": "search-only: max results. Default 10, cap 100."
    }
  }
}`)
}

type notionParams struct {
	Action string `json:"action"`
	Query  string `json:"query,omitempty"`
	ID     string `json:"id,omitempty"`
	Filter string `json:"filter,omitempty"`
	Limit  int    `json:"limit,omitempty"`
}

func (t NotionTool) Execute(ctx context.Context, params json.RawMessage) (*Result, error) {
	var p notionParams
	if err := json.Unmarshal(params, &p); err != nil {
		return &Result{Error: fmt.Sprintf("invalid parameters: %v", err)}, nil
	}
	token := strings.TrimSpace(os.Getenv("NOTION_API_KEY"))
	if token == "" {
		return &Result{Error: "NOTION_API_KEY env var is required"}, nil
	}

	switch p.Action {
	case "search":
		return t.search(ctx, token, p)
	case "get_page":
		return t.getPage(ctx, token, p)
	case "get_block":
		return t.getBlock(ctx, token, p)
	case "":
		return &Result{Error: "action required (search|get_page|get_block)"}, nil
	default:
		return &Result{Error: fmt.Sprintf("unknown action %q", p.Action)}, nil
	}
}

// ---------------------------------------------------------------------------
// search — POST /v1/search
// ---------------------------------------------------------------------------

func (NotionTool) search(ctx context.Context, token string, p notionParams) (*Result, error) {
	limit := p.Limit
	if limit <= 0 {
		limit = 10
	}
	if limit > 100 {
		limit = 100
	}

	body := map[string]any{
		"query":     p.Query,
		"page_size": limit,
	}
	if p.Filter != "" {
		body["filter"] = map[string]string{
			"property": "object",
			"value":    p.Filter,
		}
	}
	bb, _ := json.Marshal(body)

	resp, err := notionPOST(ctx, notionAPIBaseFn()+"/search", token, bb)
	if err != nil {
		return &Result{Error: err.Error()}, nil
	}

	// Decode minimal shape — Notion returns rich payloads we don't
	// need to surface in full. Pick the fields a search-result UI
	// would actually render.
	var sr struct {
		Results []struct {
			Object     string         `json:"object"`
			ID         string         `json:"id"`
			URL        string         `json:"url"`
			Properties map[string]any `json:"properties,omitempty"`
			Title      []struct {
				PlainText string `json:"plain_text"`
			} `json:"title,omitempty"`
		} `json:"results"`
		HasMore bool `json:"has_more"`
	}
	if err := json.Unmarshal(resp, &sr); err != nil {
		return &Result{Error: fmt.Sprintf("decode search: %v", err)}, nil
	}

	type resultView struct {
		Object string `json:"object"`
		ID     string `json:"id"`
		Title  string `json:"title,omitempty"`
		URL    string `json:"url"`
	}
	out := struct {
		Results []resultView `json:"results"`
		HasMore bool         `json:"has_more"`
	}{HasMore: sr.HasMore}
	for _, r := range sr.Results {
		view := resultView{Object: r.Object, ID: r.ID, URL: r.URL}
		view.Title = extractNotionTitle(r.Title, r.Properties)
		// Replace the API-provided URL with the saaslinks form when
		// Notion gave us nothing (occasionally happens for shared
		// pages). Otherwise keep the canonical URL Notion returns.
		if view.URL == "" {
			if u, ok := saaslinks.Notion(r.ID); ok {
				view.URL = u
			}
		}
		out.Results = append(out.Results, view)
	}

	js, _ := json.MarshalIndent(out, "", "  ")
	return &Result{Output: string(js)}, nil
}

// ---------------------------------------------------------------------------
// get_page — GET /v1/pages/<id>
// ---------------------------------------------------------------------------

func (NotionTool) getPage(ctx context.Context, token string, p notionParams) (*Result, error) {
	if !isNotionID(p.ID) {
		return &Result{Error: "id required (32-hex or UUID form)"}, nil
	}
	resp, err := notionGET(ctx, notionAPIBaseFn()+"/pages/"+p.ID, token)
	if err != nil {
		return &Result{Error: err.Error()}, nil
	}
	out := struct {
		Page json.RawMessage `json:"page"`
		URL  string          `json:"url"`
	}{Page: resp}
	if u, ok := saaslinks.Notion(p.ID); ok {
		out.URL = u
	}
	js, _ := json.MarshalIndent(out, "", "  ")
	return &Result{Output: string(js)}, nil
}

// ---------------------------------------------------------------------------
// get_block — GET /v1/blocks/<id>/children?page_size=<limit>
// ---------------------------------------------------------------------------

func (NotionTool) getBlock(ctx context.Context, token string, p notionParams) (*Result, error) {
	if !isNotionID(p.ID) {
		return &Result{Error: "id required (32-hex or UUID form)"}, nil
	}
	limit := p.Limit
	if limit <= 0 {
		limit = 100
	}
	if limit > 100 {
		limit = 100
	}
	endpoint := fmt.Sprintf("%s/blocks/%s/children?page_size=%d",
		notionAPIBaseFn(), p.ID, limit)
	resp, err := notionGET(ctx, endpoint, token)
	if err != nil {
		return &Result{Error: err.Error()}, nil
	}
	out := struct {
		Children json.RawMessage `json:"children"`
		URL      string          `json:"url"`
	}{Children: resp}
	if u, ok := saaslinks.Notion(p.ID); ok {
		out.URL = u
	}
	js, _ := json.MarshalIndent(out, "", "  ")
	return &Result{Output: string(js)}, nil
}

// ---------------------------------------------------------------------------
// HTTP helpers
// ---------------------------------------------------------------------------

func notionGET(ctx context.Context, endpoint, token string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	addNotionHeaders(req, token)
	return doNotion(req)
}

func notionPOST(ctx context.Context, endpoint, token string, body []byte) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint,
		bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	addNotionHeaders(req, token)
	req.Header.Set("Content-Type", "application/json")
	return doNotion(req)
}

func addNotionHeaders(req *http.Request, token string) {
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Notion-Version", notionAPIVersion)
	req.Header.Set("Accept", "application/json")
}

func doNotion(req *http.Request) ([]byte, error) {
	resp, err := notionHTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("notion: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 8*1024*1024))
	if err != nil {
		return nil, fmt.Errorf("read body: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("notion: status %d: %s",
			resp.StatusCode, truncateNotionBody(string(body)))
	}
	return body, nil
}

// truncateNotionBody caps a body string for inclusion in an error
// message. Local helper to avoid coupling to the Slack tool's
// truncateForLog (the two tools' branches may merge in either
// order; keeping helpers local avoids a follow-up dedupe slice).
func truncateNotionBody(s string) string {
	const max = 256
	if len(s) <= max {
		return s
	}
	return s[:max] + "...(truncated)"
}

// ---------------------------------------------------------------------------
// Misc
// ---------------------------------------------------------------------------

// isNotionID accepts 32-hex (no dashes) or canonical UUID forms.
func isNotionID(s string) bool {
	if len(s) > 36 {
		return false
	}
	return notionUUIDRe.MatchString(s)
}

// extractNotionTitle pulls a human-readable title from the rich-
// text array a Notion page returns. The actual location varies:
//
//   - Pages inside a database: properties["Name"].title[].plain_text
//   - Top-level pages:         properties["title"].title[].plain_text
//   - Databases:               title[].plain_text (top-level)
//
// We try the obvious candidates in order. Empty string means we
// couldn't find a title (the search-result UI shows the id as a
// fallback).
func extractNotionTitle(top []struct {
	PlainText string `json:"plain_text"`
}, props map[string]any) string {
	// Top-level title array (databases).
	for _, t := range top {
		if t.PlainText != "" {
			return t.PlainText
		}
	}
	if len(props) == 0 {
		return ""
	}
	// Look for a property named "Name" or "title" with a title-array shape.
	for _, key := range []string{"Name", "title", "Title"} {
		if raw, ok := props[key]; ok {
			if title := titleFromProp(raw); title != "" {
				return title
			}
		}
	}
	// Fallback: scan every property for one with a title array.
	for _, raw := range props {
		if title := titleFromProp(raw); title != "" {
			return title
		}
	}
	return ""
}

func titleFromProp(raw any) string {
	m, ok := raw.(map[string]any)
	if !ok {
		return ""
	}
	arr, ok := m["title"].([]any)
	if !ok {
		return ""
	}
	for _, item := range arr {
		mm, ok := item.(map[string]any)
		if !ok {
			continue
		}
		if pt, ok := mm["plain_text"].(string); ok && pt != "" {
			return pt
		}
	}
	return ""
}

var _ Tool = NotionTool{}

func init() {
	Register(NotionTool{})
}
