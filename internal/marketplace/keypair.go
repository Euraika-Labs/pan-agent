// Package marketplace owns the signing chain that protects the Phase 13
// WS#13.C skill marketplace. Skill bundles ship as a directory tree
// described by a Manifest; each manifest is signed by an Ed25519
// publisher key. The desktop install pipeline (next slice) verifies the
// signature against a trust set baked into the binary plus any user-
// added pin entries, so a bundle that wasn't signed by a trusted
// publisher can never reach the disk.
//
// This file owns the keypair primitive only: generation, hex
// serialisation, and a short fingerprint for display. Keyring-backed
// persistence + the manifest shape live in sibling files so each
// concern is independently testable.
package marketplace

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
)

// PublicKeySize / SignatureSize / SeedSize mirror the ed25519 package
// constants but are re-exported here so callers don't need to import
// crypto/ed25519 just to size a buffer.
const (
	PublicKeySize  = ed25519.PublicKeySize  // 32
	SignatureSize  = ed25519.SignatureSize  // 64
	SeedSize       = ed25519.SeedSize       // 32
	PrivateKeySize = ed25519.PrivateKeySize // 64
)

// ErrInvalidKey is returned when a key/seed failed parsing or had
// the wrong length. errors.Is supports matching the wrapper.
var ErrInvalidKey = errors.New("marketplace: invalid key")

// Keypair holds an Ed25519 publisher keypair. The private key is
// sensitive — callers should treat the struct itself as a secret and
// avoid logging it. ZeroPrivate clears the private bytes when done.
type Keypair struct {
	Public  ed25519.PublicKey
	Private ed25519.PrivateKey
}

// GenerateKeypair mints a fresh Ed25519 keypair via crypto/rand.
// Returns the public + private halves wrapped in a Keypair.
func GenerateKeypair() (*Keypair, error) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("marketplace: GenerateKeypair: %w", err)
	}
	return &Keypair{Public: pub, Private: priv}, nil
}

// FromSeed reconstructs a Keypair from a 32-byte seed. Useful when
// the seed is the persisted form (e.g. base64'd into rag_state, or
// keyring-stored). Returns ErrInvalidKey when the seed length is wrong.
func FromSeed(seed []byte) (*Keypair, error) {
	if len(seed) != SeedSize {
		return nil, fmt.Errorf("%w: seed must be %d bytes, got %d",
			ErrInvalidKey, SeedSize, len(seed))
	}
	priv := ed25519.NewKeyFromSeed(seed)
	pub, ok := priv.Public().(ed25519.PublicKey)
	if !ok {
		// ed25519.NewKeyFromSeed always returns a PrivateKey whose
		// .Public() is an ed25519.PublicKey. The cast is guaranteed
		// to succeed; the check is defensive against future stdlib
		// reshapes.
		return nil, fmt.Errorf("%w: derived public key has wrong type", ErrInvalidKey)
	}
	return &Keypair{Public: pub, Private: priv}, nil
}

// Seed returns the 32-byte seed that originally produced this
// keypair. The seed is the canonical persisted form — it's smaller
// than the full PrivateKey and can be used to reconstruct the full
// keypair via FromSeed.
func (k *Keypair) Seed() []byte {
	if len(k.Private) < SeedSize {
		return nil
	}
	return k.Private.Seed()
}

// PublicKeyHex returns the public key as a hex string. This is the
// stable on-disk + on-wire form used in Manifest.PublicKeyHex and the
// trust pin file.
func (k *Keypair) PublicKeyHex() string {
	return hex.EncodeToString(k.Public)
}

// Fingerprint returns the first 16 hex chars of sha256(public key).
// Short enough to display in a UI ("pin publisher 1a2b3c4d…"), long
// enough to keep collision odds vanishingly small for the size of a
// realistic skill marketplace.
func (k *Keypair) Fingerprint() string {
	h := sha256.Sum256(k.Public)
	return hex.EncodeToString(h[:8])
}

// ZeroPrivate overwrites the private-key bytes with zeros. Call when
// the keypair is no longer needed so the secret doesn't linger in
// memory longer than necessary. Idempotent.
func (k *Keypair) ZeroPrivate() {
	for i := range k.Private {
		k.Private[i] = 0
	}
}

// ParsePublicKey decodes a hex-encoded public key. Returns
// ErrInvalidKey when the input is malformed or the wrong length.
func ParsePublicKey(s string) (ed25519.PublicKey, error) {
	b, err := hex.DecodeString(s)
	if err != nil {
		return nil, fmt.Errorf("%w: hex decode: %v", ErrInvalidKey, err)
	}
	if len(b) != PublicKeySize {
		return nil, fmt.Errorf("%w: public key must be %d bytes, got %d",
			ErrInvalidKey, PublicKeySize, len(b))
	}
	out := make(ed25519.PublicKey, PublicKeySize)
	copy(out, b)
	return out, nil
}

// FingerprintOf returns the display fingerprint for any public key.
// Mirrors Keypair.Fingerprint but works with a bare public key — used
// by the install pipeline when only the public half is available
// (e.g. parsing a Manifest that came over the wire).
func FingerprintOf(pub ed25519.PublicKey) string {
	h := sha256.Sum256(pub)
	return hex.EncodeToString(h[:8])
}
