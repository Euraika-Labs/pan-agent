package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/euraika-labs/pan-agent/internal/marketplace"
)

// Phase 13 WS#13.C — CLI subcommand tests for `pan-agent skill ...`.
// Covers verify (signature + bundle layout) + the trust list/pin/unpin
// flow against the real marketplace.LoadTrustSet on-disk format.

const cliSkillBody = `---
name: weather-tool
description: Look up the weather at a given location
category: utility
---
# Weather

Use this skill to fetch the current weather.
`

func makeCLIBundle(t *testing.T, kp *marketplace.Keypair) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, marketplace.SkillFilename),
		[]byte(cliSkillBody), 0o644); err != nil {
		t.Fatalf("write SKILL.md: %v", err)
	}
	sum := sha256.Sum256([]byte(cliSkillBody))
	m := &marketplace.Manifest{
		Schema: marketplace.SchemaSkillV1,
		Name:   "weather-tool", Version: "1.0.0", Author: "alice",
		Description: "test", SignedAt: 1700000000,
		Files: []marketplace.ManifestFile{
			{Path: marketplace.SkillFilename,
				SHA256: hex.EncodeToString(sum[:]),
				Size:   int64(len(cliSkillBody))},
		},
	}
	if err := marketplace.Sign(m, kp); err != nil {
		t.Fatalf("Sign: %v", err)
	}
	mb, _ := json.MarshalIndent(m, "", "  ")
	if err := os.WriteFile(filepath.Join(dir, marketplace.ManifestFilename),
		mb, 0o644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
	return dir
}

// ---------------------------------------------------------------------------
// skill (top-level dispatcher)
// ---------------------------------------------------------------------------

func TestCmdSkill_NoAction(t *testing.T) {
	err := cmdSkill(nil)
	if err == nil {
		t.Error("expected error for missing action")
	}
}

func TestCmdSkill_UnknownAction(t *testing.T) {
	err := cmdSkill([]string{"banana"})
	if err == nil || !strings.Contains(err.Error(), "unknown") {
		t.Errorf("got %v, want unknown-action error", err)
	}
}

// ---------------------------------------------------------------------------
// skill verify
// ---------------------------------------------------------------------------

func TestSkillVerify_HappyPath(t *testing.T) {
	kp, _ := marketplace.GenerateKeypair()
	bundle := makeCLIBundle(t, kp)

	// Default mode: no trust set required, just signature.
	if err := cmdSkillVerify([]string{bundle}); err != nil {
		t.Errorf("verify: %v", err)
	}
}

func TestSkillVerify_StrictWithoutPin(t *testing.T) {
	t.Setenv("PAN_AGENT_HOME", t.TempDir())
	kp, _ := marketplace.GenerateKeypair()
	bundle := makeCLIBundle(t, kp)

	err := cmdSkillVerify([]string{"--strict", bundle})
	if err == nil {
		t.Error("expected ErrUntrustedPublisher under strict mode without pin")
	}
}

func TestSkillVerify_StrictWithPin(t *testing.T) {
	t.Setenv("PAN_AGENT_HOME", t.TempDir())
	kp, _ := marketplace.GenerateKeypair()
	bundle := makeCLIBundle(t, kp)

	// Pin the publisher first.
	if err := cmdSkillTrustPin([]string{kp.PublicKeyHex()}); err != nil {
		t.Fatalf("pin: %v", err)
	}
	if err := cmdSkillVerify([]string{"--strict", bundle}); err != nil {
		t.Errorf("strict verify after pin: %v", err)
	}
}

func TestSkillVerify_TamperedFails(t *testing.T) {
	kp, _ := marketplace.GenerateKeypair()
	bundle := makeCLIBundle(t, kp)
	// Tamper with SKILL.md → hash mismatch.
	if err := os.WriteFile(filepath.Join(bundle, marketplace.SkillFilename),
		[]byte("# tampered"), 0o644); err != nil {
		t.Fatalf("tamper: %v", err)
	}
	err := cmdSkillVerify([]string{bundle})
	if err == nil {
		t.Error("expected error on tampered bundle")
	}
}

func TestSkillVerify_MissingPath(t *testing.T) {
	if err := cmdSkillVerify(nil); err == nil {
		t.Error("expected missing-path error")
	}
}

// ---------------------------------------------------------------------------
// skill trust list / pin / unpin
// ---------------------------------------------------------------------------

func TestSkillTrustList_Empty(t *testing.T) {
	t.Setenv("PAN_AGENT_HOME", t.TempDir())
	if err := cmdSkillTrustList(nil); err != nil {
		t.Errorf("list empty: %v", err)
	}
}

func TestSkillTrust_PinListUnpinFlow(t *testing.T) {
	t.Setenv("PAN_AGENT_HOME", t.TempDir())
	kp, _ := marketplace.GenerateKeypair()

	// Pin.
	if err := cmdSkillTrustPin([]string{"--name", "Alice", kp.PublicKeyHex()}); err != nil {
		t.Fatalf("pin: %v", err)
	}
	// Pin again (idempotent — should succeed without error).
	if err := cmdSkillTrustPin([]string{kp.PublicKeyHex()}); err != nil {
		t.Errorf("re-pin: %v", err)
	}
	// List should now show the publisher.
	if err := cmdSkillTrustList(nil); err != nil {
		t.Errorf("list: %v", err)
	}
	// Unpin.
	if err := cmdSkillTrustUnpin([]string{kp.Fingerprint()}); err != nil {
		t.Errorf("unpin: %v", err)
	}
	// Unpin a missing fingerprint → error.
	if err := cmdSkillTrustUnpin([]string{kp.Fingerprint()}); err == nil {
		t.Error("expected error on second unpin")
	}
}

func TestSkillTrustPin_BadKey(t *testing.T) {
	t.Setenv("PAN_AGENT_HOME", t.TempDir())
	if err := cmdSkillTrustPin([]string{"zznotahex"}); err == nil {
		t.Error("expected bad-key error")
	}
}

func TestSkillTrustPin_NoArg(t *testing.T) {
	if err := cmdSkillTrustPin(nil); err == nil {
		t.Error("expected missing-key error")
	}
}

func TestSkillTrustUnpin_NoArg(t *testing.T) {
	if err := cmdSkillTrustUnpin(nil); err == nil {
		t.Error("expected missing-fingerprint error")
	}
}

func TestSkillTrust_NoAction(t *testing.T) {
	if err := cmdSkillTrust(nil); err == nil {
		t.Error("expected missing-action error")
	}
}

func TestSkillTrust_UnknownAction(t *testing.T) {
	if err := cmdSkillTrust([]string{"banana"}); err == nil {
		t.Error("expected unknown-action error")
	}
}
