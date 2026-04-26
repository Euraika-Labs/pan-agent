package gateway

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/euraika-labs/pan-agent/internal/rag"
	"github.com/euraika-labs/pan-agent/internal/storage"
)

// Phase 13 WS#13.B — handler tests for /v1/rag/*. The Index logic is
// covered in internal/rag/index_test.go; these tests pin the HTTP
// surface contract (status codes, JSON shapes, attached/unattached
// behaviour, error envelopes).

// gatewayFakeEmbedder mirrors the in-memory fake from internal/rag's
// tests but lives in the gateway package because Go test files don't
// share types across packages.
type gatewayFakeEmbedder struct {
	model string
	dim   int
	calls int64
}

func (f *gatewayFakeEmbedder) Model() string { return f.model }
func (f *gatewayFakeEmbedder) Dim() int      { return f.dim }
func (f *gatewayFakeEmbedder) Embed(_ context.Context, text string) ([]float32, error) {
	atomic.AddInt64(&f.calls, 1)
	v := make([]float32, f.dim)
	var sum int
	for _, b := range []byte(text) {
		sum += int(b)
	}
	for i := range v {
		v[i] = float32(sum%(i+7)) / 10.0
	}
	return v, nil
}
func (f *gatewayFakeEmbedder) EmbedBatch(ctx context.Context, texts []string) ([][]float32, error) {
	out := make([][]float32, len(texts))
	for i, t := range texts {
		v, _ := f.Embed(ctx, t)
		out[i] = v
	}
	return out, nil
}

// attachIndex builds an Index over a real *storage.DB and attaches it
// to the server. Returns the embedder so tests can read its call count.
func attachIndex(t *testing.T, srv *Server) *gatewayFakeEmbedder {
	t.Helper()
	em := &gatewayFakeEmbedder{model: "test-model", dim: 4}
	idx, err := rag.NewIndex(em, srv.db)
	if err != nil {
		t.Fatalf("NewIndex: %v", err)
	}
	srv.AttachRAGIndex(idx)
	return em
}

// ---------------------------------------------------------------------------
// /v1/rag/health
// ---------------------------------------------------------------------------

func TestRAGHealth_Unconfigured(t *testing.T) {
	srv := setupTestServer(t)
	mux := http.NewServeMux()
	srv.registerRoutes(mux)

	req := httptest.NewRequest("GET", "/v1/rag/health", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	var resp ragHealthResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Configured {
		t.Error("expected configured=false on fresh server")
	}
}

func TestRAGHealth_Configured(t *testing.T) {
	srv := setupTestServer(t)
	em := attachIndex(t, srv)
	mux := http.NewServeMux()
	srv.registerRoutes(mux)

	req := httptest.NewRequest("GET", "/v1/rag/health", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	var resp ragHealthResponse
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	if !resp.Configured {
		t.Error("expected configured=true after AttachRAGIndex")
	}
	if resp.Model != em.model {
		t.Errorf("Model = %q, want %q", resp.Model, em.model)
	}
	if resp.Dim != em.dim {
		t.Errorf("Dim = %d, want %d", resp.Dim, em.dim)
	}
}

// ---------------------------------------------------------------------------
// /v1/rag/index
// ---------------------------------------------------------------------------

func TestRAGIndex_Unattached_503(t *testing.T) {
	srv := setupTestServer(t)
	mux := http.NewServeMux()
	srv.registerRoutes(mux)

	body := `{"source":"msg","source_id":"x","text":"hello"}`
	req := httptest.NewRequest("POST", "/v1/rag/index", bytes.NewBufferString(body))
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503", w.Code)
	}
	var apiErr APIError
	_ = json.Unmarshal(w.Body.Bytes(), &apiErr)
	if apiErr.Code != "rag_unavailable" {
		t.Errorf("code = %q, want rag_unavailable", apiErr.Code)
	}
}

func TestRAGIndex_HappyPath(t *testing.T) {
	srv := setupTestServer(t)
	em := attachIndex(t, srv)
	mux := http.NewServeMux()
	srv.registerRoutes(mux)

	body := `{"source":"msg","source_id":"id-1","session_id":"sess-1","text":"hello world"}`
	req := httptest.NewRequest("POST", "/v1/rag/index", bytes.NewBufferString(body))
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
	var resp ragIndexResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Cached {
		t.Error("first index call: expected Cached=false")
	}
	if resp.Embedding == nil {
		t.Fatal("expected embedding in response")
	}
	if resp.Embedding.Source != "msg" || resp.Embedding.SourceID != "id-1" {
		t.Errorf("embedding metadata wrong: %+v", resp.Embedding)
	}
	if resp.Embedding.SessionID != "sess-1" {
		t.Errorf("session_id = %q, want sess-1", resp.Embedding.SessionID)
	}
	if em.calls != 1 {
		t.Errorf("embedder calls = %d, want 1", em.calls)
	}
}

func TestRAGIndex_CacheHit(t *testing.T) {
	srv := setupTestServer(t)
	em := attachIndex(t, srv)
	mux := http.NewServeMux()
	srv.registerRoutes(mux)

	// First request → miss.
	body := `{"source":"msg","source_id":"id-1","session_id":"s","text":"shared"}`
	req := httptest.NewRequest("POST", "/v1/rag/index", bytes.NewBufferString(body))
	mux.ServeHTTP(httptest.NewRecorder(), req)

	// Second request, different source_id, SAME text → cache hit.
	body2 := `{"source":"msg","source_id":"id-2","session_id":"s","text":"shared"}`
	req2 := httptest.NewRequest("POST", "/v1/rag/index", bytes.NewBufferString(body2))
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req2)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d", w.Code)
	}
	var resp ragIndexResponse
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	if !resp.Cached {
		t.Errorf("expected Cached=true on duplicate text")
	}
	if em.calls != 1 {
		t.Errorf("embedder called %d times, expected 1 (cache miss only)", em.calls)
	}
}

func TestRAGIndex_InvalidJSON(t *testing.T) {
	srv := setupTestServer(t)
	attachIndex(t, srv)
	mux := http.NewServeMux()
	srv.registerRoutes(mux)

	req := httptest.NewRequest("POST", "/v1/rag/index", bytes.NewBufferString("{not json"))
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", w.Code)
	}
}

func TestRAGIndex_ValidationError(t *testing.T) {
	srv := setupTestServer(t)
	attachIndex(t, srv)
	mux := http.NewServeMux()
	srv.registerRoutes(mux)

	// Missing source.
	body := `{"source_id":"x","text":"t"}`
	req := httptest.NewRequest("POST", "/v1/rag/index", bytes.NewBufferString(body))
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", w.Code)
	}
	var apiErr APIError
	_ = json.Unmarshal(w.Body.Bytes(), &apiErr)
	if apiErr.Code != "index_failed" {
		t.Errorf("code = %q, want index_failed", apiErr.Code)
	}
}

// ---------------------------------------------------------------------------
// /v1/rag/search
// ---------------------------------------------------------------------------

func TestRAGSearch_Unattached_503(t *testing.T) {
	srv := setupTestServer(t)
	mux := http.NewServeMux()
	srv.registerRoutes(mux)

	body := `{"query":"q","session_id":"s"}`
	req := httptest.NewRequest("POST", "/v1/rag/search", bytes.NewBufferString(body))
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503", w.Code)
	}
}

func TestRAGSearch_HappyPath(t *testing.T) {
	srv := setupTestServer(t)
	attachIndex(t, srv)
	mux := http.NewServeMux()
	srv.registerRoutes(mux)

	// Seed via index endpoint.
	for i, txt := range []string{"alpha", "beta", "gamma"} {
		body := `{"source":"msg","source_id":"id-` + string(rune('a'+i)) +
			`","session_id":"s","text":"` + txt + `"}`
		req := httptest.NewRequest("POST", "/v1/rag/index", bytes.NewBufferString(body))
		mux.ServeHTTP(httptest.NewRecorder(), req)
	}

	body := `{"query":"alpha","session_id":"s","k":2}`
	req := httptest.NewRequest("POST", "/v1/rag/search", bytes.NewBufferString(body))
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
	var resp ragSearchResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Hits) > 2 {
		t.Errorf("len = %d, K=2 not respected", len(resp.Hits))
	}
	for i, h := range resp.Hits {
		if h.Embedding == nil {
			t.Errorf("hit[%d] missing embedding", i)
		}
	}
}

func TestRAGSearch_ValidationError(t *testing.T) {
	srv := setupTestServer(t)
	attachIndex(t, srv)
	mux := http.NewServeMux()
	srv.registerRoutes(mux)

	body := `{"session_id":"s"}` // missing query
	req := httptest.NewRequest("POST", "/v1/rag/search", bytes.NewBufferString(body))
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", w.Code)
	}
}

// ---------------------------------------------------------------------------
// DELETE /v1/rag/sessions/{id}/embeddings
// ---------------------------------------------------------------------------

func TestRAGDeleteSession_NoAttachmentNeeded(t *testing.T) {
	srv := setupTestServer(t)
	mux := http.NewServeMux()
	srv.registerRoutes(mux)

	// Even without an attached Index, the delete works against storage.
	req := httptest.NewRequest("DELETE", "/v1/rag/sessions/sess-empty/embeddings", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d", w.Code)
	}
	var resp ragDeleteResponse
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	if resp.Deleted != 0 {
		t.Errorf("deleted = %d, want 0 on empty session", resp.Deleted)
	}
}

func TestRAGDeleteSession_RemovesRows(t *testing.T) {
	srv := setupTestServer(t)
	attachIndex(t, srv)
	mux := http.NewServeMux()
	srv.registerRoutes(mux)

	for i, txt := range []string{"one", "two"} {
		body := `{"source":"msg","source_id":"id-` + string(rune('a'+i)) +
			`","session_id":"sess-X","text":"` + txt + `"}`
		req := httptest.NewRequest("POST", "/v1/rag/index", bytes.NewBufferString(body))
		mux.ServeHTTP(httptest.NewRecorder(), req)
	}

	req := httptest.NewRequest("DELETE", "/v1/rag/sessions/sess-X/embeddings", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d", w.Code)
	}
	var resp ragDeleteResponse
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	if resp.Deleted != 2 {
		t.Errorf("deleted = %d, want 2", resp.Deleted)
	}

	// Confirm via storage layer that the rows are gone.
	rest, err := srv.db.ListEmbeddingsBySession("sess-X")
	if err != nil {
		t.Fatalf("ListEmbeddingsBySession: %v", err)
	}
	if len(rest) != 0 {
		t.Errorf("after delete, %d rows remain", len(rest))
	}
	// Sanity: storage error sentinel still importable.
	_ = storage.ErrEmbeddingNotFound
}
