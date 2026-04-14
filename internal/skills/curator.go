package skills

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ProposeCuratorRefinement queues a refinement of an existing active skill.
// The proposal carries the new SKILL.md body; on reviewer approval, the active
// skill is updated (with a history snapshot taken first).
func (m *Manager) ProposeCuratorRefinement(category, name, newContent, reason, sessionID string) (ProposalMetadata, ReviewResult, error) {
	if strings.TrimSpace(newContent) == "" {
		return ProposalMetadata{}, ReviewResult{}, fmt.Errorf("new content is required")
	}
	if len(newContent) > MaxSkillContentBytes {
		return ProposalMetadata{}, ReviewResult{}, fmt.Errorf("content exceeds %d bytes", MaxSkillContentBytes)
	}
	activeDir, err := resolveActiveDir(m.Profile, category, name)
	if err != nil {
		return ProposalMetadata{}, ReviewResult{}, err
	}
	activePath := filepath.Join(activeDir, "SKILL.md")
	if _, err := os.Stat(activePath); err != nil {
		return ProposalMetadata{}, ReviewResult{}, fmt.Errorf("active skill %s/%s not found", category, name)
	}

	// Pull description from the existing SKILL.md so the proposal carries
	// continuity in the inventory views.
	existingContent, _ := os.ReadFile(activePath)
	_, existingDesc := parseSkillFrontmatter(string(existingContent))

	content := EnsureFrontmatter(newContent, name, existingDesc)
	scan := m.Guard.Scan(content)
	if scan.Blocked {
		return ProposalMetadata{}, scan, fmt.Errorf("guard blocked refinement: %d finding(s)", len(scan.Findings))
	}

	meta := NewProposalMetadata(name, category, existingDesc, sessionID, "curator")
	meta.Intent = IntentRefine
	meta.IntentTargets = []string{category + "/" + name}
	meta.IntentReason = reason

	dir, err := resolveProposalDir(m.Profile, meta.ID)
	if err != nil {
		return meta, scan, err
	}
	if err := atomicWrite(filepath.Join(dir, "SKILL.md"), []byte(content), 0o600); err != nil {
		return meta, scan, err
	}
	if err := WriteMetadata(dir, meta); err != nil {
		_ = os.RemoveAll(dir)
		return meta, scan, err
	}
	return meta, scan, nil
}

// ProposeCuratorMerge queues a consolidation of multiple active skills.
// The first id in skillIDs becomes the survivor; the others are archived
// when the reviewer approves. consolidatedContent is the new SKILL.md body
// for the survivor.
func (m *Manager) ProposeCuratorMerge(skillIDs []string, consolidatedContent, reason, sessionID string) (ProposalMetadata, ReviewResult, error) {
	if len(skillIDs) < 2 {
		return ProposalMetadata{}, ReviewResult{}, fmt.Errorf("merge requires ≥2 skill ids")
	}
	if strings.TrimSpace(consolidatedContent) == "" {
		return ProposalMetadata{}, ReviewResult{}, fmt.Errorf("consolidated content is required")
	}
	if len(consolidatedContent) > MaxSkillContentBytes {
		return ProposalMetadata{}, ReviewResult{}, fmt.Errorf("content exceeds %d bytes", MaxSkillContentBytes)
	}

	survivorCat, survivorName, _, err := splitAndResolveActiveID(m.Profile, skillIDs[0])
	if err != nil {
		return ProposalMetadata{}, ReviewResult{}, err
	}
	for _, id := range skillIDs {
		_, _, dir, err := splitAndResolveActiveID(m.Profile, id)
		if err != nil {
			return ProposalMetadata{}, ReviewResult{}, err
		}
		if _, err := os.Stat(filepath.Join(dir, "SKILL.md")); err != nil {
			return ProposalMetadata{}, ReviewResult{}, fmt.Errorf("active skill %s not found", id)
		}
	}

	content := EnsureFrontmatter(consolidatedContent, survivorName, "merged: "+strings.Join(skillIDs, ","))
	scan := m.Guard.Scan(content)
	if scan.Blocked {
		return ProposalMetadata{}, scan, fmt.Errorf("guard blocked merge: %d finding(s)", len(scan.Findings))
	}

	meta := NewProposalMetadata(survivorName, survivorCat, "merge of "+strings.Join(skillIDs, ", "), sessionID, "curator")
	meta.Intent = IntentMerge
	meta.IntentTargets = skillIDs
	meta.IntentReason = reason

	dir, err := resolveProposalDir(m.Profile, meta.ID)
	if err != nil {
		return meta, scan, err
	}
	if err := atomicWrite(filepath.Join(dir, "SKILL.md"), []byte(content), 0o600); err != nil {
		return meta, scan, err
	}
	if err := WriteMetadata(dir, meta); err != nil {
		_ = os.RemoveAll(dir)
		return meta, scan, err
	}
	return meta, scan, nil
}

// SplitProposal describes one of the children of a proposed split.
type SplitProposal struct {
	Category    string `json:"category"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Content     string `json:"content"`
}

// ProposeCuratorSplit queues a split of one active skill into many. The source
// skill is archived on approval; each entry in newSkills becomes a fresh
// active skill. We persist the children as supporting files inside the
// proposal dir so the reviewer can inspect them before approval.
func (m *Manager) ProposeCuratorSplit(sourceID string, newSkills []SplitProposal, reason, sessionID string) (ProposalMetadata, ReviewResult, error) {
	srcCat, srcName, srcDir, err := splitAndResolveActiveID(m.Profile, sourceID)
	if err != nil {
		return ProposalMetadata{}, ReviewResult{}, err
	}
	if len(newSkills) < 2 {
		return ProposalMetadata{}, ReviewResult{}, fmt.Errorf("split requires ≥2 children")
	}
	if _, err := os.Stat(filepath.Join(srcDir, "SKILL.md")); err != nil {
		return ProposalMetadata{}, ReviewResult{}, fmt.Errorf("active skill %s not found", sourceID)
	}

	// Validate every child + run the guard against every child body. Also
	// verify each child's <category>/<name> resolves cleanly inside the
	// profile skills dir — the same containment guarantee enforced for any
	// other agent-supplied path.
	combinedScan := ReviewResult{Findings: nil}
	for i, c := range newSkills {
		if _, err := resolveActiveDir(m.Profile, c.Category, c.Name); err != nil {
			return ProposalMetadata{}, ReviewResult{}, fmt.Errorf("child %d: %w", i, err)
		}
		if strings.TrimSpace(c.Content) == "" {
			return ProposalMetadata{}, ReviewResult{}, fmt.Errorf("child %d: content required", i)
		}
		body := EnsureFrontmatter(c.Content, c.Name, c.Description)
		scan := m.Guard.Scan(body)
		if scan.Blocked {
			return ProposalMetadata{}, scan, fmt.Errorf("child %d (%s/%s) blocked by guard", i, c.Category, c.Name)
		}
		combinedScan.Findings = append(combinedScan.Findings, scan.Findings...)
	}

	// Build a parent proposal. Its SKILL.md is just an index of the children.
	indexBody := buildSplitIndex(sourceID, newSkills, reason)
	indexBody = EnsureFrontmatter(indexBody, srcName, "split of "+sourceID)

	meta := NewProposalMetadata(srcName, srcCat, "split of "+sourceID, sessionID, "curator")
	meta.Intent = IntentSplit
	meta.IntentTargets = []string{sourceID}
	meta.IntentReason = reason

	dir, err := resolveProposalDir(m.Profile, meta.ID)
	if err != nil {
		return meta, combinedScan, err
	}
	if err := atomicWrite(filepath.Join(dir, "SKILL.md"), []byte(indexBody), 0o600); err != nil {
		return meta, combinedScan, err
	}
	// Persist each child body as a supporting file for the reviewer to read.
	// Filenames embed (validated) category + name so ApplyCuratorIntent can
	// safely recover them later.
	for i, c := range newSkills {
		childPath := filepath.Join(dir, "split_children", fmt.Sprintf("%d_%s_%s.md", i, c.Category, c.Name))
		body := EnsureFrontmatter(c.Content, c.Name, c.Description)
		if err := atomicWrite(childPath, []byte(body), 0o600); err != nil {
			_ = os.RemoveAll(dir)
			return meta, combinedScan, err
		}
	}
	if err := WriteMetadata(dir, meta); err != nil {
		_ = os.RemoveAll(dir)
		return meta, combinedScan, err
	}
	return meta, combinedScan, nil
}

// ProposeCuratorArchive queues an archive of one active skill. No content is
// needed — the proposal is essentially a request token for the reviewer.
func (m *Manager) ProposeCuratorArchive(skillID, reason, sessionID string) (ProposalMetadata, error) {
	cat, name, srcDir, err := splitAndResolveActiveID(m.Profile, skillID)
	if err != nil {
		return ProposalMetadata{}, err
	}
	if _, err := os.Stat(filepath.Join(srcDir, "SKILL.md")); err != nil {
		return ProposalMetadata{}, fmt.Errorf("active skill %s not found", skillID)
	}

	body := EnsureFrontmatter(
		fmt.Sprintf("# Archive request\n\nCurator proposes archiving `%s`.\n\nReason: %s\n", skillID, reason),
		name, "archive request for "+skillID,
	)
	meta := NewProposalMetadata(name, cat, "archive "+skillID, sessionID, "curator")
	meta.Intent = IntentArchive
	meta.IntentTargets = []string{skillID}
	meta.IntentReason = reason

	dir, err := resolveProposalDir(m.Profile, meta.ID)
	if err != nil {
		return meta, err
	}
	if err := atomicWrite(filepath.Join(dir, "SKILL.md"), []byte(body), 0o600); err != nil {
		return meta, err
	}
	if err := WriteMetadata(dir, meta); err != nil {
		_ = os.RemoveAll(dir)
		return meta, err
	}
	return meta, nil
}

// ProposeCuratorRecategorize queues a category change for one active skill.
func (m *Manager) ProposeCuratorRecategorize(skillID, newCategory, reason, sessionID string) (ProposalMetadata, error) {
	cat, name, srcDir, err := splitAndResolveActiveID(m.Profile, skillID)
	if err != nil {
		return ProposalMetadata{}, err
	}
	if newCategory == cat {
		return ProposalMetadata{}, fmt.Errorf("new category is the same as current")
	}
	dstDir, err := resolveActiveDir(m.Profile, newCategory, name)
	if err != nil {
		return ProposalMetadata{}, err
	}
	if _, err := os.Stat(filepath.Join(srcDir, "SKILL.md")); err != nil {
		return ProposalMetadata{}, fmt.Errorf("active skill %s not found", skillID)
	}
	// Block if the target slot already exists.
	if _, err := os.Stat(filepath.Join(dstDir, "SKILL.md")); err == nil {
		return ProposalMetadata{}, fmt.Errorf("destination %s/%s already exists", newCategory, name)
	}

	body := EnsureFrontmatter(
		fmt.Sprintf("# Recategorize request\n\nCurator proposes moving `%s` → `%s/%s`.\n\nReason: %s\n",
			skillID, newCategory, name, reason),
		name, "recategorize request for "+skillID,
	)
	meta := NewProposalMetadata(name, cat, "recategorize "+skillID, sessionID, "curator")
	meta.Intent = IntentRecategorize
	meta.IntentTargets = []string{skillID}
	meta.IntentNewCategory = newCategory
	meta.IntentReason = reason

	dir, err := resolveProposalDir(m.Profile, meta.ID)
	if err != nil {
		return meta, err
	}
	if err := atomicWrite(filepath.Join(dir, "SKILL.md"), []byte(body), 0o600); err != nil {
		return meta, err
	}
	if err := WriteMetadata(dir, meta); err != nil {
		_ = os.RemoveAll(dir)
		return meta, err
	}
	return meta, nil
}

// ApplyCuratorIntent is the post-promotion hook for the reviewer. After the
// reviewer approves a curator proposal, this method performs the side-effects
// that the proposal *intent* requires beyond just promoting the SKILL.md:
//
//   - refine        → no extra work (PromoteProposal already overwrote in place
//                     via the snapshot path).
//   - merge         → archive the loser skills.
//   - split         → archive the source + materialise each child.
//   - archive       → archive the target skill.
//   - recategorize  → move the active skill directory to the new category.
//
// This must be called *after* PromoteProposal succeeds (or in lieu of it for
// archive/recategorize where there's nothing to promote).
func (m *Manager) ApplyCuratorIntent(meta ProposalMetadata, splitChildrenDir string) error {
	switch meta.Intent {
	case IntentCreate, IntentRefine:
		return nil

	case IntentMerge:
		// First entry of IntentTargets is the survivor; archive the rest.
		for _, id := range meta.IntentTargets[1:] {
			cat, name, _, err := splitAndResolveActiveID(m.Profile, id)
			if err != nil {
				return err
			}
			if err := m.DeleteActiveSkill(cat, name, "merged into "+meta.IntentTargets[0]); err != nil {
				// Soft-fail: log via returned error but allow caller to continue.
				return fmt.Errorf("merge archive %s: %w", id, err)
			}
		}
		return nil

	case IntentArchive:
		if len(meta.IntentTargets) != 1 {
			return fmt.Errorf("archive intent requires exactly one target")
		}
		cat, name, _, err := splitAndResolveActiveID(m.Profile, meta.IntentTargets[0])
		if err != nil {
			return err
		}
		return m.DeleteActiveSkill(cat, name, meta.IntentReason)

	case IntentRecategorize:
		if len(meta.IntentTargets) != 1 || meta.IntentNewCategory == "" {
			return fmt.Errorf("recategorize intent requires target + new category")
		}
		_, name, srcDir, err := splitAndResolveActiveID(m.Profile, meta.IntentTargets[0])
		if err != nil {
			return err
		}
		dstDir, err := resolveActiveDir(m.Profile, meta.IntentNewCategory, name)
		if err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(dstDir), 0o700); err != nil {
			return err
		}
		if _, err := os.Stat(dstDir); err == nil {
			return fmt.Errorf("destination %s/%s already exists", meta.IntentNewCategory, name)
		}
		return os.Rename(srcDir, dstDir)

	case IntentSplit:
		if len(meta.IntentTargets) != 1 || splitChildrenDir == "" {
			return fmt.Errorf("split intent requires source + children dir")
		}
		// Materialise each child as a new active skill. Every child path
		// goes through resolveActiveDir so a tampered split_children
		// filename cannot escape the profile skills directory.
		entries, err := os.ReadDir(splitChildrenDir)
		if err != nil {
			return fmt.Errorf("read split children: %w", err)
		}
		for _, e := range entries {
			if e.IsDir() {
				continue
			}
			data, err := os.ReadFile(filepath.Join(splitChildrenDir, e.Name()))
			if err != nil {
				return fmt.Errorf("read child %s: %w", e.Name(), err)
			}
			cat, name, ok := parseSplitChildName(e.Name())
			if !ok {
				continue
			}
			childDir, err := resolveActiveDir(m.Profile, cat, name)
			if err != nil {
				return fmt.Errorf("child %s/%s: %w", cat, name, err)
			}
			activePath := filepath.Join(childDir, "SKILL.md")
			if _, err := os.Stat(activePath); err == nil {
				return fmt.Errorf("split child %s/%s already exists", cat, name)
			}
			if err := atomicWrite(activePath, data, 0o600); err != nil {
				return fmt.Errorf("write child %s/%s: %w", cat, name, err)
			}
		}
		// Archive the source.
		srcCat, srcName, _, err := splitAndResolveActiveID(m.Profile, meta.IntentTargets[0])
		if err != nil {
			return err
		}
		return m.DeleteActiveSkill(srcCat, srcName, "split into "+strings.Join(childIDsFromDir(splitChildrenDir), ","))

	default:
		return fmt.Errorf("unknown intent %q", meta.Intent)
	}
}

// splitActiveID parses "<category>/<name>" → (category, name).
func splitActiveID(id string) (category, name string, err error) {
	parts := strings.SplitN(id, "/", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", fmt.Errorf("skill id must be '<category>/<name>', got %q", id)
	}
	return parts[0], parts[1], nil
}

// buildSplitIndex renders the parent SKILL.md of a split proposal. Reviewer-
// facing only.
func buildSplitIndex(sourceID string, children []SplitProposal, reason string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# Split request\n\nCurator proposes splitting `%s` into:\n\n", sourceID)
	for _, c := range children {
		fmt.Fprintf(&b, "- `%s/%s` — %s\n", c.Category, c.Name, c.Description)
	}
	fmt.Fprintf(&b, "\nReason: %s\n", reason)
	return b.String()
}

// parseSplitChildName recovers (category, name) from a split-child filename
// of the form `<idx>_<category>_<name>.md`.
func parseSplitChildName(filename string) (string, string, bool) {
	if !strings.HasSuffix(filename, ".md") {
		return "", "", false
	}
	core := strings.TrimSuffix(filename, ".md")
	parts := strings.SplitN(core, "_", 3)
	if len(parts) != 3 {
		return "", "", false
	}
	return parts[1], parts[2], true
}

// childIDsFromDir is a small helper for archival reasons.
func childIDsFromDir(dir string) []string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	out := make([]string, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if cat, name, ok := parseSplitChildName(e.Name()); ok {
			out = append(out, cat+"/"+name)
		}
	}
	return out
}
