package storage

import (
	"path/filepath"
	"testing"
)

// Phase 13 WS#13.B — schema migration tests for rag_embeddings + rag_state.
// The migration is the substrate; the embedder/index/search work lands in a
// follow-up. Tests cover three shapes:
//
//   1. Fresh DB: tables exist with the expected columns after Open().
//   2. Idempotency: re-running migrateRAGEmbeddings on a populated DB is
//      a no-op (no duplicate rows, no constraint surprises).
//   3. Upgrade path: simulate a v0.6.0-shape DB where the rag tables don't
//      exist yet, then call migrateRAGEmbeddings explicitly — it must
//      create them without disturbing the existing data.

// expectedRAGEmbeddingsCols mirrors db.go schema literal exactly.
var expectedRAGEmbeddingsCols = map[string]bool{
	"id":           false,
	"source":       false,
	"source_id":    false,
	"session_id":   false,
	"content_hash": false,
	"text":         false,
	"model":        false,
	"dim":          false,
	"vector":       false,
	"created_at":   false,
}

func tableExists(t *testing.T, db *DB, name string) bool {
	t.Helper()
	var count int
	err := db.db.QueryRow(
		`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name=?`,
		name,
	).Scan(&count)
	if err != nil {
		t.Fatalf("tableExists query: %v", err)
	}
	return count == 1
}

func indexExists(t *testing.T, db *DB, name string) bool {
	t.Helper()
	var count int
	err := db.db.QueryRow(
		`SELECT COUNT(*) FROM sqlite_master WHERE type='index' AND name=?`,
		name,
	).Scan(&count)
	if err != nil {
		t.Fatalf("indexExists query: %v", err)
	}
	return count == 1
}

func TestRAGEmbeddings_FreshDB(t *testing.T) {
	db := openTestDB(t)

	if !tableExists(t, db, "rag_embeddings") {
		t.Fatal("rag_embeddings table missing on fresh DB")
	}
	if !tableExists(t, db, "rag_state") {
		t.Fatal("rag_state table missing on fresh DB")
	}
	if !indexExists(t, db, "idx_rag_session") {
		t.Error("idx_rag_session index missing")
	}
	if !indexExists(t, db, "idx_rag_hash") {
		t.Error("idx_rag_hash index missing")
	}

	rows, err := db.db.Query(`PRAGMA table_info(rag_embeddings)`)
	if err != nil {
		t.Fatalf("PRAGMA table_info: %v", err)
	}
	defer rows.Close()
	got := map[string]bool{}
	for rows.Next() {
		var cid int
		var name, ctype string
		var notnull, pk int
		var dflt any
		if err := rows.Scan(&cid, &name, &ctype, &notnull, &dflt, &pk); err != nil {
			t.Fatalf("scan: %v", err)
		}
		got[name] = true
	}
	for col := range expectedRAGEmbeddingsCols {
		if !got[col] {
			t.Errorf("rag_embeddings missing column %q", col)
		}
	}
}

func TestRAGEmbeddings_Idempotent(t *testing.T) {
	db := openTestDB(t)

	if _, err := db.db.Exec(
		`INSERT INTO rag_embeddings
		 (source, source_id, content_hash, text, model, dim, vector, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		"test", "id-1", "hash-1", "hello", "test-model", 4,
		[]byte{0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0}, 1700000000,
	); err != nil {
		t.Fatalf("seed insert: %v", err)
	}
	if _, err := db.db.Exec(
		`INSERT INTO rag_state (key, value) VALUES (?, ?)`,
		"last_indexed_event_id", "42",
	); err != nil {
		t.Fatalf("seed rag_state: %v", err)
	}

	// Re-run the migration in isolation. Must not error and must not
	// touch existing rows.
	for i := 0; i < 3; i++ {
		if err := db.migrateRAGEmbeddings(); err != nil {
			t.Fatalf("migrateRAGEmbeddings re-run #%d: %v", i, err)
		}
	}

	var n int
	if err := db.db.QueryRow(`SELECT COUNT(*) FROM rag_embeddings`).Scan(&n); err != nil {
		t.Fatalf("count rag_embeddings: %v", err)
	}
	if n != 1 {
		t.Errorf("rag_embeddings row count = %d, want 1 (migration ate or duplicated rows)", n)
	}

	var v string
	if err := db.db.QueryRow(
		`SELECT value FROM rag_state WHERE key='last_indexed_event_id'`,
	).Scan(&v); err != nil {
		t.Fatalf("read rag_state: %v", err)
	}
	if v != "42" {
		t.Errorf("rag_state value = %q, want %q", v, "42")
	}
}

func TestRAGEmbeddings_UpgradePath(t *testing.T) {
	// Simulate a v0.6.0-shape DB by opening, dropping the rag tables,
	// then calling migrateRAGEmbeddings — the path an existing user's
	// DB takes when they upgrade to the WS#13.B build.
	dir := t.TempDir()
	path := filepath.Join(dir, "upgrade.db")
	db, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	// Seed an existing session so we can verify the migration doesn't
	// touch unrelated data.
	s, err := db.CreateSession("test-model")
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	// Drop the rag tables to mimic a pre-WS#13.B DB.
	for _, ddl := range []string{
		`DROP INDEX IF EXISTS idx_rag_session`,
		`DROP INDEX IF EXISTS idx_rag_hash`,
		`DROP TABLE IF EXISTS rag_embeddings`,
		`DROP TABLE IF EXISTS rag_state`,
	} {
		if _, err := db.db.Exec(ddl); err != nil {
			t.Fatalf("simulate v0.6.0 (%s): %v", ddl, err)
		}
	}
	if tableExists(t, db, "rag_embeddings") {
		t.Fatal("setup: rag_embeddings still exists after DROP")
	}

	// Run only the upgrade migration (not the full migrate()).
	if err := db.migrateRAGEmbeddings(); err != nil {
		t.Fatalf("migrateRAGEmbeddings upgrade: %v", err)
	}

	if !tableExists(t, db, "rag_embeddings") {
		t.Error("rag_embeddings not created on upgrade")
	}
	if !tableExists(t, db, "rag_state") {
		t.Error("rag_state not created on upgrade")
	}
	if !indexExists(t, db, "idx_rag_session") {
		t.Error("idx_rag_session not created on upgrade")
	}
	if !indexExists(t, db, "idx_rag_hash") {
		t.Error("idx_rag_hash not created on upgrade")
	}

	// Pre-existing session must survive the upgrade.
	got, err := db.GetSession(s.ID)
	if err != nil {
		t.Fatalf("GetSession after upgrade: %v", err)
	}
	if got.ID != s.ID {
		t.Errorf("session ID changed: %q vs %q", got.ID, s.ID)
	}

	// Migration must satisfy UNIQUE(source, source_id, model).
	if _, err := db.db.Exec(
		`INSERT INTO rag_embeddings
		 (source, source_id, content_hash, text, model, dim, vector, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		"msg", "abc", "h", "hi", "m", 1, []byte{0, 0, 0, 0}, 1,
	); err != nil {
		t.Fatalf("post-upgrade insert: %v", err)
	}
	if _, err := db.db.Exec(
		`INSERT INTO rag_embeddings
		 (source, source_id, content_hash, text, model, dim, vector, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		"msg", "abc", "h", "hi", "m", 1, []byte{0, 0, 0, 0}, 2,
	); err == nil {
		t.Error("UNIQUE(source,source_id,model) constraint not enforced after upgrade")
	}

	if err := db.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	// Reopen — full Open() path runs migrateRAGEmbeddings again on a
	// now-populated DB; must succeed without disturbing the row.
	db2, err := Open(path)
	if err != nil {
		t.Fatalf("Reopen: %v", err)
	}
	t.Cleanup(func() { _ = db2.Close() })

	var n int
	if err := db2.db.QueryRow(`SELECT COUNT(*) FROM rag_embeddings`).Scan(&n); err != nil {
		t.Fatalf("count after reopen: %v", err)
	}
	if n != 1 {
		t.Errorf("rag_embeddings rows after reopen = %d, want 1", n)
	}
}
