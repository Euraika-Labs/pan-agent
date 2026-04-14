package gateway

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/euraika-labs/pan-agent/internal/config"
)

// These tests pin down the HTTP response shapes the React UI depends on.
// Each one failed at some point during the 2026-04-14 audit and is now a
// regression fence. If the UI breaks later, the failing test points straight
// at the contract that moved.

// TestMemoryShape asserts the UI's {memory, user, stats} shape.
func TestMemoryShape(t *testing.T) {
	srv := setupTestServer(t)

	req := httptest.NewRequest(http.MethodGet, "/v1/memory", nil)
	w := httptest.NewRecorder()
	srv.handleMemoryGet(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}

	var got map[string]json.RawMessage
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	for _, key := range []string{"memory", "user", "stats"} {
		if _, ok := got[key]; !ok {
			t.Errorf("missing top-level key %q in response", key)
		}
	}

	// memory.* and user.* sections — check a couple of representative fields.
	for _, section := range []string{"memory", "user"} {
		var sec map[string]json.RawMessage
		if err := json.Unmarshal(got[section], &sec); err != nil {
			t.Fatalf("unmarshal %s: %v", section, err)
		}
		for _, field := range []string{"content", "exists", "charCount", "charLimit"} {
			if _, ok := sec[field]; !ok {
				t.Errorf("%s.%s missing", section, field)
			}
		}
	}

	// stats.totalSessions + totalMessages (previously absent, crashed UI).
	var stats map[string]int
	if err := json.Unmarshal(got["stats"], &stats); err != nil {
		t.Fatalf("stats: %v", err)
	}
	if _, ok := stats["totalSessions"]; !ok {
		t.Error("stats.totalSessions missing")
	}
	if _, ok := stats["totalMessages"]; !ok {
		t.Error("stats.totalMessages missing")
	}
}

// TestSkillsSplit confirms /v1/skills returns installed only and
// /v1/skills/bundled returns bundled only (never the combined list).
func TestSkillsSplit(t *testing.T) {
	srv := setupTestServer(t)

	// /v1/skills — installed only. On a fresh test profile no skills exist.
	w := httptest.NewRecorder()
	srv.handleSkillListInstalled(w, httptest.NewRequest(http.MethodGet, "/v1/skills", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("installed: status = %d", w.Code)
	}
	var installed []map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &installed); err != nil {
		t.Fatalf("installed: unmarshal: %v (body=%s)", err, w.Body.String())
	}
	// Empty profile: should be [] (never null).
	if installed == nil {
		t.Error("installed: expected [] not null")
	}

	// /v1/skills/bundled — bundled only.
	w = httptest.NewRecorder()
	srv.handleSkillListBundled(w, httptest.NewRequest(http.MethodGet, "/v1/skills/bundled", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("bundled: status = %d", w.Code)
	}
	var bundled []map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &bundled); err != nil {
		t.Fatalf("bundled: unmarshal: %v", err)
	}
	if bundled == nil {
		t.Error("bundled: expected [] not null")
	}
}

// TestConfigMasksSecrets confirms GET /v1/config never returns an unmasked
// API key in the env map. This is a security-critical regression fence.
func TestConfigMasksSecrets(t *testing.T) {
	srv := setupTestServer(t)

	// Seed a fake API key into the profile env. SetProfileEnvValue persists
	// to disk; setupTestServer uses a temp AgentHome so this is isolated.
	const fakeKey = "sk-supersecrettest1234567890ABC"
	if err := config.SetProfileEnvValue("", "REGOLO_API_KEY", fakeKey); err != nil {
		t.Fatalf("SetProfileEnvValue: %v", err)
	}

	w := httptest.NewRecorder()
	srv.handleConfigGet(w, httptest.NewRequest(http.MethodGet, "/v1/config", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d", w.Code)
	}

	body := w.Body.String()
	if strings.Contains(body, fakeKey) {
		t.Errorf("response leaked full API key:\n%s", body)
	}
	// Should still contain a masked form (the "sk-" prefix + the last 4).
	// Tolerant match: the masker produces "sk-***...90ABC" or similar.
	if !strings.Contains(body, "REGOLO_API_KEY") {
		t.Error("expected REGOLO_API_KEY key in response (masked value)")
	}
	if !strings.Contains(body, "***") {
		t.Error("expected masked marker *** in response")
	}
}

// TestMaskSecretHelper pins the masking function's behaviour so the
// "last-4-chars" contract doesn't silently drift.
func TestMaskSecretHelper(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"sk-1234567890abcd", "sk-***abcd"},
		{"ghp_abcdefghijkl", "ghp***ijkl"},
		{"short", "***"}, // < 8 chars ⇒ total hide
		{"", ""},         // empty passes through (the outer caller skips empty)
	}
	for _, c := range cases {
		if c.in == "" {
			continue // maskSecret is only called on non-empty
		}
		got := maskSecret(c.in)
		if got != c.want {
			t.Errorf("maskSecret(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// TestIsSecretEnvKey confirms the classifier catches common secret shapes,
// including the cloud-credential patterns the audit debate surfaced
// (ACCESS_KEY_ID, PRIVATE_KEY_*, customer-prefixed variants).
func TestIsSecretEnvKey(t *testing.T) {
	secret := []string{
		"REGOLO_API_KEY",
		"OPENAI_API_KEY",
		"ANTHROPIC_API_KEY",
		"GITHUB_TOKEN",
		"SLACK_BOT_TOKEN",
		"CLIENT_SECRET",
		"DB_PASSWORD",
		"API_KEY",           // bare
		"AWS_ACCESS_KEY_ID", // AWS pattern
		"AWS_SECRET_ACCESS_KEY",
		"GCP_PRIVATE_KEY",
		"CUSTOMER_API_KEY_PROD", // prefixed + suffixed
		"TAURI_SIGNING_KEY",
		"REDIS_AUTH_KEY",
		"CREDENTIAL_JSON",
		"STRIPE_PASSWD",
	}
	notSecret := []string{
		"AGENT_HOME",
		"PATH",
		"LANG",
		"USER",
		"LOCALAPPDATA",
		"NODE_ENV",
		"GOOS",
	}
	for _, k := range secret {
		if !isSecretEnvKey(k) {
			t.Errorf("isSecretEnvKey(%q) = false, want true", k)
		}
	}
	for _, k := range notSecret {
		if isSecretEnvKey(k) {
			t.Errorf("isSecretEnvKey(%q) = true, want false", k)
		}
	}
}

// TestOfficeEndpointsRespond confirms every /v1/office/* route returns 2xx
// (or a reasonable 4xx for GETs that need setup first). The UI's Office
// screen used to 404 on most of these.
func TestOfficeEndpointsRespond(t *testing.T) {
	srv := setupTestServer(t)
	mux := http.NewServeMux()
	srv.registerRoutes(mux)

	getRoutes := []string{
		"/v1/office/status",
		"/v1/office/logs",
		"/v1/office/config",
		"/v1/office/setup/progress",
	}
	for _, path := range getRoutes {
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))
		if w.Code >= 500 {
			t.Errorf("GET %s: status %d (5xx means handler crashed): %s", path, w.Code, w.Body.String())
		}
		if w.Code == http.StatusNotFound {
			t.Errorf("GET %s: 404 — route not registered", path)
		}
	}
}
