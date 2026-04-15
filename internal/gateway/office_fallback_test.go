package gateway

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/euraika-labs/pan-agent/internal/config"
)

// TestFallbackDetected_EndToEnd exercises POST /v1/office/fallback-detected
// along the same path the desktop/src/main.tsx WebGL2 probe takes when a
// WebView2 instance reports no usable WebGL2 context. This replaces the
// code-path half of docs/runbook.md §8 — the remaining pixel-level UX
// checks (splash copy, FallbackBanner buttons, system-browser open) must
// still be done manually on a GPU-blocked machine, but every line of
// production code this turn-key manual test would exercise is now covered
// here.
func TestFallbackDetected_EndToEnd(t *testing.T) {
	srv := setupTestServer(t)
	mux := http.NewServeMux()
	srv.registerRoutes(mux)

	before := time.Now().UTC()

	req := httptest.NewRequest("POST", "/v1/office/fallback-detected", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Fatalf("fallback-detected: got %d, want 200", w.Code)
	}

	var body fallbackResponse
	if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if body.FallbackURL != "http://localhost:8642/office/" {
		t.Errorf("fallbackUrl = %q, want http://localhost:8642/office/", body.FallbackURL)
	}

	// Until date must parse as RFC3339 and land inside the 7-day window.
	// RFC3339 loses sub-second precision, so truncate the lower bound
	// to the second before comparing.
	until, err := time.Parse(time.RFC3339, body.UntilDate)
	if err != nil {
		t.Fatalf("untilDate not RFC3339: %q (%v)", body.UntilDate, err)
	}
	minUntil := before.Add(webview2FallbackWindow).Truncate(time.Second)
	maxUntil := time.Now().UTC().Add(webview2FallbackWindow).Add(time.Second)
	if until.Before(minUntil) || until.After(maxUntil) {
		t.Errorf("untilDate = %v, want in [%v, %v]", until, minUntil, maxUntil)
	}

	// Verify persistence. The /v1/config read the FallbackBanner performs
	// to self-gate must see the same value.
	stored, err := config.GetProfileValue(srv.profile, "office.browser_fallback_until")
	if err != nil {
		t.Fatalf("read back browser_fallback_until: %v", err)
	}
	if stored != body.UntilDate {
		t.Errorf("config.yaml = %q, response = %q", stored, body.UntilDate)
	}
}

// TestFallbackDetected_Reentrant verifies that firing the probe twice does
// NOT stack windows — each invocation replaces the previous until date
// with a fresh now+7d, which is the intent of EnsureProfileValue (upsert).
// A naive Append would have produced two YAML entries and silently broken
// the probe skip on next launch.
func TestFallbackDetected_Reentrant(t *testing.T) {
	srv := setupTestServer(t)
	mux := http.NewServeMux()
	srv.registerRoutes(mux)

	post := func() fallbackResponse {
		req := httptest.NewRequest("POST", "/v1/office/fallback-detected", nil)
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, req)
		if w.Code != 200 {
			t.Fatalf("fallback: got %d, want 200", w.Code)
		}
		var b fallbackResponse
		if err := json.NewDecoder(w.Body).Decode(&b); err != nil {
			t.Fatalf("decode: %v", err)
		}
		return b
	}

	first := post()
	time.Sleep(10 * time.Millisecond) // ensure second call's now() is strictly later
	second := post()

	t1, _ := time.Parse(time.RFC3339, first.UntilDate)
	t2, _ := time.Parse(time.RFC3339, second.UntilDate)

	// RFC3339 has second precision so these may compare equal on fast
	// machines. !After is "equal or later" — the real constraint is that
	// the second call did not reset the window to a date in the past.
	if t2.Before(t1) {
		t.Errorf("second until %v is before first %v", t2, t1)
	}

	stored, err := config.GetProfileValue(srv.profile, "office.browser_fallback_until")
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if stored != second.UntilDate {
		t.Errorf("after second call, config = %q, want %q", stored, second.UntilDate)
	}
}

// TestFallbackDetected_WindowConstant pins the documented 7-day value so a
// future refactor that changes webview2FallbackWindow gets caught by a
// test failure BEFORE shipping. docs/runbook.md §8 and
// docs/migration-guide.md §4 both cite "7 days" — keep them in sync.
func TestFallbackDetected_WindowConstant(t *testing.T) {
	const want = 7 * 24 * time.Hour
	if webview2FallbackWindow != want {
		t.Errorf("webview2FallbackWindow = %v, want %v (docs/runbook.md §8 cites 7 days)",
			webview2FallbackWindow, want)
	}
}
