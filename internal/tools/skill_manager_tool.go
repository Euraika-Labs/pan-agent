package tools

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/euraika-labs/pan-agent/internal/skills"
)

// SkillManagerTool lets the agent create, edit, or delete skills. New skills
// land in the proposal queue (_proposed/<uuid>/) and must pass the reviewer
// before they become active. Edits run the guard scanner and roll back on
// block. Every successful write snapshots the previous version to _history/.
//
// This mirrors hermes-agent's skill_manage(action=...) tool with six actions.
type SkillManagerTool struct {
	Profile   string
	SessionID string // set by chat handler per-request; "" when called out-of-band
}

func (SkillManagerTool) Name() string { return "skill_manage" }

func (SkillManagerTool) Description() string {
	return "Create, edit, or delete skills. Use this when you encounter a " +
		"complex workflow that is not yet captured in an installed skill, " +
		"or when an existing skill needs refinement. New skills go to a " +
		"proposal queue and become active after review. Actions: " +
		"create (new skill), edit (replace SKILL.md), delete (archive), " +
		"write_file (add a supporting file), remove_file (remove supporting)."
}

func (SkillManagerTool) Parameters() json.RawMessage {
	return json.RawMessage(`{
  "type": "object",
  "required": ["action"],
  "properties": {
    "action": {
      "type": "string",
      "enum": ["create", "edit", "delete", "write_file", "remove_file"],
      "description": "which operation to perform"
    },
    "name":        {"type": "string", "description": "for create: kebab-case skill name. For others: '<category>/<name>'"},
    "category":    {"type": "string", "description": "required for create; e.g. 'coding', 'research'"},
    "description": {"type": "string", "description": "required for create; short description"},
    "content":     {"type": "string", "description": "SKILL.md body for create/edit"},
    "reason":      {"type": "string", "description": "why this action (esp. delete)"},
    "file_path":   {"type": "string", "description": "relative path for write_file/remove_file (under references/templates/scripts/assets)"},
    "file_content":{"type": "string", "description": "body for write_file"}
  }
}`)
}

type skillManageParams struct {
	Action      string `json:"action"`
	Name        string `json:"name"`
	Category    string `json:"category"`
	Description string `json:"description"`
	Content     string `json:"content"`
	Reason      string `json:"reason"`
	FilePath    string `json:"file_path"`
	FileContent string `json:"file_content"`
}

func (t SkillManagerTool) Execute(_ context.Context, params json.RawMessage) (*Result, error) {
	var p skillManageParams
	if err := json.Unmarshal(params, &p); err != nil {
		return &Result{Error: fmt.Sprintf("invalid parameters: %v", err)}, nil
	}

	mgr := skills.NewManager(t.Profile)

	switch p.Action {
	case "create":
		if p.Name == "" || p.Category == "" || p.Description == "" || p.Content == "" {
			return &Result{Error: "create requires name, category, description, content"}, nil
		}
		meta, scan, err := mgr.CreateProposal(p.Name, p.Category, p.Description, p.Content, t.SessionID, "agent")
		if err != nil {
			return &Result{Error: err.Error()}, nil
		}
		resp := map[string]interface{}{
			"ok":          true,
			"proposal_id": meta.ID,
			"status":      meta.Status,
			"findings":    scan.Findings,
			"message":     "skill proposed; it will activate after reviewer approval",
		}
		data, _ := json.Marshal(resp)
		return &Result{Output: string(data)}, nil

	case "edit":
		category, name, err := splitSkillID(p.Name)
		if err != nil {
			return &Result{Error: err.Error()}, nil
		}
		if p.Content == "" {
			return &Result{Error: "edit requires content"}, nil
		}
		scan, err := mgr.EditActiveSkill(category, name, p.Content)
		if err != nil {
			return &Result{Error: err.Error()}, nil
		}
		resp := map[string]interface{}{
			"ok":       true,
			"findings": scan.Findings,
			"message":  fmt.Sprintf("skill %s/%s edited; previous version saved to _history/", category, name),
		}
		data, _ := json.Marshal(resp)
		return &Result{Output: string(data)}, nil

	case "delete":
		category, name, err := splitSkillID(p.Name)
		if err != nil {
			return &Result{Error: err.Error()}, nil
		}
		if err := mgr.DeleteActiveSkill(category, name, p.Reason); err != nil {
			return &Result{Error: err.Error()}, nil
		}
		return &Result{Output: fmt.Sprintf("skill %s/%s archived", category, name)}, nil

	case "write_file":
		category, name, err := splitSkillID(p.Name)
		if err != nil {
			return &Result{Error: err.Error()}, nil
		}
		if p.FilePath == "" {
			return &Result{Error: "write_file requires file_path"}, nil
		}
		if err := mgr.WriteSupportingFile(category, name, p.FilePath, []byte(p.FileContent)); err != nil {
			return &Result{Error: err.Error()}, nil
		}
		return &Result{Output: fmt.Sprintf("wrote %s to %s/%s", p.FilePath, category, name)}, nil

	case "remove_file":
		category, name, err := splitSkillID(p.Name)
		if err != nil {
			return &Result{Error: err.Error()}, nil
		}
		if p.FilePath == "" {
			return &Result{Error: "remove_file requires file_path"}, nil
		}
		if err := mgr.RemoveSupportingFile(category, name, p.FilePath); err != nil {
			return &Result{Error: err.Error()}, nil
		}
		return &Result{Output: fmt.Sprintf("removed %s from %s/%s", p.FilePath, category, name)}, nil

	default:
		return &Result{Error: fmt.Sprintf("unknown action: %q", p.Action)}, nil
	}
}

// splitSkillID parses "<category>/<name>" into its parts.
func splitSkillID(id string) (category, name string, err error) {
	if id == "" {
		return "", "", fmt.Errorf("name is required (format '<category>/<name>')")
	}
	parts := splitOnce(id, '/')
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", fmt.Errorf("name must be '<category>/<name>', got %q", id)
	}
	return parts[0], parts[1], nil
}

// splitOnce splits s at the first occurrence of sep.
func splitOnce(s string, sep byte) []string {
	for i := 0; i < len(s); i++ {
		if s[i] == sep {
			return []string{s[:i], s[i+1:]}
		}
	}
	return []string{s}
}

var _ Tool = SkillManagerTool{}

func init() {
	Register(SkillManagerTool{Profile: ""})
}
