package marketplace

import (
	"crypto/ed25519"
	"encoding/hex"
	"errors"
	"fmt"
)

// ErrSignatureInvalid is returned by Verify when the signature does
// not check against any of the trusted public keys, or when the
// manifest was tampered with after signing. errors.Is supports the
// wrapper pattern.
var ErrSignatureInvalid = errors.New("marketplace: signature invalid")

// ErrUntrustedPublisher is returned by Verify when the manifest was
// signed correctly but by a key not in the trust set. The desktop
// install pipeline distinguishes this from a bad signature so the
// UI can offer "pin this publisher" rather than "bundle is broken".
var ErrUntrustedPublisher = errors.New("marketplace: untrusted publisher")

// Sign computes the canonical digest of m and writes the Ed25519
// signature + the publisher's public key into m.Signature and
// m.PublicKeyHex. The previous Signature/PublicKeyHex fields (if any)
// are overwritten — callers re-sign by calling Sign again.
func Sign(m *Manifest, kp *Keypair) error {
	if m == nil {
		return fmt.Errorf("marketplace: Sign: nil manifest")
	}
	if kp == nil {
		return fmt.Errorf("marketplace: Sign: nil keypair")
	}
	if len(kp.Private) != PrivateKeySize {
		return fmt.Errorf("%w: private key length %d, want %d",
			ErrInvalidKey, len(kp.Private), PrivateKeySize)
	}
	digest, err := m.CanonicalDigest()
	if err != nil {
		return fmt.Errorf("marketplace: Sign canonical: %w", err)
	}
	sig := ed25519.Sign(kp.Private, digest)
	m.Signature = hex.EncodeToString(sig)
	m.PublicKeyHex = kp.PublicKeyHex()
	return nil
}

// Verify checks m.Signature against the canonical digest using
// m.PublicKeyHex, then ensures that public key matches one of the
// trusted entries.
//
// Two failure modes:
//   - ErrSignatureInvalid: the signature is missing, malformed, or
//     doesn't verify (manifest tampered, signed by a different
//     bundle, signature truncated, etc.).
//   - ErrUntrustedPublisher: signature is valid but the public key
//     isn't in trustedPublicKeys. The UI uses this to offer a "pin
//     publisher" flow.
//
// Pass an empty trustedPublicKeys slice to reject every bundle
// regardless of signature validity. Pass nil to disable trust
// checking entirely (testing only — production code MUST supply a
// non-nil trust set to get any value out of signing).
func Verify(m *Manifest, trustedPublicKeys []ed25519.PublicKey) error {
	if m == nil {
		return fmt.Errorf("marketplace: Verify: nil manifest")
	}
	if m.Signature == "" {
		return fmt.Errorf("%w: missing signature", ErrSignatureInvalid)
	}
	if m.PublicKeyHex == "" {
		return fmt.Errorf("%w: missing public_key", ErrSignatureInvalid)
	}

	pub, err := ParsePublicKey(m.PublicKeyHex)
	if err != nil {
		return fmt.Errorf("%w: public_key: %v", ErrSignatureInvalid, err)
	}
	sig, err := hex.DecodeString(m.Signature)
	if err != nil {
		return fmt.Errorf("%w: signature hex: %v", ErrSignatureInvalid, err)
	}
	if len(sig) != SignatureSize {
		return fmt.Errorf("%w: signature length %d, want %d",
			ErrSignatureInvalid, len(sig), SignatureSize)
	}
	digest, err := m.CanonicalDigest()
	if err != nil {
		return fmt.Errorf("%w: canonical: %v", ErrSignatureInvalid, err)
	}
	if !ed25519.Verify(pub, digest, sig) {
		return fmt.Errorf("%w: ed25519 verify failed", ErrSignatureInvalid)
	}

	// Signature passes. Now check trust.
	if trustedPublicKeys == nil {
		// Test-only mode. Production callers must pass a non-nil
		// (possibly empty) slice.
		return nil
	}
	for _, t := range trustedPublicKeys {
		if pubsEqual(pub, t) {
			return nil
		}
	}
	return fmt.Errorf("%w: publisher %s not in trust set",
		ErrUntrustedPublisher, FingerprintOf(pub))
}

// pubsEqual is a constant-time-ish equality check for ed25519 public
// keys. crypto/ed25519 keys are exactly PublicKeySize bytes so a
// straight byte compare is fine for length-checked inputs; we still
// length-check defensively.
func pubsEqual(a, b ed25519.PublicKey) bool {
	if len(a) != len(b) || len(a) != PublicKeySize {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
