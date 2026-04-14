package skills

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/euraika-labs/pan-agent/internal/paths"
)

// activate installs an active skill via Create+Promote; helper for curator tests.
func activate(t *testing.T, mgr *Manager, category, name, body string) {
	t.Helper()
	meta, _, err := mgr.CreateProposal(name, category, name+" desc",
		EnsureFrontmatter(body, name, name+" desc"),
		"sess", "agent")
	if err != nil {
		t.Fatalf("CreateProposal %s/%s: %v", category, name, err)
	}
	if _, err := mgr.PromoteProposal(meta.ID, "", ""); err != nil {
		t.Fatalf("PromoteProposal %s/%s: %v", category, name, err)
	}
}

func TestProposeCuratorRefinement(t *testing.T) {
	withIsolatedAgentHome(t)
	mgr := NewManager("default")
	activate(t, mgr, "coding", "lint", "# v1\n")

	meta, scan, err := mgr.ProposeCuratorRefinement("coding", "lint", "# v2 tightened\n", "too verbose", "sess")
	if err != nil {
		t.Fatalf("ProposeCuratorRefinement: %v (scan=%+v)", err, scan)
	}
	if meta.Intent != IntentRefine {
		t.Errorf("intent = %q, want refine", meta.Intent)
	}
	if len(meta.IntentTargets) != 1 || meta.IntentTargets[0] != "coding/lint" {
		t.Errorf("intent_targets = %v, want [coding/lint]", meta.IntentTargets)
	}
	// Approval should snapshot v1 to history and overwrite with v2.
	if _, err := mgr.PromoteProposal(meta.ID, "", "approved"); err != nil {
		t.Fatalf("PromoteProposal: %v", err)
	}
	body, _ := os.ReadFile(filepath.Join(paths.ProfileSkillsDir("default"), "coding", "lint", "SKILL.md"))
	if got := string(body); got == "" || !contains(got, "v2 tightened") {
		t.Errorf("active body did not update; got %q", got)
	}
	hist, _ := mgr.ListHistory("coding", "lint")
	if len(hist) != 1 {
		t.Errorf("expected 1 history entry post-refine, got %d", len(hist))
	}
}

func TestProposeCuratorMergeAndApply(t *testing.T) {
	withIsolatedAgentHome(t)
	mgr := NewManager("default")
	activate(t, mgr, "coding", "fmt-go", "# go fmt skill\n")
	activate(t, mgr, "coding", "fmt-rust", "# rust fmt skill\n")

	meta, _, err := mgr.ProposeCuratorMerge(
		[]string{"coding/fmt-go", "coding/fmt-rust"},
		"# Merged formatter\n\nCovers go + rust.\n",
		"overlap", "sess",
	)
	if err != nil {
		t.Fatalf("ProposeCuratorMerge: %v", err)
	}
	if meta.Intent != IntentMerge {
		t.Errorf("intent = %q, want merge", meta.Intent)
	}
	// Reviewer approves and ApplyCuratorIntent archives the loser.
	if _, err := mgr.PromoteProposal(meta.ID, "", "ok"); err != nil {
		t.Fatalf("PromoteProposal: %v", err)
	}
	if err := mgr.ApplyCuratorIntent(meta, ""); err != nil {
		t.Fatalf("ApplyCuratorIntent: %v", err)
	}
	// fmt-go remains, fmt-rust is archived.
	if _, err := os.Stat(filepath.Join(paths.ProfileSkillsDir("default"), "coding", "fmt-go", "SKILL.md")); err != nil {
		t.Errorf("survivor missing: %v", err)
	}
	if _, err := os.Stat(filepath.Join(paths.ProfileSkillsDir("default"), "coding", "fmt-rust", "SKILL.md")); !os.IsNotExist(err) {
		t.Errorf("loser still present: err=%v", err)
	}
}

func TestProposeCuratorArchiveAndApply(t *testing.T) {
	withIsolatedAgentHome(t)
	mgr := NewManager("default")
	activate(t, mgr, "junk", "stale", "# Stale skill\n")

	meta, err := mgr.ProposeCuratorArchive("junk/stale", "no usage", "sess")
	if err != nil {
		t.Fatalf("ProposeCuratorArchive: %v", err)
	}
	if meta.Intent != IntentArchive {
		t.Errorf("intent = %q, want archive", meta.Intent)
	}
	if err := mgr.ApplyCuratorIntent(meta, ""); err != nil {
		t.Fatalf("ApplyCuratorIntent: %v", err)
	}
	if _, err := os.Stat(filepath.Join(paths.ProfileSkillsDir("default"), "junk", "stale", "SKILL.md")); !os.IsNotExist(err) {
		t.Errorf("archived skill should be gone, err=%v", err)
	}
}

func TestProposeCuratorRecategorizeAndApply(t *testing.T) {
	withIsolatedAgentHome(t)
	mgr := NewManager("default")
	activate(t, mgr, "misc", "tooling", "# Tooling notes\n")

	meta, err := mgr.ProposeCuratorRecategorize("misc/tooling", "infra", "better fit", "sess")
	if err != nil {
		t.Fatalf("ProposeCuratorRecategorize: %v", err)
	}
	if meta.Intent != IntentRecategorize {
		t.Errorf("intent = %q, want recategorize", meta.Intent)
	}
	if err := mgr.ApplyCuratorIntent(meta, ""); err != nil {
		t.Fatalf("ApplyCuratorIntent: %v", err)
	}
	if _, err := os.Stat(filepath.Join(paths.ProfileSkillsDir("default"), "infra", "tooling", "SKILL.md")); err != nil {
		t.Errorf("recategorized destination missing: %v", err)
	}
	if _, err := os.Stat(filepath.Join(paths.ProfileSkillsDir("default"), "misc", "tooling", "SKILL.md")); !os.IsNotExist(err) {
		t.Errorf("source should be gone, err=%v", err)
	}
}

func TestProposeCuratorSplitAndApply(t *testing.T) {
	withIsolatedAgentHome(t)
	mgr := NewManager("default")
	activate(t, mgr, "writing", "all-in-one", "# Big skill\n\ntwo concerns.\n")

	meta, _, err := mgr.ProposeCuratorSplit("writing/all-in-one",
		[]SplitProposal{
			{Category: "writing", Name: "intros", Description: "intros only", Content: "# Intros\n"},
			{Category: "writing", Name: "outros", Description: "outros only", Content: "# Outros\n"},
		},
		"single responsibility", "sess",
	)
	if err != nil {
		t.Fatalf("ProposeCuratorSplit: %v", err)
	}
	if meta.Intent != IntentSplit {
		t.Errorf("intent = %q, want split", meta.Intent)
	}

	// Materialise via ApplyCuratorIntent (children dir is inside proposal).
	splitDir := filepath.Join(paths.ProfileSkillsDir("default"), "_proposed", meta.ID, "split_children")
	if err := mgr.ApplyCuratorIntent(meta, splitDir); err != nil {
		t.Fatalf("ApplyCuratorIntent: %v", err)
	}
	for _, child := range []string{"intros", "outros"} {
		if _, err := os.Stat(filepath.Join(paths.ProfileSkillsDir("default"), "writing", child, "SKILL.md")); err != nil {
			t.Errorf("child %s missing: %v", child, err)
		}
	}
	// Source archived.
	if _, err := os.Stat(filepath.Join(paths.ProfileSkillsDir("default"), "writing", "all-in-one", "SKILL.md")); !os.IsNotExist(err) {
		t.Errorf("source should be gone, err=%v", err)
	}
}

func TestSplitRejectsTraversalChild(t *testing.T) {
	withIsolatedAgentHome(t)
	mgr := NewManager("default")
	activate(t, mgr, "writing", "src", "# src\n")

	_, _, err := mgr.ProposeCuratorSplit("writing/src",
		[]SplitProposal{
			{Category: "..", Name: "escape", Description: "x", Content: "# x\n"},
			{Category: "writing", Name: "ok", Description: "ok", Content: "# ok\n"},
		},
		"reason", "sess",
	)
	if err == nil {
		t.Errorf("expected ProposeCuratorSplit to reject traversal child")
	}
}

// contains is a tiny helper to keep the strings import out of every test file.
func contains(haystack, needle string) bool {
	return len(haystack) >= len(needle) && (indexOf(haystack, needle) >= 0)
}

func indexOf(haystack, needle string) int {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return i
		}
	}
	return -1
}
