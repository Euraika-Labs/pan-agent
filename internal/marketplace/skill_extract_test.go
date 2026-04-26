package marketplace

import (
	"crypto/ed25519"
	"errors"
	"strings"
	"testing"
)

// Phase 13 WS#13.C — extraction tests. Build verified bundles via the
// same makeBundle helper from bundle_test.go (the helper itself
// signs + writes to a temp dir), then run ExtractSkill across them.

const minimalFrontmatter = `---
name: weather-tool
description: Look up the weather at a given location
category: utility
---
# Weather

Use this skill to fetch the current weather.
`

func TestExtractSkill_HappyPath(t *testing.T) {
	t.Parallel()
	kp, _ := GenerateKeypair()
	root := makeBundle(t, kp, []fixtureFile{
		{Path: SkillFilename, Content: []byte(minimalFrontmatter)},
		{Path: "examples/sample.txt", Content: []byte("sample input")},
	}, nil)

	b, err := LoadBundle(root, []ed25519.PublicKey{kp.Public})
	if err != nil {
		t.Fatalf("LoadBundle: %v", err)
	}
	got, err := ExtractSkill(b)
	if err != nil {
		t.Fatalf("ExtractSkill: %v", err)
	}
	if got.Name != "weather-tool" {
		t.Errorf("Name = %q, want weather-tool", got.Name)
	}
	if got.Category != "utility" {
		t.Errorf("Category = %q, want utility", got.Category)
	}
	if !strings.Contains(got.Description, "weather") {
		t.Errorf("Description = %q, missing 'weather'", got.Description)
	}
	if got.Content != minimalFrontmatter {
		t.Error("Content not preserved verbatim")
	}
	if len(got.Supporting) != 1 {
		t.Errorf("supporting count = %d, want 1", len(got.Supporting))
	}
	if string(got.Supporting["examples/sample.txt"]) != "sample input" {
		t.Errorf("supporting bytes wrong: %q", got.Supporting["examples/sample.txt"])
	}
}

func TestExtractSkill_NoSkillMD(t *testing.T) {
	t.Parallel()
	kp, _ := GenerateKeypair()
	root := makeBundle(t, kp, []fixtureFile{
		{Path: "README.md", Content: []byte("# nope")},
	}, nil)
	b, _ := LoadBundle(root, []ed25519.PublicKey{kp.Public})

	_, err := ExtractSkill(b)
	if !errors.Is(err, ErrBundleInvalid) {
		t.Errorf("err = %v, want ErrBundleInvalid", err)
	}
	if !strings.Contains(err.Error(), SkillFilename) {
		t.Errorf("error should name SKILL.md: %v", err)
	}
}

func TestExtractSkill_NilBundle(t *testing.T) {
	t.Parallel()
	_, err := ExtractSkill(nil)
	if !errors.Is(err, ErrBundleInvalid) {
		t.Errorf("err = %v, want ErrBundleInvalid", err)
	}
}

func TestExtractSkill_MissingCategory(t *testing.T) {
	t.Parallel()
	kp, _ := GenerateKeypair()
	body := `---
name: x
description: a description
---
content`
	root := makeBundle(t, kp, []fixtureFile{
		{Path: SkillFilename, Content: []byte(body)},
	}, nil)
	b, _ := LoadBundle(root, []ed25519.PublicKey{kp.Public})
	_, err := ExtractSkill(b)
	if !errors.Is(err, ErrBundleInvalid) {
		t.Errorf("err = %v, want ErrBundleInvalid", err)
	}
	if !strings.Contains(err.Error(), "category") {
		t.Errorf("error should mention category: %v", err)
	}
}

func TestExtractSkill_FrontmatterFallsBackToManifest(t *testing.T) {
	t.Parallel()
	kp, _ := GenerateKeypair()
	// SKILL.md without name/description in frontmatter — fall back to
	// manifest fields. Category MUST be in frontmatter though.
	body := `---
category: utility
---
content`
	root := makeBundle(t, kp, []fixtureFile{
		{Path: SkillFilename, Content: []byte(body)},
	}, func(m *Manifest) {
		m.Name = "manifest-name"
		m.Description = "from manifest"
	})
	b, _ := LoadBundle(root, []ed25519.PublicKey{kp.Public})
	got, err := ExtractSkill(b)
	if err != nil {
		t.Fatalf("ExtractSkill: %v", err)
	}
	if got.Name != "manifest-name" {
		t.Errorf("Name = %q, want manifest-name (frontmatter fallback)", got.Name)
	}
	if got.Description != "from manifest" {
		t.Errorf("Description = %q, want from manifest", got.Description)
	}
}

func TestExtractSkill_InvalidName(t *testing.T) {
	t.Parallel()
	kp, _ := GenerateKeypair()
	body := `---
name: "has spaces and !! chars"
description: x
category: utility
---
content`
	root := makeBundle(t, kp, []fixtureFile{
		{Path: SkillFilename, Content: []byte(body)},
	}, nil)
	b, _ := LoadBundle(root, []ed25519.PublicKey{kp.Public})
	_, err := ExtractSkill(b)
	if !errors.Is(err, ErrBundleInvalid) {
		t.Errorf("err = %v, want ErrBundleInvalid", err)
	}
}

func TestExtractSkill_DescriptionLengthBounds(t *testing.T) {
	t.Parallel()
	kp, _ := GenerateKeypair()
	long := strings.Repeat("a", MaxSkillDescriptionLen+1)
	body := `---
name: x
description: ` + long + `
category: utility
---
content`
	root := makeBundle(t, kp, []fixtureFile{
		{Path: SkillFilename, Content: []byte(body)},
	}, nil)
	b, _ := LoadBundle(root, []ed25519.PublicKey{kp.Public})
	_, err := ExtractSkill(b)
	if !errors.Is(err, ErrBundleInvalid) {
		t.Errorf("err = %v, want ErrBundleInvalid (long description)", err)
	}
}

func TestExtractSkill_QuotedFrontmatterValues(t *testing.T) {
	t.Parallel()
	kp, _ := GenerateKeypair()
	body := `---
name: "valid-name"
description: 'a quoted description'
category: "tools"
---
content`
	root := makeBundle(t, kp, []fixtureFile{
		{Path: SkillFilename, Content: []byte(body)},
	}, nil)
	b, _ := LoadBundle(root, []ed25519.PublicKey{kp.Public})
	got, err := ExtractSkill(b)
	if err != nil {
		t.Fatalf("ExtractSkill: %v", err)
	}
	if got.Name != "valid-name" {
		t.Errorf("Name = %q, want valid-name (quote-stripped)", got.Name)
	}
	if got.Description != "a quoted description" {
		t.Errorf("Description = %q", got.Description)
	}
	if got.Category != "tools" {
		t.Errorf("Category = %q", got.Category)
	}
}

func TestExtractSkill_NoFrontmatter(t *testing.T) {
	t.Parallel()
	kp, _ := GenerateKeypair()
	body := `# Skill body without frontmatter

This skill has no YAML header at all.
`
	root := makeBundle(t, kp, []fixtureFile{
		{Path: SkillFilename, Content: []byte(body)},
	}, func(m *Manifest) {
		m.Name = "manifest-fallback"
		m.Description = "from manifest"
	})
	b, _ := LoadBundle(root, []ed25519.PublicKey{kp.Public})
	_, err := ExtractSkill(b)
	// No frontmatter → category missing → ErrBundleInvalid.
	if !errors.Is(err, ErrBundleInvalid) {
		t.Errorf("err = %v, want ErrBundleInvalid (missing category)", err)
	}
}

func TestExtractSkill_MultipleSupportingFiles(t *testing.T) {
	t.Parallel()
	kp, _ := GenerateKeypair()
	root := makeBundle(t, kp, []fixtureFile{
		{Path: SkillFilename, Content: []byte(minimalFrontmatter)},
		{Path: "templates/a.txt", Content: []byte("aaa")},
		{Path: "templates/b.txt", Content: []byte("bbb")},
		{Path: "examples/c.txt", Content: []byte("ccc")},
	}, nil)
	b, _ := LoadBundle(root, []ed25519.PublicKey{kp.Public})
	got, err := ExtractSkill(b)
	if err != nil {
		t.Fatalf("ExtractSkill: %v", err)
	}
	if len(got.Supporting) != 3 {
		t.Errorf("supporting count = %d, want 3", len(got.Supporting))
	}
	for _, p := range []string{"templates/a.txt", "templates/b.txt", "examples/c.txt"} {
		if _, ok := got.Supporting[p]; !ok {
			t.Errorf("missing supporting %q", p)
		}
	}
}

func TestFrontmatterField_BasicAndEdgeCases(t *testing.T) {
	t.Parallel()
	const fm = `---
name: test
description: hello
empty:
  spaced:    value with spaces
quoted: "wrapped"
---
body`
	cases := map[string]string{
		"name":        "test",
		"description": "hello",
		"empty":       "",
		"quoted":      "wrapped",
		"missing":     "",
	}
	for k, want := range cases {
		if got := frontmatterField(fm, k); got != want {
			t.Errorf("frontmatterField(%q) = %q, want %q", k, got, want)
		}
	}

	// No frontmatter block at all.
	if got := frontmatterField("no frontmatter here", "name"); got != "" {
		t.Errorf("no-block: got %q, want empty", got)
	}
	// Open-ended frontmatter (no closing `---`).
	if got := frontmatterField("---\nname: x\nbody\n", "name"); got != "" {
		t.Errorf("unclosed frontmatter: got %q, want empty", got)
	}
}
