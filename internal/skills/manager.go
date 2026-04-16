package skills

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/euraika-labs/pan-agent/internal/paths"
)

// Size + name constraints (match hermes skill_manager_tool.py).
const (
	MaxSkillContentBytes = 100_000 // 100k chars for SKILL.md
	MaxSupportingBytes   = 1 << 20 // 1 MiB per supporting file
	MaxNameLen           = 64
	MaxCategoryLen       = 64
	MaxDescriptionLen    = 1024
)

var (
	nameRegex     = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]*$`)
	categoryRegex = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]*$`)
)

// Manager performs skill CRUD operations with validation + guard scanning.
// Proposed skills (from the main agent) land in _proposed/<uuid>/ and must
// pass review. Curator-originated changes use the same pipeline with
// Source="curator".
type Manager struct {
	Profile string
	Guard   *Guard
}

// NewManager returns a manager bound to a profile.
func NewManager(profile string) *Manager {
	return &Manager{Profile: profile, Guard: NewGuard()}
}

// CreateProposal writes a new skill to the proposal queue. The agent
// (main or curator) calls this via skill_manage(action="create", ...).
func (m *Manager) CreateProposal(name, category, description, content, sessionID, source string) (ProposalMetadata, ReviewResult, error) {
	name = strings.ToLower(strings.TrimSpace(name))
	category = strings.ToLower(strings.TrimSpace(category))

	if err := ValidateName(name); err != nil {
		return ProposalMetadata{}, ReviewResult{}, err
	}
	if err := ValidateCategory(category); err != nil {
		return ProposalMetadata{}, ReviewResult{}, err
	}
	if len(description) > MaxDescriptionLen {
		return ProposalMetadata{}, ReviewResult{}, fmt.Errorf("description exceeds %d chars", MaxDescriptionLen)
	}
	if len(content) > MaxSkillContentBytes {
		return ProposalMetadata{}, ReviewResult{}, fmt.Errorf("content exceeds %d bytes", MaxSkillContentBytes)
	}

	// Collision detection against active skills.
	activeDir, err := resolveActiveDir(m.Profile, category, name)
	if err != nil {
		return ProposalMetadata{}, ReviewResult{}, err
	}
	if _, err := os.Stat(filepath.Join(activeDir, "SKILL.md")); err == nil {
		return ProposalMetadata{}, ReviewResult{}, fmt.Errorf("skill %s/%s already exists", category, name)
	}

	// Ensure SKILL.md starts with YAML frontmatter if missing.
	content = EnsureFrontmatter(content, name, description)

	// Guard-scan FIRST so blocked content never touches disk. Prior
	// ordering (write then scan then RemoveAll) left a small window where
	// the proposal directory existed on-disk with malicious content — a
	// symlink/race vector that no longer exists.
	scan := m.Guard.Scan(content)
	if scan.Blocked {
		meta := NewProposalMetadata(name, category, description, sessionID, source)
		return meta, scan, fmt.Errorf("guard blocked proposal: %d finding(s)", len(scan.Findings))
	}

	// Build proposal metadata + directory only after the scan passes.
	meta := NewProposalMetadata(name, category, description, sessionID, source)
	proposalDir, err := resolveProposalDir(m.Profile, meta.ID)
	if err != nil {
		return meta, ReviewResult{}, err
	}

	// Atomic write of clean content.
	skillMdPath := filepath.Join(proposalDir, "SKILL.md")
	if err := atomicWrite(skillMdPath, []byte(content), 0o600); err != nil {
		return meta, ReviewResult{}, err
	}

	if err := WriteMetadata(proposalDir, meta); err != nil {
		_ = os.RemoveAll(proposalDir)
		return meta, scan, err
	}

	return meta, scan, nil
}

// EditActiveSkill replaces the entire SKILL.md of an active skill, saving the
// previous version to _history/. Runs the guard scanner BEFORE writing so
// blocked content never hits the active file (prior ordering let concurrent
// readers observe blocked content during the scan-then-rollback window).
func (m *Manager) EditActiveSkill(category, name, newContent string) (ReviewResult, error) {
	if len(newContent) > MaxSkillContentBytes {
		return ReviewResult{}, fmt.Errorf("content exceeds %d bytes", MaxSkillContentBytes)
	}
	skillDir, err := resolveActiveDir(m.Profile, category, name)
	if err != nil {
		return ReviewResult{}, err
	}
	skillPath := filepath.Join(skillDir, "SKILL.md")

	// Guard-scan first. If blocked, nothing touches disk and no history
	// entry is created for a rejected edit.
	scan := m.Guard.Scan(newContent)
	if scan.Blocked {
		return scan, fmt.Errorf("guard blocked edit: %d finding(s)", len(scan.Findings))
	}

	prevData, existed := snapshotFile(skillPath)
	if !existed {
		return ReviewResult{}, fmt.Errorf("skill %s/%s not found", category, name)
	}

	// Write history snapshot before mutation so an interrupted write is
	// recoverable.
	if err := snapshotToHistory(m.Profile, category, name, prevData); err != nil {
		return ReviewResult{}, err
	}

	// Atomic write new content.
	if err := atomicWrite(skillPath, []byte(newContent), 0o600); err != nil {
		return ReviewResult{}, err
	}

	return scan, nil
}

// DeleteActiveSkill archives an active skill to _archived/<uuid>/.
func (m *Manager) DeleteActiveSkill(category, name, reason string) error {
	src, err := resolveActiveDir(m.Profile, category, name)
	if err != nil {
		return err
	}
	if _, err := os.Stat(src); os.IsNotExist(err) {
		return fmt.Errorf("skill %s/%s not found", category, name)
	}
	dstUUID := generateUUID()
	dst := archivedPath(m.Profile, dstUUID)
	if err := os.MkdirAll(filepath.Dir(dst), 0o700); err != nil {
		return err
	}
	if err := os.Rename(src, dst); err != nil {
		return fmt.Errorf("archive rename: %w", err)
	}
	// Write an archive reason marker.
	_ = os.WriteFile(filepath.Join(dst, "_archived_reason.txt"), []byte(reason), 0o600)
	return nil
}

// WriteSupportingFile writes a supporting file (reference/template/script/asset)
// under the active skill directory. Validates path traversal and size and
// runs the content through the guard scanner — scripts/*.sh and other
// supporting files are equally capable of carrying an exploit payload, so
// they must be scanned, not just SKILL.md.
func (m *Manager) WriteSupportingFile(category, name, relPath string, content []byte) error {
	if len(content) > MaxSupportingBytes {
		return fmt.Errorf("file exceeds %d bytes", MaxSupportingBytes)
	}
	if err := validateRelativePath(relPath); err != nil {
		return err
	}
	skillDir, err := resolveActiveDir(m.Profile, category, name)
	if err != nil {
		return err
	}
	target := filepath.Join(skillDir, relPath)
	// Ensure target stays inside skillDir (defense-in-depth against path tricks).
	rel, err := filepath.Rel(skillDir, target)
	if err != nil || rel == "." || strings.HasPrefix(rel, "..") ||
		strings.HasPrefix(rel, string(filepath.Separator)) {
		return fmt.Errorf("path escapes skill directory")
	}
	// Guard-scan the supporting file before it lands on disk. Binary-ish
	// assets (images, zip) rarely hit the patterns, text content can.
	if m.Guard != nil {
		if scan := m.Guard.Scan(string(content)); scan.Blocked {
			return fmt.Errorf("guard blocked supporting file %s: %d finding(s)", relPath, len(scan.Findings))
		}
	}
	return atomicWrite(target, content, 0o600)
}

// RemoveSupportingFile removes a supporting file from an active skill.
func (m *Manager) RemoveSupportingFile(category, name, relPath string) error {
	if err := validateRelativePath(relPath); err != nil {
		return err
	}
	skillDir, err := resolveActiveDir(m.Profile, category, name)
	if err != nil {
		return err
	}
	target := filepath.Join(skillDir, relPath)
	rel, err := filepath.Rel(skillDir, target)
	if err != nil || rel == "." || strings.HasPrefix(rel, "..") ||
		strings.HasPrefix(rel, string(filepath.Separator)) {
		return fmt.Errorf("path escapes skill directory")
	}
	if err := os.Remove(target); err != nil {
		return fmt.Errorf("remove: %w", err)
	}
	return nil
}

// ValidateName returns an error if name is not a valid skill name.
func ValidateName(name string) error {
	if name == "" {
		return fmt.Errorf("name is empty")
	}
	if len(name) > MaxNameLen {
		return fmt.Errorf("name exceeds %d chars", MaxNameLen)
	}
	if !nameRegex.MatchString(name) {
		return fmt.Errorf("name must match %s", nameRegex.String())
	}
	return nil
}

// ValidateCategory returns an error if category is not valid.
func ValidateCategory(category string) error {
	if category == "" {
		return fmt.Errorf("category is empty")
	}
	if len(category) > MaxCategoryLen {
		return fmt.Errorf("category exceeds %d chars", MaxCategoryLen)
	}
	if !categoryRegex.MatchString(category) {
		return fmt.Errorf("category must match %s", categoryRegex.String())
	}
	return nil
}

// validateRelativePath rejects path-traversal fragments.
func validateRelativePath(p string) error {
	if p == "" {
		return fmt.Errorf("path is empty")
	}
	if strings.Contains(p, "..") {
		return fmt.Errorf("path contains '..'")
	}
	if filepath.IsAbs(p) {
		return fmt.Errorf("path must be relative")
	}
	// SKILL.md itself is managed by the manager directly, not via write_file.
	base := filepath.Base(p)
	if strings.EqualFold(base, "SKILL.md") || strings.HasPrefix(base, "_metadata") {
		return fmt.Errorf("path %s is reserved", base)
	}
	// Restrict to well-known sub-dirs.
	parts := strings.Split(filepath.ToSlash(p), "/")
	if len(parts) == 0 {
		return fmt.Errorf("empty path")
	}
	top := parts[0]
	allowed := map[string]bool{"references": true, "templates": true, "scripts": true, "assets": true}
	if !allowed[top] {
		return fmt.Errorf("path must start with references/|templates/|scripts/|assets/")
	}
	return nil
}

// archivedPath returns the directory path for archived skills. Trusted UUID.
func archivedPath(profile, id string) string {
	return filepath.Join(paths.ProfileSkillsDir(profile), "_archived", id)
}

// snapshotToHistory copies previous SKILL.md content to a versioned dir.
// The category + name go through resolveHistoryDir so a malicious caller
// cannot escape `_history/` via a crafted name.
func snapshotToHistory(profile, category, name string, data []byte) error {
	histDir, err := resolveHistoryDir(profile, category, name)
	if err != nil {
		return fmt.Errorf("history dir: %w", err)
	}
	if err := os.MkdirAll(histDir, 0o700); err != nil {
		return err
	}
	ts := filepath.Join(histDir, fmt.Sprintf("SKILL.%d.md", nowMillis()))
	return atomicWrite(ts, data, 0o600)
}

// generateUUID is a simple UUID helper using the existing dep.
func generateUUID() string {
	// Imported via metadata.go through uuid.New().String(); replicated here
	// to avoid a new import chain from this file.
	m := NewProposalMetadata("", "", "", "", "")
	return m.ID
}

func nowMillis() int64 {
	return timeNowMillis()
}
