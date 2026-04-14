package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"

	"github.com/euraika-labs/pan-agent/internal/skills"
)

// SkillReviewTool is the reviewer agent's interface to the proposal queue.
// It is *not* exposed to the main agent — the gateway only includes it in the
// tool list when running the reviewer loop. Actions:
//
//	list     — enumerate every proposal awaiting review
//	get      — fetch one proposal in full (metadata + content + guard findings)
//	approve  — promote a proposal (optionally with refined content); for
//	           curator-originated proposals also runs ApplyCuratorIntent
//	reject   — move a proposal to _rejected/ with a reason
//	merge    — consolidate ≥2 proposals into one promotion
type SkillReviewTool struct {
	Profile string
}

func (SkillReviewTool) Name() string { return "skill_review" }

func (SkillReviewTool) Description() string {
	return "Reviewer-only tool. Inspect and act on the skill proposal queue. " +
		"Actions: list (queue overview), get (full proposal), approve " +
		"(optionally with refined content), reject (with reason), merge " +
		"(consolidate ≥2 related proposals into one)."
}

func (SkillReviewTool) Parameters() json.RawMessage {
	return json.RawMessage(`{
  "type": "object",
  "required": ["action"],
  "properties": {
    "action":         {"type": "string", "enum": ["list", "get", "approve", "reject", "merge"]},
    "proposal_id":    {"type": "string", "description": "for get/approve/reject"},
    "proposal_ids":   {"type": "array", "items": {"type": "string"}, "description": "for merge — first id is the survivor"},
    "refined_content":{"type": "string", "description": "optional reviewer rewrite for approve"},
    "reason":         {"type": "string", "description": "required for reject; optional reviewer note for approve/merge"},
    "merged_content": {"type": "string", "description": "consolidated SKILL.md body for merge"}
  }
}`)
}

type skillReviewParams struct {
	Action         string   `json:"action"`
	ProposalID     string   `json:"proposal_id"`
	ProposalIDs    []string `json:"proposal_ids"`
	RefinedContent string   `json:"refined_content"`
	Reason         string   `json:"reason"`
	MergedContent  string   `json:"merged_content"`
}

func (t SkillReviewTool) Execute(_ context.Context, params json.RawMessage) (*Result, error) {
	var p skillReviewParams
	if len(params) > 0 {
		if err := json.Unmarshal(params, &p); err != nil {
			return &Result{Error: fmt.Sprintf("invalid parameters: %v", err)}, nil
		}
	}

	mgr := skills.NewManager(t.Profile)

	switch p.Action {
	case "list":
		return reviewList(mgr)

	case "get":
		if p.ProposalID == "" {
			return &Result{Error: "get requires proposal_id"}, nil
		}
		return reviewGet(mgr, p.ProposalID)

	case "approve":
		if p.ProposalID == "" {
			return &Result{Error: "approve requires proposal_id"}, nil
		}
		return reviewApprove(mgr, p.ProposalID, p.RefinedContent, p.Reason)

	case "reject":
		if p.ProposalID == "" {
			return &Result{Error: "reject requires proposal_id"}, nil
		}
		if p.Reason == "" {
			return &Result{Error: "reject requires reason"}, nil
		}
		if err := mgr.RejectProposal(p.ProposalID, p.Reason); err != nil {
			return &Result{Error: err.Error()}, nil
		}
		out, _ := json.Marshal(map[string]string{"ok": "true", "rejected": p.ProposalID})
		return &Result{Output: string(out)}, nil

	case "merge":
		if len(p.ProposalIDs) < 2 {
			return &Result{Error: "merge requires ≥2 proposal_ids"}, nil
		}
		if p.MergedContent == "" {
			return &Result{Error: "merge requires merged_content"}, nil
		}
		meta, err := mgr.MergeProposals(p.ProposalIDs, p.MergedContent, p.Reason)
		if err != nil {
			return &Result{Error: err.Error()}, nil
		}
		out, _ := json.Marshal(map[string]string{
			"ok":          "true",
			"merged_into": meta.Category + "/" + meta.Name,
			"survivor_id": meta.ID,
		})
		return &Result{Output: string(out)}, nil

	default:
		return &Result{Error: fmt.Sprintf("unknown action: %q", p.Action)}, nil
	}
}

func reviewList(mgr *skills.Manager) (*Result, error) {
	props, err := mgr.ListProposals()
	if err != nil {
		return &Result{Error: err.Error()}, nil
	}
	type entry struct {
		ID            string   `json:"id"`
		Name          string   `json:"name"`
		Category      string   `json:"category"`
		Description   string   `json:"description"`
		Source        string   `json:"source"`
		Intent        string   `json:"intent,omitempty"`
		IntentTargets []string `json:"intent_targets,omitempty"`
		IntentReason  string   `json:"intent_reason,omitempty"`
		CreatedAt     int64    `json:"created_at"`
		Blocked       bool     `json:"guard_blocked"`
		FindingsCount int      `json:"findings_count"`
	}
	out := make([]entry, 0, len(props))
	for _, p := range props {
		out = append(out, entry{
			ID:            p.Metadata.ID,
			Name:          p.Metadata.Name,
			Category:      p.Metadata.Category,
			Description:   p.Metadata.Description,
			Source:        p.Metadata.Source,
			Intent:        p.Metadata.Intent,
			IntentTargets: p.Metadata.IntentTargets,
			IntentReason:  p.Metadata.IntentReason,
			CreatedAt:     p.Metadata.CreatedAt,
			Blocked:       p.GuardResult.Blocked,
			FindingsCount: len(p.GuardResult.Findings),
		})
	}
	data, _ := json.Marshal(out)
	return &Result{Output: string(data)}, nil
}

func reviewGet(mgr *skills.Manager, id string) (*Result, error) {
	p, err := mgr.LoadProposal(id)
	if err != nil {
		return &Result{Error: err.Error()}, nil
	}
	out := map[string]interface{}{
		"metadata":     p.Metadata,
		"content":      p.Content,
		"guard_result": p.GuardResult,
	}
	data, _ := json.Marshal(out)
	return &Result{Output: string(data)}, nil
}

// reviewApprove handles both create-style proposals and curator-intent
// proposals. For curator intents, after PromoteProposal succeeds we invoke
// ApplyCuratorIntent to perform the side-effects (archive losers, materialise
// split children, etc.).
func reviewApprove(mgr *skills.Manager, id, refined, note string) (*Result, error) {
	// Snapshot the proposal first so we still have access to the intent
	// metadata + (for splits) the children dir after promotion deletes the
	// proposal directory.
	p, err := mgr.LoadProposal(id)
	if err != nil {
		return &Result{Error: err.Error()}, nil
	}

	// Save split-children dir path *before* promotion blows it away.
	splitChildrenDir := ""
	if p.Metadata.Intent == skills.IntentSplit {
		splitChildrenDir = filepath.Join(p.Dir, "split_children")
	}

	switch p.Metadata.Intent {
	case skills.IntentArchive, skills.IntentRecategorize:
		// These intents don't promote a SKILL.md — apply the side-effect and
		// drop the proposal.
		if err := mgr.ApplyCuratorIntent(p.Metadata, ""); err != nil {
			return &Result{Error: err.Error()}, nil
		}
		// Park the proposal in _rejected with reason "applied" so we keep an
		// audit trail of what curator actions were approved.
		_ = mgr.RejectProposal(id, "applied: "+p.Metadata.Intent+" — "+note)
		out, _ := json.Marshal(map[string]string{
			"ok":      "true",
			"applied": p.Metadata.Intent,
			"target":  joinTargets(p.Metadata.IntentTargets),
		})
		return &Result{Output: string(out)}, nil

	case skills.IntentSplit:
		// Apply the split (writes children + archives source) before
		// rejecting the proposal as "applied".
		if err := mgr.ApplyCuratorIntent(p.Metadata, splitChildrenDir); err != nil {
			return &Result{Error: err.Error()}, nil
		}
		_ = mgr.RejectProposal(id, "applied: split — "+note)
		out, _ := json.Marshal(map[string]string{
			"ok":      "true",
			"applied": "split",
			"source":  joinTargets(p.Metadata.IntentTargets),
		})
		return &Result{Output: string(out)}, nil

	default:
		// IntentCreate, IntentRefine, IntentMerge — all involve a SKILL.md
		// promotion. PromoteProposal handles refine-overwrite history snapshots.
		meta, err := mgr.PromoteProposal(id, refined, note)
		if err != nil {
			return &Result{Error: err.Error()}, nil
		}
		// For curator merges, archive losers afterwards.
		if meta.Intent == skills.IntentMerge {
			if err := mgr.ApplyCuratorIntent(meta, ""); err != nil {
				// Promotion already succeeded; report partial success.
				out, _ := json.Marshal(map[string]string{
					"ok":            "true",
					"approved":      meta.Category + "/" + meta.Name,
					"merge_warning": err.Error(),
				})
				return &Result{Output: string(out)}, nil
			}
		}
		out, _ := json.Marshal(map[string]string{
			"ok":       "true",
			"approved": meta.Category + "/" + meta.Name,
		})
		return &Result{Output: string(out)}, nil
	}
}

func joinTargets(t []string) string {
	if len(t) == 0 {
		return ""
	}
	if len(t) == 1 {
		return t[0]
	}
	out := t[0]
	for _, s := range t[1:] {
		out += "," + s
	}
	return out
}

var _ Tool = SkillReviewTool{}

func init() {
	// Registered globally so tools.Get can find it; the gateway is
	// responsible for ONLY exposing this tool to the reviewer agent loop.
	Register(SkillReviewTool{Profile: ""})
}
