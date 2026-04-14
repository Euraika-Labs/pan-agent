package skills

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/google/uuid"
)

// ProposalMetadata is stored alongside SKILL.md in _proposed/<uuid>/ and
// replicated into _metadata.json when a skill is promoted to active.
type ProposalMetadata struct {
	ID           string   `json:"id"`
	Name         string   `json:"name"`
	Category     string   `json:"category"`
	Description  string   `json:"description"`
	TrustTier    string   `json:"trust_tier"` // builtin|trusted|community|agent-created
	CreatedBy    string   `json:"created_by"` // session_id
	CreatedAt    int64    `json:"created_at"`
	Source       string   `json:"source"`      // "agent"|"curator"|"reviewer"|"user"
	UsageCount   int      `json:"usage_count"`
	LastUsedAt   int64    `json:"last_used_at,omitempty"`
	Status       string   `json:"status"` // proposed|active|rejected|merged|archived
	RejectReason string   `json:"reject_reason,omitempty"`
	ParentIDs    []string `json:"parent_ids,omitempty"`
	SessionCtx   string   `json:"session_ctx,omitempty"`
}

// Status constants.
const (
	StatusProposed = "proposed"
	StatusActive   = "active"
	StatusRejected = "rejected"
	StatusMerged   = "merged"
	StatusArchived = "archived"
)

// TrustTier constants.
const (
	TrustBuiltin      = "builtin"
	TrustTrusted      = "trusted"
	TrustCommunity    = "community"
	TrustAgentCreated = "agent-created"
)

// NewProposalMetadata creates a proposal with a fresh UUID.
func NewProposalMetadata(name, category, description, sessionID, source string) ProposalMetadata {
	return ProposalMetadata{
		ID:          uuid.New().String(),
		Name:        name,
		Category:    category,
		Description: description,
		TrustTier:   TrustAgentCreated,
		CreatedBy:   sessionID,
		CreatedAt:   time.Now().UnixMilli(),
		Source:      source,
		Status:      StatusProposed,
	}
}

// WriteMetadata writes metadata.json to a skill directory.
func WriteMetadata(skillDir string, m ProposalMetadata) error {
	if err := os.MkdirAll(skillDir, 0o700); err != nil {
		return fmt.Errorf("WriteMetadata mkdir: %w", err)
	}
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return fmt.Errorf("WriteMetadata marshal: %w", err)
	}
	p := filepath.Join(skillDir, "_metadata.json")
	if err := os.WriteFile(p, data, 0o600); err != nil {
		return fmt.Errorf("WriteMetadata write: %w", err)
	}
	return nil
}

// ReadMetadata reads metadata.json from a skill directory.
// Returns a zero-value ProposalMetadata if the file does not exist.
func ReadMetadata(skillDir string) (ProposalMetadata, error) {
	p := filepath.Join(skillDir, "_metadata.json")
	data, err := os.ReadFile(p)
	if os.IsNotExist(err) {
		return ProposalMetadata{}, nil
	}
	if err != nil {
		return ProposalMetadata{}, fmt.Errorf("ReadMetadata: %w", err)
	}
	var m ProposalMetadata
	if err := json.Unmarshal(data, &m); err != nil {
		return ProposalMetadata{}, fmt.Errorf("ReadMetadata unmarshal: %w", err)
	}
	return m, nil
}
