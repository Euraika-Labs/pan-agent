package rag

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sort"

	"github.com/euraika-labs/pan-agent/internal/storage"
)

// Phase 13 WS#13.B — index wrapper. Sits between the embedder and the
// storage layer, owning the cache-then-fetch contract:
//
//   1. Compute the content-hash of the input text.
//   2. Look up an existing row by (hash, model). Hit → return that row,
//      no embedder round-trip, no write. Miss → continue.
//   3. Call the embedder.
//   4. Pack the float32 vector into a BLOB.
//   5. UpsertEmbedding — UNIQUE(source, source_id, model) means the
//      same logical entity overwrites in place under the same model.
//
// Search is the read side: embed the query, score every candidate
// in scope (currently session-scoped) by cosine similarity, return
// the top-k. The package's CosineSimilarity function does the math;
// no sqlite-vec dependency yet — that lands behind the same Search
// signature in a follow-up.

// Store is the subset of *storage.DB methods Index needs. Defined as
// an interface here so unit tests can inject an in-memory fake
// without spinning up SQLite.
type Store interface {
	UpsertEmbedding(e storage.Embedding) error
	GetEmbeddingByHash(contentHash, model string) (*storage.Embedding, error)
	ListEmbeddingsBySession(sessionID string) ([]storage.Embedding, error)
}

// IngestRequest is the input to Index.Upsert. The (Source, SourceID)
// pair uniquely identifies the originating object — for chat
// messages, source="message" and source_id is the message UUID.
type IngestRequest struct {
	Source    string
	SourceID  string
	SessionID string // optional; "" stores NULL session_id
	Text      string
}

// IngestResult reports what Upsert did:
//
//   - Cached=true: hash hit, no embedder call, the existing row is
//     returned in Embedding.
//   - Cached=false: embedded + written; Embedding holds the freshly
//     persisted row (read-back via GetEmbeddingByHash).
type IngestResult struct {
	Cached    bool
	Embedding *storage.Embedding
}

// SearchRequest is the input to Index.Search.
//
// SessionID is required for now — global search across all sessions
// is a different query shape (no per-session purge guarantees on the
// candidate set) and lands in the follow-up that adds ListAllEmbeddings.
//
// K caps the result count; scores below MinScore are dropped before
// the cap so a single dominant hit doesn't pad the result with noise.
type SearchRequest struct {
	Query     string
	SessionID string
	K         int
	MinScore  float32 // optional; 0 = no filter
}

// Hit is one search result row.
type Hit struct {
	Embedding storage.Embedding
	Score     float32 // cosine similarity, range [-1, 1]
}

// Index is the cache-then-fetch + scored-search facade.
type Index struct {
	embedder Embedder
	store    Store
}

// Embedder returns the embedder this Index was constructed with.
// Exposed for HTTP introspection (e.g. /v1/rag/health reporting the
// active model + dim). Callers should treat the returned interface
// as read-only — calling Embed directly bypasses the content-hash
// gate, which is almost never what you want.
func (idx *Index) Embedder() Embedder { return idx.embedder }

// NewIndex wires the two collaborators. Both must be non-nil — Index
// has nothing useful to do without an embedder or a store.
func NewIndex(embedder Embedder, store Store) (*Index, error) {
	if embedder == nil {
		return nil, fmt.Errorf("rag: NewIndex: embedder required")
	}
	if store == nil {
		return nil, fmt.Errorf("rag: NewIndex: store required")
	}
	return &Index{embedder: embedder, store: store}, nil
}

// Upsert embeds + writes if the content is new under this model;
// returns the cached row if (content_hash, model) already exists.
//
// Called from the embedder watcher (next slice) on every new message
// and from the search-time fallback when a query needs an immediate
// re-index of changed content.
func (idx *Index) Upsert(ctx context.Context, req IngestRequest) (IngestResult, error) {
	if req.Source == "" || req.SourceID == "" {
		return IngestResult{}, fmt.Errorf("rag: Upsert: source + source_id required")
	}
	if req.Text == "" {
		return IngestResult{}, fmt.Errorf("rag: Upsert: text is empty")
	}

	hash := ContentHash(req.Text)
	model := idx.embedder.Model()

	// Cache hit: same (hash, model) already present → reuse.
	existing, err := idx.store.GetEmbeddingByHash(hash, model)
	if err == nil {
		return IngestResult{Cached: true, Embedding: existing}, nil
	}
	if !errors.Is(err, storage.ErrEmbeddingNotFound) {
		return IngestResult{}, fmt.Errorf("rag: Upsert hash lookup: %w", err)
	}

	// Cache miss: embed + write.
	vec, err := idx.embedder.Embed(ctx, req.Text)
	if err != nil {
		return IngestResult{}, fmt.Errorf("rag: Upsert embed: %w", err)
	}
	dim := idx.embedder.Dim()
	if dim == 0 {
		// Embedder configured without dim validation — trust the
		// vector length we got.
		dim = len(vec)
	}
	if len(vec) != dim {
		return IngestResult{}, fmt.Errorf("rag: Upsert: embedder returned dim %d, expected %d",
			len(vec), dim)
	}

	blob, err := PackVector(vec)
	if err != nil {
		return IngestResult{}, fmt.Errorf("rag: Upsert pack: %w", err)
	}

	row := storage.Embedding{
		Source:      req.Source,
		SourceID:    req.SourceID,
		ContentHash: hash,
		Text:        req.Text,
		Model:       model,
		Dim:         dim,
		Vector:      blob,
	}
	if req.SessionID != "" {
		row.SessionID = sql.NullString{String: req.SessionID, Valid: true}
	}
	if err := idx.store.UpsertEmbedding(row); err != nil {
		return IngestResult{}, fmt.Errorf("rag: Upsert write: %w", err)
	}

	// Read-back so the caller sees the assigned id + created_at.
	written, err := idx.store.GetEmbeddingByHash(hash, model)
	if err != nil {
		return IngestResult{}, fmt.Errorf("rag: Upsert readback: %w", err)
	}
	return IngestResult{Cached: false, Embedding: written}, nil
}

// Search embeds Query, scores every candidate in the session against
// it, and returns the top-K hits in descending score order.
//
// Implementation is brute-force cosine over the session's embeddings —
// fine for the typical desktop session (hundreds of messages), and a
// known stopgap until sqlite-vec lands behind the same signature.
func (idx *Index) Search(ctx context.Context, req SearchRequest) ([]Hit, error) {
	if req.Query == "" {
		return nil, fmt.Errorf("rag: Search: query is empty")
	}
	if req.SessionID == "" {
		return nil, fmt.Errorf("rag: Search: session_id required (global search not yet supported)")
	}
	if req.K <= 0 {
		req.K = 10
	}

	queryVec, err := idx.embedder.Embed(ctx, req.Query)
	if err != nil {
		return nil, fmt.Errorf("rag: Search embed: %w", err)
	}

	candidates, err := idx.store.ListEmbeddingsBySession(req.SessionID)
	if err != nil {
		return nil, fmt.Errorf("rag: Search list: %w", err)
	}
	if len(candidates) == 0 {
		return nil, nil
	}

	model := idx.embedder.Model()
	hits := make([]Hit, 0, len(candidates))
	for _, c := range candidates {
		// Skip rows produced under a different model — vectors from
		// distinct models live in distinct spaces; cross-comparing
		// them would yield meaningless scores.
		if c.Model != model {
			continue
		}
		vec, err := UnpackVector(c.Vector, c.Dim)
		if err != nil {
			// One corrupt row shouldn't sink the whole search —
			// skip and continue. Surfaced if the entire result is
			// empty + we logged candidates.
			continue
		}
		score, err := CosineSimilarity(queryVec, vec)
		if err != nil {
			continue
		}
		if score < req.MinScore {
			continue
		}
		hits = append(hits, Hit{Embedding: c, Score: score})
	}

	sort.SliceStable(hits, func(i, j int) bool {
		return hits[i].Score > hits[j].Score
	})
	if len(hits) > req.K {
		hits = hits[:req.K]
	}
	return hits, nil
}
