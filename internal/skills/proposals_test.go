package skills

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/euraika-labs/pan-agent/internal/paths"
)

// proposalSampleContent returns a benign SKILL.md that should pass the guard.
func proposalSampleContent(name, desc, body string) string {
	return EnsureFrontmatter(body, name, desc)
}

// TestCreateAndListProposals end-to-ends the propose → list → load flow.
// Note: paths.AgentHome uses sync.Once so the test data dir is shared across
// every test in this binary. We therefore look up the freshly-created
// proposal by id rather than asserting the queue length.
func TestCreateAndListProposals(t *testing.T) {
	withIsolatedAgentHome(t)
	mgr := NewManager("default")

	meta, scan, err := mgr.CreateProposal(
		"refactor-uniq", "coding", "extract methods",
		proposalSampleContent("refactor-uniq", "extract methods", "# Refactor\n\nDo it carefully.\n"),
		"sess-1", "agent",
	)
	if err != nil {
		t.Fatalf("CreateProposal: %v (scan=%+v)", err, scan)
	}
	if meta.Status != StatusProposed {
		t.Errorf("status = %q, want proposed", meta.Status)
	}
	if scan.Blocked {
		t.Errorf("benign proposal blocked: %+v", scan.Findings)
	}

	props, err := mgr.ListProposals()
	if err != nil {
		t.Fatalf("ListProposals: %v", err)
	}
	found := false
	for _, p := range props {
		if p.Metadata.ID == meta.ID {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("ListProposals did not include freshly-created id %q", meta.ID)
	}

	loaded, err := mgr.LoadProposal(meta.ID)
	if err != nil {
		t.Fatalf("LoadProposal: %v", err)
	}
	if !strings.Contains(loaded.Content, "Do it carefully") {
		t.Errorf("loaded content missing body: %q", loaded.Content)
	}
}

// TestPromoteProposal moves a proposal from queue to active.
func TestPromoteProposal(t *testing.T) {
	withIsolatedAgentHome(t)
	mgr := NewManager("default")

	meta, _, err := mgr.CreateProposal(
		"summarise", "writing", "produce a TL;DR",
		proposalSampleContent("summarise", "produce a TL;DR", "# Summarise\n\nKeep it tight.\n"),
		"sess-1", "agent",
	)
	if err != nil {
		t.Fatalf("CreateProposal: %v", err)
	}

	promoted, err := mgr.PromoteProposal(meta.ID, "", "looks good")
	if err != nil {
		t.Fatalf("PromoteProposal: %v", err)
	}
	if promoted.Status != StatusActive {
		t.Errorf("promoted status = %q, want active", promoted.Status)
	}

	// Confirm the active SKILL.md exists.
	activePath := filepath.Join(paths.ProfileSkillsDir("default"), "writing", "summarise", "SKILL.md")
	if _, err := os.Stat(activePath); err != nil {
		t.Fatalf("active SKILL.md not present: %v", err)
	}

	// Confirm the proposal directory is gone.
	if _, err := os.Stat(filepath.Join(paths.ProfileSkillsDir("default"), "_proposed", meta.ID)); !os.IsNotExist(err) {
		t.Errorf("proposal dir still present after promote: err=%v", err)
	}
}

// TestPromoteProposalRefinedContent applies a reviewer rewrite.
func TestPromoteProposalRefinedContent(t *testing.T) {
	withIsolatedAgentHome(t)
	mgr := NewManager("default")

	meta, _, err := mgr.CreateProposal(
		"explain", "writing", "explain a concept",
		proposalSampleContent("explain", "explain a concept", "# Original verbose\n\nLong rambling content.\n"),
		"sess-1", "agent",
	)
	if err != nil {
		t.Fatalf("CreateProposal: %v", err)
	}

	refined := "# Tightened\n\nShort, sharp, clear.\n"
	if _, err := mgr.PromoteProposal(meta.ID, refined, "tightened"); err != nil {
		t.Fatalf("PromoteProposal: %v", err)
	}
	body, _ := os.ReadFile(filepath.Join(paths.ProfileSkillsDir("default"), "writing", "explain", "SKILL.md"))
	if !strings.Contains(string(body), "Tightened") {
		t.Errorf("refined content not written; got: %q", string(body))
	}
}

// TestRejectProposal moves to _rejected/ with a reason.
func TestRejectProposal(t *testing.T) {
	withIsolatedAgentHome(t)
	mgr := NewManager("default")

	meta, _, err := mgr.CreateProposal(
		"bad", "junk", "bad idea",
		proposalSampleContent("bad", "bad idea", "# Bad\n\nNo thanks.\n"),
		"sess-1", "agent",
	)
	if err != nil {
		t.Fatalf("CreateProposal: %v", err)
	}

	if err := mgr.RejectProposal(meta.ID, "out of scope"); err != nil {
		t.Fatalf("RejectProposal: %v", err)
	}

	rejPath := filepath.Join(paths.ProfileSkillsDir("default"), "_rejected", meta.ID)
	if _, err := os.Stat(rejPath); err != nil {
		t.Fatalf("rejected dir not present: %v", err)
	}
	// Read back the metadata and confirm the reason was recorded.
	loaded, _ := ReadMetadata(rejPath)
	if loaded.RejectReason != "out of scope" {
		t.Errorf("reject reason = %q, want %q", loaded.RejectReason, "out of scope")
	}
	if loaded.Status != StatusRejected {
		t.Errorf("status = %q, want rejected", loaded.Status)
	}
}

// TestHistoryRollbackRoundTrip exercises edit → history → rollback.
func TestHistoryRollbackRoundTrip(t *testing.T) {
	withIsolatedAgentHome(t)
	mgr := NewManager("default")

	// First, drop a fresh active skill via Create+Promote.
	meta, _, err := mgr.CreateProposal(
		"loop", "coding", "iterate cleanly",
		proposalSampleContent("loop", "iterate cleanly", "# v1\n\nFirst version.\n"),
		"sess-1", "agent",
	)
	if err != nil {
		t.Fatalf("CreateProposal: %v", err)
	}
	if _, err := mgr.PromoteProposal(meta.ID, "", ""); err != nil {
		t.Fatalf("PromoteProposal: %v", err)
	}

	// Edit it (creates a history snapshot of v1).
	v2 := proposalSampleContent("loop", "iterate cleanly", "# v2\n\nSecond version.\n")
	if _, err := mgr.EditActiveSkill("coding", "loop", v2); err != nil {
		t.Fatalf("EditActiveSkill: %v", err)
	}

	// History should show one entry.
	hist, err := mgr.ListHistory("coding", "loop")
	if err != nil {
		t.Fatalf("ListHistory: %v", err)
	}
	if len(hist) != 1 {
		t.Fatalf("len(hist) = %d, want 1", len(hist))
	}

	// Rollback restores v1.
	if err := mgr.Rollback("coding", "loop", hist[0].TimestampMs); err != nil {
		t.Fatalf("Rollback: %v", err)
	}
	body, _ := os.ReadFile(filepath.Join(paths.ProfileSkillsDir("default"), "coding", "loop", "SKILL.md"))
	if !strings.Contains(string(body), "First version") {
		t.Errorf("rollback failed; body = %q", string(body))
	}
}

// TestGuardBlocksMaliciousProposal — a SKILL.md with a credential leak must
// be rejected at proposal time.
func TestGuardBlocksMaliciousProposal(t *testing.T) {
	withIsolatedAgentHome(t)
	mgr := NewManager("default")

	bad := "# Bad\n\nAKIAAAAAAAAAAAAAAAAA\n" // AWS access key shape
	_, scan, err := mgr.CreateProposal(
		"bad", "secrets", "leaks AWS",
		proposalSampleContent("bad", "leaks AWS", bad),
		"sess-1", "agent",
	)
	if err == nil {
		t.Errorf("expected guard-block error; got scan=%+v", scan)
	}
	if !scan.Blocked {
		t.Errorf("scan should be blocked; got %+v", scan)
	}
}

// TestPromotionRejectsTraversalCategory ensures a malicious metadata category
// can't escape the profile skills dir during promotion.
func TestPromotionRejectsTraversalCategory(t *testing.T) {
	withIsolatedAgentHome(t)
	mgr := NewManager("default")

	// Synthesise a proposal directory by hand whose metadata has a bad category.
	meta := NewProposalMetadata("attack", "..", "bad", "sess", "agent")
	dir, err := resolveProposalDir("default", meta.ID)
	if err != nil {
		t.Fatalf("resolveProposalDir: %v", err)
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte("---\nname: x\ndescription: y\n---\n# x\n"), 0o600); err != nil {
		t.Fatalf("write skill: %v", err)
	}
	if err := WriteMetadata(dir, meta); err != nil {
		t.Fatalf("write metadata: %v", err)
	}

	if _, err := mgr.PromoteProposal(meta.ID, "", ""); err == nil {
		t.Errorf("PromoteProposal should reject category=%q", meta.Category)
	}
}
