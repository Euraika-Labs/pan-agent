package marketplace

import (
	"bytes"
	"crypto/ed25519"
	"encoding/hex"
	"errors"
	"strings"
	"testing"
)

// Phase 13 WS#13.C — sealed signing-primitive tests. Cover keypair
// round-trips, manifest validation, canonical-digest determinism,
// Sign/Verify happy + tampered + untrusted paths.

// ---------------------------------------------------------------------------
// Keypair
// ---------------------------------------------------------------------------

func TestGenerateKeypair_ShapeAndRoundTrip(t *testing.T) {
	t.Parallel()
	kp, err := GenerateKeypair()
	if err != nil {
		t.Fatalf("GenerateKeypair: %v", err)
	}
	if len(kp.Public) != PublicKeySize {
		t.Errorf("Public len = %d, want %d", len(kp.Public), PublicKeySize)
	}
	if len(kp.Private) != PrivateKeySize {
		t.Errorf("Private len = %d, want %d", len(kp.Private), PrivateKeySize)
	}

	// Seed round-trip.
	seed := kp.Seed()
	if len(seed) != SeedSize {
		t.Fatalf("seed len = %d, want %d", len(seed), SeedSize)
	}
	rebuilt, err := FromSeed(seed)
	if err != nil {
		t.Fatalf("FromSeed: %v", err)
	}
	if !bytes.Equal(rebuilt.Public, kp.Public) {
		t.Error("rebuilt Public differs from original")
	}
	if !bytes.Equal(rebuilt.Private, kp.Private) {
		t.Error("rebuilt Private differs from original")
	}
}

func TestFromSeed_BadLength(t *testing.T) {
	t.Parallel()
	for _, n := range []int{0, 16, 31, 33, 64} {
		_, err := FromSeed(make([]byte, n))
		if !errors.Is(err, ErrInvalidKey) {
			t.Errorf("seed length %d: err = %v, want ErrInvalidKey", n, err)
		}
	}
}

func TestPublicKeyHex_Stable(t *testing.T) {
	t.Parallel()
	kp, _ := GenerateKeypair()
	a := kp.PublicKeyHex()
	b := kp.PublicKeyHex()
	if a != b {
		t.Errorf("hex drift across calls: %q vs %q", a, b)
	}
	if len(a) != 2*PublicKeySize {
		t.Errorf("len = %d, want %d", len(a), 2*PublicKeySize)
	}
}

func TestParsePublicKey_RoundTrip(t *testing.T) {
	t.Parallel()
	kp, _ := GenerateKeypair()
	parsed, err := ParsePublicKey(kp.PublicKeyHex())
	if err != nil {
		t.Fatalf("ParsePublicKey: %v", err)
	}
	if !bytes.Equal(parsed, kp.Public) {
		t.Error("parsed bytes differ from original public key")
	}
}

func TestParsePublicKey_Errors(t *testing.T) {
	t.Parallel()
	cases := []string{
		"",
		"zz",                     // not hex
		"abcd",                   // too short
		strings.Repeat("a", 100), // too long
	}
	for _, in := range cases {
		if _, err := ParsePublicKey(in); !errors.Is(err, ErrInvalidKey) {
			t.Errorf("ParsePublicKey(%q): err = %v, want ErrInvalidKey", in, err)
		}
	}
}

func TestFingerprint_Length(t *testing.T) {
	t.Parallel()
	kp, _ := GenerateKeypair()
	fp := kp.Fingerprint()
	if len(fp) != 16 {
		t.Errorf("fingerprint len = %d, want 16", len(fp))
	}
	// Should be the same as FingerprintOf(public).
	if fp != FingerprintOf(kp.Public) {
		t.Errorf("Keypair.Fingerprint disagrees with FingerprintOf")
	}
}

func TestZeroPrivate(t *testing.T) {
	t.Parallel()
	kp, _ := GenerateKeypair()
	kp.ZeroPrivate()
	for i, b := range kp.Private {
		if b != 0 {
			t.Fatalf("byte[%d] = %x, want 0 after ZeroPrivate", i, b)
		}
	}
	// Idempotent.
	kp.ZeroPrivate()
}

// ---------------------------------------------------------------------------
// Manifest validation
// ---------------------------------------------------------------------------

func validManifest() *Manifest {
	return &Manifest{
		Schema:      SchemaSkillV1,
		Name:        "test-skill",
		Version:     "1.0.0",
		Author:      "alice",
		Description: "test",
		SignedAt:    1700000000,
		Files: []ManifestFile{
			{Path: "README.md", SHA256: hex.EncodeToString(make([]byte, 32)), Size: 10},
			{Path: "src/main.go", SHA256: hex.EncodeToString(make([]byte, 32)), Size: 100},
		},
	}
}

func TestManifest_Validate_HappyPath(t *testing.T) {
	t.Parallel()
	if err := validManifest().Validate(); err != nil {
		t.Errorf("valid manifest: %v", err)
	}
}

func TestManifest_Validate_Errors(t *testing.T) {
	t.Parallel()
	cases := map[string]func(m *Manifest){
		"wrong_schema":   func(m *Manifest) { m.Schema = "wrong" },
		"empty_name":     func(m *Manifest) { m.Name = "" },
		"empty_version":  func(m *Manifest) { m.Version = "" },
		"no_files":       func(m *Manifest) { m.Files = nil },
		"empty_path":     func(m *Manifest) { m.Files[0].Path = "" },
		"abs_path":       func(m *Manifest) { m.Files[0].Path = "/etc/x" },
		"dotdot_path":    func(m *Manifest) { m.Files[0].Path = "../escape" },
		"backslash_path": func(m *Manifest) { m.Files[0].Path = "src\\win.go" },
		"duplicate_path": func(m *Manifest) {
			m.Files = []ManifestFile{m.Files[0], m.Files[0]}
		},
		"bad_hex":       func(m *Manifest) { m.Files[0].SHA256 = "not-hex-data" },
		"short_sha":     func(m *Manifest) { m.Files[0].SHA256 = "abc123" },
		"negative_size": func(m *Manifest) { m.Files[0].Size = -1 },
	}
	for name, mut := range cases {
		t.Run(name, func(t *testing.T) {
			m := validManifest()
			mut(m)
			if err := m.Validate(); err == nil {
				t.Error("expected validation error")
			}
		})
	}
}

// ---------------------------------------------------------------------------
// CanonicalDigest determinism
// ---------------------------------------------------------------------------

func TestCanonicalDigest_Stable(t *testing.T) {
	t.Parallel()
	m := validManifest()
	a, err := m.CanonicalDigest()
	if err != nil {
		t.Fatalf("digest: %v", err)
	}
	b, err := m.CanonicalDigest()
	if err != nil {
		t.Fatalf("digest #2: %v", err)
	}
	if !bytes.Equal(a, b) {
		t.Errorf("digest drift across calls")
	}
	if len(a) != 32 {
		t.Errorf("digest len = %d, want 32", len(a))
	}
}

func TestCanonicalDigest_FileOrderIndependent(t *testing.T) {
	t.Parallel()
	m1 := validManifest()
	m2 := validManifest()
	// Reverse the file list — canonical digest must match because
	// canonicalForm sorts by Path.
	m2.Files[0], m2.Files[1] = m2.Files[1], m2.Files[0]

	d1, _ := m1.CanonicalDigest()
	d2, _ := m2.CanonicalDigest()
	if !bytes.Equal(d1, d2) {
		t.Errorf("digest depends on file-list order: %x vs %x", d1, d2)
	}
}

func TestCanonicalDigest_IgnoresSignatureFields(t *testing.T) {
	t.Parallel()
	m1 := validManifest()
	m2 := validManifest()
	m2.Signature = "deadbeef"
	m2.PublicKeyHex = "cafebabe"

	d1, _ := m1.CanonicalDigest()
	d2, _ := m2.CanonicalDigest()
	if !bytes.Equal(d1, d2) {
		t.Errorf("digest depends on signature/public_key fields")
	}
}

func TestCanonicalDigest_DependsOnSignedFields(t *testing.T) {
	t.Parallel()
	cases := map[string]func(m *Manifest){
		"name_changes":    func(m *Manifest) { m.Name = "different" },
		"version_changes": func(m *Manifest) { m.Version = "2.0.0" },
		"file_path":       func(m *Manifest) { m.Files[0].Path = "moved.md" },
		"file_sha":        func(m *Manifest) { m.Files[0].SHA256 = hex.EncodeToString(make([]byte, 32))[:62] + "ff" },
		"signed_at":       func(m *Manifest) { m.SignedAt = 9999999999 },
	}
	base, _ := validManifest().CanonicalDigest()
	for name, mut := range cases {
		t.Run(name, func(t *testing.T) {
			m := validManifest()
			mut(m)
			got, _ := m.CanonicalDigest()
			if bytes.Equal(base, got) {
				t.Errorf("digest unchanged after mutating %s", name)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Sign + Verify
// ---------------------------------------------------------------------------

func TestSign_PopulatesFields(t *testing.T) {
	t.Parallel()
	kp, _ := GenerateKeypair()
	m := validManifest()
	if err := Sign(m, kp); err != nil {
		t.Fatalf("Sign: %v", err)
	}
	if len(m.Signature) != 2*SignatureSize {
		t.Errorf("Signature hex len = %d, want %d", len(m.Signature), 2*SignatureSize)
	}
	if m.PublicKeyHex != kp.PublicKeyHex() {
		t.Errorf("PublicKeyHex mismatch")
	}
}

func TestSign_NilArgs(t *testing.T) {
	t.Parallel()
	kp, _ := GenerateKeypair()
	if err := Sign(nil, kp); err == nil {
		t.Error("nil manifest: expected error")
	}
	if err := Sign(validManifest(), nil); err == nil {
		t.Error("nil keypair: expected error")
	}
}

func TestVerify_HappyPath(t *testing.T) {
	t.Parallel()
	kp, _ := GenerateKeypair()
	m := validManifest()
	if err := Sign(m, kp); err != nil {
		t.Fatalf("Sign: %v", err)
	}
	if err := Verify(m, []ed25519.PublicKey{kp.Public}); err != nil {
		t.Errorf("Verify happy: %v", err)
	}
}

func TestVerify_TamperedManifestFails(t *testing.T) {
	t.Parallel()
	kp, _ := GenerateKeypair()
	m := validManifest()
	_ = Sign(m, kp)

	cases := map[string]func(m *Manifest){
		"name":     func(m *Manifest) { m.Name = "evil" },
		"version":  func(m *Manifest) { m.Version = "9.9.9" },
		"file_sha": func(m *Manifest) { m.Files[0].SHA256 = hex.EncodeToString(make([]byte, 32))[:62] + "ff" },
		"new_file": func(m *Manifest) {
			m.Files = append(m.Files, ManifestFile{
				Path: "evil.sh", SHA256: hex.EncodeToString(make([]byte, 32)), Size: 1,
			})
		},
	}
	for name, mut := range cases {
		t.Run(name, func(t *testing.T) {
			m2 := validManifest()
			_ = Sign(m2, kp)
			mut(m2)
			err := Verify(m2, []ed25519.PublicKey{kp.Public})
			if !errors.Is(err, ErrSignatureInvalid) {
				t.Errorf("tampered %s: err = %v, want ErrSignatureInvalid", name, err)
			}
		})
	}
}

func TestVerify_MissingFields(t *testing.T) {
	t.Parallel()
	kp, _ := GenerateKeypair()
	m := validManifest()
	_ = Sign(m, kp)

	t.Run("no_signature", func(t *testing.T) {
		m2 := *m
		m2.Signature = ""
		err := Verify(&m2, []ed25519.PublicKey{kp.Public})
		if !errors.Is(err, ErrSignatureInvalid) {
			t.Errorf("err = %v, want ErrSignatureInvalid", err)
		}
	})
	t.Run("no_pubkey", func(t *testing.T) {
		m2 := *m
		m2.PublicKeyHex = ""
		err := Verify(&m2, []ed25519.PublicKey{kp.Public})
		if !errors.Is(err, ErrSignatureInvalid) {
			t.Errorf("err = %v, want ErrSignatureInvalid", err)
		}
	})
	t.Run("malformed_signature_hex", func(t *testing.T) {
		m2 := *m
		m2.Signature = "not-hex"
		err := Verify(&m2, []ed25519.PublicKey{kp.Public})
		if !errors.Is(err, ErrSignatureInvalid) {
			t.Errorf("err = %v, want ErrSignatureInvalid", err)
		}
	})
	t.Run("short_signature", func(t *testing.T) {
		m2 := *m
		m2.Signature = "abcd"
		err := Verify(&m2, []ed25519.PublicKey{kp.Public})
		if !errors.Is(err, ErrSignatureInvalid) {
			t.Errorf("err = %v, want ErrSignatureInvalid", err)
		}
	})
}

func TestVerify_UntrustedPublisher(t *testing.T) {
	t.Parallel()
	signerKP, _ := GenerateKeypair()
	otherKP, _ := GenerateKeypair()
	m := validManifest()
	_ = Sign(m, signerKP)

	// Trust set excludes the actual signer.
	err := Verify(m, []ed25519.PublicKey{otherKP.Public})
	if !errors.Is(err, ErrUntrustedPublisher) {
		t.Errorf("err = %v, want ErrUntrustedPublisher", err)
	}
}

func TestVerify_EmptyTrustSet(t *testing.T) {
	t.Parallel()
	kp, _ := GenerateKeypair()
	m := validManifest()
	_ = Sign(m, kp)

	// Empty (non-nil) slice — every signature is untrusted.
	err := Verify(m, []ed25519.PublicKey{})
	if !errors.Is(err, ErrUntrustedPublisher) {
		t.Errorf("err = %v, want ErrUntrustedPublisher", err)
	}
}

func TestVerify_NilTrustSet_TestOnlyMode(t *testing.T) {
	t.Parallel()
	kp, _ := GenerateKeypair()
	m := validManifest()
	_ = Sign(m, kp)

	// nil trust set disables the trust check (test/dev convenience).
	if err := Verify(m, nil); err != nil {
		t.Errorf("nil trust set: %v", err)
	}
}

func TestVerify_MultiTrustedPublisher(t *testing.T) {
	t.Parallel()
	signerKP, _ := GenerateKeypair()
	otherKP, _ := GenerateKeypair()
	m := validManifest()
	_ = Sign(m, signerKP)

	// Trust set has both keys; signer's matches one of them.
	err := Verify(m, []ed25519.PublicKey{otherKP.Public, signerKP.Public})
	if err != nil {
		t.Errorf("multi-trust: %v", err)
	}
}

func TestSignVerify_RealDigest(t *testing.T) {
	t.Parallel()
	// Cross-check that we sign the canonical digest, not arbitrary
	// bytes — Verify must succeed even after Sign rewrites the
	// signature/public_key fields (which CanonicalDigest excludes).
	kp, _ := GenerateKeypair()
	m := validManifest()

	digestBefore, _ := m.CanonicalDigest()
	_ = Sign(m, kp)
	digestAfter, _ := m.CanonicalDigest()

	if !bytes.Equal(digestBefore, digestAfter) {
		t.Errorf("digest changed after Sign — sig fields not excluded from canonical form")
	}
}
