package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/euraika-labs/pan-agent/internal/paths"
	"github.com/euraika-labs/pan-agent/internal/skills"
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

	// Validate category + name BEFORE any filepath.Join. Without this,
	// a crafted payload like {"name":"../../../../etc/passwd"} would
	// traverse out of the skills dir.
	if err := skills.ValidateCategory(category); err != nil {
		return &Result{Error: "invalid category: " + err.Error()}, nil
	}
	if err := skills.ValidateName(name); err != nil {
		return &Result{Error: "invalid name: " + err.Error()}, nil
	}

	// Try installed first, then bundled. Re-run a Rel containment check
	// at each base — the validators already reject traversal fragments,
	// but containment is cheap and defense-in-depth.
	base := filepath.Clean(filepath.Join(paths.ProfileSkillsDir(t.Profile), category, name))
	baseRoot := filepath.Clean(paths.ProfileSkillsDir(t.Profile))
	if !isContained(baseRoot, base) {
		return &Result{Error: "resolved skill path escapes profile skills dir"}, nil
	}
	if _, err := os.Stat(base); os.IsNotExist(err) {
		// Fall back to bundled skills next to the executable.
		bundled := paths.BundledSkillsDir()
		if bundled == "" {
			return &Result{Error: fmt.Sprintf("skill %s not found", p.Name)}, nil
		}
		bundledRoot := filepath.Clean(bundled)
		base = filepath.Clean(filepath.Join(bundledRoot, category, name))
		if !isContained(bundledRoot, base) {
			return &Result{Error: "resolved bundled skill path escapes bundled skills dir"}, nil
		}
	}

	target := filepath.Join(base, "SKILL.md")
	if p.FilePath != "" {
		if strings.Contains(p.FilePath, "..") || filepath.IsAbs(p.FilePath) {
			return &Result{Error: "file_path must be relative and not contain .."}, nil
		}
		target = filepath.Clean(filepath.Join(base, p.FilePath))
		if !isContained(base, target) {
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

// isContained reports whether child is strictly inside base. Base and
// child must already be filepath.Clean'd. Used as a defense-in-depth
// check after validator-based path construction.
func isContained(base, child string) bool {
	rel, err := filepath.Rel(base, child)
	if err != nil {
		return false
	}
	if rel == "." {
		return true
	}
	if strings.HasPrefix(rel, "..") {
		return false
	}
	if strings.HasPrefix(rel, string(filepath.Separator)) {
		return false
	}
	return true
}

func init() {
	Register(SkillsViewTool{Profile: ""})
}
