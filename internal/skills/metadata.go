package skills

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/google/uuid"
)

// Status is the lifecycle state of a proposal or active skill. Typed so
// the compiler catches stray string values — callers must use one of the
// declared constants; unknown values round-trip via JSON but are rejected
// by IsValid().
type Status string

// Status constants. Exhaustive; add new values to statusSet below as well.
const (
	StatusProposed Status = "proposed"
	StatusActive   Status = "active"
	StatusRejected Status = "rejected"
	StatusMerged   Status = "merged"
	StatusArchived Status = "archived"
)

var statusSet = map[Status]bool{
	StatusProposed: true, StatusActive: true, StatusRejected: true,
	StatusMerged: true, StatusArchived: true,
}

// IsValid reports whether s is one of the declared Status constants.
func (s Status) IsValid() bool { return statusSet[s] }

// Intent records the action a curator proposal asks the reviewer to take.
// Typed rather than stringly-defined so the compiler flags a mistaken
// Intent("merge") vs IntentMerge.
type Intent string

// Intent constants. "create" is the explicit default for agent-authored
// brand-new proposals — previously this was the empty string which was
// indistinguishable from a zero-value ProposalMetadata{}.
const (
	IntentCreate       Intent = "create"
	IntentRefine       Intent = "refine"
	IntentMerge        Intent = "merge"
	IntentSplit        Intent = "split"
	IntentArchive      Intent = "archive"
	IntentRecategorize Intent = "recategorize"
)

var intentSet = map[Intent]bool{
	IntentCreate: true, IntentRefine: true, IntentMerge: true,
	IntentSplit: true, IntentArchive: true, IntentRecategorize: true,
}

// IsValid reports whether i is one of the declared Intent constants.
func (i Intent) IsValid() bool { return intentSet[i] }

// legacyEmptyIntent accommodates on-disk metadata from the pre-M7 era
// where IntentCreate was "" (empty string). UnmarshalJSON upgrades "" to
// IntentCreate so new code can treat the sentinel uniformly.
func (i *Intent) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return err
	}
	if s == "" {
		*i = IntentCreate
		return nil
	}
	*i = Intent(s)
	return nil
}

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
	Source       string   `json:"source"` // "agent"|"curator"|"reviewer"|"user"
	UsageCount   int      `json:"usage_count"`
	LastUsedAt   int64    `json:"last_used_at,omitempty"`
	Status       Status   `json:"status"`
	RejectReason string   `json:"reject_reason,omitempty"`
	ParentIDs    []string `json:"parent_ids,omitempty"`
	SessionCtx   string   `json:"session_ctx,omitempty"`

	Intent Intent `json:"intent,omitempty"`

	// IntentTargets lists the active-skill IDs (`<category>/<name>`) this
	// proposal acts on. For merges/splits this can be multiple entries.
	IntentTargets []string `json:"intent_targets,omitempty"`

	// IntentNewCategory is the destination category for "recategorize" intents.
	IntentNewCategory string `json:"intent_new_category,omitempty"`

	// IntentReason is the curator's free-text justification.
	IntentReason string `json:"intent_reason,omitempty"`
}

// TrustTier constants.
const (
	TrustBuiltin      = "builtin"
	TrustTrusted      = "trusted"
	TrustCommunity    = "community"
	TrustAgentCreated = "agent-created"
)

// NewProposalMetadata creates a proposal with a fresh UUID. Intent
// defaults to IntentCreate (the typed sentinel for "brand new agent
// authored skill") so a zero-value struct never silently counts as a
// "create" intent — callers that forget to set Intent will get a
// validation error rather than an ambiguous empty string.
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
		Intent:      IntentCreate,
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
