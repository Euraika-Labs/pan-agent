package marketplace

import (
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"strings"
)

// Phase 13 WS#13.C — trust-set loader. Reads pinned publisher keys
// from a JSON file on disk so the install pipeline (skillinstall.Install)
// has the trustedPublicKeys argument it needs to call Verify.
//
// File shape — small + extensible:
//
//   {
//     "version": 1,
//     "publishers": [
//       {
//         "fingerprint": "1a2b3c4d5e6f7080",
//         "public_key":  "0123…ef",
//         "name":        "Euraika Labs",
//         "added_at":    1700000000
//       }
//     ]
//   }
//
// fingerprint is informational (the UI shows it). public_key is the
// 64-hex-char Ed25519 public key. Loading is order-stable so the
// fingerprint shown in error messages matches the on-disk listing.

// TrustSetVersion is the schema version of the trust-set file. Bumped
// only on incompatible shape changes.
const TrustSetVersion = 1

// TrustEntry is one row in the trust-set file. Fingerprint is derived
// from PublicKey for cross-checking; if both are present and the
// fingerprint disagrees, LoadTrustSet errors.
type TrustEntry struct {
	Fingerprint string `json:"fingerprint,omitempty"`
	PublicKey   string `json:"public_key"`
	Name        string `json:"name,omitempty"`
	AddedAt     int64  `json:"added_at,omitempty"`
}

// TrustSet is the parsed file. Publishers preserves on-disk order.
type TrustSet struct {
	Version    int          `json:"version"`
	Publishers []TrustEntry `json:"publishers"`
}

// ErrTrustSetInvalid is returned for shape errors (wrong version,
// malformed JSON, fingerprint mismatch, etc.). Distinct from
// ErrInvalidKey so the install UI can show a clearer error.
var ErrTrustSetInvalid = errors.New("marketplace: trust set invalid")

// LoadTrustSet reads a trust-set file from path and returns the
// parsed entries plus the []ed25519.PublicKey slice ready to pass to
// Verify. Returns (TrustSet{}, nil, nil) when the file does not exist
// — callers treat that as "trust nothing" (Verify with an empty
// non-nil slice rejects every bundle).
func LoadTrustSet(path string) (*TrustSet, []ed25519.PublicKey, error) {
	body, err := os.ReadFile(path) //nolint:gosec // caller-supplied path
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			// Empty file is the documented "no trust" mode.
			return &TrustSet{Version: TrustSetVersion}, []ed25519.PublicKey{}, nil
		}
		return nil, nil, fmt.Errorf("%w: read: %v", ErrTrustSetInvalid, err)
	}
	return parseTrustSet(body)
}

// parseTrustSet does the JSON parsing + per-entry validation in one
// place so LoadTrustSet and tests can share the same code path.
func parseTrustSet(body []byte) (*TrustSet, []ed25519.PublicKey, error) {
	var ts TrustSet
	if err := json.Unmarshal(body, &ts); err != nil {
		return nil, nil, fmt.Errorf("%w: parse: %v", ErrTrustSetInvalid, err)
	}
	if ts.Version == 0 {
		// Allow callers to omit version on hand-edited files; treat
		// as the current version.
		ts.Version = TrustSetVersion
	}
	if ts.Version != TrustSetVersion {
		return nil, nil, fmt.Errorf("%w: version %d not supported (expected %d)",
			ErrTrustSetInvalid, ts.Version, TrustSetVersion)
	}

	pubs := make([]ed25519.PublicKey, 0, len(ts.Publishers))
	seen := map[string]bool{}
	for i, e := range ts.Publishers {
		if e.PublicKey == "" {
			return nil, nil, fmt.Errorf("%w: entry[%d] public_key empty",
				ErrTrustSetInvalid, i)
		}
		pub, err := ParsePublicKey(e.PublicKey)
		if err != nil {
			return nil, nil, fmt.Errorf("%w: entry[%d] %v",
				ErrTrustSetInvalid, i, err)
		}
		// Cross-check the optional fingerprint against the derived one.
		if e.Fingerprint != "" {
			derived := FingerprintOf(pub)
			if !strings.EqualFold(e.Fingerprint, derived) {
				return nil, nil, fmt.Errorf("%w: entry[%d] fingerprint %q != derived %q",
					ErrTrustSetInvalid, i, e.Fingerprint, derived)
			}
		}
		// Reject duplicate keys — confusing UX if the same publisher
		// appears twice, and Verify treats both as the same trust
		// anyway. Surface the duplication early.
		hex := strings.ToLower(e.PublicKey)
		if seen[hex] {
			return nil, nil, fmt.Errorf("%w: entry[%d] duplicate public_key",
				ErrTrustSetInvalid, i)
		}
		seen[hex] = true
		pubs = append(pubs, pub)
	}
	return &ts, pubs, nil
}

// SaveTrustSet writes the trust set to path with stable formatting
// (json.MarshalIndent, two-space). Atomic-ish via temp + rename.
//
// Auto-fills missing fingerprints from the public key so the on-disk
// listing always shows a fingerprint for every entry. Sorting is
// caller's responsibility — the file preserves the slice order.
func SaveTrustSet(path string, ts *TrustSet) error {
	if ts == nil {
		return fmt.Errorf("marketplace: SaveTrustSet: nil ts")
	}
	if ts.Version == 0 {
		ts.Version = TrustSetVersion
	}
	for i := range ts.Publishers {
		if ts.Publishers[i].Fingerprint == "" && ts.Publishers[i].PublicKey != "" {
			pub, err := ParsePublicKey(ts.Publishers[i].PublicKey)
			if err == nil {
				ts.Publishers[i].Fingerprint = FingerprintOf(pub)
			}
		}
	}
	body, err := json.MarshalIndent(ts, "", "  ")
	if err != nil {
		return fmt.Errorf("marketplace: SaveTrustSet: marshal: %w", err)
	}
	tmp, err := os.CreateTemp(stripFile(path), "trust.*.tmp")
	if err != nil {
		return fmt.Errorf("marketplace: SaveTrustSet: temp: %w", err)
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }()
	if _, err := tmp.Write(body); err != nil {
		tmp.Close()
		return fmt.Errorf("marketplace: SaveTrustSet: write: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("marketplace: SaveTrustSet: close: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("marketplace: SaveTrustSet: rename: %w", err)
	}
	return nil
}

// PinPublisher appends a new entry to the trust set. If a publisher
// with the same public key already exists, returns the existing entry
// without mutation (idempotent). Convenience for the desktop "pin
// publisher" flow.
func PinPublisher(ts *TrustSet, pub ed25519.PublicKey, name string, addedAt int64) (*TrustEntry, bool) {
	if ts == nil || len(pub) != PublicKeySize {
		return nil, false
	}
	for i := range ts.Publishers {
		existing, err := ParsePublicKey(ts.Publishers[i].PublicKey)
		if err == nil && pubsEqual(existing, pub) {
			return &ts.Publishers[i], false
		}
	}
	entry := TrustEntry{
		Fingerprint: FingerprintOf(pub),
		PublicKey:   stringFromKey(pub),
		Name:        name,
		AddedAt:     addedAt,
	}
	ts.Publishers = append(ts.Publishers, entry)
	return &ts.Publishers[len(ts.Publishers)-1], true
}

// stringFromKey returns the hex of pub, used to keep PinPublisher
// cycle-free vs. importing encoding/hex twice in the same file.
func stringFromKey(pub ed25519.PublicKey) string {
	out := make([]byte, len(pub)*2)
	const hexChars = "0123456789abcdef"
	for i, b := range pub {
		out[i*2] = hexChars[b>>4]
		out[i*2+1] = hexChars[b&0x0f]
	}
	return string(out)
}

// stripFile returns the directory portion of a path. Used by
// SaveTrustSet to scope CreateTemp to the same dir as the destination
// (so the rename is on the same filesystem). Pure stdlib-free helper
// to avoid pulling in path/filepath here when the rest of the file
// doesn't need it.
func stripFile(p string) string {
	for i := len(p) - 1; i >= 0; i-- {
		if p[i] == '/' || p[i] == '\\' {
			if i == 0 {
				return string(p[0])
			}
			return p[:i]
		}
	}
	return "."
}
