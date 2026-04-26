package skills

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Phase 13 WS#13.C — direct tests of WriteProposalFile, the helper
// the marketplace install pipeline calls to stage supporting files
// inside a proposal directory. The integration is covered in
// internal/skillinstall; these tests pin the per-call validation.

func TestWriteProposalFile_HappyPath(t *testing.T) {
	withIsolatedAgentHome(t)
	mgr := NewManager("default")

	meta, _, err := mgr.CreateProposal(
		"sup-test", "utility", "test description",
		EnsureFrontmatter("body content", "sup-test", "test description"),
		"sess", "marketplace:test",
	)
	if err != nil {
		t.Fatalf("CreateProposal: %v", err)
	}

	body := []byte("template body")
	if err := mgr.WriteProposalFile(meta.ID, "templates/x.txt", body); err != nil {
		t.Fatalf("WriteProposalFile: %v", err)
	}

	// Confirm file landed in the proposal dir.
	dir, err := resolveProposalDir(mgr.Profile, meta.ID)
	if err != nil {
		t.Fatalf("resolveProposalDir: %v", err)
	}
	got, err := os.ReadFile(filepath.Join(dir, "templates", "x.txt"))
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if string(got) != "template body" {
		t.Errorf("body mismatch: %q", got)
	}
}

func TestWriteProposalFile_RejectedPaths(t *testing.T) {
	withIsolatedAgentHome(t)
	mgr := NewManager("default")
	meta, _, err := mgr.CreateProposal(
		"sup-test2", "utility", "test description",
		EnsureFrontmatter("body", "sup-test2", "test description"),
		"sess", "marketplace:test",
	)
	if err != nil {
		t.Fatalf("CreateProposal: %v", err)
	}

	cases := []string{
		"",               // empty
		"../escape.txt",  // traversal
		"/abs/path.txt",  // abs
		"SKILL.md",       // reserved
		"_metadata.json", // reserved
		"docs/x.txt",     // not a permitted top-level
	}
	for _, p := range cases {
		err := mgr.WriteProposalFile(meta.ID, p, []byte("x"))
		if err == nil {
			t.Errorf("path %q: expected rejection", p)
		}
	}
}

func TestWriteProposalFile_NonexistentProposal(t *testing.T) {
	withIsolatedAgentHome(t)
	mgr := NewManager("default")
	err := mgr.WriteProposalFile(
		"00000000-0000-0000-0000-000000000000",
		"templates/x.txt", []byte("x"))
	if err == nil {
		t.Error("expected proposal-not-found error")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("error should mention 'not found': %v", err)
	}
}

func TestWriteProposalFile_OversizedRejected(t *testing.T) {
	withIsolatedAgentHome(t)
	mgr := NewManager("default")
	meta, _, _ := mgr.CreateProposal(
		"sup-big", "utility", "x",
		EnsureFrontmatter("body", "sup-big", "x"),
		"sess", "marketplace:test",
	)

	body := make([]byte, MaxSupportingBytes+1)
	err := mgr.WriteProposalFile(meta.ID, "templates/big.txt", body)
	if err == nil {
		t.Error("oversized: expected error")
	}
}

func TestWriteProposalFile_GuardBlocked(t *testing.T) {
	withIsolatedAgentHome(t)
	mgr := NewManager("default")
	meta, _, _ := mgr.CreateProposal(
		"sup-guard", "utility", "x",
		EnsureFrontmatter("body", "sup-guard", "x"),
		"sess", "marketplace:test",
	)

	// AKIA-prefixed string is a known guard pattern.
	err := mgr.WriteProposalFile(meta.ID, "templates/leak.txt", []byte("AKIAIOSFODNN7EXAMPLE"))
	if err == nil {
		t.Error("guard: expected block")
	}
	if !strings.Contains(err.Error(), "guard") {
		t.Errorf("error should mention guard: %v", err)
	}
}
