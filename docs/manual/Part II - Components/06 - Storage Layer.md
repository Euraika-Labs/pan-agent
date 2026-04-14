# Storage Layer

The `internal/storage` package wraps `modernc.org/sqlite` to persist sessions and messages with full-text search.

## Why modernc.org/sqlite

- Pure Go (no CGo). No C compiler needed for the database. macOS still needs CGo for the screenshot package, but the DB itself is portable.
- FTS5 included.
- Goroutine-safe (the package uses internal locking).
- Single-file database — easy to back up.

The trade-off: it's slower than `mattn/go-sqlite3` under heavy concurrent write load. For a single-user desktop app, this is fine.

## Schema

```sql
CREATE TABLE sessions (
    id          TEXT PRIMARY KEY,
    model       TEXT NOT NULL,
    created_at  TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at  TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE messages (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    session_id  TEXT NOT NULL REFERENCES sessions(id),
    role        TEXT NOT NULL,
    content     TEXT NOT NULL,
    created_at  TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX idx_messages_session ON messages(session_id);

CREATE VIRTUAL TABLE messages_fts USING fts5(
    content,
    content='messages',
    content_rowid='id',
    tokenize='porter'
);

-- Triggers to keep FTS5 in sync
CREATE TRIGGER messages_ai AFTER INSERT ON messages BEGIN
    INSERT INTO messages_fts(rowid, content) VALUES (new.id, new.content);
END;

CREATE TRIGGER messages_ad AFTER DELETE ON messages BEGIN
    INSERT INTO messages_fts(messages_fts, rowid, content)
        VALUES ('delete', old.id, old.content);
END;

CREATE TRIGGER messages_au AFTER UPDATE ON messages BEGIN
    INSERT INTO messages_fts(messages_fts, rowid, content)
        VALUES ('delete', old.id, old.content);
    INSERT INTO messages_fts(rowid, content) VALUES (new.id, new.content);
END;
```

Migrations are applied in `Open()` via `IF NOT EXISTS` clauses — there is no separate migrations system. Schema changes will need explicit migration logic when needed.

## API

```go
type DB struct { /* sql.DB wrapper */ }

func Open(path string) (*DB, error)
func (db *DB) Close() error

// Sessions
func (db *DB) CreateSession(model string) (*Session, error)
func (db *DB) GetSession(id string) (*Session, error)
func (db *DB) ListSessions(limit, offset int) ([]Session, error)
func (db *DB) UpdateSessionTimestamp(id string) error

// Messages
func (db *DB) AddMessage(sessionID, role, content string) error
func (db *DB) GetMessages(sessionID string) ([]Message, error)

// Search
func (db *DB) SearchSessions(query string, limit int) ([]SearchResult, error)
```

## Search

`SearchSessions` runs an FTS5 query against `messages_fts` and returns sessions ordered by relevance with a snippet:

```go
type SearchResult struct {
    SessionID string
    Snippet   string  // first matching chunk, with ... markers
    Score     float64
}
```

The search uses BM25 scoring (FTS5 default). Snippets use SQLite's `snippet()` function with `…` as ellipsis and 32-token windows.

## Concurrency

The DB is opened with `MaxOpenConns(1)`. All queries are serialized through one connection. This avoids "database is locked" errors at the cost of throughput.

For a single-user desktop app receiving roughly one message every few seconds, this is plenty.

## Testing

`internal/storage/sessions_test.go` opens a fresh DB in `t.TempDir()` for each test:

```go
func openTestDB(t *testing.T) *DB {
    dir := t.TempDir()
    db, _ := Open(filepath.Join(dir, "test.db"))
    t.Cleanup(func() { _ = db.Close() })
    return db
}
```

13 tests cover create, list, search, message CRUD, and concurrent access.

## Backup

```bash
# Online backup (works while pan-agent is running)
sqlite3 state.db ".backup state.db.bak"

# Or just copy the file (small risk of corruption if mid-write)
cp state.db state.db.bak
```

## Read next
- [[04 - Data and Storage]]
- [[07 - Profile System]]
