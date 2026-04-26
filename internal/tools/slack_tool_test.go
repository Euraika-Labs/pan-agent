package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// Phase 13 WS#13.D — Slack tool tests. Hermetic via httptest.

func installSlackFake(t *testing.T, h http.HandlerFunc) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	prev := slackAPIBaseFn
	slackAPIBaseFn = func() string { return srv.URL }
	t.Cleanup(func() { slackAPIBaseFn = prev })
	return srv
}

// ---------------------------------------------------------------------------
// Auth + dispatch
// ---------------------------------------------------------------------------

func TestSlack_NoToken(t *testing.T) {
	t.Setenv("SLACK_BOT_TOKEN", "")
	tool := SlackTool{}
	out, _ := tool.Execute(context.Background(),
		json.RawMessage(`{"action":"list_channels"}`))
	if !strings.Contains(out.Error, "SLACK_BOT_TOKEN") {
		t.Errorf("expected token error, got %+v", out)
	}
}

func TestSlack_InvalidJSON(t *testing.T) {
	t.Setenv("SLACK_BOT_TOKEN", "xoxb-fake")
	tool := SlackTool{}
	out, _ := tool.Execute(context.Background(), json.RawMessage(`{not-json`))
	if !strings.Contains(out.Error, "invalid parameters") {
		t.Errorf("expected parse error, got %+v", out)
	}
}

func TestSlack_UnknownAction(t *testing.T) {
	t.Setenv("SLACK_BOT_TOKEN", "xoxb-fake")
	tool := SlackTool{}
	out, _ := tool.Execute(context.Background(),
		json.RawMessage(`{"action":"post_message"}`))
	if !strings.Contains(out.Error, "unknown action") {
		t.Errorf("expected unknown-action error, got %+v", out)
	}
}

func TestSlack_EmptyAction(t *testing.T) {
	t.Setenv("SLACK_BOT_TOKEN", "xoxb-fake")
	tool := SlackTool{}
	out, _ := tool.Execute(context.Background(), json.RawMessage(`{}`))
	if !strings.Contains(out.Error, "action required") {
		t.Errorf("expected action-required error, got %+v", out)
	}
}

// ---------------------------------------------------------------------------
// list_channels
// ---------------------------------------------------------------------------

func TestSlack_ListChannels_HappyPath(t *testing.T) {
	t.Setenv("SLACK_BOT_TOKEN", "xoxb-fake")
	installSlackFake(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/conversations.list" {
			t.Errorf("path = %q", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer xoxb-fake" {
			t.Errorf("auth = %q", got)
		}
		if got := r.URL.Query().Get("types"); !strings.Contains(got, "public_channel") {
			t.Errorf("types = %q, want public_channel,private_channel", got)
		}
		fmt.Fprint(w, `{
			"ok": true,
			"channels": [
				{"id":"C111","name":"general","is_private":false,"is_archived":false,"num_members":42},
				{"id":"C222","name":"random","is_private":false,"is_archived":false,"num_members":17}
			],
			"response_metadata": {"next_cursor":"dXNlcjp1c2VyMTIz"}
		}`)
	})

	tool := SlackTool{}
	out, _ := tool.Execute(context.Background(),
		json.RawMessage(`{"action":"list_channels","workspace":"acme"}`))
	if out.Error != "" {
		t.Fatalf("Error: %s", out.Error)
	}

	var resp struct {
		Channels []struct {
			ID, Name, URL string
		} `json:"channels"`
		NextCursor string `json:"next_cursor"`
	}
	if err := json.Unmarshal([]byte(out.Output), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Channels) != 2 {
		t.Fatalf("len = %d, want 2", len(resp.Channels))
	}
	if resp.NextCursor != "dXNlcjp1c2VyMTIz" {
		t.Errorf("cursor = %q", resp.NextCursor)
	}
	if !strings.Contains(resp.Channels[0].URL, "acme.slack.com") {
		t.Errorf("URL[0] = %q, want acme.slack.com host", resp.Channels[0].URL)
	}
	if !strings.Contains(resp.Channels[0].URL, "C111") {
		t.Errorf("URL[0] missing channel id: %q", resp.Channels[0].URL)
	}
}

func TestSlack_ListChannels_NoWorkspaceOmitsURL(t *testing.T) {
	t.Setenv("SLACK_BOT_TOKEN", "xoxb-fake")
	installSlackFake(t, func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"ok":true,"channels":[{"id":"C111","name":"x"}]}`)
	})
	tool := SlackTool{}
	out, _ := tool.Execute(context.Background(),
		json.RawMessage(`{"action":"list_channels"}`))
	if out.Error != "" {
		t.Fatalf("Error: %s", out.Error)
	}
	var resp struct {
		Channels []struct {
			ID, URL string
		}
	}
	_ = json.Unmarshal([]byte(out.Output), &resp)
	if resp.Channels[0].URL != "" {
		t.Errorf("URL should be empty without workspace, got %q", resp.Channels[0].URL)
	}
}

func TestSlack_ListChannels_LimitClamped(t *testing.T) {
	t.Setenv("SLACK_BOT_TOKEN", "xoxb-fake")
	var captured string
	installSlackFake(t, func(w http.ResponseWriter, r *http.Request) {
		captured = r.URL.Query().Get("limit")
		fmt.Fprint(w, `{"ok":true,"channels":[]}`)
	})
	tool := SlackTool{}
	if _, err := tool.Execute(context.Background(),
		json.RawMessage(`{"action":"list_channels","limit":500}`)); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if captured != "200" {
		t.Errorf("limit clamp: got %q, want 200", captured)
	}
}

func TestSlack_ListChannels_DefaultLimit(t *testing.T) {
	t.Setenv("SLACK_BOT_TOKEN", "xoxb-fake")
	var captured string
	installSlackFake(t, func(w http.ResponseWriter, r *http.Request) {
		captured = r.URL.Query().Get("limit")
		fmt.Fprint(w, `{"ok":true,"channels":[]}`)
	})
	tool := SlackTool{}
	if _, err := tool.Execute(context.Background(),
		json.RawMessage(`{"action":"list_channels"}`)); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if captured != "20" {
		t.Errorf("default limit: got %q, want 20", captured)
	}
}

func TestSlack_ListChannels_OKFalse(t *testing.T) {
	t.Setenv("SLACK_BOT_TOKEN", "xoxb-fake")
	installSlackFake(t, func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"ok":false,"error":"missing_scope"}`)
	})
	tool := SlackTool{}
	out, _ := tool.Execute(context.Background(),
		json.RawMessage(`{"action":"list_channels"}`))
	if !strings.Contains(out.Error, "missing the required scope") {
		t.Errorf("expected scope error, got %+v", out)
	}
}

func TestSlack_ListChannels_HTTPError(t *testing.T) {
	t.Setenv("SLACK_BOT_TOKEN", "xoxb-fake")
	installSlackFake(t, func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	})
	tool := SlackTool{}
	out, _ := tool.Execute(context.Background(),
		json.RawMessage(`{"action":"list_channels"}`))
	if !strings.Contains(out.Error, "500") {
		t.Errorf("expected status 500 in error, got %+v", out)
	}
}

// ---------------------------------------------------------------------------
// channel_history
// ---------------------------------------------------------------------------

func TestSlack_ChannelHistory_HappyPath(t *testing.T) {
	t.Setenv("SLACK_BOT_TOKEN", "xoxb-fake")
	installSlackFake(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/conversations.history" {
			t.Errorf("path = %q", r.URL.Path)
		}
		if got := r.URL.Query().Get("channel"); got != "C111" {
			t.Errorf("channel = %q", got)
		}
		fmt.Fprint(w, `{
			"ok": true,
			"messages": [
				{"type":"message","user":"U001","text":"hello","ts":"1700000000.000100"},
				{"type":"message","user":"U002","text":"world","ts":"1700000010.000200","thread_ts":"1700000000.000100"}
			],
			"has_more": false,
			"response_metadata": {"next_cursor":""}
		}`)
	})

	tool := SlackTool{}
	out, _ := tool.Execute(context.Background(),
		json.RawMessage(`{"action":"channel_history","channel":"C111","workspace":"acme"}`))
	if out.Error != "" {
		t.Fatalf("Error: %s", out.Error)
	}

	var resp struct {
		Messages []struct {
			Text, URL string
			User      string
		}
	}
	if err := json.Unmarshal([]byte(out.Output), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Messages) != 2 {
		t.Fatalf("len = %d, want 2", len(resp.Messages))
	}
	if !strings.Contains(resp.Messages[0].URL, "C111") {
		t.Errorf("URL missing channel id: %q", resp.Messages[0].URL)
	}
	if !strings.Contains(resp.Messages[0].URL, "p1700000000000100") {
		t.Errorf("URL missing ts-derived suffix: %q", resp.Messages[0].URL)
	}
}

func TestSlack_ChannelHistory_BadChannelID(t *testing.T) {
	t.Setenv("SLACK_BOT_TOKEN", "xoxb-fake")
	tool := SlackTool{}
	for _, bad := range []string{"", "lower-case", "id with space", "../escape"} {
		out, _ := tool.Execute(context.Background(),
			json.RawMessage(fmt.Sprintf(`{"action":"channel_history","channel":%q}`, bad)))
		if out.Error == "" {
			t.Errorf("channel %q: expected error", bad)
		}
	}
}

func TestSlack_ChannelHistory_ChannelNotFound(t *testing.T) {
	t.Setenv("SLACK_BOT_TOKEN", "xoxb-fake")
	installSlackFake(t, func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"ok":false,"error":"channel_not_found"}`)
	})
	tool := SlackTool{}
	out, _ := tool.Execute(context.Background(),
		json.RawMessage(`{"action":"channel_history","channel":"C999"}`))
	if !strings.Contains(out.Error, "channel not found") {
		t.Errorf("expected channel_not_found message, got %+v", out)
	}
}

// ---------------------------------------------------------------------------
// user_info
// ---------------------------------------------------------------------------

func TestSlack_UserInfo_HappyPath(t *testing.T) {
	t.Setenv("SLACK_BOT_TOKEN", "xoxb-fake")
	installSlackFake(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/users.info" {
			t.Errorf("path = %q", r.URL.Path)
		}
		fmt.Fprint(w, `{
			"ok": true,
			"user": {
				"id":"U001",
				"name":"alice",
				"real_name":"Alice Example",
				"profile":{"display_name":"alice","email":"alice@example.com","title":"Engineer"},
				"is_bot":false
			}
		}`)
	})
	tool := SlackTool{}
	out, _ := tool.Execute(context.Background(),
		json.RawMessage(`{"action":"user_info","user":"U001"}`))
	if out.Error != "" {
		t.Fatalf("Error: %s", out.Error)
	}
	if !strings.Contains(out.Output, "Alice Example") {
		t.Errorf("output missing real_name: %s", out.Output)
	}
}

func TestSlack_UserInfo_BadID(t *testing.T) {
	t.Setenv("SLACK_BOT_TOKEN", "xoxb-fake")
	tool := SlackTool{}
	out, _ := tool.Execute(context.Background(),
		json.RawMessage(`{"action":"user_info","user":"u-lowercase"}`))
	if out.Error == "" {
		t.Error("expected error on malformed user id")
	}
}

func TestSlack_UserInfo_UserNotFound(t *testing.T) {
	t.Setenv("SLACK_BOT_TOKEN", "xoxb-fake")
	installSlackFake(t, func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"ok":false,"error":"user_not_found"}`)
	})
	tool := SlackTool{}
	out, _ := tool.Execute(context.Background(),
		json.RawMessage(`{"action":"user_info","user":"U999"}`))
	if !strings.Contains(out.Error, "user not found") {
		t.Errorf("expected user_not_found message, got %+v", out)
	}
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func TestSlack_RegisteredInRegistry(t *testing.T) {
	tool, ok := Get("slack")
	if !ok {
		t.Fatal("slack tool not registered")
	}
	if tool.Name() != "slack" {
		t.Errorf("Name = %q", tool.Name())
	}
}

func TestSlack_IsSlackID(t *testing.T) {
	cases := map[string]bool{
		"C123":                  true,
		"U001":                  true,
		"D9ABCDEF":              true,
		"":                      false,
		"lower":                 false,
		"id space":              false,
		"id-hyphen":             false,
		strings.Repeat("A", 33): false,
		strings.Repeat("A", 32): true,
	}
	for in, want := range cases {
		if got := isSlackID(in); got != want {
			t.Errorf("isSlackID(%q) = %v, want %v", in, got, want)
		}
	}
}

func TestSlack_ClampLimit(t *testing.T) {
	cases := []struct {
		v, deflt, max int
		want          string
	}{
		{0, 20, 200, "20"},
		{50, 20, 200, "50"},
		{500, 20, 200, "200"},
		{-1, 20, 200, "20"},
	}
	for _, c := range cases {
		if got := clampLimit(c.v, c.deflt, c.max); got != c.want {
			t.Errorf("clampLimit(%d, %d, %d) = %q, want %q",
				c.v, c.deflt, c.max, got, c.want)
		}
	}
}

func TestSlack_ErrTranslation(t *testing.T) {
	cases := map[string]string{
		"":                  "unknown error",
		"missing_scope":     "missing the required scope",
		"channel_not_found": "channel not found",
		"user_not_found":    "user not found",
		"invalid_auth":      "invalid auth",
		"not_authed":        "invalid auth",
		"rate_limited":      "rate limited",
		"some_other_err":    "some_other_err", // pass-through unchanged
	}
	for code, wantSubstr := range cases {
		got := slackErr(code)
		if !strings.Contains(got, wantSubstr) {
			t.Errorf("slackErr(%q) = %q, want substring %q", code, got, wantSubstr)
		}
	}
}
