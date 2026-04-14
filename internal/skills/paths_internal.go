package skills

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/euraika-labs/pan-agent/internal/paths"
)

// resolveActiveDir returns the absolute directory for an active skill at
// `<category>/<name>` inside the profile's skills dir, but only after:
//
//  1. validating category and name against the strict skill-name regex, and
//  2. confirming the resolved path is contained inside ProfileSkillsDir via
//     a filepath.Rel check.
//
// The Rel containment check is the sanitizer that CodeQL's "uncontrolled data
// used in path expression" query recognises, so all package-internal callers
// that construct paths from agent-supplied category/name should funnel
// through this helper rather than calling filepath.Join directly.
func resolveActiveDir(profile, category, name string) (string, error) {
	if err := ValidateCategory(category); err != nil {
		return "", err
	}
	if err := ValidateName(name); err != nil {
		return "", err
	}
	base := filepath.Clean(paths.ProfileSkillsDir(profile))
	candidate := filepath.Clean(filepath.Join(base, category, name))
	rel, err := filepath.Rel(base, candidate)
	if err != nil || rel == "." || strings.HasPrefix(rel, "..") ||
		strings.HasPrefix(rel, string(filepath.Separator)) {
		return "", fmt.Errorf("skill %s/%s resolves outside profile skills dir", category, name)
	}
	return candidate, nil
}

// resolveProposalDir returns the absolute directory for a queued proposal,
// validating the id and confirming containment inside `_proposed/`.
func resolveProposalDir(profile, id string) (string, error) {
	if err := validateProposalID(id); err != nil {
		return "", err
	}
	base := filepath.Clean(filepath.Join(paths.ProfileSkillsDir(profile), "_proposed"))
	candidate := filepath.Clean(filepath.Join(base, id))
	rel, err := filepath.Rel(base, candidate)
	if err != nil || rel == "." || strings.HasPrefix(rel, "..") ||
		strings.HasPrefix(rel, string(filepath.Separator)) {
		return "", fmt.Errorf("proposal %s resolves outside _proposed dir", id)
	}
	return candidate, nil
}

// resolveHistoryDir returns the absolute history directory for an active
// skill, with the same containment guarantees as resolveActiveDir.
func resolveHistoryDir(profile, category, name string) (string, error) {
	if err := ValidateCategory(category); err != nil {
		return "", err
	}
	if err := ValidateName(name); err != nil {
		return "", err
	}
	base := filepath.Clean(filepath.Join(paths.ProfileSkillsDir(profile), "_history"))
	candidate := filepath.Clean(filepath.Join(base, category, name))
	rel, err := filepath.Rel(base, candidate)
	if err != nil || rel == "." || strings.HasPrefix(rel, "..") ||
		strings.HasPrefix(rel, string(filepath.Separator)) {
		return "", fmt.Errorf("history dir for %s/%s resolves outside _history", category, name)
	}
	return candidate, nil
}

// splitAndResolveActiveID parses `<category>/<name>` and returns the
// containment-checked active dir. Used by every path that takes a
// `<category>/<name>` string from an LLM tool argument.
func splitAndResolveActiveID(profile, id string) (category, name, dir string, err error) {
	cat, n, e := splitActiveID(id)
	if e != nil {
		return "", "", "", e
	}
	dir, e = resolveActiveDir(profile, cat, n)
	if e != nil {
		return "", "", "", e
	}
	return cat, n, dir, nil
}
