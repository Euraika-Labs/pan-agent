package marketplace

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// Phase 13 WS#13.C — bundle parser + hash verifier.
//
// A bundle on disk is a directory tree:
//
//   bundle-root/
//     manifest.json                    <- Manifest (signed)
//     skill.md                         <- listed in manifest.Files
//     src/main.go                      <- listed in manifest.Files
//     ...
//
// LoadBundle opens the manifest, validates it, verifies the
// signature against a trust set, and ensures every file in the
// manifest is present on disk with a matching sha256. Files NOT in
// the manifest cause an error so a malicious unpacker can't slip
// extras past verification.
//
// The result is a Bundle struct describing what's there. The
// install-into-_proposed slice consumes Bundle to stage the tree
// into the existing reviewer-agent queue.

// ManifestFilename is the conventional name of the manifest inside
// a bundle root. Hardcoded so the parser doesn't accept arbitrary
// names (a bundle that ships two manifests would be ambiguous).
const ManifestFilename = "manifest.json"

// MaxBundleFileSize caps any single file we'll read into memory for
// hashing. Bundles are skill source trees, not arbitrary archives —
// 16MB per file is far above any realistic skill while still
// bounding the worst-case allocation a malicious bundle can force.
const MaxBundleFileSize = 16 * 1024 * 1024

// ErrBundleInvalid is returned when the bundle layout is broken
// (missing manifest, file mismatch, extra files, etc.). Distinct
// from ErrSignatureInvalid + ErrUntrustedPublisher so the install
// UI can show a different message ("bundle is corrupt" vs "wrong
// signature" vs "unknown publisher").
var ErrBundleInvalid = errors.New("marketplace: bundle invalid")

// Bundle describes a verified bundle on disk. Returned by LoadBundle
// when every manifest entry matches a file with the right sha256
// AND the signature checks against the supplied trust set.
type Bundle struct {
	// Root is the absolute path to the bundle directory.
	Root string
	// Manifest is the parsed + verified manifest.
	Manifest Manifest
}

// LoadBundle reads bundleRoot/manifest.json, validates the manifest,
// verifies the signature against trustedPublicKeys, then walks every
// file path declared in the manifest and confirms each file exists
// with the expected sha256. Extra files in the bundle root that
// aren't listed in the manifest cause ErrBundleInvalid — a producer
// can't sneak in unsigned content.
//
// trustedPublicKeys follows the same semantics as Verify:
//   - non-nil + non-empty → publisher must be in the set
//   - non-nil + empty → reject everything
//   - nil → test/dev mode (skip trust check; sig still must be valid)
func LoadBundle(bundleRoot string, trustedPublicKeys []ed25519.PublicKey) (*Bundle, error) {
	abs, err := filepath.Abs(bundleRoot)
	if err != nil {
		return nil, fmt.Errorf("%w: abs path: %v", ErrBundleInvalid, err)
	}
	st, err := os.Stat(abs)
	if err != nil {
		return nil, fmt.Errorf("%w: stat root: %v", ErrBundleInvalid, err)
	}
	if !st.IsDir() {
		return nil, fmt.Errorf("%w: bundle root %q is not a directory", ErrBundleInvalid, abs)
	}

	manifestPath := filepath.Join(abs, ManifestFilename)
	mb, err := os.ReadFile(manifestPath) //nolint:gosec // path constructed from validated abs root
	if err != nil {
		return nil, fmt.Errorf("%w: read manifest: %v", ErrBundleInvalid, err)
	}

	var m Manifest
	if err := json.Unmarshal(mb, &m); err != nil {
		return nil, fmt.Errorf("%w: parse manifest: %v", ErrBundleInvalid, err)
	}

	// Signature + trust check FIRST so we don't waste IO hashing a
	// bundle from an unknown publisher. Verify also runs Validate via
	// CanonicalDigest, so a structurally-broken manifest fails here.
	if err := Verify(&m, trustedPublicKeys); err != nil {
		// Pass through the wrapped sentinel so callers can distinguish
		// "bad sig" from "untrusted publisher" without re-parsing.
		return nil, err
	}

	// Hash every declared file + confirm exact match with manifest entry.
	expected := map[string]ManifestFile{}
	for _, f := range m.Files {
		expected[f.Path] = f
	}

	// Walk the bundle root to ensure no UNDECLARED files exist (so
	// an attacker can't slip an unsigned executable next to the
	// signed README). manifest.json itself is allowed to exist
	// without being in the file list.
	declared := map[string]bool{ManifestFilename: true}
	err = filepath.WalkDir(abs, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(abs, path)
		if err != nil {
			return fmt.Errorf("rel: %w", err)
		}
		// Normalise to forward slashes for cross-platform comparison.
		rel = filepath.ToSlash(rel)
		declared[rel] = true
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("%w: walk: %v", ErrBundleInvalid, err)
	}

	// Every file under the root (except manifest.json) must be in the manifest.
	for rel := range declared {
		if rel == ManifestFilename {
			continue
		}
		if _, ok := expected[rel]; !ok {
			return nil, fmt.Errorf("%w: undeclared file %q in bundle", ErrBundleInvalid, rel)
		}
	}

	// Every manifest entry must exist on disk with the right hash + size.
	for _, f := range m.Files {
		full := filepath.Join(abs, filepath.FromSlash(f.Path))
		// Defence-in-depth: confirm the resolved path stays under
		// the bundle root so a path that snuck through Validate
		// can't escape via symlinks the producer planted.
		if !strings.HasPrefix(full+string(filepath.Separator), abs+string(filepath.Separator)) && full != abs {
			return nil, fmt.Errorf("%w: file %q escapes bundle root", ErrBundleInvalid, f.Path)
		}
		st, err := os.Stat(full)
		if err != nil {
			return nil, fmt.Errorf("%w: stat %q: %v", ErrBundleInvalid, f.Path, err)
		}
		if st.IsDir() {
			return nil, fmt.Errorf("%w: %q is a directory, not a file", ErrBundleInvalid, f.Path)
		}
		if st.Size() != f.Size {
			return nil, fmt.Errorf("%w: %q size = %d, manifest claims %d",
				ErrBundleInvalid, f.Path, st.Size(), f.Size)
		}
		if st.Size() > MaxBundleFileSize {
			return nil, fmt.Errorf("%w: %q size %d exceeds cap %d",
				ErrBundleInvalid, f.Path, st.Size(), MaxBundleFileSize)
		}
		actual, err := hashFile(full)
		if err != nil {
			return nil, fmt.Errorf("%w: hash %q: %v", ErrBundleInvalid, f.Path, err)
		}
		if actual != f.SHA256 {
			return nil, fmt.Errorf("%w: %q sha256 = %s, manifest claims %s",
				ErrBundleInvalid, f.Path, actual, f.SHA256)
		}
	}

	return &Bundle{Root: abs, Manifest: m}, nil
}

// hashFile returns the lowercase hex sha256 of the file at path.
// Reads through io.Copy + LimitReader so the worst-case allocation
// is bounded by MaxBundleFileSize even when the on-disk file is
// larger than the size we already validated (defence against TOCTOU
// where a producer truncates a file between Stat and ReadFile).
func hashFile(path string) (string, error) {
	f, err := os.Open(path) //nolint:gosec // path validated by LoadBundle
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, io.LimitReader(f, MaxBundleFileSize+1)); err != nil {
		return "", err
	}
	// If we read more than MaxBundleFileSize, the file changed
	// post-Stat. Fail closed.
	st, err := f.Stat()
	if err != nil {
		return "", err
	}
	if st.Size() > MaxBundleFileSize {
		return "", fmt.Errorf("file grew past cap during read")
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
