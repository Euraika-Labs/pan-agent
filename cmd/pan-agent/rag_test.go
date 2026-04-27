package main

import (
	"bytes"
	"database/sql"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/euraika-labs/pan-agent/internal/storage"
)

// captureStdoutRAG runs fn with os.Stdout redirected + returns the
// captured bytes. Local copy of the helper from tools_test.go (both
// PRs land independently; the duplication will dedupe in a follow-up
// once one merges first). Drains the pipe concurrently so a fn that
// writes more than the pipe buffer (4KB on Windows) doesn't deadlock.
func captureStdoutRAG(t *testing.T, fn func() error) (string, error) {
	t.Helper()
	old := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	os.Stdout = w

	var buf bytes.Buffer
	copyDone := make(chan struct{})
	go func() {
		_, _ = io.Copy(&buf, r)
		close(copyDone)
	}()

	runErr := fn()
	_ = w.Close()
	<-copyDone
	os.Stdout = old
	return buf.String(), runErr
}

// Phase 13 WS#13.B — `pan-agent rag` CLI tests.
//
// The aggregation helper ragStatsQuery runs against a real
// *storage.DB so the SQL is exercised end-to-end. Dispatch +
// flag parsing get separate coverage.

// openTempDB returns a fresh storage.DB backed by a per-test
// temp file. PAN_AGENT_HOME is also overridden so any indirect
// path reads point at the temp directory.
func openTempDB(t *testing.T) *storage.DB {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("PAN_AGENT_HOME", dir)
	db, err := storage.Open(filepath.Join(dir, "state.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

// seedEmbedding writes one row into rag_embeddings via the public
// UpsertEmbedding API. ContentHash + SourceID are randomised per
// call so duplicate seeds don't collide on the UNIQUE constraint.
func seedEmbedding(t *testing.T, db *storage.DB, sessionID, model, sourceID string) {
	t.Helper()
	row := storage.Embedding{
		Source: "msg", SourceID: sourceID,
		ContentHash: "hash-" + sourceID,
		Text:        "text-" + sourceID,
		Model:       model,
		Dim:         4,
		Vector:      make([]byte, 16),
	}
	if sessionID != "" {
		row.SessionID = sql.NullString{String: sessionID, Valid: true}
	}
	if err := db.UpsertEmbedding(row); err != nil {
		t.Fatalf("UpsertEmbedding: %v", err)
	}
}

// ---------------------------------------------------------------------------
// dispatch
// ---------------------------------------------------------------------------

func TestCmdRAG_NoAction(t *testing.T) {
	if err := cmdRAG(nil); err == nil {
		t.Error("expected missing-action error")
	}
}

func TestCmdRAG_UnknownAction(t *testing.T) {
	if err := cmdRAG([]string{"banana"}); err == nil ||
		!strings.Contains(err.Error(), "unknown") {
		t.Errorf("got %v, want unknown-action error", err)
	}
}

// ---------------------------------------------------------------------------
// ragStatsQuery (the aggregation helper)
// ---------------------------------------------------------------------------

func TestRAGStatsQuery_Empty(t *testing.T) {
	db := openTempDB(t)
	sessions, models, total, err := ragStatsQuery(db)
	if err != nil {
		t.Fatalf("ragStatsQuery: %v", err)
	}
	if total != 0 {
		t.Errorf("total = %d, want 0", total)
	}
	if len(sessions) != 0 {
		t.Errorf("sessions = %v, want empty", sessions)
	}
	if len(models) != 0 {
		t.Errorf("models = %v, want empty", models)
	}
}

func TestRAGStatsQuery_Aggregates(t *testing.T) {
	db := openTempDB(t)
	// 3 rows in sess-A under model-X
	seedEmbedding(t, db, "sess-A", "model-X", "id-1")
	seedEmbedding(t, db, "sess-A", "model-X", "id-2")
	seedEmbedding(t, db, "sess-A", "model-X", "id-3")
	// 2 rows in sess-B under model-Y
	seedEmbedding(t, db, "sess-B", "model-Y", "id-4")
	seedEmbedding(t, db, "sess-B", "model-Y", "id-5")
	// 1 row with no session under model-Y
	seedEmbedding(t, db, "", "model-Y", "id-6")

	sessions, models, total, err := ragStatsQuery(db)
	if err != nil {
		t.Fatalf("ragStatsQuery: %v", err)
	}
	if total != 6 {
		t.Errorf("total = %d, want 6", total)
	}
	if sessions["sess-A"] != 3 {
		t.Errorf("sess-A = %d, want 3", sessions["sess-A"])
	}
	if sessions["sess-B"] != 2 {
		t.Errorf("sess-B = %d, want 2", sessions["sess-B"])
	}
	if sessions[""] != 1 {
		t.Errorf("(no session) = %d, want 1", sessions[""])
	}
	if models["model-X"] != 3 {
		t.Errorf("model-X = %d, want 3", models["model-X"])
	}
	if models["model-Y"] != 3 {
		t.Errorf("model-Y = %d, want 3", models["model-Y"])
	}
}

// ---------------------------------------------------------------------------
// stats output
// ---------------------------------------------------------------------------

func TestCmdRAGStats_EmptyDB(t *testing.T) {
	openTempDB(t)
	out, err := captureStdoutRAG(t, func() error {
		return cmdRAGStats(nil)
	})
	if err != nil {
		t.Fatalf("stats: %v", err)
	}
	if !strings.Contains(out, "0 row") {
		t.Errorf("output should mention 0 rows: %s", out)
	}
	if !strings.Contains(out, "PAN_AGENT_RAG_EMBEDDER_URL") {
		t.Errorf("output should hint at the env var: %s", out)
	}
}

func TestCmdRAGStats_PopulatedDB(t *testing.T) {
	db := openTempDB(t)
	seedEmbedding(t, db, "sess-1", "model-A", "x1")
	seedEmbedding(t, db, "sess-1", "model-A", "x2")
	seedEmbedding(t, db, "sess-2", "model-B", "x3")

	out, err := captureStdoutRAG(t, func() error {
		return cmdRAGStats(nil)
	})
	if err != nil {
		t.Fatalf("stats: %v", err)
	}
	if !strings.Contains(out, "3 row") {
		t.Errorf("expected '3 row' in output: %s", out)
	}
	if !strings.Contains(out, "By model:") {
		t.Errorf("expected 'By model:' header: %s", out)
	}
	if !strings.Contains(out, "model-A") || !strings.Contains(out, "model-B") {
		t.Errorf("expected both models listed: %s", out)
	}
	if !strings.Contains(out, "sess-1") || !strings.Contains(out, "sess-2") {
		t.Errorf("expected both sessions listed: %s", out)
	}
}

// ---------------------------------------------------------------------------
// purge
// ---------------------------------------------------------------------------

func TestCmdRAGPurge_RequiresSession(t *testing.T) {
	openTempDB(t)
	if err := cmdRAGPurge(nil); err == nil ||
		!strings.Contains(err.Error(), "session required") {
		t.Errorf("got %v, want session-required error", err)
	}
}

func TestCmdRAGPurge_DeletesByID(t *testing.T) {
	db := openTempDB(t)
	seedEmbedding(t, db, "sess-A", "m", "id-1")
	seedEmbedding(t, db, "sess-A", "m", "id-2")
	seedEmbedding(t, db, "sess-B", "m", "id-3")

	out, err := captureStdoutRAG(t, func() error {
		return cmdRAGPurge([]string{"--session=sess-A"})
	})
	if err != nil {
		t.Fatalf("purge: %v", err)
	}
	if !strings.Contains(out, "Deleted 2") {
		t.Errorf("expected 'Deleted 2' in output: %s", out)
	}

	// Confirm sess-B intact.
	rest, err := db.ListEmbeddingsBySession("sess-B")
	if err != nil {
		t.Fatalf("ListEmbeddingsBySession: %v", err)
	}
	if len(rest) != 1 {
		t.Errorf("sess-B affected: %d rows remain", len(rest))
	}
}

func TestCmdRAGPurge_NoMatch(t *testing.T) {
	openTempDB(t)
	out, err := captureStdoutRAG(t, func() error {
		return cmdRAGPurge([]string{"--session=nope"})
	})
	if err != nil {
		t.Fatalf("purge no-match: %v", err)
	}
	if !strings.Contains(out, "Deleted 0") {
		t.Errorf("expected 'Deleted 0': %s", out)
	}
}
