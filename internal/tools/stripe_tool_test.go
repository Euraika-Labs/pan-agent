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

// Phase 13 WS#13.D — Stripe tool tests. Hermetic via httptest;
// covers parameter validation, env-var resolution, list/get happy
// paths, error paths, and the dashboard-URL injection.

// installStripeFake sets the stripeAPIBaseFn override + restores the
// production value via t.Cleanup. The fake server is what every
// stripeGET call ends up hitting during the test.
func installStripeFake(t *testing.T, handler http.HandlerFunc) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)

	prev := stripeAPIBaseFn
	stripeAPIBaseFn = func() string { return srv.URL }
	t.Cleanup(func() { stripeAPIBaseFn = prev })
	return srv
}

func TestStripe_NoAPIKey(t *testing.T) {
	t.Setenv("STRIPE_API_KEY", "")
	t.Setenv("STRIPE_TEST_API_KEY", "")
	out, _ := StripeTool{}.Execute(context.Background(),
		json.RawMessage(`{"action":"list"}`))
	if !strings.Contains(out.Error, "STRIPE_API_KEY") {
		t.Errorf("expected env-var error, got %+v", out)
	}
}

func TestStripe_InvalidJSON(t *testing.T) {
	t.Setenv("STRIPE_API_KEY", "sk_test_fake")
	out, _ := StripeTool{}.Execute(context.Background(),
		json.RawMessage(`{not-json`))
	if !strings.Contains(out.Error, "invalid parameters") {
		t.Errorf("expected parse error, got %+v", out)
	}
}

func TestStripe_UnknownAction(t *testing.T) {
	t.Setenv("STRIPE_API_KEY", "sk_test_fake")
	out, _ := StripeTool{}.Execute(context.Background(),
		json.RawMessage(`{"action":"refund"}`))
	if !strings.Contains(out.Error, "unknown action") {
		t.Errorf("expected unknown-action error, got %+v", out)
	}
}

func TestStripe_EmptyAction(t *testing.T) {
	t.Setenv("STRIPE_API_KEY", "sk_test_fake")
	out, _ := StripeTool{}.Execute(context.Background(),
		json.RawMessage(`{}`))
	if !strings.Contains(out.Error, "action required") {
		t.Errorf("expected action-required error, got %+v", out)
	}
}

func TestStripe_List_HappyPath(t *testing.T) {
	t.Setenv("STRIPE_API_KEY", "sk_test_fake")
	installStripeFake(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/charges" {
			t.Errorf("path = %q, want /charges", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer sk_test_fake" {
			t.Errorf("auth header = %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{
			"object": "list",
			"data": [
				{"id":"ch_111","amount":1000,"currency":"usd","status":"succeeded","created":1700000000},
				{"id":"ch_222","amount":2500,"currency":"eur","status":"succeeded","created":1700000100}
			],
			"has_more": false
		}`)
	})

	out, err := StripeTool{}.Execute(context.Background(),
		json.RawMessage(`{"action":"list","limit":2}`))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if out.Error != "" {
		t.Fatalf("unexpected Error: %s", out.Error)
	}

	var resp struct {
		Charges []struct {
			ID  string `json:"id"`
			URL string `json:"url"`
		} `json:"charges"`
		HasMore bool `json:"has_more"`
	}
	if err := json.Unmarshal([]byte(out.Output), &resp); err != nil {
		t.Fatalf("decode output: %v\noutput: %s", err, out.Output)
	}
	if len(resp.Charges) != 2 {
		t.Fatalf("len = %d, want 2", len(resp.Charges))
	}
	if !strings.Contains(resp.Charges[0].URL, "ch_111") {
		t.Errorf("URL[0] = %q, want one containing ch_111", resp.Charges[0].URL)
	}
	if !strings.Contains(resp.Charges[0].URL, "dashboard.stripe.com") {
		t.Errorf("URL[0] = %q, want stripe dashboard host", resp.Charges[0].URL)
	}
}

func TestStripe_List_LimitClamped(t *testing.T) {
	t.Setenv("STRIPE_API_KEY", "sk_test_fake")
	var capturedLimit string
	installStripeFake(t, func(w http.ResponseWriter, r *http.Request) {
		capturedLimit = r.URL.Query().Get("limit")
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"object":"list","data":[],"has_more":false}`)
	})

	tool := StripeTool{}
	if _, err := tool.Execute(context.Background(),
		json.RawMessage(`{"action":"list","limit":500}`)); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if capturedLimit != "100" {
		t.Errorf("limit clamp: server saw %q, want 100", capturedLimit)
	}
}

func TestStripe_List_DefaultLimit(t *testing.T) {
	t.Setenv("STRIPE_API_KEY", "sk_test_fake")
	var capturedLimit string
	installStripeFake(t, func(w http.ResponseWriter, r *http.Request) {
		capturedLimit = r.URL.Query().Get("limit")
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"object":"list","data":[],"has_more":false}`)
	})
	tool := StripeTool{}
	if _, err := tool.Execute(context.Background(),
		json.RawMessage(`{"action":"list"}`)); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if capturedLimit != "10" {
		t.Errorf("default limit: server saw %q, want 10", capturedLimit)
	}
}

func TestStripe_List_Pagination(t *testing.T) {
	t.Setenv("STRIPE_API_KEY", "sk_test_fake")
	var capturedCursor string
	installStripeFake(t, func(w http.ResponseWriter, r *http.Request) {
		capturedCursor = r.URL.Query().Get("starting_after")
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"object":"list","data":[],"has_more":false}`)
	})
	tool := StripeTool{}
	if _, err := tool.Execute(context.Background(),
		json.RawMessage(`{"action":"list","starting_after":"ch_zzz"}`)); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if capturedCursor != "ch_zzz" {
		t.Errorf("cursor: server saw %q, want ch_zzz", capturedCursor)
	}
}

func TestStripe_List_HTTPError(t *testing.T) {
	t.Setenv("STRIPE_API_KEY", "sk_test_fake")
	installStripeFake(t, func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"error":{"message":"out of credits"}}`, http.StatusPaymentRequired)
	})
	out, _ := StripeTool{}.Execute(context.Background(),
		json.RawMessage(`{"action":"list"}`))
	if !strings.Contains(out.Error, "402") {
		t.Errorf("expected status 402 in error, got %+v", out)
	}
}

func TestStripe_Get_HappyPath(t *testing.T) {
	t.Setenv("STRIPE_API_KEY", "sk_test_fake")
	installStripeFake(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/charges/ch_abc123" {
			t.Errorf("path = %q", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"id":"ch_abc123","amount":1000,"status":"succeeded"}`)
	})

	out, _ := StripeTool{}.Execute(context.Background(),
		json.RawMessage(`{"action":"get","id":"ch_abc123"}`))
	if out.Error != "" {
		t.Fatalf("Error: %s", out.Error)
	}
	if !strings.Contains(out.Output, "ch_abc123") {
		t.Errorf("output should contain charge id: %s", out.Output)
	}
	if !strings.Contains(out.Output, "dashboard.stripe.com") {
		t.Errorf("output should contain dashboard URL: %s", out.Output)
	}
}

func TestStripe_Get_MissingID(t *testing.T) {
	t.Setenv("STRIPE_API_KEY", "sk_test_fake")
	out, _ := StripeTool{}.Execute(context.Background(),
		json.RawMessage(`{"action":"get"}`))
	if !strings.Contains(out.Error, "id required") {
		t.Errorf("expected id-required error, got %+v", out)
	}
}

func TestStripe_Get_MalformedID(t *testing.T) {
	t.Setenv("STRIPE_API_KEY", "sk_test_fake")
	for _, badID := range []string{
		"../etc/passwd",
		"id with spaces",
		"id?query",
		"id#frag",
		"",
	} {
		out, _ := StripeTool{}.Execute(context.Background(),
			json.RawMessage(fmt.Sprintf(`{"action":"get","id":%q}`, badID)))
		if out.Error == "" {
			t.Errorf("id %q: expected error, got %+v", badID, out)
		}
	}
}

func TestStripe_TestModeRoutesToTestKey(t *testing.T) {
	t.Setenv("STRIPE_API_KEY", "sk_live_fake")
	t.Setenv("STRIPE_TEST_API_KEY", "sk_test_only")
	var capturedAuth string
	installStripeFake(t, func(w http.ResponseWriter, r *http.Request) {
		capturedAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"object":"list","data":[],"has_more":false}`)
	})

	tool := StripeTool{}
	if _, err := tool.Execute(context.Background(),
		json.RawMessage(`{"action":"list","test_mode":true}`)); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if capturedAuth != "Bearer sk_test_only" {
		t.Errorf("auth = %q, want Bearer sk_test_only", capturedAuth)
	}
}

func TestStripe_TestModeFallsBackToLiveKey(t *testing.T) {
	// Only live key set, but caller passes test_mode=true.
	t.Setenv("STRIPE_API_KEY", "sk_live_fake")
	t.Setenv("STRIPE_TEST_API_KEY", "")
	var capturedAuth string
	installStripeFake(t, func(w http.ResponseWriter, r *http.Request) {
		capturedAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"object":"list","data":[],"has_more":false}`)
	})

	out, _ := StripeTool{}.Execute(context.Background(),
		json.RawMessage(`{"action":"list","test_mode":true,"limit":1}`))
	if out.Error != "" {
		t.Fatalf("Error: %s", out.Error)
	}
	if capturedAuth != "Bearer sk_live_fake" {
		t.Errorf("auth = %q, want fallback to live key", capturedAuth)
	}
}

func TestStripe_TestModeURLPointsToTestDashboard(t *testing.T) {
	t.Setenv("STRIPE_API_KEY", "sk_live_fake")
	t.Setenv("STRIPE_TEST_API_KEY", "sk_test_only")
	installStripeFake(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"object":"list","data":[{"id":"ch_xyz","amount":100,"currency":"usd","status":"succeeded","created":1700000000}],"has_more":false}`)
	})

	out, _ := StripeTool{}.Execute(context.Background(),
		json.RawMessage(`{"action":"list","test_mode":true}`))
	if !strings.Contains(out.Output, "/test/payments/") {
		t.Errorf("test mode URL should include /test/payments/, got: %s", out.Output)
	}
}

func TestStripe_RegisteredInDefaultRegistry(t *testing.T) {
	tool, ok := Get("stripe")
	if !ok {
		t.Fatal("stripe tool not registered")
	}
	if tool.Name() != "stripe" {
		t.Errorf("Name = %q", tool.Name())
	}
	if !strings.Contains(tool.Description(), "Stripe") {
		t.Errorf("Description should mention Stripe: %s", tool.Description())
	}
}

func TestStripe_IsStripeID(t *testing.T) {
	cases := map[string]bool{
		"ch_abc123":              true,
		"pi_1AbC2D3eF4g5H6":      true,
		"":                       false,
		"id with space":          false,
		"id\nnewline":            false,
		"id?q=v":                 false,
		"../escape":              false,
		strings.Repeat("a", 256): false,
		strings.Repeat("a", 255): true,
		"ch_test_-with-hyphen":   true,
	}
	for in, want := range cases {
		if got := isStripeID(in); got != want {
			t.Errorf("isStripeID(%q) = %v, want %v", in, got, want)
		}
	}
}

func TestStripe_TruncateBody(t *testing.T) {
	short := "hello"
	if got := truncateStripeBody(short); got != short {
		t.Errorf("short truncated: %q", got)
	}
	long := strings.Repeat("x", 1000)
	got := truncateStripeBody(long)
	if !strings.HasSuffix(got, "(truncated)") {
		t.Errorf("long not truncated: %s", got[len(got)-30:])
	}
}
