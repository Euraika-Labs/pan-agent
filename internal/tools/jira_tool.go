package tools

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/euraika-labs/pan-agent/internal/saaslinks"
)

// Phase 13 WS#13.D — Jira read-only tool.
//
// Surfaces three actions over Jira's Cloud REST API v3:
//
//   search    JQL-driven search across issues.
//   get_issue Fetch one issue by key (e.g. "PAN-42").
//   myself    Current authenticated user (auth probe / debug).
//
// Auth — two paths:
//
//   Cloud (atlassian.net)  → JIRA_HOST + JIRA_EMAIL + JIRA_API_TOKEN.
//                            Basic auth with email:token base64-encoded.
//   Self-hosted / Server   → JIRA_HOST + JIRA_BEARER. Bearer header with
//                            a personal-access-token.
//
// Picking between the two: presence of JIRA_BEARER prefers bearer
// auth; otherwise the Cloud Basic-auth path runs. Tells the user
// what's missing when neither set is complete.
//
// Output JSON includes the saaslinks.Jira issue URL so the receipt
// UI surfaces a click-through to the host's web UI.
//
// Read-only by design — no create/edit/transition/comment actions
// in this slice. Those land behind the existing approval gate.

const jiraAPIPath = "/rest/api/3"

// jiraHostFn lets tests override the host. Production reads from env.
var jiraHostFn = func() string { return strings.TrimSpace(os.Getenv("JIRA_HOST")) }

// jiraBaseURLFn returns the full base URL the tool should hit.
// Tests substitute an httptest server URL; production builds
// "https://<host><jiraAPIPath>".
var jiraBaseURLFn = func() string {
	host := jiraHostFn()
	if host == "" {
		return ""
	}
	if !strings.HasPrefix(host, "http://") && !strings.HasPrefix(host, "https://") {
		host = "https://" + host
	}
	return strings.TrimSuffix(host, "/") + jiraAPIPath
}

var jiraHTTPClient = &http.Client{Timeout: 15 * time.Second}

// jiraIssueKeyRe matches the canonical PROJECT-NNN issue-key shape.
// Atlassian docs: project key is 2-10 uppercase letters; issue
// number is 1+ digits.
var jiraIssueKeyRe = regexp.MustCompile(`^[A-Z][A-Z0-9]{1,9}-[1-9][0-9]{0,9}$`)

// JiraTool is the read-only Jira API tool.
type JiraTool struct{}

func (JiraTool) Name() string { return "jira" }

func (JiraTool) Description() string {
	return "Read-only Jira Cloud API client. " +
		"Actions: search (JQL-driven across issues), " +
		"get_issue (one issue by key), " +
		"myself (current user; auth probe). " +
		"Returns JSON with the Jira data + a host web URL " +
		"for human review. Requires JIRA_HOST + (JIRA_EMAIL + " +
		"JIRA_API_TOKEN) for Cloud, or JIRA_BEARER for self-hosted."
}

func (JiraTool) Parameters() json.RawMessage {
	return json.RawMessage(`{
  "type": "object",
  "required": ["action"],
  "properties": {
    "action": {
      "type": "string",
      "enum": ["search", "get_issue", "myself"]
    },
    "jql": {
      "type": "string",
      "description": "search-only: JQL query string (e.g. 'project = PAN AND status = \"In Progress\"')."
    },
    "issue_key": {
      "type": "string",
      "description": "get_issue-only: the issue key (e.g. 'PAN-42')."
    },
    "limit": {
      "type": "integer",
      "minimum": 1,
      "maximum": 100,
      "description": "search-only: max issues. Default 25, cap 100."
    },
    "fields": {
      "type": "string",
      "description": "search-only: comma-separated field list. Default: 'summary,status,assignee,priority,updated'."
    }
  }
}`)
}

type jiraParams struct {
	Action   string `json:"action"`
	JQL      string `json:"jql,omitempty"`
	IssueKey string `json:"issue_key,omitempty"`
	Limit    int    `json:"limit,omitempty"`
	Fields   string `json:"fields,omitempty"`
}

// Execute dispatches the action. Auth + base URL probe runs first
// so misconfiguration produces a clear error before we even look at
// the action.
func (t JiraTool) Execute(ctx context.Context, params json.RawMessage) (*Result, error) {
	var p jiraParams
	if err := json.Unmarshal(params, &p); err != nil {
		return &Result{Error: fmt.Sprintf("invalid parameters: %v", err)}, nil
	}
	host := jiraHostFn()
	if host == "" {
		return &Result{Error: "JIRA_HOST env var is required"}, nil
	}
	auth, err := jiraAuth()
	if err != nil {
		return &Result{Error: err.Error()}, nil
	}

	switch p.Action {
	case "search":
		return t.search(ctx, host, auth, p)
	case "get_issue":
		return t.getIssue(ctx, host, auth, p)
	case "myself":
		return t.myself(ctx, auth)
	case "":
		return &Result{Error: "action required (search|get_issue|myself)"}, nil
	default:
		return &Result{Error: fmt.Sprintf("unknown action %q", p.Action)}, nil
	}
}

// jiraAuth returns the value to put in the Authorization header.
// Bearer wins when JIRA_BEARER is set; otherwise Basic email:token.
func jiraAuth() (string, error) {
	if bearer := strings.TrimSpace(os.Getenv("JIRA_BEARER")); bearer != "" {
		return "Bearer " + bearer, nil
	}
	email := strings.TrimSpace(os.Getenv("JIRA_EMAIL"))
	token := strings.TrimSpace(os.Getenv("JIRA_API_TOKEN"))
	if email == "" || token == "" {
		return "", fmt.Errorf(
			"jira auth missing — set JIRA_BEARER for self-hosted, or JIRA_EMAIL + JIRA_API_TOKEN for Cloud")
	}
	enc := base64.StdEncoding.EncodeToString([]byte(email + ":" + token))
	return "Basic " + enc, nil
}

// ---------------------------------------------------------------------------
// search — GET /rest/api/3/search
// ---------------------------------------------------------------------------

func (JiraTool) search(ctx context.Context, host, auth string, p jiraParams) (*Result, error) {
	if strings.TrimSpace(p.JQL) == "" {
		return &Result{Error: "jql required"}, nil
	}
	limit := p.Limit
	if limit <= 0 {
		limit = 25
	}
	if limit > 100 {
		limit = 100
	}
	fields := strings.TrimSpace(p.Fields)
	if fields == "" {
		fields = "summary,status,assignee,priority,updated"
	}

	q := url.Values{}
	q.Set("jql", p.JQL)
	q.Set("maxResults", fmt.Sprintf("%d", limit))
	q.Set("fields", fields)

	body, err := jiraGET(ctx, jiraBaseURLFn()+"/search?"+q.Encode(), auth)
	if err != nil {
		return &Result{Error: err.Error()}, nil
	}

	var resp struct {
		Issues []struct {
			Key    string          `json:"key"`
			Fields json.RawMessage `json:"fields"`
		} `json:"issues"`
		Total      int `json:"total"`
		MaxResults int `json:"maxResults"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return &Result{Error: fmt.Sprintf("decode search: %v", err)}, nil
	}

	type issueView struct {
		Key    string          `json:"key"`
		URL    string          `json:"url"`
		Fields json.RawMessage `json:"fields"`
	}
	out := struct {
		Issues     []issueView `json:"issues"`
		Total      int         `json:"total"`
		MaxResults int         `json:"max_results"`
	}{Total: resp.Total, MaxResults: resp.MaxResults}
	for _, i := range resp.Issues {
		view := issueView{Key: i.Key, Fields: i.Fields}
		if u, ok := saaslinks.Jira(host, i.Key); ok {
			view.URL = u
		}
		out.Issues = append(out.Issues, view)
	}

	js, _ := json.MarshalIndent(out, "", "  ")
	return &Result{Output: string(js)}, nil
}

// ---------------------------------------------------------------------------
// get_issue — GET /rest/api/3/issue/<key>
// ---------------------------------------------------------------------------

func (JiraTool) getIssue(ctx context.Context, host, auth string, p jiraParams) (*Result, error) {
	if !isJiraIssueKey(p.IssueKey) {
		return &Result{Error: "issue_key must match PROJECT-NNN (e.g. PAN-42)"}, nil
	}

	body, err := jiraGET(ctx, jiraBaseURLFn()+"/issue/"+p.IssueKey, auth)
	if err != nil {
		return &Result{Error: err.Error()}, nil
	}

	out := struct {
		Issue json.RawMessage `json:"issue"`
		URL   string          `json:"url"`
	}{Issue: body}
	if u, ok := saaslinks.Jira(host, p.IssueKey); ok {
		out.URL = u
	}
	js, _ := json.MarshalIndent(out, "", "  ")
	return &Result{Output: string(js)}, nil
}

// ---------------------------------------------------------------------------
// myself — GET /rest/api/3/myself
// ---------------------------------------------------------------------------

func (JiraTool) myself(ctx context.Context, auth string) (*Result, error) {
	body, err := jiraGET(ctx, jiraBaseURLFn()+"/myself", auth)
	if err != nil {
		return &Result{Error: err.Error()}, nil
	}
	return &Result{Output: string(body)}, nil
}

// ---------------------------------------------------------------------------
// HTTP helpers
// ---------------------------------------------------------------------------

func jiraGET(ctx context.Context, endpoint, auth string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Authorization", auth)
	req.Header.Set("Accept", "application/json")

	resp, err := jiraHTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("jira: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 8*1024*1024))
	if err != nil {
		return nil, fmt.Errorf("read body: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("jira: status %d: %s",
			resp.StatusCode, truncateJiraBody(string(body)))
	}
	return body, nil
}

func truncateJiraBody(s string) string {
	const max = 256
	if len(s) <= max {
		return s
	}
	return s[:max] + "...(truncated)"
}

func isJiraIssueKey(s string) bool {
	if s == "" || len(s) > 32 {
		return false
	}
	return jiraIssueKeyRe.MatchString(s)
}

var _ Tool = JiraTool{}

func init() {
	Register(JiraTool{})
}
