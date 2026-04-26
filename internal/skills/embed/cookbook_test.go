package embed

import (
	"strings"
	"testing"

	"github.com/euraika-labs/pan-agent/internal/skills"
)

// TestCookbookFS_BundledSkillsHaveValidFrontmatter walks every cookbook
// markdown file the binary embedded and asserts each one starts with a
// valid YAML frontmatter block carrying name + description.
//
// This is the load-bearing invariant for Phase 13 WS#13.E: the runtime
// walker (future SeedCookbook) parses frontmatter to surface skills in
// the desktop UI. A cookbook entry that ships without valid frontmatter
// would be silently dropped at runtime, so we fail the build (test
// pass is a prerequisite for shipping a new cookbook entry).
func TestCookbookFS_BundledSkillsHaveValidFrontmatter(t *testing.T) {
	entries, err := CookbookFS.ReadDir(CookbookRoot)
	if err != nil {
		t.Fatalf("ReadDir cookbook root: %v", err)
	}
	if len(entries) == 0 {
		t.Fatalf("cookbook root is empty — no skills bundled")
	}
	for _, e := range entries {
		if e.IsDir() {
			t.Errorf("nested directory %q not yet supported in cookbook", e.Name())
			continue
		}
		if !strings.HasSuffix(e.Name(), ".md") {
			t.Errorf("non-.md file %q in cookbook", e.Name())
			continue
		}
		path := CookbookRoot + "/" + e.Name()
		content, err := CookbookFS.ReadFile(path)
		if err != nil {
			t.Errorf("read %q: %v", path, err)
			continue
		}
		if err := skills.ValidateFrontmatter(string(content)); err != nil {
			t.Errorf("frontmatter invalid for %q: %v", path, err)
		}
	}
}

// TestCookbookFS_NamesDistinct guarantees the bundled skills don't
// collide on `name:` (the runtime keys on it). Catches a copy-paste
// bug where a new cookbook entry forgets to rename the frontmatter
// after duplicating an existing one.
func TestCookbookFS_NamesDistinct(t *testing.T) {
	entries, err := CookbookFS.ReadDir(CookbookRoot)
	if err != nil {
		t.Fatalf("ReadDir cookbook root: %v", err)
	}
	seen := make(map[string]string)
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		path := CookbookRoot + "/" + e.Name()
		content, err := CookbookFS.ReadFile(path)
		if err != nil {
			t.Fatalf("read %q: %v", path, err)
		}
		name := extractFrontmatterField(string(content), "name")
		if name == "" {
			t.Errorf("cookbook %q has empty/missing name in frontmatter", path)
			continue
		}
		if prev, dup := seen[name]; dup {
			t.Errorf("name collision: %q is used by both %q and %q", name, prev, path)
		}
		seen[name] = path
	}
	if len(seen) < len(entries) {
		// Already errored above; this is a sanity guard for the count.
		return
	}
}

// TestCookbookFS_KnownEntriesPresent pins the v0.7.0 cookbook contents
// so a future refactor can't accidentally drop a starter skill.
func TestCookbookFS_KnownEntriesPresent(t *testing.T) {
	wantNames := map[string]bool{
		"slack-summarise-channel":  true,
		"reminders-create":         true,
		"vscode-open-under-cursor": true,
	}
	entries, err := CookbookFS.ReadDir(CookbookRoot)
	if err != nil {
		t.Fatalf("ReadDir cookbook root: %v", err)
	}
	got := make(map[string]bool)
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		path := CookbookRoot + "/" + e.Name()
		content, err := CookbookFS.ReadFile(path)
		if err != nil {
			continue
		}
		got[extractFrontmatterField(string(content), "name")] = true
	}
	for want := range wantNames {
		if !got[want] {
			t.Errorf("cookbook missing expected starter skill: %q", want)
		}
	}
}

// extractFrontmatterField returns the trimmed value of a top-level
// `key: value` line in the leading YAML frontmatter block, or "" if
// the field is absent. Matches the simple-scalar contract of
// skills.ValidateFrontmatter without requiring a full YAML parser.
func extractFrontmatterField(content, field string) string {
	trimmed := strings.TrimLeft(content, "\r\n\t ")
	if !strings.HasPrefix(trimmed, "---") {
		return ""
	}
	body := trimmed[3:]
	end := strings.Index(body, "---")
	if end == -1 {
		return ""
	}
	for _, line := range strings.Split(body[:end], "\n") {
		line = strings.TrimRight(line, "\r")
		// Match "<field>: …" exactly to avoid prefix collisions
		// (e.g. "name_alt:" wouldn't match the field "name").
		prefix := field + ":"
		if !strings.HasPrefix(strings.TrimSpace(line), prefix) {
			continue
		}
		v := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), prefix))
		// Strip surrounding quotes if present.
		v = strings.Trim(v, `"'`)
		return v
	}
	return ""
}
