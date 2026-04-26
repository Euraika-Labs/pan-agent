package marketplace

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// SchemaSkillV1 is the manifest schema identifier for the v1 skill
// bundle shape. The schema string is signed alongside the rest of the
// manifest so a future v2 producer can't forge a v1 bundle.
const SchemaSkillV1 = "pan-agent.skill.v1"

// ManifestFile records one file in a signed bundle. Path is the
// bundle-relative path with forward slashes; SHA256 is the lowercase
// hex digest of the raw file bytes; Size is informational + lets the
// installer pre-allocate.
type ManifestFile struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
	Size   int64  `json:"size"`
}

// Manifest describes a signed skill bundle. CanonicalDigest serialises
// the manifest to a deterministic byte representation (sorted file
// list, sorted JSON keys, signature/public-key fields excluded) so
// Sign + Verify always agree on what got signed.
//
// Field tags are JSON-aligned for the wire format. The Signature +
// PublicKeyHex fields carry the proof; everything else is the
// payload. CanonicalDigest pins this asymmetry — a wire format that
// added new fields without bumping Schema would break the digest and
// fail Verify, which is the desired conservative behaviour.
type Manifest struct {
	Schema      string         `json:"schema"`
	Name        string         `json:"name"`
	Version     string         `json:"version"`
	Author      string         `json:"author,omitempty"`
	Description string         `json:"description,omitempty"`
	SignedAt    int64          `json:"signed_at"`
	Files       []ManifestFile `json:"files"`

	// Signature block — present after Sign, omitted by CanonicalDigest.
	Signature    string `json:"signature,omitempty"`
	PublicKeyHex string `json:"public_key,omitempty"`
}

// Validate runs schema/required-field checks. Returns nil when the
// manifest is structurally OK (signature validity is a separate
// pass via Verify).
func (m *Manifest) Validate() error {
	if m == nil {
		return fmt.Errorf("marketplace: nil manifest")
	}
	if m.Schema != SchemaSkillV1 {
		return fmt.Errorf("marketplace: schema = %q, want %q", m.Schema, SchemaSkillV1)
	}
	if m.Name == "" {
		return fmt.Errorf("marketplace: name required")
	}
	if m.Version == "" {
		return fmt.Errorf("marketplace: version required")
	}
	if len(m.Files) == 0 {
		return fmt.Errorf("marketplace: at least one file required")
	}
	seen := map[string]bool{}
	for i, f := range m.Files {
		if f.Path == "" {
			return fmt.Errorf("marketplace: file[%d] path empty", i)
		}
		if strings.HasPrefix(f.Path, "/") {
			return fmt.Errorf("marketplace: file[%d] path %q must be relative", i, f.Path)
		}
		if strings.Contains(f.Path, "..") {
			return fmt.Errorf("marketplace: file[%d] path %q must not contain dotdot", i, f.Path)
		}
		if strings.Contains(f.Path, "\\") {
			return fmt.Errorf("marketplace: file[%d] path %q must use forward slashes", i, f.Path)
		}
		if seen[f.Path] {
			return fmt.Errorf("marketplace: duplicate file path %q", f.Path)
		}
		seen[f.Path] = true
		if len(f.SHA256) != 64 {
			return fmt.Errorf("marketplace: file[%d] sha256 must be 64 hex chars, got %d",
				i, len(f.SHA256))
		}
		if _, err := hex.DecodeString(f.SHA256); err != nil {
			return fmt.Errorf("marketplace: file[%d] sha256 not hex: %v", i, err)
		}
		if f.Size < 0 {
			return fmt.Errorf("marketplace: file[%d] size negative", i)
		}
	}
	return nil
}

// canonicalForm returns the deterministic byte serialisation of the
// signed payload. The signature + public-key fields are excluded; the
// file list is sorted by Path; JSON keys are emitted in field-order
// via the explicit map shape so a future Manifest field addition
// can't silently change the canonical form for old keys.
func (m *Manifest) canonicalForm() ([]byte, error) {
	if err := m.Validate(); err != nil {
		return nil, err
	}
	files := make([]ManifestFile, len(m.Files))
	copy(files, m.Files)
	sort.Slice(files, func(i, j int) bool { return files[i].Path < files[j].Path })

	// Build a map with only the signed fields, then serialise. Using
	// a tightly-typed struct here would tie canonical form to the Go
	// shape; using a map lets us control the key order exactly.
	signed := []struct {
		Key   string
		Value any
	}{
		{"schema", m.Schema},
		{"name", m.Name},
		{"version", m.Version},
		{"author", m.Author},
		{"description", m.Description},
		{"signed_at", m.SignedAt},
		{"files", files},
	}

	// Emit keys in the order above (NOT sorted) — sorting the order
	// would be equally deterministic but conventionally sign in
	// declaration order so future readers can match struct ↔ digest
	// without remembering to sort.
	var b strings.Builder
	b.WriteString("{")
	for i, kv := range signed {
		if i > 0 {
			b.WriteString(",")
		}
		k, err := json.Marshal(kv.Key)
		if err != nil {
			return nil, fmt.Errorf("marketplace: canonicalForm key: %w", err)
		}
		v, err := json.Marshal(kv.Value)
		if err != nil {
			return nil, fmt.Errorf("marketplace: canonicalForm value for %q: %w", kv.Key, err)
		}
		b.Write(k)
		b.WriteString(":")
		b.Write(v)
	}
	b.WriteString("}")
	return []byte(b.String()), nil
}

// CanonicalDigest returns sha256(canonicalForm). Sign + Verify both
// use this as the message bytes so they agree on what got signed.
func (m *Manifest) CanonicalDigest() ([]byte, error) {
	form, err := m.canonicalForm()
	if err != nil {
		return nil, err
	}
	d := sha256.Sum256(form)
	return d[:], nil
}
