package storage

import (
	"database/sql"
	"errors"
	"testing"
)

// Phase 13 WS#13.B — typed-CRUD tests over the rag_embeddings +
// rag_state schema. Tests own the contract the rag package will rely
// on: Upsert obeys the UNIQUE constraint, GetByHash distinguishes
// model variants, session list/delete are session-scoped, state
// cursor round-trips.

// makeVector returns a 4-byte-per-dim BLOB filled with zero bytes —
// content is irrelevant to these tests, only length and dim matching.
func makeVector(dim int) []byte { return make([]byte, 4*dim) }

func seedEmbedding(t *testing.T, db *DB, source, sourceID, model string) Embedding {
	t.Helper()
	e := Embedding{
		Source:      source,
		SourceID:    sourceID,
		SessionID:   sql.NullString{String: "sess-1", Valid: true},
		ContentHash: "hash-" + sourceID,
		Text:        "text-" + sourceID,
		Model:       model,
		Dim:         8,
		Vector:      makeVector(8),
		CreatedAt:   1700000000,
	}
	if err := db.UpsertEmbedding(e); err != nil {
		t.Fatalf("seedEmbedding(%s/%s/%s): %v", source, sourceID, model, err)
	}
	return e
}

func TestUpsertEmbedding_Insert(t *testing.T) {
	db := openTestDB(t)
	e := seedEmbedding(t, db, "msg", "id-1", "model-A")

	got, err := db.GetEmbeddingByHash(e.ContentHash, e.Model)
	if err != nil {
		t.Fatalf("GetEmbeddingByHash: %v", err)
	}
	if got.SourceID != "id-1" || got.Model != "model-A" {
		t.Errorf("got %+v, mismatched on insert", got)
	}
}

func TestUpsertEmbedding_UpdateOnConflict(t *testing.T) {
	db := openTestDB(t)
	first := seedEmbedding(t, db, "msg", "id-1", "model-A")

	// Same (source, source_id, model) — must update, not insert.
	updated := Embedding{
		Source: "msg", SourceID: "id-1",
		SessionID:   sql.NullString{String: "sess-2", Valid: true},
		ContentHash: "hash-CHANGED",
		Text:        "text-CHANGED", Model: "model-A",
		Dim: 8, Vector: makeVector(8), CreatedAt: 1700000999,
	}
	if err := db.UpsertEmbedding(updated); err != nil {
		t.Fatalf("upsert update: %v", err)
	}

	// Original hash should no longer find a row.
	if _, err := db.GetEmbeddingByHash(first.ContentHash, "model-A"); !errors.Is(err, ErrEmbeddingNotFound) {
		t.Errorf("expected ErrEmbeddingNotFound for old hash, got %v", err)
	}
	got, err := db.GetEmbeddingByHash("hash-CHANGED", "model-A")
	if err != nil {
		t.Fatalf("GetEmbeddingByHash new hash: %v", err)
	}
	if got.Text != "text-CHANGED" || got.SessionID.String != "sess-2" {
		t.Errorf("update did not propagate: %+v", got)
	}

	// Sanity: only ONE row total under (msg, id-1).
	var n int
	if err := db.db.QueryRow(
		`SELECT COUNT(*) FROM rag_embeddings WHERE source='msg' AND source_id='id-1'`,
	).Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 1 {
		t.Errorf("UNIQUE constraint slipped — row count = %d, want 1", n)
	}
}

func TestUpsertEmbedding_DistinctModelsCoexist(t *testing.T) {
	db := openTestDB(t)
	seedEmbedding(t, db, "msg", "id-1", "model-A")
	seedEmbedding(t, db, "msg", "id-1", "model-B")

	// Same content_hash + DIFFERENT models — both rows persist.
	a, err := db.GetEmbeddingByHash("hash-id-1", "model-A")
	if err != nil {
		t.Fatalf("model-A lookup: %v", err)
	}
	b, err := db.GetEmbeddingByHash("hash-id-1", "model-B")
	if err != nil {
		t.Fatalf("model-B lookup: %v", err)
	}
	if a.ID == b.ID {
		t.Errorf("expected distinct rows, got same id %d", a.ID)
	}
}

func TestUpsertEmbedding_ValidationErrors(t *testing.T) {
	db := openTestDB(t)
	cases := []struct {
		name string
		e    Embedding
	}{
		{"empty_source", Embedding{SourceID: "x", Model: "m", Dim: 1, Vector: []byte{0, 0, 0, 0}, ContentHash: "h"}},
		{"empty_source_id", Embedding{Source: "s", Model: "m", Dim: 1, Vector: []byte{0, 0, 0, 0}, ContentHash: "h"}},
		{"empty_model", Embedding{Source: "s", SourceID: "x", Dim: 1, Vector: []byte{0, 0, 0, 0}, ContentHash: "h"}},
		{"dim_zero", Embedding{Source: "s", SourceID: "x", Model: "m", Dim: 0, Vector: []byte{}, ContentHash: "h"}},
		{"dim_blob_mismatch", Embedding{Source: "s", SourceID: "x", Model: "m", Dim: 4, Vector: []byte{0, 0, 0, 0}, ContentHash: "h"}},
		{"empty_hash", Embedding{Source: "s", SourceID: "x", Model: "m", Dim: 1, Vector: []byte{0, 0, 0, 0}, ContentHash: ""}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := db.UpsertEmbedding(tc.e); err == nil {
				t.Error("expected validation error")
			}
		})
	}
}

func TestGetEmbeddingByHash_NotFound(t *testing.T) {
	db := openTestDB(t)
	_, err := db.GetEmbeddingByHash("nope", "model-A")
	if !errors.Is(err, ErrEmbeddingNotFound) {
		t.Errorf("err = %v, want ErrEmbeddingNotFound", err)
	}
}

func TestListEmbeddingsBySession(t *testing.T) {
	db := openTestDB(t)
	// Seed three rows in two sessions.
	for i, sid := range []string{"sess-A", "sess-A", "sess-B"} {
		e := Embedding{
			Source: "msg", SourceID: idForIndex(i),
			SessionID:   sql.NullString{String: sid, Valid: true},
			ContentHash: "h-" + idForIndex(i),
			Text:        "t-" + idForIndex(i),
			Model:       "m", Dim: 4, Vector: makeVector(4),
			CreatedAt: int64(1700000000 + i),
		}
		if err := db.UpsertEmbedding(e); err != nil {
			t.Fatalf("seed[%d]: %v", i, err)
		}
	}

	a, err := db.ListEmbeddingsBySession("sess-A")
	if err != nil {
		t.Fatalf("List sess-A: %v", err)
	}
	if len(a) != 2 {
		t.Errorf("sess-A count = %d, want 2", len(a))
	}
	b, err := db.ListEmbeddingsBySession("sess-B")
	if err != nil {
		t.Fatalf("List sess-B: %v", err)
	}
	if len(b) != 1 {
		t.Errorf("sess-B count = %d, want 1", len(b))
	}

	// Insertion-order ordering.
	if a[0].SourceID > a[1].SourceID {
		t.Errorf("ListEmbeddingsBySession not ASC: %q,%q", a[0].SourceID, a[1].SourceID)
	}
}

func TestDeleteEmbeddingsBySession(t *testing.T) {
	db := openTestDB(t)
	for i, sid := range []string{"sess-A", "sess-A", "sess-B"} {
		e := Embedding{
			Source: "msg", SourceID: idForIndex(i),
			SessionID:   sql.NullString{String: sid, Valid: true},
			ContentHash: "h-" + idForIndex(i),
			Text:        "t-" + idForIndex(i),
			Model:       "m", Dim: 4, Vector: makeVector(4),
			CreatedAt: 1700000000,
		}
		if err := db.UpsertEmbedding(e); err != nil {
			t.Fatalf("seed[%d]: %v", i, err)
		}
	}

	n, err := db.DeleteEmbeddingsBySession("sess-A")
	if err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if n != 2 {
		t.Errorf("deleted = %d, want 2", n)
	}

	// sess-B intact.
	rest, err := db.ListEmbeddingsBySession("sess-B")
	if err != nil {
		t.Fatalf("List sess-B post-delete: %v", err)
	}
	if len(rest) != 1 {
		t.Errorf("sess-B affected by sess-A delete: count = %d", len(rest))
	}
}

func TestRAGState_RoundTrip(t *testing.T) {
	db := openTestDB(t)

	// Unset key returns ok=false, no error.
	v, ok, err := db.GetRAGState("missing")
	if err != nil {
		t.Fatalf("Get missing: %v", err)
	}
	if ok || v != "" {
		t.Errorf("missing key returned ok=%v v=%q, want false/empty", ok, v)
	}

	// Set + Get.
	if err := db.SetRAGState("cursor", "42"); err != nil {
		t.Fatalf("Set: %v", err)
	}
	v, ok, err = db.GetRAGState("cursor")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !ok || v != "42" {
		t.Errorf("got ok=%v v=%q, want true/42", ok, v)
	}

	// Update overwrites.
	if err := db.SetRAGState("cursor", "100"); err != nil {
		t.Fatalf("Set update: %v", err)
	}
	v, _, _ = db.GetRAGState("cursor")
	if v != "100" {
		t.Errorf("after update, v = %q, want 100", v)
	}
}

func TestRAGState_EmptyKeyError(t *testing.T) {
	db := openTestDB(t)
	if _, _, err := db.GetRAGState(""); err == nil {
		t.Error("Get empty key: expected error")
	}
	if err := db.SetRAGState("", "v"); err == nil {
		t.Error("Set empty key: expected error")
	}
}

// idForIndex returns a stable string id for the seed loop. Defined
// here (not as a const) because Go doesn't allow const slices.
func idForIndex(i int) string {
	return [...]string{"a", "b", "c", "d", "e", "f"}[i%6]
}
