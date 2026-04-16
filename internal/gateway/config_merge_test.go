package gateway

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/euraika-labs/pan-agent/internal/config"
)

// TestConfigPutMergeEmptyBaseURL reproduces the 0.4.0 chat regression:
// a partial PUT /v1/config body that only carries a model name (with
// empty provider + empty baseUrl) must not clobber the existing baseUrl
// on disk. 0.4.0 did clobber it, which reset s.llmClient.BaseURL to ""
// and the next chat failed with
//
//	http do: Post "/chat/completions": unsupported protocol scheme ""
//
// The fix is in handleConfigPut: empty strings merge with the current
// on-disk config rather than overwriting.
func TestConfigPutMergeEmptyBaseURL(t *testing.T) {
	srv := setupTestServer(t)
	mux := http.NewServeMux()
	srv.registerRoutes(mux)

	put := func(body string) int {
		req := httptest.NewRequest("PUT", "/v1/config", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, req)
		return w.Code
	}

	// 1. Full PUT seeds the profile with a real baseUrl.
	if code := put(`{"model":{"provider":"regolo","model":"gpt-oss-120b","baseUrl":"https://api.regolo.ai/v1"}}`); code != 200 {
		t.Fatalf("seed PUT: got %d, want 200", code)
	}
	got := config.GetModelConfig("default")
	if got.BaseURL != "https://api.regolo.ai/v1" {
		t.Fatalf("after seed: BaseURL = %q, want %q", got.BaseURL, "https://api.regolo.ai/v1")
	}

	// 2. UI-shaped partial PUT: only model changes, provider + baseUrl empty.
	//    This is the exact payload the Settings screen's auto-save was
	//    emitting during state hydration.
	if code := put(`{"model":{"provider":"","model":"mistral-small-4-119b","baseUrl":""}}`); code != 200 {
		t.Fatalf("partial PUT: got %d, want 200", code)
	}

	// 3. baseUrl must be preserved; model must be updated.
	got = config.GetModelConfig("default")
	if got.BaseURL != "https://api.regolo.ai/v1" {
		t.Errorf("after partial PUT: BaseURL = %q, want preserved %q",
			got.BaseURL, "https://api.regolo.ai/v1")
	}
	if got.Model != "mistral-small-4-119b" {
		t.Errorf("after partial PUT: Model = %q, want updated %q",
			got.Model, "mistral-small-4-119b")
	}
	if got.Provider != "regolo" {
		t.Errorf("after partial PUT: Provider = %q, want preserved %q",
			got.Provider, "regolo")
	}
}
