package storage

import (
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

// ListSessions returns sessions ordered newest-first, with pagination.
func (d *DB) ListSessions(limit, offset int) ([]Session, error) {
	const q = `
SELECT id, source, started_at, ended_at, message_count, model, title
FROM sessions
ORDER BY started_at DESC
LIMIT ? OFFSET ?`

	rows, err := d.db.Query(q, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("ListSessions query: %w", err)
	}
	defer rows.Close()

	var sessions []Session
	for rows.Next() {
		var s Session
		var model, title sql.NullString
		if err := rows.Scan(
			&s.ID, &s.Source, &s.StartedAt, &s.EndedAt,
			&s.MessageCount, &model, &title,
		); err != nil {
			return nil, fmt.Errorf("ListSessions scan: %w", err)
		}
		s.Model = model.String
		s.Title = title.String
		sessions = append(sessions, s)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("ListSessions rows: %w", err)
	}
	return sessions, nil
}

// SearchSessions performs a full-text search over message content using the
// FTS5 virtual table. Each word in query is prefix-matched. Results are ranked
// by relevance (FTS5's built-in rank column) and deduplicated by session.
func (d *DB) SearchSessions(query string, limit int) ([]SearchResult, error) {
	sanitized := sanitizeFTS(query)
	if sanitized == "" {
		return nil, nil
	}

	const q = `
SELECT DISTINCT
    m.session_id,
    s.title,
    s.started_at,
    s.source,
    s.message_count,
    s.model,
    snippet(messages_fts, 0, '<<', '>>', '...', 40) AS snippet
FROM messages_fts
JOIN messages m ON m.id = messages_fts.rowid
JOIN sessions s ON s.id = m.session_id
WHERE messages_fts MATCH ?
ORDER BY rank
LIMIT ?`

	rows, err := d.db.Query(q, sanitized, limit)
	if err != nil {
		return nil, fmt.Errorf("SearchSessions query: %w", err)
	}
	defer rows.Close()

	var results []SearchResult
	for rows.Next() {
		var r SearchResult
		var title, model sql.NullString
		if err := rows.Scan(
			&r.SessionID, &title, &r.StartedAt, &r.Source,
			&r.MessageCount, &model, &r.Snippet,
		); err != nil {
			return nil, fmt.Errorf("SearchSessions scan: %w", err)
		}
		r.Title = title.String
		r.Model = model.String
		results = append(results, r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("SearchSessions rows: %w", err)
	}
	return results, nil
}

// GetMessages returns all user/assistant messages for a session, ordered by
// (timestamp, id). Tool messages and NULL content are excluded, matching the
// behaviour of the original TypeScript implementation.
func (d *DB) GetMessages(sessionID string) ([]Message, error) {
	const q = `
SELECT id, session_id, role, content, timestamp
FROM messages
WHERE session_id = ?
  AND role IN ('user', 'assistant')
  AND content IS NOT NULL
ORDER BY timestamp, id`

	rows, err := d.db.Query(q, sessionID)
	if err != nil {
		return nil, fmt.Errorf("GetMessages query: %w", err)
	}
	defer rows.Close()

	var msgs []Message
	for rows.Next() {
		var m Message
		if err := rows.Scan(&m.ID, &m.SessionID, &m.Role, &m.Content, &m.Timestamp); err != nil {
			return nil, fmt.Errorf("GetMessages scan: %w", err)
		}
		msgs = append(msgs, m)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("GetMessages rows: %w", err)
	}
	return msgs, nil
}

// CreateSession inserts a new session row with a fresh UUID and the current
// Unix timestamp as started_at. The source is always "pan-agent".
func (d *DB) CreateSession(model string) (*Session, error) {
	s := &Session{
		ID:        uuid.New().String(),
		Source:    "pan-agent",
		StartedAt: time.Now().UnixMilli(),
		Model:     model,
	}

	const q = `
INSERT INTO sessions (id, source, started_at, model)
VALUES (?, ?, ?, ?)`

	if _, err := d.db.Exec(q, s.ID, s.Source, s.StartedAt, s.Model); err != nil {
		return nil, fmt.Errorf("CreateSession exec: %w", err)
	}
	return s, nil
}

// AddMessage inserts a message, updates the FTS5 index, and increments the
// session's message_count in a single transaction.
func (d *DB) AddMessage(sessionID, role, content string) error {
	ts := time.Now().UnixMilli()

	tx, err := d.db.Begin()
	if err != nil {
		return fmt.Errorf("AddMessage begin: %w", err)
	}
	defer func() {
		// Rollback is a no-op after a successful Commit.
		_ = tx.Rollback()
	}()

	// Insert the message and capture its auto-generated rowid.
	res, err := tx.Exec(
		`INSERT INTO messages (session_id, role, content, timestamp) VALUES (?, ?, ?, ?)`,
		sessionID, role, content, ts,
	)
	if err != nil {
		return fmt.Errorf("AddMessage insert message: %w", err)
	}
	msgID, err := res.LastInsertId()
	if err != nil {
		return fmt.Errorf("AddMessage last insert id: %w", err)
	}

	// Update the FTS5 external-content table.
	// Because messages_fts is a content= table we must maintain it manually.
	if _, err := tx.Exec(
		`INSERT INTO messages_fts (rowid, content) VALUES (?, ?)`,
		msgID, content,
	); err != nil {
		return fmt.Errorf("AddMessage fts insert: %w", err)
	}

	// Increment the denormalised message_count on the session.
	if _, err := tx.Exec(
		`UPDATE sessions SET message_count = message_count + 1 WHERE id = ?`,
		sessionID,
	); err != nil {
		return fmt.Errorf("AddMessage update count: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("AddMessage commit: %w", err)
	}
	return nil
}

// UpdateTitle sets the title for an existing session.
func (d *DB) UpdateTitle(sessionID, title string) error {
	if _, err := d.db.Exec(
		`UPDATE sessions SET title = ? WHERE id = ?`,
		title, sessionID,
	); err != nil {
		return fmt.Errorf("UpdateTitle exec: %w", err)
	}
	return nil
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

// sanitizeFTS converts a free-text query into a safe FTS5 MATCH expression.
// CountSessions returns the total number of sessions recorded.
func (d *DB) CountSessions() (int, error) {
	var n int
	if err := d.db.QueryRow(`SELECT COUNT(*) FROM sessions`).Scan(&n); err != nil {
		return 0, fmt.Errorf("CountSessions: %w", err)
	}
	return n, nil
}

// CountMessages returns the total number of messages across all sessions.
func (d *DB) CountMessages() (int, error) {
	var n int
	if err := d.db.QueryRow(`SELECT COUNT(*) FROM messages`).Scan(&n); err != nil {
		return 0, fmt.Errorf("CountMessages: %w", err)
	}
	return n, nil
}

// Each whitespace-delimited word is double-quoted (stripping any embedded
// double-quotes) and given a trailing * for prefix matching, matching the
// strategy used in the original TypeScript implementation.
//
// Returns an empty string if the query contains no usable words.
func sanitizeFTS(query string) string {
	words := strings.Fields(strings.TrimSpace(query))
	if len(words) == 0 {
		return ""
	}
	parts := make([]string, 0, len(words))
	for _, w := range words {
		clean := strings.ReplaceAll(w, `"`, "")
		if clean == "" {
			continue
		}
		parts = append(parts, fmt.Sprintf(`"%s"*`, clean))
	}
	return strings.Join(parts, " ")
}
