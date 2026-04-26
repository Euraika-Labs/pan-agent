package rag

import (
	"context"
	"database/sql"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/euraika-labs/pan-agent/internal/storage"
)

// Phase 13 WS#13.B — index wrapper tests. Two test surfaces:
//
//   1. Unit tests against in-memory fakes — fast, deterministic,
//      cover the cache-then-fetch and Search math without SQLite.
//   2. One integration test against a real *storage.DB to verify
//      the storage.Embedding round-trip survives sql.NullString +
//      BLOB column shapes the fake skips.

// ---------------------------------------------------------------------------
// Fakes
// ---------------------------------------------------------------------------

// fakeEmbedder returns a deterministic vector for each input. The
// vector seed lets tests construct queries that score predictably
// against seeded content.
type fakeEmbedder struct {
	model string
	dim   int
	calls int64                       // atomic; counts Embed + EmbedBatch entries
	build func(text string) []float32 // optional override
}

func (f *fakeEmbedder) Model() string { return f.model }
func (f *fakeEmbedder) Dim() int      { return f.dim }

func (f *fakeEmbedder) Embed(_ context.Context, text string) ([]float32, error) {
	atomic.AddInt64(&f.calls, 1)
	if f.build != nil {
		return f.build(text), nil
	}
	v := make([]float32, f.dim)
	// Hash-ish: distribute the byte sum across dims. Crude but stable.
	var sum int
	for _, b := range []byte(text) {
		sum += int(b)
	}
	for i := range v {
		v[i] = float32(sum%(i+7)) / 10.0
	}
	return v, nil
}

func (f *fakeEmbedder) EmbedBatch(ctx context.Context, texts []string) ([][]float32, error) {
	out := make([][]float32, len(texts))
	for i, t := range texts {
		v, err := f.Embed(ctx, t)
		if err != nil {
			return nil, err
		}
		out[i] = v
	}
	return out, nil
}

func (f *fakeEmbedder) callCount() int64 { return atomic.LoadInt64(&f.calls) }

// fakeStore implements rag.Store with maps; tests check insert/lookup
// behaviour without touching SQLite.
type fakeStore struct {
	rows []storage.Embedding
}

func (s *fakeStore) UpsertEmbedding(e storage.Embedding) error {
	for i, r := range s.rows {
		if r.Source == e.Source && r.SourceID == e.SourceID && r.Model == e.Model {
			e.ID = r.ID
			s.rows[i] = e
			return nil
		}
	}
	e.ID = int64(len(s.rows) + 1)
	s.rows = append(s.rows, e)
	return nil
}

func (s *fakeStore) GetEmbeddingByHash(hash, model string) (*storage.Embedding, error) {
	for i := len(s.rows) - 1; i >= 0; i-- {
		if s.rows[i].ContentHash == hash && s.rows[i].Model == model {
			row := s.rows[i]
			return &row, nil
		}
	}
	return nil, storage.ErrEmbeddingNotFound
}

func (s *fakeStore) ListEmbeddingsBySession(sessionID string) ([]storage.Embedding, error) {
	var out []storage.Embedding
	for _, r := range s.rows {
		if r.SessionID.Valid && r.SessionID.String == sessionID {
			out = append(out, r)
		}
	}
	return out, nil
}

// ---------------------------------------------------------------------------
// Tests — unit
// ---------------------------------------------------------------------------

func TestNewIndex_NilArgs(t *testing.T) {
	t.Parallel()
	if _, err := NewIndex(nil, &fakeStore{}); err == nil {
		t.Error("nil embedder: expected error")
	}
	if _, err := NewIndex(&fakeEmbedder{model: "m", dim: 4}, nil); err == nil {
		t.Error("nil store: expected error")
	}
}

func TestIndex_Upsert_CacheMissThenHit(t *testing.T) {
	t.Parallel()
	em := &fakeEmbedder{model: "m-1", dim: 4}
	st := &fakeStore{}
	idx, err := NewIndex(em, st)
	if err != nil {
		t.Fatalf("NewIndex: %v", err)
	}

	// First call: cache miss → embeds.
	r1, err := idx.Upsert(context.Background(), IngestRequest{
		Source: "msg", SourceID: "id-1", SessionID: "s-1", Text: "hello",
	})
	if err != nil {
		t.Fatalf("first Upsert: %v", err)
	}
	if r1.Cached {
		t.Error("first Upsert reported Cached=true")
	}
	if em.callCount() != 1 {
		t.Errorf("embedder calls = %d, want 1", em.callCount())
	}

	// Second call with SAME text: cache hit → no embed, returns existing row.
	r2, err := idx.Upsert(context.Background(), IngestRequest{
		Source: "msg", SourceID: "id-2", SessionID: "s-1", Text: "hello",
	})
	if err != nil {
		t.Fatalf("second Upsert: %v", err)
	}
	if !r2.Cached {
		t.Error("second Upsert (same text): expected Cached=true")
	}
	if em.callCount() != 1 {
		t.Errorf("embedder called twice — cache miss bug: calls = %d", em.callCount())
	}

	// Third call with DIFFERENT text: another cache miss.
	if _, err := idx.Upsert(context.Background(), IngestRequest{
		Source: "msg", SourceID: "id-3", SessionID: "s-1", Text: "different",
	}); err != nil {
		t.Fatalf("third Upsert: %v", err)
	}
	if em.callCount() != 2 {
		t.Errorf("after differing text, calls = %d, want 2", em.callCount())
	}
}

func TestIndex_Upsert_DistinctModelsBothEmbed(t *testing.T) {
	t.Parallel()
	st := &fakeStore{}
	emA := &fakeEmbedder{model: "model-A", dim: 4}
	emB := &fakeEmbedder{model: "model-B", dim: 4}
	idxA, _ := NewIndex(emA, st)
	idxB, _ := NewIndex(emB, st)

	// Same text + same source under TWO models → both must embed,
	// because the cache key is (hash, model) not just hash.
	if _, err := idxA.Upsert(context.Background(), IngestRequest{
		Source: "msg", SourceID: "id-1", Text: "shared text",
	}); err != nil {
		t.Fatalf("model-A Upsert: %v", err)
	}
	if _, err := idxB.Upsert(context.Background(), IngestRequest{
		Source: "msg", SourceID: "id-1", Text: "shared text",
	}); err != nil {
		t.Fatalf("model-B Upsert: %v", err)
	}
	if emA.callCount() != 1 {
		t.Errorf("model-A calls = %d, want 1", emA.callCount())
	}
	if emB.callCount() != 1 {
		t.Errorf("model-B calls = %d, want 1", emB.callCount())
	}
}

func TestIndex_Upsert_ValidationErrors(t *testing.T) {
	t.Parallel()
	idx, _ := NewIndex(&fakeEmbedder{model: "m", dim: 4}, &fakeStore{})
	cases := []IngestRequest{
		{SourceID: "id", Text: "t"},               // no source
		{Source: "msg", Text: "t"},                // no source_id
		{Source: "msg", SourceID: "id"},           // no text
		{Source: "msg", SourceID: "id", Text: ""}, // empty text
	}
	for _, c := range cases {
		if _, err := idx.Upsert(context.Background(), c); err == nil {
			t.Errorf("expected error for req %+v", c)
		}
	}
}

func TestIndex_Upsert_DimMismatchPropagates(t *testing.T) {
	t.Parallel()
	em := &fakeEmbedder{model: "m", dim: 4, build: func(string) []float32 {
		return []float32{1, 2} // wrong dim
	}}
	idx, _ := NewIndex(em, &fakeStore{})
	_, err := idx.Upsert(context.Background(), IngestRequest{
		Source: "msg", SourceID: "id", Text: "x",
	})
	if err == nil {
		t.Error("expected dim-mismatch error")
	}
}

func TestIndex_Search_RanksByCosine(t *testing.T) {
	t.Parallel()
	// Build an embedder where the vector is a function of the input,
	// so we can engineer a query close to one specific seed.
	dim := 4
	em := &fakeEmbedder{model: "m", dim: dim, build: func(text string) []float32 {
		switch text {
		case "apple":
			return []float32{1, 0, 0, 0}
		case "banana":
			return []float32{0, 1, 0, 0}
		case "cherry":
			return []float32{0, 0, 1, 0}
		case "query-near-apple":
			return []float32{0.95, 0.05, 0, 0}
		}
		return []float32{0, 0, 0, 1}
	}}
	st := &fakeStore{}
	idx, _ := NewIndex(em, st)

	for i, txt := range []string{"apple", "banana", "cherry"} {
		_, err := idx.Upsert(context.Background(), IngestRequest{
			Source:    "msg",
			SourceID:  []string{"id-a", "id-b", "id-c"}[i],
			SessionID: "s-1",
			Text:      txt,
		})
		if err != nil {
			t.Fatalf("seed %s: %v", txt, err)
		}
	}

	hits, err := idx.Search(context.Background(), SearchRequest{
		Query: "query-near-apple", SessionID: "s-1", K: 3,
	})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(hits) != 3 {
		t.Fatalf("len = %d, want 3", len(hits))
	}
	if hits[0].Embedding.Text != "apple" {
		t.Errorf("top hit = %q, want apple", hits[0].Embedding.Text)
	}
	for i := 1; i < len(hits); i++ {
		if hits[i].Score > hits[i-1].Score {
			t.Errorf("scores not descending at i=%d (%v > %v)",
				i, hits[i].Score, hits[i-1].Score)
		}
	}
}

func TestIndex_Search_KCap(t *testing.T) {
	t.Parallel()
	em := &fakeEmbedder{model: "m", dim: 2}
	st := &fakeStore{}
	idx, _ := NewIndex(em, st)
	for i := 0; i < 5; i++ {
		_, err := idx.Upsert(context.Background(), IngestRequest{
			Source: "msg", SourceID: idForIndexLocal(i),
			SessionID: "s-1", Text: idForIndexLocal(i),
		})
		if err != nil {
			t.Fatalf("seed[%d]: %v", i, err)
		}
	}
	hits, err := idx.Search(context.Background(), SearchRequest{
		Query: "q", SessionID: "s-1", K: 2,
	})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(hits) > 2 {
		t.Errorf("K=2 not respected, got %d hits", len(hits))
	}
}

func TestIndex_Search_MinScoreFilter(t *testing.T) {
	t.Parallel()
	em := &fakeEmbedder{model: "m", dim: 2, build: func(text string) []float32 {
		// All seeds orthogonal to the query → score 0.
		if text == "q" {
			return []float32{1, 0}
		}
		return []float32{0, 1}
	}}
	st := &fakeStore{}
	idx, _ := NewIndex(em, st)
	if _, err := idx.Upsert(context.Background(), IngestRequest{
		Source: "msg", SourceID: "id-1", SessionID: "s", Text: "orthogonal",
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	hits, err := idx.Search(context.Background(), SearchRequest{
		Query: "q", SessionID: "s", K: 5, MinScore: 0.1,
	})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(hits) != 0 {
		t.Errorf("expected 0 hits under MinScore filter, got %d", len(hits))
	}
}

func TestIndex_Search_FiltersByModel(t *testing.T) {
	t.Parallel()
	st := &fakeStore{}
	emA := &fakeEmbedder{model: "model-A", dim: 2}
	emB := &fakeEmbedder{model: "model-B", dim: 2}
	idxA, _ := NewIndex(emA, st)
	idxB, _ := NewIndex(emB, st)

	// Seed under model-A.
	if _, err := idxA.Upsert(context.Background(), IngestRequest{
		Source: "msg", SourceID: "id-1", SessionID: "s", Text: "hello",
	}); err != nil {
		t.Fatalf("seed model-A: %v", err)
	}
	// Seed same text under model-B.
	if _, err := idxB.Upsert(context.Background(), IngestRequest{
		Source: "msg", SourceID: "id-1", SessionID: "s", Text: "hello",
	}); err != nil {
		t.Fatalf("seed model-B: %v", err)
	}

	// Search via model-B should ONLY see model-B rows. Cross-model
	// scoring is meaningless and must be filtered out.
	hits, err := idxB.Search(context.Background(), SearchRequest{
		Query: "hello", SessionID: "s", K: 10,
	})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	for _, h := range hits {
		if h.Embedding.Model != "model-B" {
			t.Errorf("cross-model leak: hit Model = %q", h.Embedding.Model)
		}
	}
}

func TestIndex_Search_ValidationErrors(t *testing.T) {
	t.Parallel()
	idx, _ := NewIndex(&fakeEmbedder{model: "m", dim: 2}, &fakeStore{})
	cases := []SearchRequest{
		{SessionID: "s"}, // empty query
		{Query: "q"},     // empty session
	}
	for _, c := range cases {
		if _, err := idx.Search(context.Background(), c); err == nil {
			t.Errorf("expected error for req %+v", c)
		}
	}
}

func TestIndex_Search_EmptySession(t *testing.T) {
	t.Parallel()
	idx, _ := NewIndex(&fakeEmbedder{model: "m", dim: 2}, &fakeStore{})
	hits, err := idx.Search(context.Background(), SearchRequest{
		Query: "q", SessionID: "no-such-session", K: 5,
	})
	if err != nil {
		t.Errorf("err = %v, want nil for empty session", err)
	}
	if len(hits) != 0 {
		t.Errorf("len = %d, want 0", len(hits))
	}
}

// ---------------------------------------------------------------------------
// Tests — integration with real *storage.DB
// ---------------------------------------------------------------------------

func TestIndex_Integration_RealStorage(t *testing.T) {
	// Real SQLite via the schema/migration the codec/storage tests
	// exercise. Ensures the storage.Embedding round-trip (including
	// sql.NullString session_id and BLOB vector) survives Index's
	// pack/unpack pipeline.
	dir := t.TempDir()
	db, err := storage.Open(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatalf("storage.Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	em := &fakeEmbedder{model: "m-real", dim: 4, build: func(text string) []float32 {
		switch text {
		case "alpha":
			return []float32{1, 0, 0, 0}
		case "beta":
			return []float32{0, 1, 0, 0}
		case "query":
			return []float32{0.9, 0.1, 0, 0}
		}
		return []float32{0, 0, 0, 1}
	}}
	idx, err := NewIndex(em, db)
	if err != nil {
		t.Fatalf("NewIndex: %v", err)
	}

	// Seed.
	for i, txt := range []string{"alpha", "beta"} {
		r, err := idx.Upsert(context.Background(), IngestRequest{
			Source: "msg", SourceID: idForIndexLocal(i),
			SessionID: "sess-int", Text: txt,
		})
		if err != nil {
			t.Fatalf("Upsert %s: %v", txt, err)
		}
		if r.Cached {
			t.Errorf("Upsert %s reported Cached on first write", txt)
		}
		if r.Embedding.ID == 0 {
			t.Errorf("Upsert %s: embedding ID not assigned", txt)
		}
		if !r.Embedding.SessionID.Valid {
			t.Errorf("Upsert %s: session_id not persisted", txt)
		}
	}

	// Cache hit on re-ingest of identical text under same model.
	em.calls = 0 // reset
	r, err := idx.Upsert(context.Background(), IngestRequest{
		Source: "msg", SourceID: "id-dup", SessionID: "sess-int", Text: "alpha",
	})
	if err != nil {
		t.Fatalf("re-ingest: %v", err)
	}
	if !r.Cached {
		t.Error("re-ingest expected Cached=true")
	}
	if em.callCount() != 0 {
		t.Errorf("embedder called on cache hit: %d", em.callCount())
	}

	// Search.
	hits, err := idx.Search(context.Background(), SearchRequest{
		Query: "query", SessionID: "sess-int", K: 5,
	})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(hits) != 2 {
		t.Fatalf("hits = %d, want 2", len(hits))
	}
	if hits[0].Embedding.Text != "alpha" {
		t.Errorf("top hit = %q, want alpha", hits[0].Embedding.Text)
	}

	// Confirm sql.NullString survived the round-trip.
	if !hits[0].Embedding.SessionID.Valid ||
		hits[0].Embedding.SessionID.String != "sess-int" {
		t.Errorf("session_id lost in round-trip: %+v", hits[0].Embedding.SessionID)
	}
	_ = sql.ErrNoRows // keep import live in case of future error-mapping work
}

// idForIndexLocal mirrors the helper in rag_test.go (the storage
// package), kept local because rag_test.go's idForIndex is in a
// different package.
func idForIndexLocal(i int) string {
	return [...]string{"a", "b", "c", "d", "e", "f"}[i%6]
}
