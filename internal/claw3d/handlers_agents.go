package claw3d

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/euraika-labs/pan-agent/internal/storage"
	"github.com/google/uuid"
)

// Claw3D protocol v3: agents.* methods map onto the office_agents table.
// files.{get,set} store a JSON blob keyed by agent id; we treat it as an
// opaque string inside identity_json to avoid a second table until we need
// structured queries against it.
func init() {
	registerMethod("agents.list", handleAgentsList)
	registerMethod("agents.create", handleAgentsCreate)
	registerMethod("agents.update", handleAgentsUpdate)
	registerMethod("agents.delete", handleAgentsDelete)
	registerMethod("agents.files.get", handleAgentsFilesGet)
	registerMethod("agents.files.set", handleAgentsFilesSet)
}

// errNoDB is returned by handlers that require persistence when the Adapter
// was constructed without a *storage.DB (tests, smoke runs). It surfaces as
// a wire error so clients can show an actionable message.
var errNoDB = errors.New("claw3d: adapter has no database — M1 smoke mode")

func requireDB(c *adapterClient) (*storage.DB, error) {
	if c.adapter == nil || c.adapter.db == nil {
		return nil, errNoDB
	}
	return c.adapter.db, nil
}

func handleAgentsList(_ context.Context, c *adapterClient, _ json.RawMessage) (any, error) {
	db, err := requireDB(c)
	if err != nil {
		return nil, err
	}
	agents, err := db.ListOfficeAgents()
	if err != nil {
		return nil, err
	}
	return map[string]any{"agents": agents}, nil
}

type agentsCreateParams struct {
	Name      string          `json:"name"`
	Workspace string          `json:"workspace,omitempty"`
	Role      string          `json:"role,omitempty"`
	Identity  json.RawMessage `json:"identity,omitempty"`
}

func handleAgentsCreate(_ context.Context, c *adapterClient, raw json.RawMessage) (any, error) {
	db, err := requireDB(c)
	if err != nil {
		return nil, err
	}
	var p agentsCreateParams
	if err := json.Unmarshal(raw, &p); err != nil {
		return nil, err
	}
	if p.Name == "" {
		return nil, errors.New("agents.create: name required")
	}
	id := uuid.NewString()
	agent := storage.OfficeAgent{
		ID:           id,
		Name:         p.Name,
		Workspace:    p.Workspace,
		Role:         p.Role,
		IdentityJSON: string(p.Identity),
		CreatedAt:    time.Now().UnixMilli(),
	}
	if err := db.CreateOfficeAgent(agent); err != nil {
		return nil, err
	}
	_ = db.AuditOffice("local", "agents.create", id, "ok")
	return map[string]any{"agent": agent}, nil
}

type agentsUpdateParams struct {
	ID        string          `json:"id"`
	Name      *string         `json:"name,omitempty"`
	Workspace *string         `json:"workspace,omitempty"`
	Role      *string         `json:"role,omitempty"`
	Identity  json.RawMessage `json:"identity,omitempty"`
}

func handleAgentsUpdate(_ context.Context, c *adapterClient, raw json.RawMessage) (any, error) {
	db, err := requireDB(c)
	if err != nil {
		return nil, err
	}
	var p agentsUpdateParams
	if err := json.Unmarshal(raw, &p); err != nil {
		return nil, err
	}
	if p.ID == "" {
		return nil, errors.New("agents.update: id required")
	}
	existing, err := db.GetOfficeAgent(p.ID)
	if err != nil {
		return nil, err
	}
	if existing == nil {
		return nil, errors.New("agents.update: unknown id")
	}
	if p.Name != nil {
		existing.Name = *p.Name
	}
	if p.Workspace != nil {
		existing.Workspace = *p.Workspace
	}
	if p.Role != nil {
		existing.Role = *p.Role
	}
	if p.Identity != nil {
		existing.IdentityJSON = string(p.Identity)
	}
	if err := db.CreateOfficeAgent(*existing); err != nil {
		return nil, err
	}
	_ = db.AuditOffice("local", "agents.update", p.ID, "ok")
	return map[string]any{"agent": existing}, nil
}

type agentsDeleteParams struct {
	ID string `json:"id"`
}

func handleAgentsDelete(_ context.Context, c *adapterClient, raw json.RawMessage) (any, error) {
	db, err := requireDB(c)
	if err != nil {
		return nil, err
	}
	var p agentsDeleteParams
	if err := json.Unmarshal(raw, &p); err != nil {
		return nil, err
	}
	if p.ID == "" {
		return nil, errors.New("agents.delete: id required")
	}
	if err := db.DeleteOfficeAgent(p.ID); err != nil {
		return nil, err
	}
	_ = db.AuditOffice("local", "agents.delete", p.ID, "ok")
	return map[string]bool{"deleted": true}, nil
}

type agentsFilesGetParams struct {
	ID string `json:"id"`
}

// agents.files.get returns whatever JSON blob we stashed in identity_json.
// The Node adapter has richer semantics (actual per-agent file trees); for
// M2 the blob is the SSOT and clients treat it as opaque.
func handleAgentsFilesGet(_ context.Context, c *adapterClient, raw json.RawMessage) (any, error) {
	db, err := requireDB(c)
	if err != nil {
		return nil, err
	}
	var p agentsFilesGetParams
	if err := json.Unmarshal(raw, &p); err != nil {
		return nil, err
	}
	agent, err := db.GetOfficeAgent(p.ID)
	if err != nil {
		return nil, err
	}
	if agent == nil {
		return nil, errors.New("agents.files.get: unknown id")
	}
	var files any
	if agent.IdentityJSON != "" {
		_ = json.Unmarshal([]byte(agent.IdentityJSON), &files)
	}
	return map[string]any{"files": files}, nil
}

type agentsFilesSetParams struct {
	ID    string          `json:"id"`
	Files json.RawMessage `json:"files"`
}

func handleAgentsFilesSet(_ context.Context, c *adapterClient, raw json.RawMessage) (any, error) {
	db, err := requireDB(c)
	if err != nil {
		return nil, err
	}
	var p agentsFilesSetParams
	if err := json.Unmarshal(raw, &p); err != nil {
		return nil, err
	}
	existing, err := db.GetOfficeAgent(p.ID)
	if err != nil {
		return nil, err
	}
	if existing == nil {
		return nil, errors.New("agents.files.set: unknown id")
	}
	existing.IdentityJSON = string(p.Files)
	if err := db.CreateOfficeAgent(*existing); err != nil {
		return nil, err
	}
	_ = db.AuditOffice("local", "agents.files.set", p.ID, "ok")
	return map[string]bool{"ok": true}, nil
}
