package gateway

import (
	"encoding/json"
	"net/http"

	"github.com/euraika-labs/pan-agent/internal/rag"
	"github.com/euraika-labs/pan-agent/internal/storage"
)

// Phase 13 WS#13.B — HTTP endpoints over the rag.Index.
//
// The slice is intentionally narrow:
//
//   POST   /v1/rag/index                              — Upsert one entry
//   POST   /v1/rag/search                             — top-K cosine search
//   DELETE /v1/rag/sessions/{id}/embeddings           — purge a session
//   GET    /v1/rag/health                             — ready / not configured
//
// The Index instance is attached to the Server lazily (see
// Server.AttachRAGIndex). When unattached, write/search endpoints
// return 503 with a clear message; the health endpoint reports
// "configured": false so the desktop UI can surface a setup hint.
//
// No Embedder configuration code is wired in this slice — that lands
// alongside the desktop's RAG settings panel. Existing deployments
// stay unaffected because the routes 503 until something calls
// AttachRAGIndex.

// AttachRAGIndex wires a configured rag.Index into the server. Safe to
// call from main.go after Embedder construction; the handlers re-read
// the pointer on every request through getRAGIndex so callers can
// hot-swap the index without restarting the server.
func (s *Server) AttachRAGIndex(idx *rag.Index) {
	s.ragMu.Lock()
	defer s.ragMu.Unlock()
	s.ragIndex = idx
}

// getRAGIndex returns the current attached index under a read lock.
// Returns nil when no index has been attached — handlers must check.
func (s *Server) getRAGIndex() *rag.Index {
	s.ragMu.RLock()
	defer s.ragMu.RUnlock()
	return s.ragIndex
}

// ---------------------------------------------------------------------------
// Request / response shapes
// ---------------------------------------------------------------------------

type ragIndexRequest struct {
	Source    string `json:"source"`
	SourceID  string `json:"source_id"`
	SessionID string `json:"session_id,omitempty"`
	Text      string `json:"text"`
}

type ragIndexResponse struct {
	Cached    bool             `json:"cached"`
	Embedding *ragEmbeddingDTO `json:"embedding,omitempty"`
}

type ragEmbeddingDTO struct {
	ID          int64  `json:"id"`
	Source      string `json:"source"`
	SourceID    string `json:"source_id"`
	SessionID   string `json:"session_id,omitempty"`
	ContentHash string `json:"content_hash"`
	Text        string `json:"text"`
	Model       string `json:"model"`
	Dim         int    `json:"dim"`
	CreatedAt   int64  `json:"created_at"`
	// Vector is intentionally omitted: a 384-dim vector at 4 bytes/dim
	// is 1.5KB per row; clients that need the raw vector should add a
	// dedicated endpoint. The HTTP surface stays metadata-only.
}

func toEmbeddingDTO(e *storage.Embedding) *ragEmbeddingDTO {
	if e == nil {
		return nil
	}
	dto := &ragEmbeddingDTO{
		ID: e.ID, Source: e.Source, SourceID: e.SourceID,
		ContentHash: e.ContentHash, Text: e.Text, Model: e.Model,
		Dim: e.Dim, CreatedAt: e.CreatedAt,
	}
	if e.SessionID.Valid {
		dto.SessionID = e.SessionID.String
	}
	return dto
}

type ragSearchRequest struct {
	Query     string  `json:"query"`
	SessionID string  `json:"session_id"`
	K         int     `json:"k,omitempty"`
	MinScore  float32 `json:"min_score,omitempty"`
}

type ragSearchResponse struct {
	Hits []ragSearchHit `json:"hits"`
}

type ragSearchHit struct {
	Embedding *ragEmbeddingDTO `json:"embedding"`
	Score     float32          `json:"score"`
}

type ragHealthResponse struct {
	Configured bool   `json:"configured"`
	Model      string `json:"model,omitempty"`
	Dim        int    `json:"dim,omitempty"`
}

type ragDeleteResponse struct {
	Deleted int64 `json:"deleted"`
}

// ---------------------------------------------------------------------------
// Handlers
// ---------------------------------------------------------------------------

// handleRAGIndex POSTs a single text into the index. Returns
// {"cached": bool, "embedding": {...}} so the caller can tell whether
// the embedder ran or the content-hash gate served a cache hit.
func (s *Server) handleRAGIndex(w http.ResponseWriter, r *http.Request) {
	idx := s.getRAGIndex()
	if idx == nil {
		writeAPIError(w, http.StatusServiceUnavailable,
			"rag_unavailable", "RAG index not configured", nil)
		return
	}

	var req ragIndexRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAPIError(w, http.StatusBadRequest,
			"invalid_request", "invalid JSON body", nil)
		return
	}

	res, err := idx.Upsert(r.Context(), rag.IngestRequest{
		Source: req.Source, SourceID: req.SourceID,
		SessionID: req.SessionID, Text: req.Text,
	})
	if err != nil {
		writeAPIError(w, http.StatusBadRequest,
			"index_failed", err.Error(), nil)
		return
	}
	writeJSON(w, http.StatusOK, ragIndexResponse{
		Cached:    res.Cached,
		Embedding: toEmbeddingDTO(res.Embedding),
	})
}

// handleRAGSearch POSTs a query and returns the top-K hits as
// metadata-only DTOs (vector bytes excluded — see ragEmbeddingDTO).
func (s *Server) handleRAGSearch(w http.ResponseWriter, r *http.Request) {
	idx := s.getRAGIndex()
	if idx == nil {
		writeAPIError(w, http.StatusServiceUnavailable,
			"rag_unavailable", "RAG index not configured", nil)
		return
	}

	var req ragSearchRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAPIError(w, http.StatusBadRequest,
			"invalid_request", "invalid JSON body", nil)
		return
	}

	hits, err := idx.Search(r.Context(), rag.SearchRequest{
		Query: req.Query, SessionID: req.SessionID,
		K: req.K, MinScore: req.MinScore,
	})
	if err != nil {
		writeAPIError(w, http.StatusBadRequest,
			"search_failed", err.Error(), nil)
		return
	}

	out := ragSearchResponse{Hits: make([]ragSearchHit, 0, len(hits))}
	for _, h := range hits {
		// Take a stable address of the loop variable so toEmbeddingDTO
		// doesn't capture an aliased pointer (Go ≥1.22 makes this safe
		// per-iteration but the explicit copy reads more clearly).
		emb := h.Embedding
		out.Hits = append(out.Hits, ragSearchHit{
			Embedding: toEmbeddingDTO(&emb),
			Score:     h.Score,
		})
	}
	writeJSON(w, http.StatusOK, out)
}

// handleRAGSessionEmbeddingsDelete purges every embedding tied to
// the session id from the URL. Doesn't require an attached Index —
// the storage call works against whatever rows exist.
func (s *Server) handleRAGSessionEmbeddingsDelete(w http.ResponseWriter, r *http.Request) {
	sid := r.PathValue("id")
	if sid == "" {
		writeAPIError(w, http.StatusBadRequest,
			"invalid_request", "session id required", nil)
		return
	}
	n, err := s.db.DeleteEmbeddingsBySession(sid)
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError,
			"internal_error", err.Error(), nil)
		return
	}
	writeJSON(w, http.StatusOK, ragDeleteResponse{Deleted: n})
}

// handleRAGHealth reports whether the index is configured. Returns
// 200 in either case so the desktop UI can render a setup banner
// without needing to distinguish "not yet wired" from a real error.
func (s *Server) handleRAGHealth(w http.ResponseWriter, _ *http.Request) {
	idx := s.getRAGIndex()
	if idx == nil {
		writeJSON(w, http.StatusOK, ragHealthResponse{Configured: false})
		return
	}
	em := idx.Embedder()
	if em == nil {
		// Should be unreachable — Index is constructed with a non-nil
		// embedder. Defensive: surface the impossible case rather
		// than panic.
		writeJSON(w, http.StatusOK, ragHealthResponse{Configured: true})
		return
	}
	writeJSON(w, http.StatusOK, ragHealthResponse{
		Configured: true,
		Model:      em.Model(),
		Dim:        em.Dim(),
	})
}
