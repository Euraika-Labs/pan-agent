// Package skills lists, installs, and uninstalls agent skills.
//
// Installed skills live at:
//
//	<ProfileHome>/skills/<category>/<skill-name>/SKILL.md
//
// Bundled skills (shipped with the binary) live at:
//
//	<exe-dir>/skills/<category>/<skill-name>/SKILL.md
//
// Each SKILL.md may begin with YAML frontmatter delimited by "---" lines that
// carry "name" and "description" fields.  The parser falls back to the first
// Markdown heading and first non-heading paragraph when frontmatter is absent,
// mirroring the TypeScript implementation.
package skills

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/euraika-labs/pan-agent/internal/paths"
)

// ---------------------------------------------------------------------------
// Constants (match TypeScript values)
// ---------------------------------------------------------------------------

const (
	skillFrontmatterReadLen = 4000
	skillDescMaxLen         = 120
)

// ---------------------------------------------------------------------------
// Types
// ---------------------------------------------------------------------------

// Skill describes a single skill entry, combining the InstalledSkill and
// SkillSearchResult shapes from the TypeScript source into one unified struct.
type Skill struct {
	// Name is the human-readable display name parsed from SKILL.md.
	Name string
	// Category is the parent directory name (e.g. "coding", "research").
	Category string
	// Description is the short description parsed from SKILL.md frontmatter.
	Description string
	// Path is the absolute path to the skill directory (contains SKILL.md).
	Path string
	// Installed is true when the skill is present in a profile's skills dir.
	Installed bool
	// Bundled is true when the skill comes from the binary's bundled skills dir.
	Bundled bool
}

// ---------------------------------------------------------------------------
// Frontmatter parser
// ---------------------------------------------------------------------------

// parseSkillFrontmatter extracts name and description from the first 4 KB of
// a SKILL.md file.  It handles YAML frontmatter (--- … ---) and falls back to
// the first Markdown heading / paragraph when frontmatter is absent.
func parseSkillFrontmatter(content string) (name, description string) {
	if !strings.HasPrefix(content, "---") {
		// Fallback: first # heading → name, first non-heading line → description.
		for _, line := range strings.Split(content, "\n") {
			line = strings.TrimRight(line, "\r")
			if name == "" && strings.HasPrefix(line, "# ") {
				name = strings.TrimSpace(line[2:])
				continue
			}
			if description == "" && line != "" && !strings.HasPrefix(line, "#") && !strings.HasPrefix(line, "---") {
				if len(line) > skillDescMaxLen {
					line = line[:skillDescMaxLen]
				}
				description = strings.TrimSpace(line)
			}
			if name != "" && description != "" {
				break
			}
		}
		return
	}

	// Locate closing "---".
	endIdx := strings.Index(content[3:], "---")
	if endIdx == -1 {
		return
	}
	frontmatter := content[3 : endIdx+3]

	for _, line := range strings.Split(frontmatter, "\n") {
		line = strings.TrimRight(line, "\r")
		if name == "" {
			if after, ok := yamlScalarLine(line, "name"); ok {
				name = after
			}
		}
		if description == "" {
			if after, ok := yamlScalarLine(line, "description"); ok {
				description = after
			}
		}
	}
	return
}

// yamlScalarLine parses a line of the form "  key: value" and returns the
// trimmed, unquoted value.  ok is false when the line does not match.
func yamlScalarLine(line, key string) (string, bool) {
	trimmed := strings.TrimSpace(line)
	prefix := key + ":"
	if !strings.HasPrefix(trimmed, prefix) {
		return "", false
	}
	val := strings.TrimSpace(trimmed[len(prefix):])
	val = strings.Trim(val, `"'`)
	return val, true
}

// ---------------------------------------------------------------------------
// Directory walker
// ---------------------------------------------------------------------------

// walkSkillsDir walks a root skills directory and returns all skills found.
// root must be structured as root/<category>/<skill-name>/SKILL.md.
func walkSkillsDir(root string, installed, bundled bool) ([]Skill, error) {
	entries, err := os.ReadDir(root)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("skills: read dir %s: %w", root, err)
	}

	var skills []Skill

	for _, catEntry := range entries {
		if !catEntry.IsDir() {
			continue
		}
		category := catEntry.Name()
		catPath := filepath.Join(root, category)

		skillEntries, err := os.ReadDir(catPath)
		if err != nil {
			continue // best-effort; skip unreadable categories
		}

		for _, skillEntry := range skillEntries {
			if !skillEntry.IsDir() {
				continue
			}
			skillDir := filepath.Join(catPath, skillEntry.Name())
			skillFile := filepath.Join(skillDir, "SKILL.md")

			if _, err := os.Stat(skillFile); os.IsNotExist(err) {
				continue
			}

			name, desc := parseFromFile(skillFile, skillEntry.Name())

			skills = append(skills, Skill{
				Name:        name,
				Category:    category,
				Description: desc,
				Path:        skillDir,
				Installed:   installed,
				Bundled:     bundled,
			})
		}
	}

	sort.Slice(skills, func(i, j int) bool {
		if skills[i].Category != skills[j].Category {
			return skills[i].Category < skills[j].Category
		}
		return skills[i].Name < skills[j].Name
	})

	return skills, nil
}

// parseFromFile reads up to skillFrontmatterReadLen bytes from skillFile and
// returns the parsed name and description, falling back to entryName.
func parseFromFile(skillFile, entryName string) (name, desc string) {
	data, err := os.ReadFile(skillFile)
	if err != nil {
		return entryName, ""
	}
	content := string(data)
	if len(content) > skillFrontmatterReadLen {
		content = content[:skillFrontmatterReadLen]
	}
	name, desc = parseSkillFrontmatter(content)
	if name == "" {
		name = entryName
	}
	return
}

// ---------------------------------------------------------------------------
// Public API
// ---------------------------------------------------------------------------

// ListInstalled returns all skills installed under the profile's skills
// directory.  Pass "" or "default" for the default profile.
func ListInstalled(profile string) ([]Skill, error) {
	root := paths.ProfileSkillsDir(profile)
	return walkSkillsDir(root, true, false)
}

// ListBundled returns all skills shipped with the binary.  Returns nil when
// the bundled skills directory does not exist (e.g. development builds).
func ListBundled() ([]Skill, error) {
	root := paths.BundledSkillsDir()
	if root == "" {
		return nil, nil
	}
	return walkSkillsDir(root, false, true)
}

// GetContent reads and returns the full SKILL.md content for the skill at
// path.  path may be either the skill directory or the SKILL.md file itself.
// Returns "" when the file does not exist.
func GetContent(path string) (string, error) {
	// Accept both the directory and the file path.
	if !strings.HasSuffix(path, "SKILL.md") {
		path = filepath.Join(path, "SKILL.md")
	}
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("skills: read content %s: %w", path, err)
	}
	return string(data), nil
}

// Install creates the skill directory structure for the given id under the
// profile's skills directory.  id must be of the form "category/skill-name".
// The directory is created with an empty SKILL.md so that ListInstalled can
// discover it.
func Install(id, profile string) error {
	if err := validateID(id); err != nil {
		return err
	}
	skillDir := filepath.Join(paths.ProfileSkillsDir(profile), filepath.FromSlash(id))
	if err := os.MkdirAll(skillDir, 0o700); err != nil {
		return fmt.Errorf("skills: install mkdir %s: %w", skillDir, err)
	}
	skillFile := filepath.Join(skillDir, "SKILL.md")
	if _, err := os.Stat(skillFile); os.IsNotExist(err) {
		if err := os.WriteFile(skillFile, []byte(""), 0o600); err != nil {
			return fmt.Errorf("skills: install write SKILL.md: %w", err)
		}
	}
	return nil
}

// Uninstall removes the skill directory for the given id from the profile's
// skills directory.  id must be of the form "category/skill-name".
func Uninstall(id, profile string) error {
	if err := validateID(id); err != nil {
		return err
	}
	skillDir := filepath.Join(paths.ProfileSkillsDir(profile), filepath.FromSlash(id))
	if _, err := os.Stat(skillDir); os.IsNotExist(err) {
		return fmt.Errorf("skills: skill %q not installed", id)
	}
	if err := os.RemoveAll(skillDir); err != nil {
		return fmt.Errorf("skills: uninstall %s: %w", skillDir, err)
	}
	return nil
}

// validateID checks that id has exactly one "/" separator and no path-traversal
// components.
func validateID(id string) error {
	parts := strings.SplitN(id, "/", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return fmt.Errorf("skills: id %q must be in the form 'category/skill-name'", id)
	}
	for _, p := range parts {
		if strings.Contains(p, "..") || strings.ContainsAny(p, `\:*?"<>|`) {
			return fmt.Errorf("skills: id %q contains invalid characters", id)
		}
	}
	return nil
}
