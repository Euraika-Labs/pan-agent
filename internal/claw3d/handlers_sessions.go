package claw3d

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/euraika-labs/pan-agent/internal/storage"
	"github.com/google/uuid"
)

// sessions.* maps onto office_sessions + office_messages. A session is owned
// by an agent and carries the conversation history the 3D avatar "speaks."
func init() {
	registerMethod("sessions.list", handleSessionsList)
	registerMethod("sessions.preview", handleSessionsPreview)
	registerMethod("sessions.patch", handleSessionsPatch)
	registerMethod("sessions.reset", handleSessionsReset)
}

type sessionsListParams struct {
	AgentID string `json:"agentId,omitempty"`
}

func handleSessionsList(_ context.Context, c *adapterClient, raw json.RawMessage) (any, error) {
	db, err := requireDB(c)
	if err != nil {
		return nil, err
	}
	var p sessionsListParams
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, &p)
	}
	sessions, err := db.ListOfficeSessions(p.AgentID)
	if err != nil {
		return nil, err
	}
	return map[string]any{"sessions": sessions}, nil
}

type sessionsPreviewParams struct {
	ID    string `json:"id"`
	Limit int    `json:"limit,omitempty"`
}

func handleSessionsPreview(_ context.Context, c *adapterClient, raw json.RawMessage) (any, error) {
	db, err := requireDB(c)
	if err != nil {
		return nil, err
	}
	var p sessionsPreviewParams
	if err := json.Unmarshal(raw, &p); err != nil {
		return nil, err
	}
	if p.ID == "" {
		return nil, errors.New("sessions.preview: id required")
	}
	msgs, err := db.ListOfficeMessages(p.ID, p.Limit)
	if err != nil {
		return nil, err
	}
	return map[string]any{"sessionId": p.ID, "messages": msgs}, nil
}

type sessionsPatchParams struct {
	ID       string          `json:"id"`
	AgentID  string          `json:"agentId,omitempty"`
	State    string          `json:"state,omitempty"`
	Settings json.RawMessage `json:"settings,omitempty"`
}

// sessions.patch upserts a session. If the id is unknown and AgentID is given,
// a new session is created; otherwise it updates state/settings.
func handleSessionsPatch(_ context.Context, c *adapterClient, raw json.RawMessage) (any, error) {
	db, err := requireDB(c)
	if err != nil {
		return nil, err
	}
	var p sessionsPatchParams
	if err := json.Unmarshal(raw, &p); err != nil {
		return nil, err
	}
	if p.ID == "" {
		p.ID = uuid.NewString()
	}
	if p.State == "" {
		p.State = "idle"
	}
	sess := storage.OfficeSession{
		ID:           p.ID,
		AgentID:      p.AgentID,
		State:        p.State,
		SettingsJSON: string(p.Settings),
		CreatedAt:    time.Now().UnixMilli(),
	}
	if err := db.CreateOfficeSession(sess); err != nil {
		return nil, err
	}
	_ = db.AuditOffice("local", "sessions.patch", p.ID, p.State)
	return map[string]any{"session": sess}, nil
}

type sessionsResetParams struct {
	ID string `json:"id"`
}

func handleSessionsReset(_ context.Context, c *adapterClient, raw json.RawMessage) (any, error) {
	db, err := requireDB(c)
	if err != nil {
		return nil, err
	}
	var p sessionsResetParams
	if err := json.Unmarshal(raw, &p); err != nil {
		return nil, err
	}
	if p.ID == "" {
		return nil, errors.New("sessions.reset: id required")
	}
	// Reset = wipe messages, keep session row with fresh timestamps.
	if err := db.ResetOfficeSession(p.ID); err != nil {
		return nil, err
	}
	_ = db.AuditOffice("local", "sessions.reset", p.ID, "ok")
	return map[string]bool{"reset": true}, nil
}
