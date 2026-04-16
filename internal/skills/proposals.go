package skills

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/euraika-labs/pan-agent/internal/paths"
)

// Proposal is a queued skill awaiting reviewer action. It pairs the parsed
// metadata with the SKILL.md body so callers (the reviewer agent + HTTP
// endpoints) can render or refine it without a second disk hit.
type Proposal struct {
	Metadata    ProposalMetadata `json:"metadata"`
	Content     string           `json:"content"`
	GuardResult ReviewResult     `json:"guard_result"`
	Dir         string           `json:"-"`
}

// HistoryEntry is one rollback-able snapshot of a previously-active skill.
type HistoryEntry struct {
	Category    string `json:"category"`
	Name        string `json:"name"`
	TimestampMs int64  `json:"timestamp_ms"`
	Path        string `json:"path"`
}

// ListProposals returns all queued proposals for the profile, newest first.
func (m *Manager) ListProposals() ([]Proposal, error) {
	root := filepath.Join(paths.ProfileSkillsDir(m.Profile), "_proposed")
	entries, err := os.ReadDir(root)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("ListProposals readdir: %w", err)
	}
	out := make([]Proposal, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		dir := filepath.Join(root, e.Name())
		p, err := m.LoadProposal(e.Name())
		if err != nil {
			// Skip unreadable proposals rather than failing the whole list.
			continue
		}
		p.Dir = dir
		out = append(out, p)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].Metadata.CreatedAt > out[j].Metadata.CreatedAt
	})
	return out, nil
}

// LoadProposal reads a single proposal by ID.
func (m *Manager) LoadProposal(id string) (Proposal, error) {
	dir, err := resolveProposalDir(m.Profile, id)
	if err != nil {
		return Proposal{}, err
	}
	meta, err := ReadMetadata(dir)
	if err != nil {
		return Proposal{}, err
	}
	if meta.ID == "" {
		return Proposal{}, fmt.Errorf("proposal %s has no metadata", id)
	}
	body, err := os.ReadFile(filepath.Join(dir, "SKILL.md"))
	if err != nil {
		return Proposal{}, fmt.Errorf("read SKILL.md: %w", err)
	}
	content := string(body)
	scan := m.Guard.Scan(content)
	return Proposal{
		Metadata:    meta,
		Content:     content,
		GuardResult: scan,
		Dir:         dir,
	}, nil
}

// PromoteProposal moves a proposal from `_proposed/<id>/` to its active
// `<category>/<name>/` location, optionally replacing the SKILL.md body with
// a reviewer-refined version. Saves a history snapshot if the destination
// already exists (an "edit-via-proposal" flow).
func (m *Manager) PromoteProposal(id, refinedContent, reviewerNote string) (ProposalMetadata, error) {
	p, err := m.LoadProposal(id)
	if err != nil {
		return ProposalMetadata{}, err
	}
	if p.Metadata.Status != StatusProposed {
		return ProposalMetadata{}, fmt.Errorf("proposal %s is not in 'proposed' state (got %q)", id, p.Metadata.Status)
	}
	activeDir, err := resolveActiveDir(m.Profile, p.Metadata.Category, p.Metadata.Name)
	if err != nil {
		return p.Metadata, err
	}

	content := p.Content
	if strings.TrimSpace(refinedContent) != "" {
		if len(refinedContent) > MaxSkillContentBytes {
			return p.Metadata, fmt.Errorf("refined content exceeds %d bytes", MaxSkillContentBytes)
		}
		content = EnsureFrontmatter(refinedContent, p.Metadata.Name, p.Metadata.Description)
		// Re-scan: a reviewer-supplied refinement still has to clear the guard.
		scan := m.Guard.Scan(content)
		if scan.Blocked {
			return p.Metadata, fmt.Errorf("refined content blocked by guard: %d finding(s)", len(scan.Findings))
		}
	}

	activePath := filepath.Join(activeDir, "SKILL.md")

	// If we're overwriting an existing skill, snapshot it first.
	if prev, existed := snapshotFile(activePath); existed {
		if err := snapshotToHistory(m.Profile, p.Metadata.Category, p.Metadata.Name, prev); err != nil {
			return p.Metadata, fmt.Errorf("history snapshot: %w", err)
		}
	}

	if err := atomicWrite(activePath, []byte(content), 0o600); err != nil {
		return p.Metadata, err
	}

	// Update + persist metadata into the active dir.
	p.Metadata.Status = StatusActive
	if reviewerNote != "" {
		p.Metadata.SessionCtx = strings.TrimSpace(p.Metadata.SessionCtx + "\nreviewer: " + reviewerNote)
	}
	if err := WriteMetadata(activeDir, p.Metadata); err != nil {
		return p.Metadata, err
	}

	// Drop the proposal directory.
	_ = os.RemoveAll(p.Dir)
	return p.Metadata, nil
}

// RejectProposal moves a proposal from `_proposed/<id>/` to `_rejected/<id>/`,
// recording the reason in metadata. The directory is preserved as audit trail.
func (m *Manager) RejectProposal(id, reason string) error {
	p, err := m.LoadProposal(id)
	if err != nil {
		return err
	}
	if p.Metadata.Status != StatusProposed {
		return fmt.Errorf("proposal %s is not in 'proposed' state", id)
	}
	// Containment-check the destination too — same id, different parent dir.
	rejBase := filepath.Clean(filepath.Join(paths.ProfileSkillsDir(m.Profile), "_rejected"))
	dst := filepath.Clean(filepath.Join(rejBase, id))
	rel, relErr := filepath.Rel(rejBase, dst)
	if relErr != nil || rel == "." || strings.HasPrefix(rel, "..") ||
		strings.HasPrefix(rel, string(filepath.Separator)) {
		return fmt.Errorf("rejected path resolves outside _rejected dir")
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o700); err != nil {
		return fmt.Errorf("rejected mkdir: %w", err)
	}
	if err := os.Rename(p.Dir, dst); err != nil {
		return fmt.Errorf("rejected rename: %w", err)
	}
	p.Metadata.Status = StatusRejected
	p.Metadata.RejectReason = reason
	if err := WriteMetadata(dst, p.Metadata); err != nil {
		return err
	}
	return nil
}

// MergeProposals consolidates several proposals into one promotion. The first
// ID in ids is the "winner": its target category/name is used, its metadata
// becomes the new skill's metadata, and the others are moved to `_merged/`
// with parent_ids cross-referencing the winner. The consolidated SKILL.md
// content is supplied by the reviewer.
func (m *Manager) MergeProposals(ids []string, consolidatedContent, reviewerNote string) (ProposalMetadata, error) {
	if len(ids) < 2 {
		return ProposalMetadata{}, fmt.Errorf("merge requires ≥2 proposal ids")
	}
	if strings.TrimSpace(consolidatedContent) == "" {
		return ProposalMetadata{}, fmt.Errorf("consolidated content is required")
	}
	if len(consolidatedContent) > MaxSkillContentBytes {
		return ProposalMetadata{}, fmt.Errorf("consolidated content exceeds %d bytes", MaxSkillContentBytes)
	}

	winner, err := m.LoadProposal(ids[0])
	if err != nil {
		return ProposalMetadata{}, fmt.Errorf("load winner %s: %w", ids[0], err)
	}

	parents := []string{winner.Metadata.ID}

	// Validate all the losers exist + collect them.
	losers := make([]Proposal, 0, len(ids)-1)
	for _, id := range ids[1:] {
		p, err := m.LoadProposal(id)
		if err != nil {
			return ProposalMetadata{}, fmt.Errorf("load merge source %s: %w", id, err)
		}
		losers = append(losers, p)
		parents = append(parents, p.Metadata.ID)
	}

	// Promote winner with the consolidated content (this also runs the guard).
	winner.Metadata.ParentIDs = parents
	if err := WriteMetadata(winner.Dir, winner.Metadata); err != nil {
		return winner.Metadata, err
	}
	promoted, err := m.PromoteProposal(winner.Metadata.ID, consolidatedContent, "merged from "+strings.Join(parents[1:], ","))
	if err != nil {
		return winner.Metadata, err
	}
	_ = reviewerNote // currently unused — included in PromoteProposal note

	// Park each loser under _merged/<id>/ for audit. Any failure in this
	// loop leaves the previously-renamed losers in _merged/ (we cannot un-
	// promote the winner once it's active) but we MUST continue so that
	// earlier losers aren't orphaned in `_proposed/`. Errors are collected
	// and returned together; per-loser metadata write errors no longer
	// silently drop.
	mergeBase := filepath.Clean(filepath.Join(paths.ProfileSkillsDir(m.Profile), "_merged"))
	var errs []string
	for _, l := range losers {
		dst := filepath.Clean(filepath.Join(mergeBase, l.Metadata.ID))
		rel, relErr := filepath.Rel(mergeBase, dst)
		if relErr != nil || rel == "." || strings.HasPrefix(rel, "..") ||
			strings.HasPrefix(rel, string(filepath.Separator)) {
			errs = append(errs, fmt.Sprintf("%s: merged path resolves outside _merged dir", l.Metadata.ID))
			continue
		}
		if err := os.MkdirAll(filepath.Dir(dst), 0o700); err != nil {
			errs = append(errs, fmt.Sprintf("%s: mkdir: %v", l.Metadata.ID, err))
			continue
		}
		if err := os.Rename(l.Dir, dst); err != nil {
			errs = append(errs, fmt.Sprintf("%s: rename: %v", l.Metadata.ID, err))
			continue
		}
		l.Metadata.Status = StatusMerged
		l.Metadata.ParentIDs = []string{promoted.ID}
		if err := WriteMetadata(dst, l.Metadata); err != nil {
			errs = append(errs, fmt.Sprintf("%s: metadata write: %v", l.Metadata.ID, err))
			// fall through: the move succeeded, only metadata failed
		}
	}

	if len(errs) > 0 {
		return promoted, fmt.Errorf("merge partial failure: %s", strings.Join(errs, "; "))
	}
	return promoted, nil
}

// ListHistory returns rollback-able snapshots for an active skill, newest first.
func (m *Manager) ListHistory(category, name string) ([]HistoryEntry, error) {
	histDir, err := resolveHistoryDir(m.Profile, category, name)
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(histDir)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("ListHistory readdir: %w", err)
	}
	out := make([]HistoryEntry, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		ts, ok := parseHistoryTimestamp(e.Name())
		if !ok {
			continue
		}
		out = append(out, HistoryEntry{
			Category:    category,
			Name:        name,
			TimestampMs: ts,
			Path:        filepath.Join(histDir, e.Name()),
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].TimestampMs > out[j].TimestampMs })
	return out, nil
}

// Rollback restores the active SKILL.md from a history snapshot identified by
// its timestamp (in milliseconds). The current active version is itself
// snapshotted to history before being overwritten, so rollback is reversible.
func (m *Manager) Rollback(category, name string, timestampMs int64) error {
	histDir, err := resolveHistoryDir(m.Profile, category, name)
	if err != nil {
		return err
	}
	activeDir, err := resolveActiveDir(m.Profile, category, name)
	if err != nil {
		return err
	}
	histPath := filepath.Join(histDir, fmt.Sprintf("SKILL.%d.md", timestampMs))
	histData, err := os.ReadFile(histPath)
	if err != nil {
		return fmt.Errorf("rollback read snapshot: %w", err)
	}
	activePath := filepath.Join(activeDir, "SKILL.md")
	if prev, existed := snapshotFile(activePath); existed {
		if err := snapshotToHistory(m.Profile, category, name, prev); err != nil {
			return fmt.Errorf("rollback snapshot current: %w", err)
		}
	}
	return atomicWrite(activePath, histData, 0o600)
}

// parseHistoryTimestamp extracts the millisecond timestamp from a snapshot
// filename of the form `SKILL.<ms>.md`.
func parseHistoryTimestamp(filename string) (int64, bool) {
	if !strings.HasPrefix(filename, "SKILL.") || !strings.HasSuffix(filename, ".md") {
		return 0, false
	}
	core := strings.TrimSuffix(strings.TrimPrefix(filename, "SKILL."), ".md")
	v, err := strconv.ParseInt(core, 10, 64)
	if err != nil {
		return 0, false
	}
	return v, true
}

// validateProposalID rejects path-traversal-style IDs. UUIDs from the metadata
// generator are alphanumeric+dashes; anything else is rejected.
func validateProposalID(id string) error {
	if id == "" {
		return fmt.Errorf("proposal id is empty")
	}
	if strings.ContainsAny(id, `/\:*?"<>|`) || strings.Contains(id, "..") {
		return fmt.Errorf("proposal id contains invalid characters")
	}
	if len(id) > 64 {
		return fmt.Errorf("proposal id too long")
	}
	return nil
}
