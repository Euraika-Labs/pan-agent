package storage

import (
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// Phase 13 WS#13.B — storage helpers for the rag_embeddings + rag_state
// tables (schema in db.go's migrate() + migrateRAGEmbeddings()).
//
// This file owns the typed CRUD surface the rag package will call. The
// rag package stays free of database/sql so codec/embedder/index can
// be unit-tested in isolation; the bridge lives here.

// Embedding is a single row of rag_embeddings as the application sees
// it. Vector is the raw little-endian-packed BLOB; rag.UnpackVector
// converts to []float32. Source/SourceID together uniquely identify
// the originating object (e.g. source="message", source_id=msg_id).
type Embedding struct {
	ID          int64
	Source      string
	SourceID    string
	SessionID   sql.NullString
	ContentHash string
	Text        string
	Model       string
	Dim         int
	Vector      []byte
	CreatedAt   int64 // unix seconds
}

// ErrEmbeddingNotFound is returned by GetEmbeddingByHash and
// GetEmbedding when no row matches. Callers use errors.Is to detect
// the absence so they can fall through to "embed and write".
var ErrEmbeddingNotFound = errors.New("storage: embedding not found")

// UpsertEmbedding writes (or updates) an embedding row. The UNIQUE
// constraint on (source, source_id, model) lets the same content be
// re-indexed under a different model without colliding; calling
// UpsertEmbedding twice with the same (source, source_id, model)
// updates the existing row.
//
// Vector dim is taken from len(vector)/4 — callers that have already
// packed via rag.PackVector should pass the original dim explicitly.
// We accept dim here so the row is self-describing without trusting
// arithmetic on the BLOB length (extra defence against blob/dim drift).
func (d *DB) UpsertEmbedding(e Embedding) error {
	if e.Source == "" || e.SourceID == "" || e.Model == "" {
		return fmt.Errorf("storage: UpsertEmbedding: source / source_id / model required")
	}
	if e.Dim <= 0 || len(e.Vector) != 4*e.Dim {
		return fmt.Errorf("storage: UpsertEmbedding: dim=%d but len(vector)=%d (want %d)",
			e.Dim, len(e.Vector), 4*e.Dim)
	}
	if e.ContentHash == "" {
		return fmt.Errorf("storage: UpsertEmbedding: content_hash required")
	}
	if e.CreatedAt == 0 {
		e.CreatedAt = time.Now().Unix()
	}
	const q = `
INSERT INTO rag_embeddings
  (source, source_id, session_id, content_hash, text, model, dim, vector, created_at)
VALUES
  (?,      ?,         ?,          ?,            ?,    ?,     ?,   ?,      ?)
ON CONFLICT(source, source_id, model) DO UPDATE SET
  session_id   = excluded.session_id,
  content_hash = excluded.content_hash,
  text         = excluded.text,
  dim          = excluded.dim,
  vector       = excluded.vector,
  created_at   = excluded.created_at`
	_, err := d.db.Exec(q,
		e.Source, e.SourceID, e.SessionID,
		e.ContentHash, e.Text, e.Model,
		e.Dim, e.Vector, e.CreatedAt,
	)
	if err != nil {
		return fmt.Errorf("storage: UpsertEmbedding: %w", err)
	}
	return nil
}

// GetEmbeddingByHash returns the most recent embedding row whose
// (content_hash, model) pair matches. The dedup gate before re-
// embedding: identical content under the same model is a cache hit.
//
// Returns ErrEmbeddingNotFound when no row matches. The model arg is
// required because the same hash can have multiple rows under
// different embedding models.
func (d *DB) GetEmbeddingByHash(contentHash, model string) (*Embedding, error) {
	if contentHash == "" || model == "" {
		return nil, fmt.Errorf("storage: GetEmbeddingByHash: content_hash + model required")
	}
	const q = `
SELECT id, source, source_id, session_id, content_hash, text, model, dim, vector, created_at
FROM   rag_embeddings
WHERE  content_hash = ? AND model = ?
ORDER  BY created_at DESC
LIMIT  1`
	row := d.db.QueryRow(q, contentHash, model)
	e := &Embedding{}
	if err := row.Scan(
		&e.ID, &e.Source, &e.SourceID, &e.SessionID,
		&e.ContentHash, &e.Text, &e.Model,
		&e.Dim, &e.Vector, &e.CreatedAt,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrEmbeddingNotFound
		}
		return nil, fmt.Errorf("storage: GetEmbeddingByHash: %w", err)
	}
	return e, nil
}

// ListEmbeddingsBySession returns every embedding row tied to a
// session, ordered by id ASC (insertion order). Used by:
//
//   - The next-slice search path, which loads candidates for a
//     cosine-similarity scan when sqlite-vec is not yet on top.
//   - Per-session purges (delete-on-session-end) — callers list, then
//     bulk-delete by id.
func (d *DB) ListEmbeddingsBySession(sessionID string) ([]Embedding, error) {
	if sessionID == "" {
		return nil, fmt.Errorf("storage: ListEmbeddingsBySession: session_id required")
	}
	const q = `
SELECT id, source, source_id, session_id, content_hash, text, model, dim, vector, created_at
FROM   rag_embeddings
WHERE  session_id = ?
ORDER  BY id ASC`
	rows, err := d.db.Query(q, sessionID)
	if err != nil {
		return nil, fmt.Errorf("storage: ListEmbeddingsBySession: %w", err)
	}
	defer rows.Close()
	var out []Embedding
	for rows.Next() {
		var e Embedding
		if err := rows.Scan(
			&e.ID, &e.Source, &e.SourceID, &e.SessionID,
			&e.ContentHash, &e.Text, &e.Model,
			&e.Dim, &e.Vector, &e.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("storage: ListEmbeddingsBySession scan: %w", err)
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// DeleteEmbeddingsBySession removes every embedding tied to a
// session. Returns the count of deleted rows. Used by session-end
// hooks so a transient chat doesn't leave embeddings lingering.
func (d *DB) DeleteEmbeddingsBySession(sessionID string) (int64, error) {
	if sessionID == "" {
		return 0, fmt.Errorf("storage: DeleteEmbeddingsBySession: session_id required")
	}
	res, err := d.db.Exec(
		`DELETE FROM rag_embeddings WHERE session_id = ?`, sessionID)
	if err != nil {
		return 0, fmt.Errorf("storage: DeleteEmbeddingsBySession: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("storage: DeleteEmbeddingsBySession affected: %w", err)
	}
	return n, nil
}

// GetRAGState reads a value from the rag_state cursor table. Returns
// ("", false, nil) when the key isn't set yet — callers treat that
// as "start from the beginning".
func (d *DB) GetRAGState(key string) (string, bool, error) {
	if key == "" {
		return "", false, fmt.Errorf("storage: GetRAGState: key required")
	}
	var v string
	err := d.db.QueryRow(`SELECT value FROM rag_state WHERE key = ?`, key).Scan(&v)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", false, nil
		}
		return "", false, fmt.Errorf("storage: GetRAGState: %w", err)
	}
	return v, true, nil
}

// ListMessagesSince returns messages with id > afterID, ordered ASC,
// capped at limit rows. Used by the rag watcher to drain new messages
// in id order so the cursor advances monotonically. Pass afterID=0 to
// start from the beginning.
//
// limit must be positive; <= 0 returns an error so we don't accidentally
// stream the whole table on a misuse.
func (d *DB) ListMessagesSince(afterID int64, limit int) ([]Message, error) {
	if limit <= 0 {
		return nil, fmt.Errorf("storage: ListMessagesSince: limit must be > 0, got %d", limit)
	}
	const q = `
SELECT id, session_id, role, content, timestamp
FROM   messages
WHERE  id > ?
ORDER  BY id ASC
LIMIT  ?`
	rows, err := d.db.Query(q, afterID, limit)
	if err != nil {
		return nil, fmt.Errorf("storage: ListMessagesSince: %w", err)
	}
	defer rows.Close()
	var out []Message
	for rows.Next() {
		var m Message
		var content sql.NullString
		if err := rows.Scan(&m.ID, &m.SessionID, &m.Role, &content, &m.Timestamp); err != nil {
			return nil, fmt.Errorf("storage: ListMessagesSince scan: %w", err)
		}
		if content.Valid {
			m.Content = content.String
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// SetRAGState upserts a single key/value into rag_state. Used by the
// embedder watcher to record its progress (last_indexed_event_id,
// last_indexed_message_id, etc.) so a crash-resume picks up cleanly.
func (d *DB) SetRAGState(key, value string) error {
	if key == "" {
		return fmt.Errorf("storage: SetRAGState: key required")
	}
	_, err := d.db.Exec(
		`INSERT INTO rag_state (key, value) VALUES (?, ?)
		 ON CONFLICT(key) DO UPDATE SET value = excluded.value`,
		key, value,
	)
	if err != nil {
		return fmt.Errorf("storage: SetRAGState: %w", err)
	}
	return nil
}
