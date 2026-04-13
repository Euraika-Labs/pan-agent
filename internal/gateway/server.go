// Package gateway implements the HTTP API server for pan-agent.
//
// It exposes a REST + SSE interface on localhost (default port 8642) that the
// Tauri desktop frontend — or any HTTP client — can talk to. It replaces both
// the predecessor Electron IPC bridge.
package gateway

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/euraika-labs/pan-agent/internal/approval"
	"github.com/euraika-labs/pan-agent/internal/config"
	"github.com/euraika-labs/pan-agent/internal/llm"
	"github.com/euraika-labs/pan-agent/internal/storage"
)

// Server is the pan-agent HTTP API server.
type Server struct {
	// Addr is the TCP address the server listens on, e.g. "127.0.0.1:8642".
	Addr string

	// profile is the active pan-agent profile name ("" == "default").
	profile string

	// Service dependencies — all set by New, never replaced after Start.
	db        *storage.DB
	approvals *approval.Store

	// llmMu guards llmClient for concurrent reads and writes.
	llmMu sync.RWMutex

	// llmClient is the active LLM client, derived from the stored model config.
	// It may be nil if no model has been configured yet.
	// All access must go through getLLMClient() or hold llmMu.
	llmClient *llm.Client

	httpServer *http.Server

	// gatewayMu guards gatewayRunning for concurrent access.
	gatewayMu      sync.RWMutex
	gatewayRunning bool
}

// getLLMClient returns the current LLM client under a read lock.
// The returned pointer must not be stored long-term; callers should call this
// each time they need the client to pick up any concurrent updates.
func (s *Server) getLLMClient() *llm.Client {
	s.llmMu.RLock()
	defer s.llmMu.RUnlock()
	return s.llmClient
}

// New creates a Server that will listen on addr (e.g. "127.0.0.1:8642").
//
// db must be an open *storage.DB obtained from storage.Open. The profile string
// selects which pan-agent profile to read configuration from; pass "" for the
// default profile.
func New(addr string, db *storage.DB, profile string) *Server {
	s := &Server{
		Addr:      addr,
		profile:   profile,
		db:        db,
		approvals: approval.NewStore(),
	}

	// Bootstrap the LLM client from the persisted model config.
	mc := config.GetModelConfig(profile)
	if mc.BaseURL != "" && mc.Model != "" {
		s.refreshLLMClient(mc.BaseURL, mc.Model, s.profile)
	}

	mux := http.NewServeMux()
	s.registerRoutes(mux)

	s.httpServer = &http.Server{
		Addr:    addr,
		Handler: withMiddleware(mux),
		// ReadTimeout covers the time to read the request headers+body.
		ReadTimeout: 30 * time.Second,
		// WriteTimeout must be 0 for long-lived SSE streaming responses.
		WriteTimeout: 0,
		IdleTimeout:  120 * time.Second,
	}

	return s
}

// Start begins accepting HTTP connections. It blocks until the server stops
// (including after a graceful Stop). The only "expected" non-error return is
// http.ErrServerClosed after a clean shutdown.
func (s *Server) Start() error {
	ln, err := net.Listen("tcp", s.Addr)
	if err != nil {
		return fmt.Errorf("gateway: listen %s: %w", s.Addr, err)
	}
	fmt.Printf("pan-agent API listening on http://%s\n", s.Addr)
	return s.httpServer.Serve(ln)
}

// Stop gracefully shuts down the server. It waits for in-flight requests to
// finish or until ctx is cancelled, whichever comes first.
func (s *Server) Stop(ctx context.Context) error {
	return s.httpServer.Shutdown(ctx)
}

// isGatewayRunning returns the current messaging gateway state.
func (s *Server) isGatewayRunning() bool {
	s.gatewayMu.RLock()
	defer s.gatewayMu.RUnlock()
	return s.gatewayRunning
}

// resolveProfile returns the profile from the ?profile= query param, falling
// back to the server's default profile.
func (s *Server) resolveProfile(r *http.Request) string {
	if p := r.URL.Query().Get("profile"); p != "" {
		return p
	}
	return s.profile
}

// refreshLLMClient rebuilds the in-process LLM client from the given model
// config and the profile's .env file. It acquires llmMu for the swap.
func (s *Server) refreshLLMClient(baseURL, model, profile string) {
	env, _ := config.ReadProfileEnv(profile)
	apiKey := env["REGOLO_API_KEY"]
	if apiKey == "" {
		apiKey = env["OPENAI_API_KEY"]
	}
	if apiKey == "" {
		apiKey = env["API_KEY"]
	}
	if apiKey == "" {
		apiKey = os.Getenv("OPENAI_API_KEY")
	}
	s.llmMu.Lock()
	s.llmClient = llm.NewClient(baseURL, apiKey, model)
	s.llmMu.Unlock()
}
