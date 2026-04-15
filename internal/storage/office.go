package storage

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"time"
)

// ---------------------------------------------------------------------------
// Claw3D Office data access (Option A M2).
//
// These types and methods back the native Go adapter. They deliberately mirror
// the shape the Node adapter emitted so protocol conformance fixtures remain
// reusable across engines. `json` tags use camelCase because the Claw3D React
// client reads them directly.
// ---------------------------------------------------------------------------

// OfficeAgent is a persisted 3D-office agent registry entry.
type OfficeAgent struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	Workspace    string `json:"workspace"`
	IdentityJSON string `json:"identity,omitempty"` // opaque JSON blob
	Role         string `json:"role"`
	CreatedAt    int64  `json:"createdAt"`
	UpdatedAt    int64  `json:"updatedAt"`
}

// OfficeSession represents an agent conversation context inside the office.
type OfficeSession struct {
	ID           string `json:"id"`
	AgentID      string `json:"agentId"`
	State        string `json:"state"`
	SettingsJSON string `json:"settings,omitempty"`
	CreatedAt    int64  `json:"createdAt"`
	UpdatedAt    int64  `json:"updatedAt"`
}

// OfficeMessage is a single chat turn within an OfficeSession.
type OfficeMessage struct {
	ID        int64  `json:"id"`
	SessionID string `json:"sessionId"`
	Role      string `json:"role"`
	Content   string `json:"content"`
	CreatedAt int64  `json:"createdAt"`
}

// OfficeCron is a scheduled job attached to an office agent.
type OfficeCron struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Schedule    string `json:"schedule"`
	PayloadJSON string `json:"payload"`
	Enabled     bool   `json:"enabled"`
	LastRun     *int64 `json:"lastRun,omitempty"`
}

// CreateOfficeAgent upserts an agent row. Idempotent — existing rows have
// their name/workspace/identity/role/updated_at refreshed.
func (d *DB) CreateOfficeAgent(a OfficeAgent) error {
	now := time.Now().UnixMilli()
	if a.CreatedAt == 0 {
		a.CreatedAt = now
	}
	_, err := d.db.Exec(`
INSERT INTO office_agents (id, name, workspace, identity_json, role, created_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(id) DO UPDATE SET
  name=excluded.name, workspace=excluded.workspace,
  identity_json=excluded.identity_json, role=excluded.role,
  updated_at=excluded.updated_at`,
		a.ID, a.Name, a.Workspace, a.IdentityJSON, a.Role, a.CreatedAt, now)
	if err != nil {
		return fmt.Errorf("CreateOfficeAgent: %w", err)
	}
	return nil
}

// ListOfficeAgents returns all agent registry entries ordered by creation.
func (d *DB) ListOfficeAgents() ([]OfficeAgent, error) {
	rows, err := d.db.Query(`
SELECT id, name, COALESCE(workspace,''), COALESCE(identity_json,''),
       COALESCE(role,''), created_at, updated_at
FROM office_agents ORDER BY created_at ASC`)
	if err != nil {
		return nil, fmt.Errorf("ListOfficeAgents: %w", err)
	}
	defer rows.Close()
	out := make([]OfficeAgent, 0)
	for rows.Next() {
		var a OfficeAgent
		if err := rows.Scan(&a.ID, &a.Name, &a.Workspace, &a.IdentityJSON,
			&a.Role, &a.CreatedAt, &a.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// DeleteOfficeAgent removes an agent and cascades to its sessions/messages.
func (d *DB) DeleteOfficeAgent(id string) error {
	_, err := d.db.Exec(`DELETE FROM office_agents WHERE id = ?`, id)
	return err
}

// GetOfficeAgent returns a single agent by id; ErrNoRows if absent.
func (d *DB) GetOfficeAgent(id string) (*OfficeAgent, error) {
	var a OfficeAgent
	err := d.db.QueryRow(`
SELECT id, name, COALESCE(workspace,''), COALESCE(identity_json,''),
       COALESCE(role,''), created_at, updated_at
FROM office_agents WHERE id = ?`, id).Scan(
		&a.ID, &a.Name, &a.Workspace, &a.IdentityJSON,
		&a.Role, &a.CreatedAt, &a.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &a, nil
}

// CreateOfficeSession inserts a session row and initialises its timestamps.
func (d *DB) CreateOfficeSession(s OfficeSession) error {
	now := time.Now().UnixMilli()
	if s.CreatedAt == 0 {
		s.CreatedAt = now
	}
	_, err := d.db.Exec(`
INSERT INTO office_sessions (id, agent_id, state, settings_json, created_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?)
ON CONFLICT(id) DO UPDATE SET
  state=excluded.state, settings_json=excluded.settings_json,
  updated_at=excluded.updated_at`,
		s.ID, s.AgentID, s.State, s.SettingsJSON, s.CreatedAt, now)
	return err
}

// ListOfficeSessions returns sessions, optionally filtered by agent.
func (d *DB) ListOfficeSessions(agentID string) ([]OfficeSession, error) {
	q := `SELECT id, agent_id, COALESCE(state,'idle'), COALESCE(settings_json,''),
                 created_at, updated_at
          FROM office_sessions`
	args := []any{}
	if agentID != "" {
		q += ` WHERE agent_id = ?`
		args = append(args, agentID)
	}
	q += ` ORDER BY created_at DESC`
	rows, err := d.db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]OfficeSession, 0)
	for rows.Next() {
		var s OfficeSession
		if err := rows.Scan(&s.ID, &s.AgentID, &s.State, &s.SettingsJSON,
			&s.CreatedAt, &s.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// AppendOfficeMessage adds a message turn to an office session.
//
// The content_hash column is populated via sha256(content) at insert time
// so the office_messages_content_hash_idx lookup index (added in the M4 W2
// migration) can answer "does this session have a matching message" queries
// without a table scan.
//
// Per Gate-2 decision: hash is a LOOKUP key, NOT a uniqueness constraint.
// Legitimate duplicates (user retries the same thumbs-up; two identical
// "OK" acknowledgements in rapid sequence) are allowed and stored. The
// importer handles dedup at the migration boundary via the audit-digest
// on the source file, not here per-message.
func (d *DB) AppendOfficeMessage(m OfficeMessage) (int64, error) {
	if m.CreatedAt == 0 {
		m.CreatedAt = time.Now().UnixMilli()
	}
	h := sha256.Sum256([]byte(m.Content))
	hash := hex.EncodeToString(h[:])

	res, err := d.db.Exec(`
INSERT INTO office_messages (session_id, role, content, content_hash, created_at)
VALUES (?, ?, ?, ?, ?)`, m.SessionID, m.Role, m.Content, hash, m.CreatedAt)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// ListOfficeMessages returns session messages oldest-first.
func (d *DB) ListOfficeMessages(sessionID string, limit int) ([]OfficeMessage, error) {
	if limit <= 0 {
		limit = 200
	}
	rows, err := d.db.Query(`
SELECT id, session_id, role, content, created_at
FROM office_messages WHERE session_id = ? ORDER BY created_at ASC LIMIT ?`,
		sessionID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]OfficeMessage, 0)
	for rows.Next() {
		var m OfficeMessage
		if err := rows.Scan(&m.ID, &m.SessionID, &m.Role, &m.Content, &m.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// ResetOfficeSession drops the message history for a session without
// deleting the session row itself. Used by Claw3D's sessions.reset call so
// the same 3D agent can continue in a "blank slate" mode.
func (d *DB) ResetOfficeSession(id string) error {
	_, err := d.db.Exec(`DELETE FROM office_messages WHERE session_id = ?`, id)
	return err
}

// HasMigrationDigest returns true if an office_audit row already exists
// with method="migration.v1" and the given digest. Used by RunMigration
// to implement idempotency — re-running against the same source file is
// a free skip.
func (d *DB) HasMigrationDigest(digest string) (bool, error) {
	var n int
	err := d.db.QueryRow(
		`SELECT COUNT(*) FROM office_audit
		 WHERE method = 'migration.v1' AND params_digest = ?`,
		digest,
	).Scan(&n)
	if err != nil {
		return false, err
	}
	return n > 0, nil
}

// AuditOffice records a state-changing protocol call. Cheap, append-only.
func (d *DB) AuditOffice(actor, method, digest, result string) error {
	_, err := d.db.Exec(`
INSERT INTO office_audit (ts, actor, method, params_digest, result)
VALUES (?, ?, ?, ?, ?)`,
		time.Now().UnixMilli(), actor, method, digest, result)
	return err
}
