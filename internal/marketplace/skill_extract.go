package marketplace

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// Phase 13 WS#13.C — bundle → skill contents.
//
// A verified Bundle is just a directory tree the producer signed. The
// install pipeline needs to translate that into something the existing
// reviewer-agent queue understands: one SKILL.md (with frontmatter)
// plus zero or more supporting files.
//
// ExtractSkill is the boundary. Given a *Bundle, it:
//
//   1. Locates the SKILL.md file (must be in the manifest).
//   2. Parses the frontmatter to pull out name + category +
//      description (the same fields skills.CreateProposal needs).
//   3. Reads every other manifest-declared file as a supporting file.
//   4. Returns a SkillContents struct ready to hand to the next slice
//      that will call into internal/skills.
//
// We deliberately don't import internal/skills here — that boundary
// is owned by the next slice. Keeping the marketplace package
// skill-package-free makes both sides independently testable + lets
// the same parser feed into a future "preview manifest before
// install" UI flow that doesn't touch the proposal queue.

// SkillFilename is the conventional path to the skill's main file.
// Hardcoded so a producer can't rename it to slip the parser.
const SkillFilename = "SKILL.md"

// MinDescriptionLen / MaxDescriptionLen / MaxSkillContentLen mirror
// the bounds the existing skill manager enforces. Surfacing them
// here lets the marketplace reject malformed bundles BEFORE the
// reviewer pipeline sees them so error messages stay close to the
// producer.
const (
	MaxSkillNameLen        = 63
	MinSkillDescriptionLen = 1
	MaxSkillDescriptionLen = 512
	MaxSkillContentLen     = 256 * 1024 // 256KB cap on SKILL.md
	MaxSupportingFileLen   = 1024 * 1024
)

// nameRe mirrors paths.ValidateProfile + skills.ValidateName: ASCII
// alphanumeric, underscore, hyphen, length 1..63, must start with
// alphanumeric. Marketplace bundles that pass this same shape can
// be staged without a second validation pass on the skills side.
var nameRe = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_-]{0,62}$`)

// SkillContents is the parsed view a Bundle yields up to the
// install pipeline. Content is the raw SKILL.md bytes (frontmatter
// included, since the existing reviewer agent re-parses); Supporting
// holds every other manifest-listed file by its bundle-relative path.
type SkillContents struct {
	Name        string
	Category    string
	Description string
	Content     string            // raw SKILL.md bytes (frontmatter included)
	Supporting  map[string][]byte // bundle-relative path → bytes
}

// ExtractSkill walks b's manifest, locates SKILL.md, parses
// frontmatter, and reads every other declared file. Returns
// ErrBundleInvalid-wrapped errors for shape problems so the install
// caller can present a unified "bundle rejected" UX.
//
// Pre-condition: b was returned from LoadBundle (signature + hashes
// already validated). ExtractSkill re-reads the files but trusts the
// hash check that already ran — no fresh sha256 pass.
func ExtractSkill(b *Bundle) (*SkillContents, error) {
	if b == nil {
		return nil, fmt.Errorf("%w: nil bundle", ErrBundleInvalid)
	}

	// Find SKILL.md in the manifest.
	var skillEntry *ManifestFile
	for i := range b.Manifest.Files {
		if b.Manifest.Files[i].Path == SkillFilename {
			skillEntry = &b.Manifest.Files[i]
			break
		}
	}
	if skillEntry == nil {
		return nil, fmt.Errorf("%w: bundle missing %s", ErrBundleInvalid, SkillFilename)
	}
	if skillEntry.Size > MaxSkillContentLen {
		return nil, fmt.Errorf("%w: %s exceeds %d bytes",
			ErrBundleInvalid, SkillFilename, MaxSkillContentLen)
	}

	skillBytes, err := os.ReadFile(filepath.Join(b.Root, SkillFilename)) //nolint:gosec // path validated by LoadBundle
	if err != nil {
		return nil, fmt.Errorf("%w: read %s: %v", ErrBundleInvalid, SkillFilename, err)
	}
	skillStr := string(skillBytes)

	// Pull required fields from frontmatter.
	name := frontmatterField(skillStr, "name")
	if name == "" {
		// Fall back to manifest's name if frontmatter omits it. Helpful
		// for hand-authored bundles.
		name = b.Manifest.Name
	}
	if !nameRe.MatchString(name) {
		return nil, fmt.Errorf("%w: name %q is invalid (alphanumeric/underscore/hyphen, max %d chars)",
			ErrBundleInvalid, name, MaxSkillNameLen)
	}

	description := frontmatterField(skillStr, "description")
	if description == "" {
		description = b.Manifest.Description
	}
	if len(description) < MinSkillDescriptionLen {
		return nil, fmt.Errorf("%w: description is empty", ErrBundleInvalid)
	}
	if len(description) > MaxSkillDescriptionLen {
		return nil, fmt.Errorf("%w: description exceeds %d chars",
			ErrBundleInvalid, MaxSkillDescriptionLen)
	}

	category := frontmatterField(skillStr, "category")
	if category == "" {
		// Marketplace bundles MUST declare a category — there's no
		// safe default; categorisation drives where the skill lands
		// in the UI tree.
		return nil, fmt.Errorf("%w: SKILL.md frontmatter missing category", ErrBundleInvalid)
	}
	if !nameRe.MatchString(category) {
		return nil, fmt.Errorf("%w: category %q is invalid", ErrBundleInvalid, category)
	}

	// Read every other declared file as supporting.
	supporting := map[string][]byte{}
	for _, f := range b.Manifest.Files {
		if f.Path == SkillFilename {
			continue
		}
		if f.Size > MaxSupportingFileLen {
			return nil, fmt.Errorf("%w: supporting file %q exceeds %d bytes",
				ErrBundleInvalid, f.Path, MaxSupportingFileLen)
		}
		full := filepath.Join(b.Root, filepath.FromSlash(f.Path))
		body, err := os.ReadFile(full) //nolint:gosec // path validated by LoadBundle
		if err != nil {
			return nil, fmt.Errorf("%w: read supporting %q: %v",
				ErrBundleInvalid, f.Path, err)
		}
		supporting[f.Path] = body
	}

	return &SkillContents{
		Name:        name,
		Category:    category,
		Description: description,
		Content:     skillStr,
		Supporting:  supporting,
	}, nil
}

// frontmatterField returns the value of `key:` inside the leading
// YAML frontmatter block (`---\n…\n---`). Returns "" when the block
// is absent, the key isn't there, or the value is blank. Pure
// string-scan to avoid pulling in a YAML lib for two fields.
//
// Limitations:
//   - Single-line scalar values only. Multi-line / block scalars
//     return the first line.
//   - Trims surrounding whitespace + double-quotes.
func frontmatterField(content, key string) string {
	if !strings.HasPrefix(content, "---\n") && !strings.HasPrefix(content, "---\r\n") {
		return ""
	}
	rest := strings.TrimPrefix(content, "---")
	rest = strings.TrimPrefix(rest, "\r")
	rest = strings.TrimPrefix(rest, "\n")
	end := strings.Index(rest, "\n---")
	if end < 0 {
		return ""
	}
	block := rest[:end]
	prefix := key + ":"
	for _, line := range strings.Split(block, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, prefix) {
			continue
		}
		v := strings.TrimSpace(strings.TrimPrefix(line, prefix))
		v = strings.TrimSuffix(strings.TrimPrefix(v, `"`), `"`)
		v = strings.TrimSuffix(strings.TrimPrefix(v, `'`), `'`)
		return v
	}
	return ""
}
