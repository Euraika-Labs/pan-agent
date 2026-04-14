package tools

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/euraika-labs/pan-agent/internal/skills"
	"github.com/euraika-labs/pan-agent/internal/storage"
)

// SkillCuratorTool is the curator agent's interface for re-arranging the
// active skill library. Like SkillReviewTool, the gateway is responsible for
// only exposing it to the curator loop. Every action *proposes* a change —
// the reviewer must approve before anything mutates active state.
type SkillCuratorTool struct {
	Profile string
	DB      *storage.DB // optional — when nil, list_active_with_usage returns 0 counts
}

func (SkillCuratorTool) Name() string { return "skill_curator" }

func (SkillCuratorTool) Description() string {
	return "Curator-only tool. Inspect active skills with usage stats and " +
		"propose refinements/merges/splits/archives/recategorisations. " +
		"Each proposal lands in the reviewer queue."
}

func (SkillCuratorTool) Parameters() json.RawMessage {
	return json.RawMessage(`{
  "type": "object",
  "required": ["action"],
  "properties": {
    "action": {
      "type": "string",
      "enum": ["list_active_with_usage", "propose_refinement",
               "propose_merge", "propose_split",
               "propose_archive", "propose_recategorize"]
    },
    "skill_id":     {"type": "string", "description": "<category>/<name> for refinement/archive/recategorize/split"},
    "skill_ids":    {"type": "array", "items": {"type": "string"}, "description": "for merge — first id is the survivor"},
    "new_content":  {"type": "string", "description": "for propose_refinement"},
    "consolidated":{"type": "string", "description": "for propose_merge"},
    "new_category": {"type": "string", "description": "for propose_recategorize"},
    "new_skills":   {
       "type": "array",
       "description": "for propose_split — children to materialise on approval",
       "items": {
         "type": "object",
         "required": ["category", "name", "description", "content"],
         "properties": {
           "category":    {"type": "string"},
           "name":        {"type": "string"},
           "description": {"type": "string"},
           "content":     {"type": "string"}
         }
       }
    },
    "reason":       {"type": "string", "description": "free-text justification (recommended for every action)"}
  }
}`)
}

type skillCuratorParams struct {
	Action       string                 `json:"action"`
	SkillID      string                 `json:"skill_id"`
	SkillIDs     []string               `json:"skill_ids"`
	NewContent   string                 `json:"new_content"`
	Consolidated string                 `json:"consolidated"`
	NewCategory  string                 `json:"new_category"`
	NewSkills    []skills.SplitProposal `json:"new_skills"`
	Reason       string                 `json:"reason"`
}

func (t SkillCuratorTool) Execute(_ context.Context, params json.RawMessage) (*Result, error) {
	var p skillCuratorParams
	if len(params) > 0 {
		if err := json.Unmarshal(params, &p); err != nil {
			return &Result{Error: fmt.Sprintf("invalid parameters: %v", err)}, nil
		}
	}

	mgr := skills.NewManager(t.Profile)

	switch p.Action {
	case "list_active_with_usage":
		return curatorListActive(mgr, t.Profile, t.DB)

	case "propose_refinement":
		cat, name, err := splitSkillID(p.SkillID)
		if err != nil {
			return &Result{Error: err.Error()}, nil
		}
		if p.NewContent == "" {
			return &Result{Error: "propose_refinement requires new_content"}, nil
		}
		meta, scan, err := mgr.ProposeCuratorRefinement(cat, name, p.NewContent, p.Reason, "curator")
		if err != nil {
			return &Result{Error: err.Error()}, nil
		}
		return curatorOK(meta, scan)

	case "propose_merge":
		if len(p.SkillIDs) < 2 {
			return &Result{Error: "propose_merge requires ≥2 skill_ids"}, nil
		}
		if p.Consolidated == "" {
			return &Result{Error: "propose_merge requires consolidated"}, nil
		}
		meta, scan, err := mgr.ProposeCuratorMerge(p.SkillIDs, p.Consolidated, p.Reason, "curator")
		if err != nil {
			return &Result{Error: err.Error()}, nil
		}
		return curatorOK(meta, scan)

	case "propose_split":
		if p.SkillID == "" {
			return &Result{Error: "propose_split requires skill_id"}, nil
		}
		if len(p.NewSkills) < 2 {
			return &Result{Error: "propose_split requires ≥2 entries in new_skills"}, nil
		}
		meta, scan, err := mgr.ProposeCuratorSplit(p.SkillID, p.NewSkills, p.Reason, "curator")
		if err != nil {
			return &Result{Error: err.Error()}, nil
		}
		return curatorOK(meta, scan)

	case "propose_archive":
		if p.SkillID == "" {
			return &Result{Error: "propose_archive requires skill_id"}, nil
		}
		meta, err := mgr.ProposeCuratorArchive(p.SkillID, p.Reason, "curator")
		if err != nil {
			return &Result{Error: err.Error()}, nil
		}
		return curatorOK(meta, skills.ReviewResult{})

	case "propose_recategorize":
		if p.SkillID == "" || p.NewCategory == "" {
			return &Result{Error: "propose_recategorize requires skill_id and new_category"}, nil
		}
		meta, err := mgr.ProposeCuratorRecategorize(p.SkillID, p.NewCategory, p.Reason, "curator")
		if err != nil {
			return &Result{Error: err.Error()}, nil
		}
		return curatorOK(meta, skills.ReviewResult{})

	default:
		return &Result{Error: fmt.Sprintf("unknown action: %q", p.Action)}, nil
	}
}

// curatorListActive enumerates active installed skills and joins them with
// usage stats from the SkillUsage table.
func curatorListActive(mgr *skills.Manager, profile string, db *storage.DB) (*Result, error) {
	installed, err := skills.ListInstalled(profile)
	if err != nil {
		return &Result{Error: err.Error()}, nil
	}
	type entry struct {
		ID          string `json:"id"`
		Name        string `json:"name"`
		Category    string `json:"category"`
		Description string `json:"description"`
		Usage       int    `json:"usage_count"`
		SuccessRate int    `json:"success_rate_pct"`
		LastUsedAt  int64  `json:"last_used_at"`
	}
	out := make([]entry, 0, len(installed))
	for _, s := range installed {
		id := s.Category + "/" + s.Name
		e := entry{
			ID: id, Name: s.Name, Category: s.Category, Description: s.Description,
		}
		if db != nil {
			stats, err := db.GetSkillUsageStats(id)
			if err == nil {
				e.Usage = stats.TotalCount
				e.SuccessRate = stats.SuccessRate
				e.LastUsedAt = stats.LastUsedAt
			}
		}
		out = append(out, e)
	}
	_ = mgr // mgr currently unused here; reserved for future use (e.g., reading metadata)
	data, _ := json.Marshal(out)
	return &Result{Output: string(data)}, nil
}

func curatorOK(meta skills.ProposalMetadata, scan skills.ReviewResult) (*Result, error) {
	resp := map[string]interface{}{
		"ok":          true,
		"proposal_id": meta.ID,
		"intent":      meta.Intent,
		"status":      meta.Status,
		"findings":    scan.Findings,
	}
	data, _ := json.Marshal(resp)
	return &Result{Output: string(data)}, nil
}

var _ Tool = SkillCuratorTool{}

func init() {
	Register(SkillCuratorTool{Profile: ""})
}
