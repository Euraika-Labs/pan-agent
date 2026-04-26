package gateway

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/euraika-labs/pan-agent/internal/rag"
)

// Phase 13 WS#13.B — server-lifecycle wiring for the RAG watcher.
//
// The pieces shipped in earlier slices:
//
//   #42 codec + storage helpers
//   #43 embedder client
//   #44 Index wrapper
//   #45 HTTP endpoints (with lazy AttachRAGIndex)
//   #46 polling watcher
//   #47 chat-loop retrieval (env-gated)
//
// This slice connects them at startup. When the operator sets the
// PAN_AGENT_RAG_EMBEDDER_URL + PAN_AGENT_RAG_EMBEDDER_MODEL env vars,
// the gateway constructs an HTTPEmbedder, an Index over the existing
// *storage.DB, and a Watcher; then attaches the index + starts the
// watcher. Without those env vars the wiring is a no-op and chat
// continues to work without semantic-recall context.

// Env var names — declared as constants so tests can reference the
// same symbols + a future config-file backend can substitute. The
// nosec annotations cover gosec G101 (hardcoded credentials): these
// are the NAMES of env vars users set, not the values.
const (
	envRAGEmbedderURL     = "PAN_AGENT_RAG_EMBEDDER_URL"
	envRAGEmbedderModel   = "PAN_AGENT_RAG_EMBEDDER_MODEL"
	envRAGEmbedderAPIKey  = "PAN_AGENT_RAG_EMBEDDER_API_KEY" // #nosec G101 -- env var name, not a credential value
	envRAGEmbedderDim     = "PAN_AGENT_RAG_EMBEDDER_DIM"
	envRAGWatcherInterval = "PAN_AGENT_RAG_WATCHER_INTERVAL"
)

// ConfigureRAGFromEnv reads the RAG-related env vars + builds the
// embedder, index, and watcher. Called from Server.Start so the
// wiring happens automatically when the operator configures the
// embedder. Three documented outcomes:
//
//   - Both URL + Model unset → no-op, returns nil. Logs nothing
//     (the operator opted out by not setting the env vars).
//   - Embedder/Index/Watcher construction fails → returns the
//     error wrapped. Caller decides whether to surface (Start
//     logs to stderr; tests assert via the return value).
//   - Success → index attached + watcher running. The first poll
//     tick fires immediately so the indexer catches up to the
//     existing message log on a fresh start.
//
// Idempotent: calling twice on the same server replaces the
// previous index + restarts the watcher with the new config.
func (s *Server) ConfigureRAGFromEnv(ctx context.Context) error {
	url := strings.TrimSpace(os.Getenv(envRAGEmbedderURL))
	model := strings.TrimSpace(os.Getenv(envRAGEmbedderModel))
	if url == "" && model == "" {
		// Operator opted out. Not an error.
		return nil
	}
	if url == "" || model == "" {
		return fmt.Errorf(
			"both %s and %s must be set (or both unset to disable RAG)",
			envRAGEmbedderURL, envRAGEmbedderModel)
	}

	cfg := rag.HTTPEmbedderConfig{
		BaseURL: url,
		APIKey:  strings.TrimSpace(os.Getenv(envRAGEmbedderAPIKey)),
		Model:   model,
	}
	if dimStr := strings.TrimSpace(os.Getenv(envRAGEmbedderDim)); dimStr != "" {
		dim, err := strconv.Atoi(dimStr)
		if err != nil || dim < 0 {
			return fmt.Errorf("%s must be a non-negative integer, got %q",
				envRAGEmbedderDim, dimStr)
		}
		cfg.Dim = dim
	}

	em, err := rag.NewHTTPEmbedder(cfg)
	if err != nil {
		return fmt.Errorf("build embedder: %w", err)
	}
	idx, err := rag.NewIndex(em, s.db)
	if err != nil {
		return fmt.Errorf("build index: %w", err)
	}
	s.AttachRAGIndex(idx)

	// Watcher. Stop any previously-running watcher first so a
	// re-call replaces cleanly.
	s.StopRAGWatcher()

	opts := rag.WatcherOptions{}
	if iv := strings.TrimSpace(os.Getenv(envRAGWatcherInterval)); iv != "" {
		dur, err := time.ParseDuration(iv)
		if err != nil {
			return fmt.Errorf("%s: %w", envRAGWatcherInterval, err)
		}
		opts.Interval = dur
	}

	w, err := rag.NewWatcher(idx, s.db, opts)
	if err != nil {
		return fmt.Errorf("build watcher: %w", err)
	}
	if err := w.Start(ctx); err != nil {
		return fmt.Errorf("start watcher: %w", err)
	}
	s.ragMu.Lock()
	s.ragWatcher = w
	s.ragMu.Unlock()
	return nil
}

// StopRAGWatcher halts the watcher goroutine if one is running.
// Idempotent: safe to call multiple times or when no watcher was
// ever attached. Called from Server.Stop.
func (s *Server) StopRAGWatcher() {
	s.ragMu.Lock()
	w := s.ragWatcher
	s.ragWatcher = nil
	s.ragMu.Unlock()
	if w != nil {
		_ = w.Stop()
	}
}

// ragWatcherRunning reports whether a watcher is currently attached
// + running. Exposed for tests + a future status endpoint.
func (s *Server) ragWatcherRunning() bool {
	s.ragMu.RLock()
	w := s.ragWatcher
	s.ragMu.RUnlock()
	return w != nil && w.Running()
}
