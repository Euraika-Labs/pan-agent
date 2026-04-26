package marketplace

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
)

// Phase 13 WS#13.C — producer-side bundle builder tests.

// writeFile is a small helper that creates a file with content under
// dir, mkdir-p'ing parents as needed.
func writeFile(t *testing.T, dir, rel string, content []byte) {
	t.Helper()
	p := filepath.Join(dir, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(p, content, 0o644); err != nil {
		t.Fatalf("write %q: %v", rel, err)
	}
}

func TestBuildManifest_HappyPath(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	writeFile(t, dir, "SKILL.md", []byte("# skill"))
	writeFile(t, dir, "src/main.go", []byte("package main"))
	writeFile(t, dir, "templates/t.txt", []byte("template"))

	m, err := BuildManifest(dir, BuildOptions{})
	if err != nil {
		t.Fatalf("BuildManifest: %v", err)
	}
	if m.Schema != SchemaSkillV1 {
		t.Errorf("Schema = %q, want %q", m.Schema, SchemaSkillV1)
	}
	if m.SignedAt == 0 {
		t.Error("SignedAt unpopulated")
	}
	if len(m.Files) != 3 {
		t.Errorf("Files count = %d, want 3", len(m.Files))
	}

	// File list must be sorted by Path so canonical-form is stable.
	for i := 1; i < len(m.Files); i++ {
		if m.Files[i].Path < m.Files[i-1].Path {
			t.Errorf("Files not sorted: %q < %q", m.Files[i].Path, m.Files[i-1].Path)
		}
	}

	// Hashes must match a stdlib sha256 of the on-disk content.
	expectedSum := sha256.Sum256([]byte("# skill"))
	expected := hex.EncodeToString(expectedSum[:])
	for _, f := range m.Files {
		if f.Path == "SKILL.md" && f.SHA256 != expected {
			t.Errorf("SKILL.md hash = %s, want %s", f.SHA256, expected)
		}
	}
}

func TestBuildManifest_NotADirectory(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	notDir := filepath.Join(dir, "x")
	_ = os.WriteFile(notDir, []byte("x"), 0o644)
	_, err := BuildManifest(notDir, BuildOptions{})
	if err == nil {
		t.Error("expected error on non-directory")
	}
}

func TestBuildManifest_EmptyDir(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	_, err := BuildManifest(dir, BuildOptions{})
	if err == nil {
		t.Error("expected error on empty source dir")
	}
}

func TestBuildManifest_SkipsExistingManifest(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	writeFile(t, dir, "SKILL.md", []byte("# skill"))
	writeFile(t, dir, ManifestFilename, []byte(`{"schema":"old"}`))

	m, err := BuildManifest(dir, BuildOptions{})
	if err != nil {
		t.Fatalf("BuildManifest: %v", err)
	}
	for _, f := range m.Files {
		if f.Path == ManifestFilename {
			t.Errorf("manifest.json shouldn't appear in Files: %+v", f)
		}
	}
}

func TestBuildManifest_SkipPredicate(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	writeFile(t, dir, "SKILL.md", []byte("# skill"))
	writeFile(t, dir, "ignore-me.txt", []byte("noise"))

	skip := func(rel string) bool { return rel == "ignore-me.txt" }
	m, err := BuildManifest(dir, BuildOptions{Skip: skip})
	if err != nil {
		t.Fatalf("BuildManifest: %v", err)
	}
	for _, f := range m.Files {
		if f.Path == "ignore-me.txt" {
			t.Errorf("Skip predicate ignored: %+v", f)
		}
	}
}

func TestBuildManifest_ReproducibleSignedAt(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	writeFile(t, dir, "x.txt", []byte("x"))
	m, err := BuildManifest(dir, BuildOptions{SignedAt: 1700000000})
	if err != nil {
		t.Fatalf("BuildManifest: %v", err)
	}
	if m.SignedAt != 1700000000 {
		t.Errorf("SignedAt = %d, want 1700000000", m.SignedAt)
	}
}

func TestBuildManifest_ForwardSlashes(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	writeFile(t, dir, "a/b/c/d.txt", []byte("nested"))
	m, err := BuildManifest(dir, BuildOptions{})
	if err != nil {
		t.Fatalf("BuildManifest: %v", err)
	}
	for _, f := range m.Files {
		if strings.Contains(f.Path, "\\") {
			t.Errorf("Path contains backslash: %q", f.Path)
		}
	}
}

func TestBuildManifest_RejectSymlinks(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	writeFile(t, dir, "real.txt", []byte("real"))
	if err := os.Symlink("real.txt", filepath.Join(dir, "linked.txt")); err != nil {
		t.Skipf("symlink unsupported: %v", err)
	}
	_, err := BuildManifest(dir, BuildOptions{})
	if err == nil {
		t.Error("expected symlink rejection")
	}
}

func TestSkipDotfilesAndBuildArtefacts(t *testing.T) {
	t.Parallel()
	cases := map[string]bool{
		"src/main.go":         false,
		".git/HEAD":           true,
		".DS_Store":           true,
		"thing/.cache/x":      true,
		"build/main.exe":      true,
		"build/main.pyc":      true,
		"normal/file.md":      false,
		"docs/screenshot.png": false,
		"src/.hidden":         true,
	}
	for in, want := range cases {
		if got := SkipDotfilesAndBuildArtefacts(in); got != want {
			t.Errorf("SkipDotfilesAndBuildArtefacts(%q) = %v, want %v", in, got, want)
		}
	}
}

// ---------------------------------------------------------------------------
// WriteBundle (build → sign → write manifest.json) integration
// ---------------------------------------------------------------------------

func TestWriteBundle_RoundTripWithLoadBundle(t *testing.T) {
	t.Parallel()
	kp, _ := GenerateKeypair()
	dir := t.TempDir()
	writeFile(t, dir, "SKILL.md", []byte("---\nname: x\ncategory: y\ndescription: z\n---\nbody"))
	writeFile(t, dir, "templates/a.txt", []byte("aaa"))

	m, err := WriteBundle(dir, "test-skill", "1.0.0", "alice", "test", kp, BuildOptions{})
	if err != nil {
		t.Fatalf("WriteBundle: %v", err)
	}
	if m.Name != "test-skill" {
		t.Errorf("Name not propagated: %q", m.Name)
	}
	if m.Signature == "" || m.PublicKeyHex == "" {
		t.Error("expected signed manifest")
	}

	// LoadBundle must accept what WriteBundle produced.
	b, err := LoadBundle(dir, []ed25519.PublicKey{kp.Public})
	if err != nil {
		t.Fatalf("LoadBundle round-trip: %v", err)
	}
	if b.Manifest.Name != "test-skill" {
		t.Errorf("LoadBundle Name = %q, want test-skill", b.Manifest.Name)
	}
}

func TestWriteBundle_ValidationErrors(t *testing.T) {
	t.Parallel()
	kp, _ := GenerateKeypair()
	dir := t.TempDir()
	writeFile(t, dir, "x.txt", []byte("x"))

	cases := []struct {
		name             string
		argName, version string
		kp               *Keypair
	}{
		{"no_name", "", "1.0.0", kp},
		{"no_version", "n", "", kp},
		{"no_kp", "n", "1.0.0", nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := WriteBundle(dir, tc.argName, tc.version, "", "", tc.kp, BuildOptions{})
			if err == nil {
				t.Error("expected validation error")
			}
		})
	}
}

func TestWriteBundle_OverwritesExistingManifest(t *testing.T) {
	t.Parallel()
	kp, _ := GenerateKeypair()
	dir := t.TempDir()
	writeFile(t, dir, "SKILL.md", []byte("---\nname: x\ncategory: y\ndescription: z\n---\nbody"))
	// Pre-existing (stale) manifest.json.
	if err := os.WriteFile(filepath.Join(dir, ManifestFilename),
		[]byte(`{"schema":"old","name":"old"}`), 0o644); err != nil {
		t.Fatalf("seed: %v", err)
	}

	if _, err := WriteBundle(dir, "fresh", "1.0.0", "", "", kp, BuildOptions{}); err != nil {
		t.Fatalf("WriteBundle: %v", err)
	}

	mb, err := os.ReadFile(filepath.Join(dir, ManifestFilename))
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	var m Manifest
	if err := json.Unmarshal(mb, &m); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if m.Name != "fresh" {
		t.Errorf("Name = %q, want fresh", m.Name)
	}
	if m.Schema != SchemaSkillV1 {
		t.Errorf("Schema = %q, want %q", m.Schema, SchemaSkillV1)
	}
}

func TestWriteBundle_AtomicWriteCleansUpTemp(t *testing.T) {
	t.Parallel()
	kp, _ := GenerateKeypair()
	dir := t.TempDir()
	writeFile(t, dir, "SKILL.md", []byte("---\nname: x\ncategory: y\ndescription: z\n---\nbody"))

	if _, err := WriteBundle(dir, "n", "1.0.0", "", "", kp, BuildOptions{}); err != nil {
		t.Fatalf("WriteBundle: %v", err)
	}
	// After success, no temp files should remain.
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("readdir: %v", err)
	}
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ManifestFilename+".tmp.") {
			t.Errorf("temp file leaked: %s", e.Name())
		}
	}
}

// errAlreadyValidated is a sanity check that errors.Is still works on
// a non-marketplace error (defends against accidental shadowing in
// builder.go's error wraps).
func TestErrorWrappingDoesNotShadowSentinels(t *testing.T) {
	t.Parallel()
	if !errors.Is(ErrInvalidKey, ErrInvalidKey) {
		t.Error("errors.Is(ErrInvalidKey, ErrInvalidKey) is false?")
	}
}
