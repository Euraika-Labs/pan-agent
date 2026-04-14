package skills

import (
	"fmt"
	"strings"
	"time"
)

// EnsureFrontmatter prepends a minimal YAML frontmatter block to content if
// not already present. This keeps every proposal skill-parseable by the
// existing walker in this package.
func EnsureFrontmatter(content, name, description string) string {
	trimmed := strings.TrimLeft(content, "\r\n\t ")
	if strings.HasPrefix(trimmed, "---") {
		return content
	}
	// Escape double quotes in description for YAML safety.
	escDesc := strings.ReplaceAll(description, `"`, `\"`)
	fm := fmt.Sprintf("---\nname: %s\ndescription: \"%s\"\n---\n\n", name, escDesc)
	return fm + content
}

// ValidateFrontmatter confirms content starts with a valid frontmatter block
// containing at least `name` and `description`.
func ValidateFrontmatter(content string) error {
	if !strings.HasPrefix(strings.TrimLeft(content, "\r\n\t "), "---") {
		return fmt.Errorf("missing YAML frontmatter (--- at start)")
	}
	// Locate closing fence.
	end := strings.Index(content[3:], "---")
	if end == -1 {
		return fmt.Errorf("missing closing YAML frontmatter fence")
	}
	block := content[3 : end+3]
	hasName := false
	hasDesc := false
	for _, line := range strings.Split(block, "\n") {
		line = strings.TrimRight(line, "\r")
		if _, ok := yamlScalarLine(line, "name"); ok {
			hasName = true
		}
		if _, ok := yamlScalarLine(line, "description"); ok {
			hasDesc = true
		}
	}
	if !hasName {
		return fmt.Errorf("frontmatter missing required field: name")
	}
	if !hasDesc {
		return fmt.Errorf("frontmatter missing required field: description")
	}
	return nil
}

// timeNowMillis returns wall-clock millis.
func timeNowMillis() int64 {
	return time.Now().UnixMilli()
}
