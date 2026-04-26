package marketplace

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Phase 13 WS#13.C — producer-side bundle builder. Pair with the
// consumer-side LoadBundle (in bundle.go) for symmetry: BuildManifest
// walks a source directory, computes hashes, and yields an unsigned
// Manifest the caller can populate + Sign.
//
// The two ends share the same canonical-form invariants — the
// builder deliberately doesn't bake in a publisher identity, version
// scheme, or signing key. Those come from the calling layer (a
// future "publish a skill" CLI subcommand or a test helper).
//
// Why not also write manifest.json from this package: keeping the
// builder a pure function of (sourceDir) → Manifest lets a producer
// inspect the result before signing. WriteBundle is the convenience
// wrapper that puts the whole pipeline (build → sign → write) in
// one call when the caller doesn't need the intermediate object.

// BuildOptions configures BuildManifest. Zero-value Options work for
// most cases; callers customise to skip files (build artefacts, dot
// files) or override the timestamp for reproducible builds.
type BuildOptions struct {
	// Skip is called for every walked file; when it returns true, the
	// file is excluded from the manifest. nil → no exclusions.
	// Useful filters: skip "manifest.json" (we add it ourselves), skip
	// ".git" directories, skip build artefacts like "*.pyc".
	Skip func(relPath string) bool

	// SignedAt overrides the time.Now() default for reproducible
	// builds. 0 → use time.Now().Unix().
	SignedAt int64
}

// BuildManifest walks sourceDir and produces an UNSIGNED Manifest
// describing every file under it (subject to opts.Skip).
//
// The returned manifest has Schema set to SchemaSkillV1 + SignedAt
// populated; the caller fills in Name + Version + Author +
// Description before calling Sign.
//
// Files in the manifest are sorted by Path so canonicalForm yields
// the same digest regardless of OS walk order. Path values use
// forward slashes to match LoadBundle's expectations.
//
// ManifestFilename ("manifest.json") is automatically excluded — a
// builder that included its own would create a chicken-and-egg
// problem (the manifest would need to declare itself, but then
// signing would change its hash, and so on).
func BuildManifest(sourceDir string, opts BuildOptions) (*Manifest, error) {
	abs, err := filepath.Abs(sourceDir)
	if err != nil {
		return nil, fmt.Errorf("marketplace: BuildManifest: abs path: %w", err)
	}
	st, err := os.Stat(abs)
	if err != nil {
		return nil, fmt.Errorf("marketplace: BuildManifest: stat: %w", err)
	}
	if !st.IsDir() {
		return nil, fmt.Errorf("marketplace: BuildManifest: %q is not a directory", abs)
	}

	var files []ManifestFile
	err = filepath.WalkDir(abs, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			return nil
		}
		// Symlinks deserve a dedicated decision. For now we reject —
		// a malicious symlink that follows out of the source tree
		// would let the producer slip in arbitrary content (the
		// consumer-side LoadBundle would catch it, but rejecting
		// upstream gives a faster + clearer error).
		if d.Type()&fs.ModeSymlink != 0 {
			return fmt.Errorf("marketplace: BuildManifest: refusing to follow symlink %q",
				path)
		}
		rel, err := filepath.Rel(abs, path)
		if err != nil {
			return fmt.Errorf("rel: %w", err)
		}
		rel = filepath.ToSlash(rel)
		if rel == ManifestFilename {
			// Skip our own manifest if present (idempotent re-build).
			return nil
		}
		if opts.Skip != nil && opts.Skip(rel) {
			return nil
		}
		// Hash + size while we have the file open so the on-disk
		// state is consistent (vs. doing a separate stat then read).
		f, err := os.Open(path) //nolint:gosec // walking a caller-supplied dir
		if err != nil {
			return fmt.Errorf("open %q: %w", rel, err)
		}
		h := sha256.New()
		size, err := io.Copy(h, f)
		f.Close()
		if err != nil {
			return fmt.Errorf("read %q: %w", rel, err)
		}
		if size > MaxBundleFileSize {
			return fmt.Errorf("file %q size %d exceeds cap %d",
				rel, size, MaxBundleFileSize)
		}
		files = append(files, ManifestFile{
			Path:   rel,
			SHA256: hex.EncodeToString(h.Sum(nil)),
			Size:   size,
		})
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("marketplace: BuildManifest: %w", err)
	}
	if len(files) == 0 {
		return nil, fmt.Errorf("marketplace: BuildManifest: no files under %q", abs)
	}

	sort.Slice(files, func(i, j int) bool { return files[i].Path < files[j].Path })

	signedAt := opts.SignedAt
	if signedAt == 0 {
		signedAt = time.Now().Unix()
	}

	return &Manifest{
		Schema:   SchemaSkillV1,
		SignedAt: signedAt,
		Files:    files,
	}, nil
}

// SkipDotfilesAndBuildArtefacts is a convenience Skip predicate
// callers can pass via BuildOptions. Excludes paths under any
// dot-prefixed segment (.git, .DS_Store, etc.) and common build
// artefact extensions.
//
// Producers are free to write their own filter; this is just the
// useful default for hand-authored skill repositories.
func SkipDotfilesAndBuildArtefacts(rel string) bool {
	for _, seg := range strings.Split(rel, "/") {
		if strings.HasPrefix(seg, ".") {
			return true
		}
	}
	switch filepath.Ext(rel) {
	case ".pyc", ".pyo", ".o", ".obj", ".exe", ".dll", ".so", ".dylib":
		return true
	}
	return false
}

// WriteBundle is the convenience wrapper that builds a manifest,
// applies caller-supplied identity fields (Name/Version/Author/Desc),
// signs it with kp, and writes the manifest.json file into sourceDir
// in place. The directory tree itself is left untouched — the caller
// is responsible for providing a directory that already contains the
// files to ship.
//
// Returns the signed Manifest so callers can inspect Files / digest.
//
// WriteBundle does NOT copy the source tree elsewhere. If the
// producer wants the bundle in an output directory separate from
// their source checkout, they should copy the tree first then
// point WriteBundle at the copy.
func WriteBundle(sourceDir string, name, version, author, description string,
	kp *Keypair, opts BuildOptions,
) (*Manifest, error) {
	if name == "" {
		return nil, fmt.Errorf("marketplace: WriteBundle: name required")
	}
	if version == "" {
		return nil, fmt.Errorf("marketplace: WriteBundle: version required")
	}
	if kp == nil {
		return nil, fmt.Errorf("marketplace: WriteBundle: kp required")
	}

	m, err := BuildManifest(sourceDir, opts)
	if err != nil {
		return nil, err
	}
	m.Name = name
	m.Version = version
	m.Author = author
	m.Description = description

	if err := Sign(m, kp); err != nil {
		return nil, fmt.Errorf("marketplace: WriteBundle: sign: %w", err)
	}

	body, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("marketplace: WriteBundle: marshal: %w", err)
	}

	abs, err := filepath.Abs(sourceDir)
	if err != nil {
		return nil, fmt.Errorf("marketplace: WriteBundle: abs: %w", err)
	}
	target := filepath.Join(abs, ManifestFilename)
	// Atomic-ish write: write to a temp file in the same dir, then
	// rename. Same dir guarantees a same-filesystem rename.
	tmp, err := os.CreateTemp(abs, ManifestFilename+".tmp.*")
	if err != nil {
		return nil, fmt.Errorf("marketplace: WriteBundle: temp: %w", err)
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }() // no-op if rename succeeded
	if _, err := tmp.Write(body); err != nil {
		tmp.Close()
		return nil, fmt.Errorf("marketplace: WriteBundle: write: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return nil, fmt.Errorf("marketplace: WriteBundle: close: %w", err)
	}
	if err := os.Rename(tmpName, target); err != nil {
		return nil, fmt.Errorf("marketplace: WriteBundle: rename: %w", err)
	}
	return m, nil
}
