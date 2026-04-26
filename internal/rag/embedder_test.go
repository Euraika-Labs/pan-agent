package rag

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// Phase 13 WS#13.B — embedder client tests. Uses httptest fakes so
// the suite stays hermetic; no real provider call ever fires.

// fakeEmbedServer returns an httptest.Server that responds to
// /embeddings with the supplied vectors (one per input text).
// The handler closure can be replaced via the returned hook to
// inject error cases.
type fakeEmbedServer struct {
	*httptest.Server
	handler func(w http.ResponseWriter, r *http.Request)
}

func newFakeEmbedServer(t *testing.T) *fakeEmbedServer {
	t.Helper()
	s := &fakeEmbedServer{}
	s.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.handler != nil {
			s.handler(w, r)
			return
		}
		http.NotFound(w, r)
	}))
	t.Cleanup(s.Close)
	return s
}

// happyHandler echoes back len(input) vectors of dim D, each filled
// with the input's index as a float32 (so callers can verify ordering).
func happyHandler(dim int) func(w http.ResponseWriter, r *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		var req embedRequest
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &req)
		var out embedResponse
		out.Model = req.Model
		for i := range req.Input {
			vec := make([]float32, dim)
			for j := range vec {
				vec[j] = float32(i)
			}
			out.Data = append(out.Data, struct {
				Embedding []float32 `json:"embedding"`
				Index     int       `json:"index"`
			}{Embedding: vec, Index: i})
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(out)
	}
}

func TestNewHTTPEmbedder_Validation(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		cfg  HTTPEmbedderConfig
	}{
		{"no_baseurl", HTTPEmbedderConfig{Model: "m"}},
		{"no_model", HTTPEmbedderConfig{BaseURL: "http://x"}},
		{"negative_dim", HTTPEmbedderConfig{BaseURL: "http://x", Model: "m", Dim: -1}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := NewHTTPEmbedder(tc.cfg); err == nil {
				t.Error("expected validation error")
			}
		})
	}
}

func TestHTTPEmbedder_Embed_HappyPath(t *testing.T) {
	t.Parallel()
	srv := newFakeEmbedServer(t)
	srv.handler = happyHandler(8)
	em, err := NewHTTPEmbedder(HTTPEmbedderConfig{
		BaseURL: srv.URL, Model: "m-1", Dim: 8, APIKey: "tok",
	})
	if err != nil {
		t.Fatalf("NewHTTPEmbedder: %v", err)
	}

	v, err := em.Embed(context.Background(), "hello")
	if err != nil {
		t.Fatalf("Embed: %v", err)
	}
	if len(v) != 8 {
		t.Errorf("len = %d, want 8", len(v))
	}
	if v[0] != 0 {
		t.Errorf("v[0] = %v, want 0 (input index)", v[0])
	}
	if em.Model() != "m-1" || em.Dim() != 8 {
		t.Errorf("Model/Dim getters wrong: %q/%d", em.Model(), em.Dim())
	}
}

func TestHTTPEmbedder_EmbedBatch_OrderingAndAuth(t *testing.T) {
	t.Parallel()
	srv := newFakeEmbedServer(t)
	gotAuth := ""
	srv.handler = func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		// Reply with vectors in REVERSE order (provider misbehaving)
		// so the test pins our re-ordering by Index.
		var req embedRequest
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &req)
		var out embedResponse
		for i := len(req.Input) - 1; i >= 0; i-- {
			vec := make([]float32, 4)
			vec[0] = float32(i)
			out.Data = append(out.Data, struct {
				Embedding []float32 `json:"embedding"`
				Index     int       `json:"index"`
			}{Embedding: vec, Index: i})
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(out)
	}

	em, _ := NewHTTPEmbedder(HTTPEmbedderConfig{
		BaseURL: srv.URL, Model: "m", Dim: 4, APIKey: "secret-key",
	})
	vs, err := em.EmbedBatch(context.Background(), []string{"a", "b", "c"})
	if err != nil {
		t.Fatalf("EmbedBatch: %v", err)
	}
	if len(vs) != 3 {
		t.Fatalf("len = %d, want 3", len(vs))
	}
	for i, v := range vs {
		if v[0] != float32(i) {
			t.Errorf("vs[%d][0] = %v, want %v — re-ordering broken",
				i, v[0], float32(i))
		}
	}
	if gotAuth != "Bearer secret-key" {
		t.Errorf("Authorization = %q, want Bearer secret-key", gotAuth)
	}
}

func TestHTTPEmbedder_NoAPIKey_OmitsAuth(t *testing.T) {
	t.Parallel()
	srv := newFakeEmbedServer(t)
	gotAuth := "<unset>"
	srv.handler = func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		happyHandler(2)(w, r)
	}

	em, _ := NewHTTPEmbedder(HTTPEmbedderConfig{
		BaseURL: srv.URL, Model: "m", Dim: 2,
	})
	if _, err := em.Embed(context.Background(), "x"); err != nil {
		t.Fatalf("Embed: %v", err)
	}
	if gotAuth != "" {
		t.Errorf("expected empty Authorization for local provider, got %q", gotAuth)
	}
}

func TestHTTPEmbedder_DimMismatch(t *testing.T) {
	t.Parallel()
	srv := newFakeEmbedServer(t)
	// Server returns 8-dim, embedder configured for 4-dim.
	srv.handler = happyHandler(8)

	em, _ := NewHTTPEmbedder(HTTPEmbedderConfig{
		BaseURL: srv.URL, Model: "m", Dim: 4,
	})
	_, err := em.Embed(context.Background(), "x")
	if !errors.Is(err, ErrEmbedFailed) {
		t.Errorf("err = %v, want ErrEmbedFailed", err)
	}
	if !strings.Contains(err.Error(), "dim") {
		t.Errorf("err message should mention dim, got %v", err)
	}
}

func TestHTTPEmbedder_DimZero_AcceptsAny(t *testing.T) {
	t.Parallel()
	// Dim=0 means skip validation — useful for tests + first-time
	// provisioning where the caller doesn't know the model dim yet.
	srv := newFakeEmbedServer(t)
	srv.handler = happyHandler(384)

	em, _ := NewHTTPEmbedder(HTTPEmbedderConfig{
		BaseURL: srv.URL, Model: "m", Dim: 0,
	})
	v, err := em.Embed(context.Background(), "x")
	if err != nil {
		t.Fatalf("Embed: %v", err)
	}
	if len(v) != 384 {
		t.Errorf("len = %d, want 384 (server's dim)", len(v))
	}
}

func TestHTTPEmbedder_HTTPError(t *testing.T) {
	t.Parallel()
	srv := newFakeEmbedServer(t)
	srv.handler = func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"error":{"message":"out of credits"}}`, http.StatusPaymentRequired)
	}
	em, _ := NewHTTPEmbedder(HTTPEmbedderConfig{
		BaseURL: srv.URL, Model: "m", Dim: 0,
	})
	_, err := em.Embed(context.Background(), "x")
	if !errors.Is(err, ErrEmbedFailed) {
		t.Errorf("err = %v, want wrapped ErrEmbedFailed", err)
	}
	if !strings.Contains(err.Error(), "402") {
		t.Errorf("err should include status 402, got %v", err)
	}
}

func TestHTTPEmbedder_StructuredError(t *testing.T) {
	t.Parallel()
	srv := newFakeEmbedServer(t)
	srv.handler = func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"error":{"message":"model not found","type":"invalid_request_error"}}`))
	}
	em, _ := NewHTTPEmbedder(HTTPEmbedderConfig{
		BaseURL: srv.URL, Model: "m", Dim: 0,
	})
	_, err := em.Embed(context.Background(), "x")
	if !errors.Is(err, ErrEmbedFailed) {
		t.Errorf("err = %v, want ErrEmbedFailed", err)
	}
	if !strings.Contains(err.Error(), "model not found") {
		t.Errorf("err should include provider message, got %v", err)
	}
}

func TestHTTPEmbedder_BatchSizeMismatch(t *testing.T) {
	t.Parallel()
	srv := newFakeEmbedServer(t)
	// Server returns FEWER vectors than requested.
	srv.handler = func(w http.ResponseWriter, r *http.Request) {
		var out embedResponse
		out.Data = append(out.Data, struct {
			Embedding []float32 `json:"embedding"`
			Index     int       `json:"index"`
		}{Embedding: []float32{1, 2, 3}, Index: 0})
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(out)
	}
	em, _ := NewHTTPEmbedder(HTTPEmbedderConfig{
		BaseURL: srv.URL, Model: "m", Dim: 0,
	})
	_, err := em.EmbedBatch(context.Background(), []string{"a", "b"})
	if !errors.Is(err, ErrEmbedFailed) {
		t.Errorf("err = %v, want ErrEmbedFailed", err)
	}
}

func TestHTTPEmbedder_DuplicateIndex(t *testing.T) {
	t.Parallel()
	srv := newFakeEmbedServer(t)
	srv.handler = func(w http.ResponseWriter, r *http.Request) {
		var out embedResponse
		// Two replies both at index 0 — provider bug we must reject.
		for i := 0; i < 2; i++ {
			out.Data = append(out.Data, struct {
				Embedding []float32 `json:"embedding"`
				Index     int       `json:"index"`
			}{Embedding: []float32{0, 0}, Index: 0})
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(out)
	}
	em, _ := NewHTTPEmbedder(HTTPEmbedderConfig{
		BaseURL: srv.URL, Model: "m", Dim: 0,
	})
	_, err := em.EmbedBatch(context.Background(), []string{"a", "b"})
	if !errors.Is(err, ErrEmbedFailed) {
		t.Errorf("err = %v, want ErrEmbedFailed (dup index)", err)
	}
}

func TestHTTPEmbedder_OutOfRangeIndex(t *testing.T) {
	t.Parallel()
	srv := newFakeEmbedServer(t)
	srv.handler = func(w http.ResponseWriter, r *http.Request) {
		out := embedResponse{Data: []struct {
			Embedding []float32 `json:"embedding"`
			Index     int       `json:"index"`
		}{{Embedding: []float32{0}, Index: 99}}}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(out)
	}
	em, _ := NewHTTPEmbedder(HTTPEmbedderConfig{
		BaseURL: srv.URL, Model: "m", Dim: 0,
	})
	_, err := em.EmbedBatch(context.Background(), []string{"a"})
	if !errors.Is(err, ErrEmbedFailed) {
		t.Errorf("err = %v, want ErrEmbedFailed", err)
	}
}

func TestHTTPEmbedder_EmptyBatch(t *testing.T) {
	t.Parallel()
	em, _ := NewHTTPEmbedder(HTTPEmbedderConfig{
		BaseURL: "http://x", Model: "m", Dim: 0,
	})
	_, err := em.EmbedBatch(context.Background(), nil)
	if !errors.Is(err, ErrEmbedFailed) {
		t.Errorf("err = %v, want ErrEmbedFailed (empty batch)", err)
	}
}

func TestHTTPEmbedder_ContextCancel(t *testing.T) {
	t.Parallel()
	srv := newFakeEmbedServer(t)
	// Slow handler that respects context cancellation.
	srv.handler = func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
			return
		case <-time.After(2 * time.Second):
			happyHandler(2)(w, r)
		}
	}

	em, _ := NewHTTPEmbedder(HTTPEmbedderConfig{
		BaseURL: srv.URL, Model: "m", Dim: 2,
	})
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	_, err := em.Embed(ctx, "x")
	if err == nil {
		t.Error("expected context-cancellation error")
	}
}

func TestHTTPEmbedder_TrailingSlashInBaseURL(t *testing.T) {
	t.Parallel()
	srv := newFakeEmbedServer(t)
	srv.handler = happyHandler(2)
	em, err := NewHTTPEmbedder(HTTPEmbedderConfig{
		BaseURL: srv.URL + "/", // trailing slash
		Model:   "m", Dim: 2,
	})
	if err != nil {
		t.Fatalf("NewHTTPEmbedder: %v", err)
	}
	if _, err := em.Embed(context.Background(), "x"); err != nil {
		t.Errorf("trailing slash should be stripped, got: %v", err)
	}
}
