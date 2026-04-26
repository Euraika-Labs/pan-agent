package gateway

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/euraika-labs/pan-agent/internal/rag"
)

// Phase 13 WS#13.B — server-lifecycle wiring tests for the RAG
// watcher. The watcher itself is covered in internal/rag's suite;
// these tests verify the env-driven boot path: env vars in →
// embedder + index + watcher constructed → watcher running →
// Stop halts cleanly.

// fakeEmbeddingsServer returns an httptest server that responds to
// /embeddings with one zero-vector per input. Lets the wiring test
// run end-to-end without a real embedder provider.
func fakeEmbeddingsServer(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Model string   `json:"model"`
			Input []string `json:"input"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		var out struct {
			Data []struct {
				Embedding []float32 `json:"embedding"`
				Index     int       `json:"index"`
			} `json:"data"`
			Model string `json:"model"`
		}
		out.Model = req.Model
		for i := range req.Input {
			out.Data = append(out.Data, struct {
				Embedding []float32 `json:"embedding"`
				Index     int       `json:"index"`
			}{Embedding: make([]float32, 4), Index: i})
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(out)
	}))
	t.Cleanup(srv.Close)
	return srv
}

// ---------------------------------------------------------------------------
// ConfigureRAGFromEnv
// ---------------------------------------------------------------------------

func TestConfigureRAGFromEnv_NoEnv_NoOp(t *testing.T) {
	srv := setupTestServer(t)
	t.Setenv(envRAGEmbedderURL, "")
	t.Setenv(envRAGEmbedderModel, "")

	if err := srv.ConfigureRAGFromEnv(context.Background()); err != nil {
		t.Errorf("expected nil for unset env, got %v", err)
	}
	if srv.getRAGIndex() != nil {
		t.Error("expected no index attached")
	}
	if srv.ragWatcherRunning() {
		t.Error("expected watcher not running")
	}
}

func TestConfigureRAGFromEnv_PartialEnvErrors(t *testing.T) {
	srv := setupTestServer(t)

	// URL only.
	t.Setenv(envRAGEmbedderURL, "http://x")
	t.Setenv(envRAGEmbedderModel, "")
	if err := srv.ConfigureRAGFromEnv(context.Background()); err == nil {
		t.Error("expected error when only URL is set")
	}

	// Model only.
	t.Setenv(envRAGEmbedderURL, "")
	t.Setenv(envRAGEmbedderModel, "model-x")
	if err := srv.ConfigureRAGFromEnv(context.Background()); err == nil {
		t.Error("expected error when only Model is set")
	}
}

func TestConfigureRAGFromEnv_HappyPath(t *testing.T) {
	srv := setupTestServer(t)
	emb := fakeEmbeddingsServer(t)

	t.Setenv(envRAGEmbedderURL, emb.URL)
	t.Setenv(envRAGEmbedderModel, "test-model")
	t.Setenv(envRAGEmbedderDim, "4")
	t.Setenv(envRAGWatcherInterval, "100ms")

	if err := srv.ConfigureRAGFromEnv(context.Background()); err != nil {
		t.Fatalf("ConfigureRAGFromEnv: %v", err)
	}
	if srv.getRAGIndex() == nil {
		t.Error("expected index attached")
	}
	if !srv.ragWatcherRunning() {
		t.Error("expected watcher running")
	}

	// StopRAGWatcher should bring it down cleanly.
	srv.StopRAGWatcher()
	if srv.ragWatcherRunning() {
		t.Error("expected watcher stopped after StopRAGWatcher")
	}
}

func TestConfigureRAGFromEnv_BadDim(t *testing.T) {
	srv := setupTestServer(t)
	emb := fakeEmbeddingsServer(t)

	t.Setenv(envRAGEmbedderURL, emb.URL)
	t.Setenv(envRAGEmbedderModel, "test-model")
	t.Setenv(envRAGEmbedderDim, "not-a-number")

	if err := srv.ConfigureRAGFromEnv(context.Background()); err == nil {
		t.Error("expected error on bad dim")
	}
}

func TestConfigureRAGFromEnv_NegativeDim(t *testing.T) {
	srv := setupTestServer(t)
	emb := fakeEmbeddingsServer(t)

	t.Setenv(envRAGEmbedderURL, emb.URL)
	t.Setenv(envRAGEmbedderModel, "test-model")
	t.Setenv(envRAGEmbedderDim, "-5")

	if err := srv.ConfigureRAGFromEnv(context.Background()); err == nil {
		t.Error("expected error on negative dim")
	}
}

func TestConfigureRAGFromEnv_BadInterval(t *testing.T) {
	srv := setupTestServer(t)
	emb := fakeEmbeddingsServer(t)

	t.Setenv(envRAGEmbedderURL, emb.URL)
	t.Setenv(envRAGEmbedderModel, "test-model")
	t.Setenv(envRAGWatcherInterval, "bogus")

	if err := srv.ConfigureRAGFromEnv(context.Background()); err == nil {
		t.Error("expected error on bad interval")
	}
}

func TestConfigureRAGFromEnv_Idempotent(t *testing.T) {
	srv := setupTestServer(t)
	emb := fakeEmbeddingsServer(t)

	t.Setenv(envRAGEmbedderURL, emb.URL)
	t.Setenv(envRAGEmbedderModel, "test-model")
	t.Setenv(envRAGEmbedderDim, "4")
	t.Setenv(envRAGWatcherInterval, "100ms")

	if err := srv.ConfigureRAGFromEnv(context.Background()); err != nil {
		t.Fatalf("first call: %v", err)
	}
	first := srv.getRAGIndex()

	if err := srv.ConfigureRAGFromEnv(context.Background()); err != nil {
		t.Fatalf("second call: %v", err)
	}
	second := srv.getRAGIndex()

	// Second call replaces the index (different pointer).
	if first == second {
		t.Error("expected re-call to construct a new index")
	}
	if !srv.ragWatcherRunning() {
		t.Error("expected watcher still running after re-config")
	}
	srv.StopRAGWatcher()
}

// ---------------------------------------------------------------------------
// StopRAGWatcher safety
// ---------------------------------------------------------------------------

func TestStopRAGWatcher_NoOpWhenUnattached(t *testing.T) {
	srv := setupTestServer(t)
	srv.StopRAGWatcher() // should not panic
	if srv.ragWatcherRunning() {
		t.Error("ragWatcherRunning true on fresh server")
	}
}

func TestStopRAGWatcher_Idempotent(t *testing.T) {
	srv := setupTestServer(t)
	emb := fakeEmbeddingsServer(t)

	t.Setenv(envRAGEmbedderURL, emb.URL)
	t.Setenv(envRAGEmbedderModel, "test-model")
	t.Setenv(envRAGEmbedderDim, "4")

	if err := srv.ConfigureRAGFromEnv(context.Background()); err != nil {
		t.Fatalf("configure: %v", err)
	}
	srv.StopRAGWatcher()
	srv.StopRAGWatcher() // second call should not panic
	if srv.ragWatcherRunning() {
		t.Error("watcher still running after double Stop")
	}
}

// Sanity: imports via env constants must match the documented names so
// a config-file backend (future) can read them by string lookup.
func TestEnvVarNamesPinned(t *testing.T) {
	cases := map[string]string{
		envRAGEmbedderURL:     "PAN_AGENT_RAG_EMBEDDER_URL",
		envRAGEmbedderModel:   "PAN_AGENT_RAG_EMBEDDER_MODEL",
		envRAGEmbedderAPIKey:  "PAN_AGENT_RAG_EMBEDDER_API_KEY",
		envRAGEmbedderDim:     "PAN_AGENT_RAG_EMBEDDER_DIM",
		envRAGWatcherInterval: "PAN_AGENT_RAG_WATCHER_INTERVAL",
	}
	for got, want := range cases {
		if got != want {
			t.Errorf("env name = %q, want %q", got, want)
		}
	}
}

// Sanity: rag package types are still in the expected shape (catches
// silent breakage from upstream rag refactors).
func TestRAGTypeShapeUnchanged(t *testing.T) {
	var _ rag.HTTPEmbedderConfig
	var _ rag.WatcherOptions
	if !strings.Contains(envRAGEmbedderURL, "RAG") {
		t.Error("env constants drifted")
	}
}
