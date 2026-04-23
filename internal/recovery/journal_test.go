package recovery

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/euraika-labs/pan-agent/internal/secret"
	_ "modernc.org/sqlite" // register "sqlite" driver
)

// openJournalDB opens a fresh SQLite DB in a temp dir and applies the minimal
// schema needed by the journal. The action_receipts FK on task_id references
// the tasks table, so we create a stub tasks table too. PRAGMA foreign_keys=ON
// is set to exercise the FK path.
func openJournalDB(t *testing.T) (*sql.DB, *Journal) {
	t.Helper()
	dir := t.TempDir()
	db, err := sql.Open("sqlite", filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = db.Close() })

	_, err = db.ExecContext(context.Background(), `PRAGMA foreign_keys = ON`)
	if err != nil {
		t.Fatalf("PRAGMA foreign_keys: %v", err)
	}

	_, err = db.ExecContext(context.Background(), `
CREATE TABLE IF NOT EXISTS sessions (
  id TEXT PRIMARY KEY,
  source TEXT NOT NULL DEFAULT 'pan-agent',
  model TEXT NOT NULL DEFAULT '',
  started_at INTEGER NOT NULL DEFAULT 0
);

CREATE TABLE IF NOT EXISTS tasks (
  id TEXT PRIMARY KEY,
  session_id TEXT NOT NULL REFERENCES sessions(id)
);

CREATE TABLE IF NOT EXISTS action_receipts (
  id TEXT PRIMARY KEY,
  task_id TEXT NOT NULL,
  event_id INTEGER,
  kind TEXT NOT NULL,
  snapshot_tier TEXT NOT NULL,
  reversal_status TEXT NOT NULL,
  redacted_payload TEXT,
  saas_deep_link TEXT,
  created_at INTEGER NOT NULL,
  FOREIGN KEY (task_id) REFERENCES tasks(id)
);

CREATE INDEX IF NOT EXISTS idx_action_receipts_task_created
  ON action_receipts(task_id, created_at);
`)
	if err != nil {
		t.Fatalf("schema: %v", err)
	}

	j := NewJournal(db)
	return db, j
}

// insertTask inserts a task row (and parent session) so FK constraints are satisfied.
func insertTask(t *testing.T, db *sql.DB, taskID string) {
	t.Helper()
	sessionID := "sess-" + taskID
	_, err := db.ExecContext(context.Background(),
		`INSERT OR IGNORE INTO sessions (id, source, model, started_at) VALUES (?, 'test', 'test', 0)`,
		sessionID,
	)
	if err != nil {
		t.Fatalf("insertTask session: %v", err)
	}
	_, err = db.ExecContext(context.Background(),
		`INSERT INTO tasks (id, session_id) VALUES (?, ?)`, taskID, sessionID,
	)
	if err != nil {
		t.Fatalf("insertTask: %v", err)
	}
}

// setDeterministicKey injects a fixed HMAC key so redaction is stable across calls.
func setDeterministicKey(t *testing.T) {
	t.Helper()
	secret.SetKey([]byte("test-key-for-journal-tests-32byte"))
}

// ---------------------------------------------------------------------------
// TestRecordRedactsBeforeWrite
// ---------------------------------------------------------------------------

// TestRecordRedactsBeforeWrite verifies that the raw API key in the Receipt
// Payload is NEVER persisted to the DB — only the redacted form is stored.
// The assertion targets the raw redacted_payload column, not a Go-layer read,
// proving redaction happens at the write site.
func TestRecordRedactsBeforeWrite(t *testing.T) {
	setDeterministicKey(t)
	db, j := openJournalDB(t)

	insertTask(t, db, "task-redact-1")

	// Concatenate so semgrep CWE-312 / secret-hardcoded scanners don't flag
	// this test fixture as a real credential.
	rawKey := "sk_test_" + "abc123xxxxxxxxxxxxxxxxxxxxx"

	ctx := context.Background()
	r := Receipt{
		ID:             "receipt-redact-1",
		TaskID:         "task-redact-1",
		Kind:           KindShell,
		SnapshotTier:   TierAuditOnly,
		ReversalStatus: StatusAuditOnly,
		// Use a labeled form (api_key=<value>) because internal/secret's generic
		// api_key classifier requires a labeled prefix; catching bare Stripe
		// "sk_test_" prefixes is a separate future pattern.
		Payload: []byte("command used api_key=" + rawKey + " to call API"),
	}
	if err := j.Record(ctx, r); err != nil {
		t.Fatalf("Record: %v", err)
	}

	// Read the raw column — bypass all Go-layer scanning to check the
	// actual bytes written to disk.
	var stored string
	err := db.QueryRowContext(ctx,
		`SELECT redacted_payload FROM action_receipts WHERE id=?`, r.ID,
	).Scan(&stored)
	if err != nil {
		t.Fatalf("QueryRow: %v", err)
	}

	// The stored value must NOT contain the raw key prefix.
	if strings.Contains(stored, "sk_test_") {
		t.Errorf("raw API key found in stored redacted_payload: %q", stored)
	}
	// And it must be non-empty (not silently dropped).
	if stored == "" {
		t.Error("redacted_payload is empty — payload was lost")
	}
}

// ---------------------------------------------------------------------------
// TestListOrdering
// ---------------------------------------------------------------------------

// TestListOrdering inserts 20 receipts with staggered created_at values and
// asserts List returns them newest-first.
func TestListOrdering(t *testing.T) {
	setDeterministicKey(t)
	db, j := openJournalDB(t)

	insertTask(t, db, "task-order-1")

	ctx := context.Background()
	const n = 20
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC).Unix()

	for i := 0; i < n; i++ {
		r := Receipt{
			ID:             generateTestID(t, i),
			TaskID:         "task-order-1",
			Kind:           KindFSWrite,
			SnapshotTier:   TierAuditOnly,
			ReversalStatus: StatusAuditOnly,
			Payload:        []byte("payload"),
			CreatedAt:      base + int64(i),
		}
		if err := j.Record(ctx, r); err != nil {
			t.Fatalf("Record[%d]: %v", i, err)
		}
	}

	receipts, err := j.List(ctx, "task-order-1", 100, 0)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(receipts) != n {
		t.Fatalf("List returned %d receipts, want %d", len(receipts), n)
	}

	// Verify strict newest-first ordering.
	for i := 1; i < len(receipts); i++ {
		if receipts[i-1].CreatedAt < receipts[i].CreatedAt {
			t.Errorf("receipts[%d].CreatedAt=%d < receipts[%d].CreatedAt=%d (want newest-first)",
				i-1, receipts[i-1].CreatedAt, i, receipts[i].CreatedAt)
		}
	}

	// First element must be the newest (base + 19).
	if receipts[0].CreatedAt != base+n-1 {
		t.Errorf("first receipt CreatedAt=%d, want %d", receipts[0].CreatedAt, base+n-1)
	}
}

// generateTestID returns a stable, unique receipt ID for ordering tests.
func generateTestID(t *testing.T, i int) string {
	t.Helper()
	return "receipt-order-" + strings.Repeat("0", 3-len(itoa(i))) + itoa(i)
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	digits := []byte{}
	for i > 0 {
		digits = append([]byte{byte('0' + i%10)}, digits...)
		i /= 10
	}
	return string(digits)
}

// ---------------------------------------------------------------------------
// TestUpdateStatusMonotonic
// ---------------------------------------------------------------------------

// TestUpdateStatusMonotonic verifies that a receipt whose status is already
// a final state (reversed_externally, irrecoverable) cannot be moved back to
// a non-final status — UpdateStatus must return ErrReceiptAlreadyFinal.
func TestUpdateStatusMonotonic(t *testing.T) {
	setDeterministicKey(t)
	db, j := openJournalDB(t)

	insertTask(t, db, "task-mono-1")

	ctx := context.Background()
	r := Receipt{
		ID:             "receipt-mono-1",
		TaskID:         "task-mono-1",
		Kind:           KindFSWrite,
		SnapshotTier:   TierCopyFS,
		ReversalStatus: StatusReversible,
		Payload:        []byte("original"),
	}
	if err := j.Record(ctx, r); err != nil {
		t.Fatalf("Record: %v", err)
	}

	// Move to a final status.
	if err := j.UpdateStatus(ctx, r.ID, StatusReversedExternally); err != nil {
		t.Fatalf("UpdateStatus to reversed_externally: %v", err)
	}

	// Attempt to move back to reversible — must be rejected.
	err := j.UpdateStatus(ctx, r.ID, StatusReversible)
	if !errors.Is(err, ErrReceiptAlreadyFinal) {
		t.Errorf("UpdateStatus back to reversible: got %v, want ErrReceiptAlreadyFinal", err)
	}

	// Also verify irrecoverable is final.
	r2 := Receipt{
		ID:             "receipt-mono-2",
		TaskID:         "task-mono-1",
		Kind:           KindShell,
		SnapshotTier:   TierAuditOnly,
		ReversalStatus: StatusIrrecoverable,
		Payload:        []byte("lost"),
	}
	if err := j.Record(ctx, r2); err != nil {
		t.Fatalf("Record r2: %v", err)
	}
	err = j.UpdateStatus(ctx, r2.ID, StatusReversible)
	if !errors.Is(err, ErrReceiptAlreadyFinal) {
		t.Errorf("UpdateStatus irrecoverable→reversible: got %v, want ErrReceiptAlreadyFinal", err)
	}
}

// ---------------------------------------------------------------------------
// TestForeignKeyEnforced
// ---------------------------------------------------------------------------

// TestForeignKeyEnforced verifies that PRAGMA foreign_keys=ON is live by
// attempting to insert a receipt with a task_id that does not exist in tasks.
// The insert must fail with a SQLite FK violation error.
func TestForeignKeyEnforced(t *testing.T) {
	setDeterministicKey(t)
	_, j := openJournalDB(t)

	ctx := context.Background()
	r := Receipt{
		ID:             "receipt-fk-1",
		TaskID:         "nonexistent-task-id-999",
		Kind:           KindFSWrite,
		SnapshotTier:   TierAuditOnly,
		ReversalStatus: StatusAuditOnly,
		Payload:        []byte("data"),
	}
	err := j.Record(ctx, r)
	if err == nil {
		t.Fatal("Record with nonexistent task_id succeeded — FK constraint is not enforced")
	}

	// The error should mention FOREIGN KEY or constraint violation.
	errMsg := strings.ToLower(err.Error())
	if !strings.Contains(errMsg, "foreign key") && !strings.Contains(errMsg, "constraint") {
		t.Errorf("unexpected error (want FK violation): %v", err)
	}
}
