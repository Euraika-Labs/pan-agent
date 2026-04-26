package skillinstall

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/euraika-labs/pan-agent/internal/marketplace"
	"github.com/euraika-labs/pan-agent/internal/skills"
)

// Phase 13 WS#13.C — install pipeline tests. The marketplace +
// skills primitives are covered in their own packages; these
// tests pin the bridge contract: bundle in → proposal out, with
// proper error mapping for the four failure modes the desktop UI
// distinguishes (sig invalid / untrusted / bundle invalid /
// already exists).

const skillBody = `---
name: weather-tool
description: Look up the weather at a given location
category: utility
---
# Weather

Use this skill to fetch the current weather.
`

// makeSignedBundle writes a small bundle to a temp dir, signed by
// kp. supportingFiles are staged alongside the SKILL.md; their
// paths must match what the test expects to see in the resulting
// proposal directory.
func makeSignedBundle(t *testing.T, kp *marketplace.Keypair, supportingFiles map[string][]byte) string {
	t.Helper()
	dir := t.TempDir()

	// Write SKILL.md.
	skillPath := filepath.Join(dir, marketplace.SkillFilename)
	if err := os.WriteFile(skillPath, []byte(skillBody), 0o644); err != nil {
		t.Fatalf("write SKILL.md: %v", err)
	}

	// Write supporting files.
	for rel, body := range supportingFiles {
		full := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		if err := os.WriteFile(full, body, 0o644); err != nil {
			t.Fatalf("write %s: %v", rel, err)
		}
	}

	// Build manifest manually so we don't depend on the producer-
	// side BuildManifest helper that may or may not be in this
	// branch.
	files := []marketplace.ManifestFile{
		manifestFileFor(marketplace.SkillFilename, []byte(skillBody)),
	}
	for rel, body := range supportingFiles {
		files = append(files, manifestFileFor(rel, body))
	}
	m := &marketplace.Manifest{
		Schema: marketplace.SchemaSkillV1,
		Name:   "weather-tool", Version: "1.0.0", Author: "alice",
		Description: "test", SignedAt: 1700000000,
		Files: files,
	}
	if err := marketplace.Sign(m, kp); err != nil {
		t.Fatalf("Sign: %v", err)
	}
	mb, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if err := os.WriteFile(
		filepath.Join(dir, marketplace.ManifestFilename), mb, 0o644,
	); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
	return dir
}

func manifestFileFor(path string, body []byte) marketplace.ManifestFile {
	sum := sha256.Sum256(body)
	return marketplace.ManifestFile{
		Path: path, SHA256: hex.EncodeToString(sum[:]), Size: int64(len(body)),
	}
}

// setupManager creates a Manager with PAN_AGENT_HOME isolated to a
// temp dir — Install writes into the configured profile's skills
// directory; we don't want to pollute the developer's real one.
func setupManager(t *testing.T) *skills.Manager {
	t.Helper()
	t.Setenv("PAN_AGENT_HOME", t.TempDir())
	return skills.NewManager("")
}

// ---------------------------------------------------------------------------
// Happy path
// ---------------------------------------------------------------------------

func TestInstall_HappyPath(t *testing.T) {
	kp, _ := marketplace.GenerateKeypair()
	root := makeSignedBundle(t, kp, map[string][]byte{
		"templates/example.txt": []byte("template body"),
		"references/guide.md":   []byte("guide body"),
	})
	mgr := setupManager(t)

	res, err := Install(root, []ed25519.PublicKey{kp.Public}, mgr, "session-1")
	if err != nil {
		t.Fatalf("Install: %v", err)
	}
	if res.ProposalID == "" {
		t.Error("ProposalID empty")
	}
	if res.SkillName != "weather-tool" {
		t.Errorf("SkillName = %q, want weather-tool", res.SkillName)
	}
	if res.Category != "utility" {
		t.Errorf("Category = %q, want utility", res.Category)
	}
	if res.Supporting != 2 {
		t.Errorf("Supporting = %d, want 2", res.Supporting)
	}
	if !strings.HasPrefix(res.Publisher, kp.PublicKeyHex()[:16]) {
		t.Errorf("Publisher prefix mismatch: %q", res.Publisher)
	}
}

// ---------------------------------------------------------------------------
// Error mapping
// ---------------------------------------------------------------------------

func TestInstall_NilManager(t *testing.T) {
	kp, _ := marketplace.GenerateKeypair()
	root := makeSignedBundle(t, kp, nil)
	_, err := Install(root, []ed25519.PublicKey{kp.Public}, nil, "s")
	if err == nil {
		t.Error("expected nil-manager error")
	}
}

func TestInstall_UntrustedPublisher(t *testing.T) {
	signer, _ := marketplace.GenerateKeypair()
	other, _ := marketplace.GenerateKeypair()
	root := makeSignedBundle(t, signer, nil)
	mgr := setupManager(t)

	_, err := Install(root, []ed25519.PublicKey{other.Public}, mgr, "s")
	if !errors.Is(err, marketplace.ErrUntrustedPublisher) {
		t.Errorf("err = %v, want ErrUntrustedPublisher", err)
	}
}

func TestInstall_TamperedBundle(t *testing.T) {
	kp, _ := marketplace.GenerateKeypair()
	root := makeSignedBundle(t, kp, nil)
	// Tamper with SKILL.md after signing — hash mismatch.
	if err := os.WriteFile(
		filepath.Join(root, marketplace.SkillFilename),
		[]byte("# tampered"), 0o644,
	); err != nil {
		t.Fatalf("tamper: %v", err)
	}
	mgr := setupManager(t)

	_, err := Install(root, []ed25519.PublicKey{kp.Public}, mgr, "s")
	if !errors.Is(err, marketplace.ErrBundleInvalid) {
		t.Errorf("err = %v, want ErrBundleInvalid", err)
	}
}

func TestInstall_AlreadyExists(t *testing.T) {
	kp, _ := marketplace.GenerateKeypair()
	root := makeSignedBundle(t, kp, nil)
	mgr := setupManager(t)

	// First install: succeeds.
	res, err := Install(root, []ed25519.PublicKey{kp.Public}, mgr, "s")
	if err != nil {
		t.Fatalf("first Install: %v", err)
	}

	// Promote the proposal to active so a SECOND install hits the
	// active-collision check.
	// (Manually copy the SKILL.md to the active path. The skills
	// reviewer would normally handle promotion; for this test we
	// just mimic the resulting on-disk state.)
	if err := promoteProposalToActive(t, mgr, res); err != nil {
		t.Fatalf("promote: %v", err)
	}

	// Second install: should fail with ErrAlreadyExists.
	_, err = Install(root, []ed25519.PublicKey{kp.Public}, mgr, "s")
	if !errors.Is(err, ErrAlreadyExists) {
		t.Errorf("err = %v, want ErrAlreadyExists", err)
	}
}

// promoteProposalToActive simulates the reviewer agent promoting a
// proposal so a second install of the same skill hits the active
// collision check. It writes the skill into the active path.
func promoteProposalToActive(t *testing.T, mgr *skills.Manager, res *Result) error {
	t.Helper()
	// The skills package doesn't expose the active-path resolver; the
	// simplest cross-package mimic is to call EditActiveSkill — but
	// that requires the active dir to already exist. So instead we
	// use the public CreateProposal + a manual filesystem move. To
	// keep the test independent of internal layout, we just do a
	// filesystem-level mkdir of an active dir with a SKILL.md.
	// Path: paths.SkillsDir() / <category> / <name> / SKILL.md.
	home := os.Getenv("PAN_AGENT_HOME")
	if home == "" {
		t.Fatalf("PAN_AGENT_HOME not set")
	}
	activeDir := filepath.Join(home, "skills", res.Category, res.SkillName)
	if err := os.MkdirAll(activeDir, 0o755); err != nil {
		return err
	}
	return os.WriteFile(
		filepath.Join(activeDir, "SKILL.md"),
		[]byte(skillBody), 0o600,
	)
}

func TestInstall_BundleNotFound(t *testing.T) {
	mgr := setupManager(t)
	_, err := Install("/nonexistent/path/x", nil, mgr, "s")
	if !errors.Is(err, marketplace.ErrBundleInvalid) {
		t.Errorf("err = %v, want ErrBundleInvalid", err)
	}
}

func TestInstall_SupportingFileGuardBlocked(t *testing.T) {
	kp, _ := marketplace.GenerateKeypair()
	// Plant a supporting file with content that the skills.Guard
	// will block (an AKIA-prefixed AWS key is a known guard pattern).
	root := makeSignedBundle(t, kp, map[string][]byte{
		"references/creds.txt": []byte("AKIAIOSFODNN7EXAMPLE"),
	})
	mgr := setupManager(t)

	_, err := Install(root, []ed25519.PublicKey{kp.Public}, mgr, "s")
	if err == nil {
		t.Error("expected guard-block error")
	}
	if !strings.Contains(err.Error(), "creds.txt") {
		t.Errorf("error should name the file: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func TestStringContains(t *testing.T) {
	if !stringContains("the quick brown fox", "quick") {
		t.Error("stringContains positive case")
	}
	if stringContains("hello", "world") {
		t.Error("stringContains negative case")
	}
	if !stringContains("foo", "foo") {
		t.Error("stringContains exact match")
	}
	if stringContains("", "x") {
		t.Error("stringContains empty haystack")
	}
}
