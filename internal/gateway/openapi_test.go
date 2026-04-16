package gateway

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestOpenAPIEndpoint verifies that GET /v1/openapi.yaml returns the
// embedded OpenAPI document with the right content type. Guards against
// regressions where the embed directive or route wiring breaks silently.
func TestOpenAPIEndpoint(t *testing.T) {
	srv := setupTestServer(t)
	mux := http.NewServeMux()
	srv.registerRoutes(mux)

	req := httptest.NewRequest("GET", "/v1/openapi.yaml", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Fatalf("openapi: got %d, want 200", w.Code)
	}
	if ct := w.Header().Get("Content-Type"); !strings.Contains(ct, "yaml") {
		t.Errorf("openapi: Content-Type = %q, want contains \"yaml\"", ct)
	}
	body := w.Body.String()
	if !strings.Contains(body, "openapi: 3.") {
		t.Errorf("openapi: body missing 'openapi:' header; first 200 bytes: %.200s", body)
	}
	if !strings.Contains(body, "APIError") {
		t.Errorf("openapi: body missing APIError schema (unified error envelope)")
	}
}
