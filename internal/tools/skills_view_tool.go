package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/euraika-labs/pan-agent/internal/paths"
)

// SkillsViewTool returns the full SKILL.md content (or a supporting file)
// for a named skill. Agents use this for progressive disclosure.
type SkillsViewTool struct {
	Profile string
}

func (SkillsViewTool) Name() string { return "skill_view" }

func (SkillsViewTool) Description() string {
	return "Read the full SKILL.md content for a named skill. Pass name as " +
		"'<category>/<skill-name>'. Optionally pass file_path to read a " +
		"supporting file under the skill directory (references/, templates/, " +
		"scripts/, assets/)."
}

func (SkillsViewTool) Parameters() json.RawMessage {
	return json.RawMessage(`{
  "type": "object",
  "required": ["name"],
  "properties": {
    "name":       {"type": "string", "description": "<category>/<skill-name>"},
    "file_path":  {"type": "string", "description": "optional relative path to a supporting file"}
  }
}`)
}

type skillsViewParams struct {
	Name     string `json:"name"`
	FilePath string `json:"file_path"`
}

func (t SkillsViewTool) Execute(_ context.Context, params json.RawMessage) (*Result, error) {
	var p skillsViewParams
	if err := json.Unmarshal(params, &p); err != nil {
		return &Result{Error: fmt.Sprintf("invalid parameters: %v", err)}, nil
	}
	if p.Name == "" {
		return &Result{Error: "name is required"}, nil
	}

	parts := strings.SplitN(p.Name, "/", 2)
	if len(parts) != 2 {
		return &Result{Error: "name must be '<category>/<skill-name>'"}, nil
	}
	category, name := parts[0], parts[1]

	// Try installed first, then bundled.
	base := filepath.Join(paths.ProfileSkillsDir(t.Profile), category, name)
	if _, err := os.Stat(base); os.IsNotExist(err) {
		// Fall back to bundled skills next to the executable.
		bundled := paths.BundledSkillsDir()
		if bundled == "" {
			return &Result{Error: fmt.Sprintf("skill %s not found", p.Name)}, nil
		}
		base = filepath.Join(bundled, category, name)
	}

	target := filepath.Join(base, "SKILL.md")
	if p.FilePath != "" {
		if strings.Contains(p.FilePath, "..") {
			return &Result{Error: "file_path contains .."}, nil
		}
		target = filepath.Join(base, p.FilePath)
		// Ensure target is inside base.
		rel, err := filepath.Rel(base, target)
		if err != nil || strings.HasPrefix(rel, "..") {
			return &Result{Error: "file_path escapes skill directory"}, nil
		}
	}

	data, err := os.ReadFile(target)
	if err != nil {
		return &Result{Error: fmt.Sprintf("read %s: %v", target, err)}, nil
	}
	// Cap at 100KB returned.
	const maxReturn = 100_000
	if len(data) > maxReturn {
		data = append(data[:maxReturn], []byte("\n... (truncated)")...)
	}
	return &Result{Output: string(data)}, nil
}

var _ Tool = SkillsViewTool{}

func init() {
	Register(SkillsViewTool{Profile: ""})
}
