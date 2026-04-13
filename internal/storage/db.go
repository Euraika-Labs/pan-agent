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
	"database/sql"
	"fmt"

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
	return d, nil
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
`
	_, err := d.db.Exec(schema)
	if err != nil {
		return fmt.Errorf("migrate exec: %w", err)
	}
	return nil
}
