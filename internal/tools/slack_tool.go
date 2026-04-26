package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/euraika-labs/pan-agent/internal/saaslinks"
)

// Phase 13 WS#13.D — Slack read-only tool.
//
// Surfaces three actions over the Slack Web API:
//
//   list_channels        Paginated public + private channel list.
//   channel_history      Recent messages from a channel id.
//   user_info            Lookup a Slack user by id.
//
// Auth: SLACK_BOT_TOKEN env var (the xoxb- bot token from the
// Slack app's OAuth & Permissions settings). The Slack-token
// secret recogniser in internal/secret (#39) tokenises any
// xoxb-/xoxp- shape that flows OUTBOUND to the LLM, so a leak
// from prompt history is already protected — this tool consumes
// the token via env only.
//
// Output JSON includes a saaslinks.Slack URL pointing at the
// relevant channel/thread so the user can click through to the
// Slack web app from the receipt UI.
//
// Read-only by design — no post/edit/delete actions in this
// slice. Those land behind the existing approval gate when we
// add them.

const slackAPIBase = "https://slack.com/api"

// slackAPIBaseFn lets tests substitute a fake server URL. Production
// always uses slackAPIBase.
var slackAPIBaseFn = func() string { return slackAPIBase }

// slackHTTPClient — 15s timeout matches the Stripe tool. Slack's
// own SLA is sub-second; this caps slow-network failure modes.
var slackHTTPClient = &http.Client{Timeout: 15 * time.Second}

// SlackTool is the read-only Slack Web API tool.
type SlackTool struct{}

func (SlackTool) Name() string { return "slack" }

func (SlackTool) Description() string {
	return "Read-only Slack Web API client. " +
		"Actions: list_channels (paginated public + private), " +
		"channel_history (recent messages from one channel), " +
		"user_info (lookup user by id). " +
		"Returns JSON with the Slack data + a slack.com web URL " +
		"for human review. Requires SLACK_BOT_TOKEN env var."
}

func (SlackTool) Parameters() json.RawMessage {
	return json.RawMessage(`{
  "type": "object",
  "required": ["action"],
  "properties": {
    "action": {
      "type": "string",
      "enum": ["list_channels", "channel_history", "user_info"]
    },
    "channel": {
      "type": "string",
      "description": "channel_history-only: channel id (C…) or DM id (D…)."
    },
    "user": {
      "type": "string",
      "description": "user_info-only: user id (U…)."
    },
    "limit": {
      "type": "integer",
      "minimum": 1,
      "maximum": 200,
      "description": "Per-action max rows. Default 20, cap 200."
    },
    "cursor": {
      "type": "string",
      "description": "Pagination cursor (Slack-style 'next_cursor')."
    },
    "workspace": {
      "type": "string",
      "description": "Workspace subdomain for the dashboard URL (e.g. 'acme' for acme.slack.com). Optional — defaults to omitting the URL when unset."
    }
  }
}`)
}

type slackParams struct {
	Action    string `json:"action"`
	Channel   string `json:"channel,omitempty"`
	User      string `json:"user,omitempty"`
	Limit     int    `json:"limit,omitempty"`
	Cursor    string `json:"cursor,omitempty"`
	Workspace string `json:"workspace,omitempty"`
}

func (t SlackTool) Execute(ctx context.Context, params json.RawMessage) (*Result, error) {
	var p slackParams
	if err := json.Unmarshal(params, &p); err != nil {
		return &Result{Error: fmt.Sprintf("invalid parameters: %v", err)}, nil
	}
	token := strings.TrimSpace(os.Getenv("SLACK_BOT_TOKEN"))
	if token == "" {
		return &Result{Error: "SLACK_BOT_TOKEN env var is required"}, nil
	}

	switch p.Action {
	case "list_channels":
		return t.listChannels(ctx, token, p)
	case "channel_history":
		return t.channelHistory(ctx, token, p)
	case "user_info":
		return t.userInfo(ctx, token, p)
	case "":
		return &Result{Error: "action required (list_channels|channel_history|user_info)"}, nil
	default:
		return &Result{Error: fmt.Sprintf("unknown action %q", p.Action)}, nil
	}
}

// ---------------------------------------------------------------------------
// list_channels — GET /api/conversations.list
// ---------------------------------------------------------------------------

func (SlackTool) listChannels(ctx context.Context, token string, p slackParams) (*Result, error) {
	q := url.Values{}
	q.Set("limit", clampLimit(p.Limit, 20, 200))
	q.Set("types", "public_channel,private_channel")
	if p.Cursor != "" {
		q.Set("cursor", p.Cursor)
	}

	body, err := slackGET(ctx, slackAPIBaseFn()+"/conversations.list?"+q.Encode(), token)
	if err != nil {
		return &Result{Error: err.Error()}, nil
	}

	var resp struct {
		OK       bool   `json:"ok"`
		Error    string `json:"error,omitempty"`
		Channels []struct {
			ID         string `json:"id"`
			Name       string `json:"name"`
			IsPrivate  bool   `json:"is_private"`
			IsArchived bool   `json:"is_archived"`
			NumMembers int    `json:"num_members"`
		} `json:"channels"`
		ResponseMetadata struct {
			NextCursor string `json:"next_cursor"`
		} `json:"response_metadata"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return &Result{Error: fmt.Sprintf("decode list_channels: %v", err)}, nil
	}
	if !resp.OK {
		return &Result{Error: "slack: " + slackErr(resp.Error)}, nil
	}

	type channelView struct {
		ID         string `json:"id"`
		Name       string `json:"name"`
		IsPrivate  bool   `json:"is_private"`
		IsArchived bool   `json:"is_archived"`
		NumMembers int    `json:"num_members"`
		URL        string `json:"url,omitempty"`
	}
	out := struct {
		Channels   []channelView `json:"channels"`
		NextCursor string        `json:"next_cursor,omitempty"`
	}{NextCursor: resp.ResponseMetadata.NextCursor}

	for _, c := range resp.Channels {
		view := channelView{
			ID: c.ID, Name: c.Name, IsPrivate: c.IsPrivate,
			IsArchived: c.IsArchived, NumMembers: c.NumMembers,
		}
		if p.Workspace != "" {
			if u, ok := saaslinks.Slack(p.Workspace, c.ID, ""); ok {
				view.URL = u
			}
		}
		out.Channels = append(out.Channels, view)
	}

	js, _ := json.MarshalIndent(out, "", "  ")
	return &Result{Output: string(js)}, nil
}

// ---------------------------------------------------------------------------
// channel_history — GET /api/conversations.history
// ---------------------------------------------------------------------------

func (SlackTool) channelHistory(ctx context.Context, token string, p slackParams) (*Result, error) {
	if !isSlackID(p.Channel) {
		return &Result{Error: "channel id required (C… or D…)"}, nil
	}
	q := url.Values{}
	q.Set("channel", p.Channel)
	q.Set("limit", clampLimit(p.Limit, 20, 200))
	if p.Cursor != "" {
		q.Set("cursor", p.Cursor)
	}

	body, err := slackGET(ctx, slackAPIBaseFn()+"/conversations.history?"+q.Encode(), token)
	if err != nil {
		return &Result{Error: err.Error()}, nil
	}

	var resp struct {
		OK       bool   `json:"ok"`
		Error    string `json:"error,omitempty"`
		Messages []struct {
			Type      string `json:"type"`
			User      string `json:"user"`
			Text      string `json:"text"`
			Timestamp string `json:"ts"`
			ThreadTS  string `json:"thread_ts,omitempty"`
		} `json:"messages"`
		HasMore          bool `json:"has_more"`
		ResponseMetadata struct {
			NextCursor string `json:"next_cursor"`
		} `json:"response_metadata"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return &Result{Error: fmt.Sprintf("decode channel_history: %v", err)}, nil
	}
	if !resp.OK {
		return &Result{Error: "slack: " + slackErr(resp.Error)}, nil
	}

	type messageView struct {
		Type      string `json:"type"`
		User      string `json:"user"`
		Text      string `json:"text"`
		Timestamp string `json:"ts"`
		ThreadTS  string `json:"thread_ts,omitempty"`
		URL       string `json:"url,omitempty"`
	}
	out := struct {
		Messages   []messageView `json:"messages"`
		HasMore    bool          `json:"has_more"`
		NextCursor string        `json:"next_cursor,omitempty"`
	}{HasMore: resp.HasMore, NextCursor: resp.ResponseMetadata.NextCursor}

	for _, m := range resp.Messages {
		view := messageView{
			Type: m.Type, User: m.User, Text: m.Text,
			Timestamp: m.Timestamp, ThreadTS: m.ThreadTS,
		}
		if p.Workspace != "" && m.Timestamp != "" {
			if u, ok := saaslinks.Slack(p.Workspace, p.Channel, m.Timestamp); ok {
				view.URL = u
			}
		}
		out.Messages = append(out.Messages, view)
	}

	js, _ := json.MarshalIndent(out, "", "  ")
	return &Result{Output: string(js)}, nil
}

// ---------------------------------------------------------------------------
// user_info — GET /api/users.info
// ---------------------------------------------------------------------------

func (SlackTool) userInfo(ctx context.Context, token string, p slackParams) (*Result, error) {
	if !isSlackID(p.User) {
		return &Result{Error: "user id required (U…)"}, nil
	}
	q := url.Values{}
	q.Set("user", p.User)

	body, err := slackGET(ctx, slackAPIBaseFn()+"/users.info?"+q.Encode(), token)
	if err != nil {
		return &Result{Error: err.Error()}, nil
	}

	var resp struct {
		OK    bool   `json:"ok"`
		Error string `json:"error,omitempty"`
		User  struct {
			ID       string `json:"id"`
			Name     string `json:"name"`
			RealName string `json:"real_name"`
			Profile  struct {
				DisplayName string `json:"display_name"`
				Email       string `json:"email"`
				Title       string `json:"title"`
			} `json:"profile"`
			IsBot   bool `json:"is_bot"`
			Deleted bool `json:"deleted"`
			IsAdmin bool `json:"is_admin"`
		} `json:"user"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return &Result{Error: fmt.Sprintf("decode user_info: %v", err)}, nil
	}
	if !resp.OK {
		return &Result{Error: "slack: " + slackErr(resp.Error)}, nil
	}

	js, _ := json.MarshalIndent(resp.User, "", "  ")
	return &Result{Output: string(js)}, nil
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// slackGET does the auth + body cap + status check. Returns the raw
// body — Slack returns 200 even on logical errors (the per-response
// `ok: false` field carries them), so callers parse + check both.
func slackGET(ctx context.Context, endpoint, token string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/json")

	resp, err := slackHTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("slack: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 8*1024*1024))
	if err != nil {
		return nil, fmt.Errorf("read body: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("slack: status %d: %s",
			resp.StatusCode, truncateForLog(string(body), 256))
	}
	return body, nil
}

// slackErr humanises Slack's machine-readable error codes.
func slackErr(code string) string {
	switch code {
	case "":
		return "unknown error"
	case "missing_scope":
		return "bot token is missing the required scope (check the Slack app's OAuth permissions)"
	case "channel_not_found":
		return "channel not found (or bot not invited to it)"
	case "user_not_found":
		return "user not found"
	case "invalid_auth", "not_authed":
		return "invalid auth (check SLACK_BOT_TOKEN)"
	case "rate_limited":
		return "rate limited (back off + retry)"
	default:
		return code
	}
}

// isSlackID returns true when s looks like a Slack object id —
// uppercase + digits + underscore, length 1..32. Slack ids are
// always 9–11 chars in practice; the cap is generous.
func isSlackID(s string) bool {
	if s == "" || len(s) > 32 {
		return false
	}
	for _, c := range s {
		ok := (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '_'
		if !ok {
			return false
		}
	}
	return true
}

// clampLimit returns a string-form limit clamped to [min, max].
// 0 → default. Used by both list_channels + channel_history.
func clampLimit(v, deflt, max int) string {
	if v <= 0 {
		v = deflt
	}
	if v > max {
		v = max
	}
	return fmt.Sprintf("%d", v)
}

// truncateForLog caps a body string for inclusion in an error
// message. Local helper to avoid stripeAPI's truncateStripeBody
// (different package-private symbol).
func truncateForLog(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "...(truncated)"
}

var _ Tool = SlackTool{}

func init() {
	Register(SlackTool{})
}
