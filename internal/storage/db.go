// Package storage manages the SQLite database for Pan-Agent session history.
// Pan-Agent owns this database (unlike the old read-only predecessor
// Agent's DB). The database lives at the path provided to Open; callers
// typically use paths.StateDB() to obtain that path.
//
// The driver is modernc.org/sqlite — a pure-Go port that requires no CGo and
// no pre-installed SQLite shared library. Add it to the module with:
//
//	go get modernc.org/sqlite
package storage

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"time"

	_ "modernc.org/sqlite" // register the "sqlite" driver
)

// DB wraps a *sql.DB and provides all persistence operations for Pan-Agent.
type DB struct {
	db *sql.DB
}

// Open opens (or creates) the SQLite database at dbPath and runs the schema
// migration so that all required tables exist. The caller must call Close when
// done.
func Open(dbPath string) (*DB, error) {
	sqlDB, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("storage.Open: %w", err)
	}

	// SQLite performs best with a single writer; the default pool of 1 is fine.
	sqlDB.SetMaxOpenConns(1)

	d := &DB{db: sqlDB}
	if err := d.migrate(); err != nil {
		_ = sqlDB.Close()
		return nil, fmt.Errorf("storage.Open migrate: %w", err)
	}
	// M4 W2: content_hash column + lookup index. Runs after the base
	// migration so tables exist. Self-resuming (WHERE content_hash IS NULL
	// predicate) — safe to interrupt and re-run.
	if err := d.migrateOfficeContentHash(); err != nil {
		_ = sqlDB.Close()
		return nil, fmt.Errorf("storage.Open migrateOfficeContentHash: %w", err)
	}
	// M5 (0.4.2): messages_fts was historically declared `content=messages`
	// but the code inserts rowid+content manually — the external-content
	// contract requires triggers we never installed, so session-cascade
	// deletes orphaned the FTS index. Detect the old shape and rebuild as
	// a plain (contentless-style) FTS5 table.
	if err := d.migrateMessagesFTS(); err != nil {
		_ = sqlDB.Close()
		return nil, fmt.Errorf("storage.Open migrateMessagesFTS: %w", err)
	}
	if err := d.migrateSessionBudget(); err != nil {
		_ = sqlDB.Close()
		return nil, fmt.Errorf("storage.Open migrateSessionBudget: %w", err)
	}
	return d, nil
}

// migrateMessagesFTS drops the old `content=messages` FTS5 virtual table
// (if present) and recreates it as a plain content-holding FTS5 table,
// repopulating from messages. Idempotent: the probe checks the CREATE
// statement, so re-running on an already-migrated DB is a no-op.
func (d *DB) migrateMessagesFTS() error {
	var createSQL string
	err := d.db.QueryRow(
		`SELECT sql FROM sqlite_master WHERE type='table' AND name='messages_fts'`,
	).Scan(&createSQL)
	if err == sql.ErrNoRows {
		return nil // table not yet created; base migration will handle it
	}
	if err != nil {
		return fmt.Errorf("probe messages_fts: %w", err)
	}
	// Already migrated — no content= clause in the recorded CREATE.
	if !containsCaseInsensitive(createSQL, "content=") {
		return nil
	}
	// Rebuild.
	tx, err := d.db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.Exec(`DROP TABLE messages_fts`); err != nil {
		return fmt.Errorf("drop old fts: %w", err)
	}
	if _, err := tx.Exec(`CREATE VIRTUAL TABLE messages_fts USING fts5(content)`); err != nil {
		return fmt.Errorf("create new fts: %w", err)
	}
	if _, err := tx.Exec(
		`INSERT INTO messages_fts (rowid, content) SELECT id, content FROM messages`,
	); err != nil {
		return fmt.Errorf("repopulate fts: %w", err)
	}
	return tx.Commit()
}

func containsCaseInsensitive(haystack, needle string) bool {
	// Avoid an extra import of strings here — the needle and haystack are
	// both SQL text. Do a simple bytewise case-fold scan.
	if len(needle) == 0 {
		return true
	}
	h := make([]byte, len(haystack))
	for i := 0; i < len(haystack); i++ {
		c := haystack[i]
		if c >= 'A' && c <= 'Z' {
			c += 'a' - 'A'
		}
		h[i] = c
	}
	n := make([]byte, len(needle))
	for i := 0; i < len(needle); i++ {
		c := needle[i]
		if c >= 'A' && c <= 'Z' {
			c += 'a' - 'A'
		}
		n[i] = c
	}
outer:
	for i := 0; i+len(n) <= len(h); i++ {
		for j := 0; j < len(n); j++ {
			if h[i+j] != n[j] {
				continue outer
			}
		}
		return true
	}
	return false
}

// migrateOfficeContentHash is the 0.4.0 M4 W2 schema migration that:
//  1. Adds `content_hash TEXT` column to office_messages (no-op if present).
//  2. Backfills sha256(content) for any rows with NULL content_hash in
//     batches of 500, committing each batch so a crash resumes cleanly.
//  3. Creates a LOOKUP index on (session_id, content_hash) — deliberately
//     NOT UNIQUE, per Gate-2 decision: content is not identity, and
//     uniqueness would force retry-as-same-message to silently drop.
//
// The function is idempotent: repeated calls are a near-free no-op (one
// PRAGMA probe + one SELECT that finds zero rows to backfill).
func (d *DB) migrateOfficeContentHash() error {
	// Step 1: probe for the column. Cheap — table_info is a PRAGMA read.
	var n int
	if err := d.db.QueryRow(
		`SELECT COUNT(*) FROM pragma_table_info('office_messages') WHERE name='content_hash'`,
	).Scan(&n); err != nil {
		return err
	}
	if n == 0 {
		// Add the column nullable so existing rows remain valid during
		// backfill. We never enforce NOT NULL in a separate step because
		// SQLite's ALTER TABLE ADD COLUMN NOT NULL requires a default,
		// and the default would pollute the unique search space.
		if _, err := d.db.Exec(
			`ALTER TABLE office_messages ADD COLUMN content_hash TEXT`,
		); err != nil {
			return fmt.Errorf("add content_hash: %w", err)
		}
	}

	// Step 2: batched backfill. The WHERE IS NULL predicate means any
	// crash mid-backfill resumes from the first unfilled row on next
	// open. 10ms sleep between batches yields to readers during the
	// migration — important on large installs where this could otherwise
	// hold the WAL for seconds. 500-row batches chosen to keep each
	// transaction well under the ~1MB WAL-checkpoint threshold on typical
	// message sizes.
	const batchSize = 500
	for {
		tx, err := d.db.Begin()
		if err != nil {
			return err
		}
		rows, err := tx.Query(
			`SELECT id, content FROM office_messages
			 WHERE content_hash IS NULL LIMIT ?`, batchSize,
		)
		if err != nil {
			_ = tx.Rollback()
			return err
		}
		type pair struct {
			ID   int64
			Hash string
		}
		var batch []pair
		for rows.Next() {
			var id int64
			var content string
			if err := rows.Scan(&id, &content); err != nil {
				rows.Close()
				_ = tx.Rollback()
				return err
			}
			h := sha256.Sum256([]byte(content))
			batch = append(batch, pair{id, hex.EncodeToString(h[:])})
		}
		rows.Close()
		if len(batch) == 0 {
			_ = tx.Rollback() // nothing to commit, release the tx
			break
		}
		for _, b := range batch {
			if _, err := tx.Exec(
				`UPDATE office_messages SET content_hash=? WHERE id=?`,
				b.Hash, b.ID,
			); err != nil {
				_ = tx.Rollback()
				return err
			}
		}
		if err := tx.Commit(); err != nil {
			return err
		}
		time.Sleep(10 * time.Millisecond) // yield to concurrent readers
	}

	// Step 3: create the lookup index (non-unique per Gate-2).
	_, err := d.db.Exec(
		`CREATE INDEX IF NOT EXISTS office_messages_content_hash_idx
		 ON office_messages(session_id, content_hash)`,
	)
	return err
}

// migrateSessionBudget adds per-session cost budget columns (v0.5.0).
// Each column is probed individually so a partial-failure (crash between
// ALTER statements) recovers cleanly on the next open.
func (d *DB) migrateSessionBudget() error {
	cols := []struct {
		name string
		ddl  string
	}{
		{"token_budget_used", `ALTER TABLE sessions ADD COLUMN token_budget_used INTEGER DEFAULT 0`},
		{"token_budget_cap", `ALTER TABLE sessions ADD COLUMN token_budget_cap  INTEGER DEFAULT 0`},
		{"cost_used_usd", `ALTER TABLE sessions ADD COLUMN cost_used_usd     REAL    DEFAULT 0`},
		{"cost_cap_usd", `ALTER TABLE sessions ADD COLUMN cost_cap_usd      REAL    DEFAULT 0`},
	}
	for _, c := range cols {
		var n int
		if err := d.db.QueryRow(
			`SELECT COUNT(*) FROM pragma_table_info('sessions') WHERE name=?`, c.name,
		).Scan(&n); err != nil {
			return err
		}
		if n > 0 {
			continue
		}
		if _, err := d.db.Exec(c.ddl); err != nil {
			return fmt.Errorf("migrateSessionBudget(%s): %w", c.name, err)
		}
	}
	return nil
}

// Close releases the underlying database connection.
func (d *DB) Close() error {
	return d.db.Close()
}

// RawDB returns the underlying *sql.DB. Used by packages (e.g. internal/recovery)
// that need direct SQL access on the same connection pool without going through
// the storage.DB facade.
func (d *DB) RawDB() *sql.DB {
	return d.db
}

// migrate creates all required tables and indexes if they do not already exist.
// It is safe to call on an existing database — every statement uses IF NOT EXISTS.
func (d *DB) migrate() error {
	const schema = `
PRAGMA journal_mode = WAL;
PRAGMA foreign_keys = ON;

CREATE TABLE IF NOT EXISTS sessions (
    id                TEXT    PRIMARY KEY,
    source            TEXT    DEFAULT 'pan-agent',
    started_at        INTEGER NOT NULL,
    ended_at          INTEGER,
    message_count     INTEGER DEFAULT 0,
    model             TEXT,
    title             TEXT,
    token_budget_used INTEGER DEFAULT 0,
    token_budget_cap  INTEGER DEFAULT 0,
    cost_used_usd     REAL    DEFAULT 0,
    cost_cap_usd      REAL    DEFAULT 0
);

CREATE TABLE IF NOT EXISTS messages (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    session_id TEXT    NOT NULL REFERENCES sessions(id),
    role       TEXT    NOT NULL,
    content    TEXT,
    timestamp  INTEGER NOT NULL
);

CREATE INDEX IF NOT EXISTS messages_session_idx ON messages(session_id);

-- messages_fts: plain content-holding FTS5. We previously declared this
-- with content=messages + content_rowid=id (external-content) but the
-- code never installed the required triggers, so cascade-deletes orphaned
-- the index. M5 migration (migrateMessagesFTS) rebuilds existing DBs with
-- this shape; new DBs start here directly.
CREATE VIRTUAL TABLE IF NOT EXISTS messages_fts USING fts5(content);

CREATE TABLE IF NOT EXISTS skill_usage (
    id           INTEGER PRIMARY KEY AUTOINCREMENT,
    session_id   TEXT    NOT NULL REFERENCES sessions(id),
    skill_id     TEXT    NOT NULL,
    message_id   INTEGER,
    used_at      INTEGER NOT NULL,
    outcome      TEXT    DEFAULT 'unknown',
    context_hint TEXT
);

CREATE INDEX IF NOT EXISTS skill_usage_skill_idx   ON skill_usage(skill_id);
CREATE INDEX IF NOT EXISTS skill_usage_session_idx ON skill_usage(session_id);
CREATE INDEX IF NOT EXISTS skill_usage_used_at_idx ON skill_usage(used_at DESC);

-- ---------------------------------------------------------------------------
-- Claw3D Office tables (Option A M2) — backing store for the native Go adapter
-- that replaces the Node.js hermes-gateway-adapter.js. These tables persist
-- state the Node adapter previously kept in ~/.hermes/clawd3d-history.json
-- plus in-memory Maps. Migration tool (cmd/pan-agent migrate-office) lifts
-- legacy JSON into these tables idempotently.
-- ---------------------------------------------------------------------------

CREATE TABLE IF NOT EXISTS office_agents (
    id            TEXT    PRIMARY KEY,
    name          TEXT    NOT NULL,
    workspace     TEXT,
    identity_json TEXT,
    role          TEXT,
    created_at    INTEGER NOT NULL,
    updated_at    INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS office_sessions (
    id            TEXT    PRIMARY KEY,
    agent_id      TEXT    NOT NULL REFERENCES office_agents(id) ON DELETE CASCADE,
    state         TEXT    DEFAULT 'idle',
    settings_json TEXT,
    created_at    INTEGER NOT NULL,
    updated_at    INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS office_messages (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    session_id TEXT    NOT NULL REFERENCES office_sessions(id) ON DELETE CASCADE,
    role       TEXT    NOT NULL,
    content    TEXT    NOT NULL,
    created_at INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS office_messages_session_idx
    ON office_messages(session_id, created_at);

CREATE TABLE IF NOT EXISTS office_cron (
    id           TEXT    PRIMARY KEY,
    name         TEXT    NOT NULL,
    schedule     TEXT    NOT NULL,
    payload_json TEXT    NOT NULL,
    enabled      INTEGER NOT NULL DEFAULT 1,
    last_run     INTEGER
);

CREATE TABLE IF NOT EXISTS office_audit (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    ts            INTEGER NOT NULL,
    actor         TEXT,
    method        TEXT    NOT NULL,
    params_digest TEXT,
    result        TEXT
);
CREATE INDEX IF NOT EXISTS office_audit_ts_idx ON office_audit(ts DESC);

-- ---------------------------------------------------------------------------
-- Phase 12 WS#4 — durable task runner
-- ---------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS tasks (
    id                   TEXT    PRIMARY KEY,
    plan_json            TEXT,
    status               TEXT    NOT NULL DEFAULT 'queued',
    session_id           TEXT    NOT NULL REFERENCES sessions(id),
    created_at           INTEGER NOT NULL,
    last_heartbeat_at    INTEGER,
    next_plan_step_index INTEGER DEFAULT 0,
    token_budget_cap     INTEGER DEFAULT 0,
    cost_cap_usd         REAL    DEFAULT 0
);
CREATE INDEX IF NOT EXISTS idx_tasks_session_created
    ON tasks(session_id, created_at);

CREATE TABLE IF NOT EXISTS task_events (
    id           INTEGER PRIMARY KEY AUTOINCREMENT,
    task_id      TEXT    NOT NULL,
    step_id      TEXT    NOT NULL,
    attempt      INTEGER NOT NULL DEFAULT 1,
    sequence     INTEGER NOT NULL,
    kind         TEXT    NOT NULL,
    payload_json TEXT,
    created_at   INTEGER NOT NULL,
    FOREIGN KEY (task_id) REFERENCES tasks(id),
    UNIQUE (task_id, step_id, kind, attempt)
);
CREATE INDEX IF NOT EXISTS idx_task_events_task_seq
    ON task_events(task_id, sequence);

-- ---------------------------------------------------------------------------
-- Phase 12 WS#2 — action_receipts (append-only action journal)
-- kind:             'fs_write'|'fs_delete'|'shell'|'browser_form'|'saas_api'
-- snapshot_tier:    'cow'|'copyfs'|'audit_only'
-- reversal_status:  'reversible'|'audit_only'|'reversed_externally'|'irrecoverable'
-- redacted_payload: HMAC-masked via internal/secret before write — raw bytes
--                   never reach this column.
-- ---------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS action_receipts (
    id               TEXT    PRIMARY KEY,
    task_id          TEXT    NOT NULL,
    event_id         INTEGER,
    kind             TEXT    NOT NULL,
    snapshot_tier    TEXT    NOT NULL,
    reversal_status  TEXT    NOT NULL,
    redacted_payload TEXT,
    saas_deep_link   TEXT,
    created_at       INTEGER NOT NULL,
    FOREIGN KEY (task_id)  REFERENCES tasks(id),
    FOREIGN KEY (event_id) REFERENCES task_events(id)
);
CREATE INDEX IF NOT EXISTS idx_action_receipts_task_created
    ON action_receipts(task_id, created_at);
`
	_, err := d.db.Exec(schema)
	if err != nil {
		return fmt.Errorf("migrate exec: %w", err)
	}
	return nil
}
