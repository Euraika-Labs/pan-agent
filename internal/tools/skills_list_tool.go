package tools

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/euraika-labs/pan-agent/internal/skills"
)

// SkillsListTool returns a concise list of installed + bundled skills.
// Shape matches the hermes "skills_list" read-only tool so agents can
// discover available skills without flooding the context window.
type SkillsListTool struct {
	Profile string
}

func (SkillsListTool) Name() string { return "skills_list" }

func (SkillsListTool) Description() string {
	return "List installed and bundled skills available to the agent. " +
		"Returns name, category, and short description for each skill. " +
		"Use skill_view to load full content."
}

func (SkillsListTool) Parameters() json.RawMessage {
	return json.RawMessage(`{
  "type": "object",
  "properties": {
    "category": {"type": "string", "description": "optional category filter (e.g. coding, research)"}
  }
}`)
}

type skillsListParams struct {
	Category string `json:"category"`
}

type skillsListEntry struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Category    string `json:"category"`
	Description string `json:"description"`
	Bundled     bool   `json:"bundled"`
}

func (t SkillsListTool) Execute(_ context.Context, params json.RawMessage) (*Result, error) {
	var p skillsListParams
	if len(params) > 0 {
		_ = json.Unmarshal(params, &p) // params are optional
	}

	installed, err := skills.ListInstalled(t.Profile)
	if err != nil {
		return &Result{Error: fmt.Sprintf("list installed: %v", err)}, nil
	}
	bundled, _ := skills.ListBundled()

	entries := make([]skillsListEntry, 0, len(installed)+len(bundled))
	for _, s := range installed {
		if p.Category != "" && s.Category != p.Category {
			continue
		}
		entries = append(entries, skillsListEntry{
			ID:          s.Category + "/" + s.Name,
			Name:        s.Name,
			Category:    s.Category,
			Description: s.Description,
			Bundled:     false,
		})
	}
	for _, s := range bundled {
		if p.Category != "" && s.Category != p.Category {
			continue
		}
		entries = append(entries, skillsListEntry{
			ID:          s.Category + "/" + s.Name,
			Name:        s.Name,
			Category:    s.Category,
			Description: s.Description,
			Bundled:     true,
		})
	}

	data, err := json.Marshal(entries)
	if err != nil {
		return &Result{Error: fmt.Sprintf("marshal: %v", err)}, nil
	}
	return &Result{Output: string(data)}, nil
}

var _ Tool = SkillsListTool{}

func init() {
	Register(SkillsListTool{Profile: ""})
}
