package tools

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// Phase 13 WS#13.D — Jira tool tests. Hermetic via httptest.
//
// The tool reads JIRA_HOST + (JIRA_EMAIL + JIRA_API_TOKEN) or
// JIRA_BEARER from env. Tests inject a fake host via jiraHostFn /
// jiraBaseURLFn so the actual TCP target is the httptest server.

func installJiraFake(t *testing.T, h http.HandlerFunc) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	prevHost := jiraHostFn
	prevBase := jiraBaseURLFn
	jiraHostFn = func() string { return "test.atlassian.net" }
	jiraBaseURLFn = func() string { return srv.URL }
	t.Cleanup(func() {
		jiraHostFn = prevHost
		jiraBaseURLFn = prevBase
	})
	return srv
}

// ---------------------------------------------------------------------------
// Auth + dispatch
// ---------------------------------------------------------------------------

func TestJira_NoHost(t *testing.T) {
	prev := jiraHostFn
	jiraHostFn = func() string { return "" }
	t.Cleanup(func() { jiraHostFn = prev })
	t.Setenv("JIRA_EMAIL", "a@b.c")
	t.Setenv("JIRA_API_TOKEN", "tok")

	tool := JiraTool{}
	out, _ := tool.Execute(context.Background(),
		json.RawMessage(`{"action":"myself"}`))
	if !strings.Contains(out.Error, "JIRA_HOST") {
		t.Errorf("expected host error, got %+v", out)
	}
}

func TestJira_NoAuth(t *testing.T) {
	installJiraFake(t, nil)
	t.Setenv("JIRA_EMAIL", "")
	t.Setenv("JIRA_API_TOKEN", "")
	t.Setenv("JIRA_BEARER", "")

	tool := JiraTool{}
	out, _ := tool.Execute(context.Background(),
		json.RawMessage(`{"action":"myself"}`))
	if !strings.Contains(out.Error, "auth missing") {
		t.Errorf("expected auth error, got %+v", out)
	}
}

func TestJira_InvalidJSON(t *testing.T) {
	installJiraFake(t, nil)
	t.Setenv("JIRA_EMAIL", "a@b.c")
	t.Setenv("JIRA_API_TOKEN", "tok")
	tool := JiraTool{}
	out, _ := tool.Execute(context.Background(), json.RawMessage(`{not-json`))
	if !strings.Contains(out.Error, "invalid parameters") {
		t.Errorf("expected parse error, got %+v", out)
	}
}

func TestJira_UnknownAction(t *testing.T) {
	installJiraFake(t, nil)
	t.Setenv("JIRA_EMAIL", "a@b.c")
	t.Setenv("JIRA_API_TOKEN", "tok")
	tool := JiraTool{}
	out, _ := tool.Execute(context.Background(),
		json.RawMessage(`{"action":"create_issue"}`))
	if !strings.Contains(out.Error, "unknown action") {
		t.Errorf("expected unknown-action error, got %+v", out)
	}
}

func TestJira_EmptyAction(t *testing.T) {
	installJiraFake(t, nil)
	t.Setenv("JIRA_EMAIL", "a@b.c")
	t.Setenv("JIRA_API_TOKEN", "tok")
	tool := JiraTool{}
	out, _ := tool.Execute(context.Background(), json.RawMessage(`{}`))
	if !strings.Contains(out.Error, "action required") {
		t.Errorf("expected action-required error, got %+v", out)
	}
}

// ---------------------------------------------------------------------------
// Auth shapes
// ---------------------------------------------------------------------------

func TestJira_BearerAuth(t *testing.T) {
	var captured string
	installJiraFake(t, func(w http.ResponseWriter, r *http.Request) {
		captured = r.Header.Get("Authorization")
		fmt.Fprint(w, `{"accountId":"u-1"}`)
	})
	t.Setenv("JIRA_EMAIL", "")
	t.Setenv("JIRA_API_TOKEN", "")
	t.Setenv("JIRA_BEARER", "self-hosted-pat")

	tool := JiraTool{}
	if _, err := tool.Execute(context.Background(),
		json.RawMessage(`{"action":"myself"}`)); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if captured != "Bearer self-hosted-pat" {
		t.Errorf("auth = %q, want Bearer self-hosted-pat", captured)
	}
}

func TestJira_BasicAuth(t *testing.T) {
	var captured string
	installJiraFake(t, func(w http.ResponseWriter, r *http.Request) {
		captured = r.Header.Get("Authorization")
		fmt.Fprint(w, `{"accountId":"u-1"}`)
	})
	t.Setenv("JIRA_BEARER", "")
	t.Setenv("JIRA_EMAIL", "alice@example.com")
	t.Setenv("JIRA_API_TOKEN", "secret-token")

	tool := JiraTool{}
	if _, err := tool.Execute(context.Background(),
		json.RawMessage(`{"action":"myself"}`)); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	want := "Basic " + base64.StdEncoding.EncodeToString([]byte("alice@example.com:secret-token"))
	if captured != want {
		t.Errorf("auth = %q, want %q", captured, want)
	}
}

func TestJira_BearerPreferredOverBasic(t *testing.T) {
	var captured string
	installJiraFake(t, func(w http.ResponseWriter, r *http.Request) {
		captured = r.Header.Get("Authorization")
		fmt.Fprint(w, `{"accountId":"u-1"}`)
	})
	t.Setenv("JIRA_BEARER", "bearer-wins")
	t.Setenv("JIRA_EMAIL", "alice@example.com")
	t.Setenv("JIRA_API_TOKEN", "tok")

	tool := JiraTool{}
	if _, err := tool.Execute(context.Background(),
		json.RawMessage(`{"action":"myself"}`)); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.HasPrefix(captured, "Bearer ") {
		t.Errorf("expected Bearer auth when JIRA_BEARER set, got %q", captured)
	}
}

// ---------------------------------------------------------------------------
// search
// ---------------------------------------------------------------------------

func TestJira_Search_HappyPath(t *testing.T) {
	t.Setenv("JIRA_EMAIL", "a@b.c")
	t.Setenv("JIRA_API_TOKEN", "tok")
	t.Setenv("JIRA_BEARER", "")
	installJiraFake(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/search" {
			t.Errorf("path = %q", r.URL.Path)
		}
		if got := r.URL.Query().Get("jql"); got != "project = PAN" {
			t.Errorf("jql = %q", got)
		}
		if got := r.URL.Query().Get("maxResults"); got != "25" {
			t.Errorf("maxResults = %q, want 25 (default)", got)
		}
		fmt.Fprint(w, `{
			"issues":[
				{"key":"PAN-42","fields":{"summary":"Fix bug"}},
				{"key":"PAN-43","fields":{"summary":"Add feature"}}
			],
			"total":2,"maxResults":25
		}`)
	})
	tool := JiraTool{}
	out, _ := tool.Execute(context.Background(),
		json.RawMessage(`{"action":"search","jql":"project = PAN"}`))
	if out.Error != "" {
		t.Fatalf("Error: %s", out.Error)
	}

	var resp struct {
		Issues []struct {
			Key, URL string
		}
		Total int
	}
	if err := json.Unmarshal([]byte(out.Output), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Issues) != 2 {
		t.Fatalf("len = %d, want 2", len(resp.Issues))
	}
	if resp.Total != 2 {
		t.Errorf("total = %d, want 2", resp.Total)
	}
	if !strings.Contains(resp.Issues[0].URL, "PAN-42") {
		t.Errorf("URL[0] should contain PAN-42: %q", resp.Issues[0].URL)
	}
	if !strings.Contains(resp.Issues[0].URL, "atlassian.net") {
		t.Errorf("URL[0] should reference atlassian.net: %q", resp.Issues[0].URL)
	}
}

func TestJira_Search_LimitClamp(t *testing.T) {
	t.Setenv("JIRA_EMAIL", "a@b.c")
	t.Setenv("JIRA_API_TOKEN", "tok")
	t.Setenv("JIRA_BEARER", "")
	var captured string
	installJiraFake(t, func(w http.ResponseWriter, r *http.Request) {
		captured = r.URL.Query().Get("maxResults")
		fmt.Fprint(w, `{"issues":[],"total":0}`)
	})
	tool := JiraTool{}
	if _, err := tool.Execute(context.Background(),
		json.RawMessage(`{"action":"search","jql":"x","limit":500}`)); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if captured != "100" {
		t.Errorf("maxResults clamp: got %q, want 100", captured)
	}
}

func TestJira_Search_CustomFields(t *testing.T) {
	t.Setenv("JIRA_EMAIL", "a@b.c")
	t.Setenv("JIRA_API_TOKEN", "tok")
	t.Setenv("JIRA_BEARER", "")
	var captured string
	installJiraFake(t, func(w http.ResponseWriter, r *http.Request) {
		captured = r.URL.Query().Get("fields")
		fmt.Fprint(w, `{"issues":[],"total":0}`)
	})
	tool := JiraTool{}
	if _, err := tool.Execute(context.Background(),
		json.RawMessage(`{"action":"search","jql":"x","fields":"summary,labels"}`)); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if captured != "summary,labels" {
		t.Errorf("fields = %q", captured)
	}
}

func TestJira_Search_DefaultFields(t *testing.T) {
	t.Setenv("JIRA_EMAIL", "a@b.c")
	t.Setenv("JIRA_API_TOKEN", "tok")
	t.Setenv("JIRA_BEARER", "")
	var captured string
	installJiraFake(t, func(w http.ResponseWriter, r *http.Request) {
		captured = r.URL.Query().Get("fields")
		fmt.Fprint(w, `{"issues":[],"total":0}`)
	})
	tool := JiraTool{}
	if _, err := tool.Execute(context.Background(),
		json.RawMessage(`{"action":"search","jql":"x"}`)); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(captured, "summary") {
		t.Errorf("default fields should include 'summary', got %q", captured)
	}
}

func TestJira_Search_MissingJQL(t *testing.T) {
	installJiraFake(t, nil)
	t.Setenv("JIRA_EMAIL", "a@b.c")
	t.Setenv("JIRA_API_TOKEN", "tok")
	t.Setenv("JIRA_BEARER", "")
	tool := JiraTool{}
	out, _ := tool.Execute(context.Background(),
		json.RawMessage(`{"action":"search"}`))
	if !strings.Contains(out.Error, "jql required") {
		t.Errorf("expected jql-required error, got %+v", out)
	}
}

func TestJira_Search_HTTPError(t *testing.T) {
	t.Setenv("JIRA_EMAIL", "a@b.c")
	t.Setenv("JIRA_API_TOKEN", "tok")
	t.Setenv("JIRA_BEARER", "")
	installJiraFake(t, func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"errorMessages":["JQL parse error"]}`, http.StatusBadRequest)
	})
	tool := JiraTool{}
	out, _ := tool.Execute(context.Background(),
		json.RawMessage(`{"action":"search","jql":"BAD JQL"}`))
	if !strings.Contains(out.Error, "400") {
		t.Errorf("expected 400 in error, got %+v", out)
	}
}

// ---------------------------------------------------------------------------
// get_issue
// ---------------------------------------------------------------------------

func TestJira_GetIssue_HappyPath(t *testing.T) {
	t.Setenv("JIRA_EMAIL", "a@b.c")
	t.Setenv("JIRA_API_TOKEN", "tok")
	t.Setenv("JIRA_BEARER", "")
	installJiraFake(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/issue/PAN-42" {
			t.Errorf("path = %q", r.URL.Path)
		}
		fmt.Fprint(w, `{"key":"PAN-42","fields":{"summary":"Fix bug"}}`)
	})
	tool := JiraTool{}
	out, _ := tool.Execute(context.Background(),
		json.RawMessage(`{"action":"get_issue","issue_key":"PAN-42"}`))
	if out.Error != "" {
		t.Fatalf("Error: %s", out.Error)
	}
	if !strings.Contains(out.Output, "PAN-42") {
		t.Errorf("output should contain key: %s", out.Output)
	}
	if !strings.Contains(out.Output, "atlassian.net") {
		t.Errorf("output should include URL: %s", out.Output)
	}
}

func TestJira_GetIssue_BadKey(t *testing.T) {
	t.Setenv("JIRA_EMAIL", "a@b.c")
	t.Setenv("JIRA_API_TOKEN", "tok")
	t.Setenv("JIRA_BEARER", "")
	tool := JiraTool{}
	for _, bad := range []string{
		"",
		"lowercase-1",
		"PAN_42", // underscore
		"PAN 42", // space
		"P-1",    // project too short (1 letter)
		"PAN-",   // missing number
		"-42",    // missing project
		"PAN-0",  // leading-zero rule
		"../etc/passwd",
	} {
		out, _ := tool.Execute(context.Background(),
			json.RawMessage(fmt.Sprintf(`{"action":"get_issue","issue_key":%q}`, bad)))
		if out.Error == "" {
			t.Errorf("key %q: expected error", bad)
		}
	}
}

func TestJira_GetIssue_404(t *testing.T) {
	t.Setenv("JIRA_EMAIL", "a@b.c")
	t.Setenv("JIRA_API_TOKEN", "tok")
	t.Setenv("JIRA_BEARER", "")
	installJiraFake(t, func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"errorMessages":["Issue does not exist"]}`, http.StatusNotFound)
	})
	tool := JiraTool{}
	out, _ := tool.Execute(context.Background(),
		json.RawMessage(`{"action":"get_issue","issue_key":"PAN-9999"}`))
	if !strings.Contains(out.Error, "404") {
		t.Errorf("expected 404 error, got %+v", out)
	}
}

// ---------------------------------------------------------------------------
// myself
// ---------------------------------------------------------------------------

func TestJira_Myself_HappyPath(t *testing.T) {
	t.Setenv("JIRA_EMAIL", "a@b.c")
	t.Setenv("JIRA_API_TOKEN", "tok")
	t.Setenv("JIRA_BEARER", "")
	installJiraFake(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/myself" {
			t.Errorf("path = %q", r.URL.Path)
		}
		fmt.Fprint(w, `{"accountId":"u-1","displayName":"Alice","emailAddress":"a@b.c"}`)
	})
	tool := JiraTool{}
	out, _ := tool.Execute(context.Background(),
		json.RawMessage(`{"action":"myself"}`))
	if out.Error != "" {
		t.Fatalf("Error: %s", out.Error)
	}
	if !strings.Contains(out.Output, "u-1") {
		t.Errorf("output should contain accountId: %s", out.Output)
	}
}

func TestJira_Myself_401(t *testing.T) {
	t.Setenv("JIRA_EMAIL", "a@b.c")
	t.Setenv("JIRA_API_TOKEN", "wrong")
	t.Setenv("JIRA_BEARER", "")
	installJiraFake(t, func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"errorMessages":["unauthorized"]}`, http.StatusUnauthorized)
	})
	tool := JiraTool{}
	out, _ := tool.Execute(context.Background(),
		json.RawMessage(`{"action":"myself"}`))
	if !strings.Contains(out.Error, "401") {
		t.Errorf("expected 401, got %+v", out)
	}
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func TestJira_RegisteredInRegistry(t *testing.T) {
	tool, ok := Get("jira")
	if !ok {
		t.Fatal("jira tool not registered")
	}
	if tool.Name() != "jira" {
		t.Errorf("Name = %q", tool.Name())
	}
}

func TestJira_IsIssueKey(t *testing.T) {
	cases := map[string]bool{
		"PAN-42":           true,
		"AB-1":             true,
		"PROJ123-9":        true,
		"":                 false,
		"pan-42":           false, // lowercase
		"P-1":              false, // 1-letter project
		"PROJECT-":         false, // empty number
		"-1":               false,
		"PAN_42":           false,
		"PAN 42":           false,
		"PAN-0":            false, // leading zero rule (must start [1-9])
		"PROJECTABCDEFG-1": false, // project >10 chars
	}
	for in, want := range cases {
		if got := isJiraIssueKey(in); got != want {
			t.Errorf("isJiraIssueKey(%q) = %v, want %v", in, got, want)
		}
	}
}

func TestJira_TruncateBody(t *testing.T) {
	if got := truncateJiraBody("short"); got != "short" {
		t.Errorf("short: %q", got)
	}
	long := strings.Repeat("x", 1000)
	got := truncateJiraBody(long)
	if !strings.HasSuffix(got, "(truncated)") {
		t.Errorf("not truncated: %s", got[len(got)-30:])
	}
}
