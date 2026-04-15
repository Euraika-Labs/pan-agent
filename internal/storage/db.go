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
	return d, nil
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

// Close releases the underlying database connection.
func (d *DB) Close() error {
	return d.db.Close()
}

// migrate creates all required tables and indexes if they do not already exist.
// It is safe to call on an existing database — every statement uses IF NOT EXISTS.
func (d *DB) migrate() error {
	const schema = `
PRAGMA journal_mode = WAL;
PRAGMA foreign_keys = ON;

CREATE TABLE IF NOT EXISTS sessions (
    id            TEXT    PRIMARY KEY,
    source        TEXT    DEFAULT 'pan-agent',
    started_at    INTEGER NOT NULL,
    ended_at      INTEGER,
    message_count INTEGER DEFAULT 0,
    model         TEXT,
    title         TEXT
);

CREATE TABLE IF NOT EXISTS messages (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    session_id TEXT    NOT NULL REFERENCES sessions(id),
    role       TEXT    NOT NULL,
    content    TEXT,
    timestamp  INTEGER NOT NULL
);

CREATE INDEX IF NOT EXISTS messages_session_idx ON messages(session_id);

CREATE VIRTUAL TABLE IF NOT EXISTS messages_fts
    USING fts5(content, content=messages, content_rowid=id);

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
`
	_, err := d.db.Exec(schema)
	if err != nil {
		return fmt.Errorf("migrate exec: %w", err)
	}
	return nil
}
