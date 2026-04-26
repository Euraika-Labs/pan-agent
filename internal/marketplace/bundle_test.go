package marketplace

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// Phase 13 WS#13.C — bundle parser/verifier tests. The signing
// primitives are covered in marketplace_test.go; these tests pin
// the on-disk verification contract: layout checks, hash matching,
// undeclared-file rejection, signature/trust pass-through.

// fixtureFile holds one file's bytes + intended bundle path.
type fixtureFile struct {
	Path    string
	Content []byte
}

// makeBundle writes a bundle to a temp dir signed with kp. Returns
// the bundle root path. trustOverride lets tests forge a manifest
// claim that disagrees with the bytes on disk.
func makeBundle(t *testing.T, kp *Keypair, files []fixtureFile, mutate func(m *Manifest)) string {
	t.Helper()
	root := t.TempDir()

	// Write the actual files.
	for _, ff := range files {
		full := filepath.Join(root, filepath.FromSlash(ff.Path))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatalf("mkdir %q: %v", filepath.Dir(full), err)
		}
		if err := os.WriteFile(full, ff.Content, 0o644); err != nil {
			t.Fatalf("write %q: %v", full, err)
		}
	}

	// Build a manifest that matches what we wrote.
	manifestFiles := make([]ManifestFile, 0, len(files))
	for _, ff := range files {
		sum := sha256.Sum256(ff.Content)
		manifestFiles = append(manifestFiles, ManifestFile{
			Path: ff.Path, SHA256: hex.EncodeToString(sum[:]), Size: int64(len(ff.Content)),
		})
	}
	m := &Manifest{
		Schema: SchemaSkillV1, Name: "test", Version: "1.0.0",
		Author: "test", Description: "test fixture", SignedAt: 1700000000,
		Files: manifestFiles,
	}
	if mutate != nil {
		mutate(m)
	}
	if err := Sign(m, kp); err != nil {
		t.Fatalf("Sign: %v", err)
	}

	// Write manifest.json.
	mb, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, ManifestFilename), mb, 0o644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
	return root
}

func TestLoadBundle_HappyPath(t *testing.T) {
	t.Parallel()
	kp, _ := GenerateKeypair()
	root := makeBundle(t, kp, []fixtureFile{
		{Path: "README.md", Content: []byte("# hello")},
		{Path: "src/main.go", Content: []byte("package main")},
	}, nil)

	b, err := LoadBundle(root, []ed25519.PublicKey{kp.Public})
	if err != nil {
		t.Fatalf("LoadBundle: %v", err)
	}
	if b == nil {
		t.Fatal("nil bundle")
	}
	if b.Manifest.Name != "test" {
		t.Errorf("Name = %q, want test", b.Manifest.Name)
	}
	if len(b.Manifest.Files) != 2 {
		t.Errorf("Files count = %d, want 2", len(b.Manifest.Files))
	}
}

func TestLoadBundle_MissingManifest(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	_, err := LoadBundle(root, nil)
	if !errors.Is(err, ErrBundleInvalid) {
		t.Errorf("err = %v, want ErrBundleInvalid", err)
	}
}

func TestLoadBundle_NotADirectory(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	f := filepath.Join(root, "notadir")
	if err := os.WriteFile(f, []byte("x"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	_, err := LoadBundle(f, nil)
	if !errors.Is(err, ErrBundleInvalid) {
		t.Errorf("err = %v, want ErrBundleInvalid", err)
	}
}

func TestLoadBundle_NotJSON(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	if err := os.WriteFile(
		filepath.Join(root, ManifestFilename),
		[]byte("not json at all"),
		0o644,
	); err != nil {
		t.Fatalf("write: %v", err)
	}
	_, err := LoadBundle(root, nil)
	if !errors.Is(err, ErrBundleInvalid) {
		t.Errorf("err = %v, want ErrBundleInvalid", err)
	}
}

func TestLoadBundle_TamperedFile(t *testing.T) {
	t.Parallel()
	kp, _ := GenerateKeypair()
	root := makeBundle(t, kp, []fixtureFile{
		{Path: "README.md", Content: []byte("# hello")},
	}, nil)

	// Modify the file post-sign — sha mismatch must fail.
	if err := os.WriteFile(filepath.Join(root, "README.md"), []byte("# tampered"), 0o644); err != nil {
		t.Fatalf("tamper: %v", err)
	}
	_, err := LoadBundle(root, []ed25519.PublicKey{kp.Public})
	if !errors.Is(err, ErrBundleInvalid) {
		t.Errorf("err = %v, want ErrBundleInvalid", err)
	}
}

func TestLoadBundle_TamperedManifestFailsBeforeHash(t *testing.T) {
	t.Parallel()
	kp, _ := GenerateKeypair()
	root := makeBundle(t, kp, []fixtureFile{
		{Path: "README.md", Content: []byte("# hello")},
	}, nil)

	// Mutate the manifest's claimed hash so the signature no longer
	// covers the on-disk bytes — must fail with ErrSignatureInvalid,
	// NOT ErrBundleInvalid (signature check runs before hashing).
	mb, _ := os.ReadFile(filepath.Join(root, ManifestFilename))
	var m Manifest
	_ = json.Unmarshal(mb, &m)
	m.Files[0].SHA256 = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	tampered, _ := json.MarshalIndent(&m, "", "  ")
	_ = os.WriteFile(filepath.Join(root, ManifestFilename), tampered, 0o644)

	_, err := LoadBundle(root, []ed25519.PublicKey{kp.Public})
	if !errors.Is(err, ErrSignatureInvalid) {
		t.Errorf("err = %v, want ErrSignatureInvalid", err)
	}
}

func TestLoadBundle_UndeclaredFileRejected(t *testing.T) {
	t.Parallel()
	kp, _ := GenerateKeypair()
	root := makeBundle(t, kp, []fixtureFile{
		{Path: "README.md", Content: []byte("# hello")},
	}, nil)

	// Drop an extra file the manifest doesn't list.
	if err := os.WriteFile(filepath.Join(root, "evil.sh"), []byte("rm -rf /"), 0o644); err != nil {
		t.Fatalf("plant: %v", err)
	}
	_, err := LoadBundle(root, []ed25519.PublicKey{kp.Public})
	if !errors.Is(err, ErrBundleInvalid) {
		t.Errorf("err = %v, want ErrBundleInvalid", err)
	}
}

func TestLoadBundle_MissingFile(t *testing.T) {
	t.Parallel()
	kp, _ := GenerateKeypair()
	root := makeBundle(t, kp, []fixtureFile{
		{Path: "README.md", Content: []byte("# hello")},
		{Path: "absent.go", Content: []byte("hi")},
	}, nil)

	// Delete a file the manifest declared.
	_ = os.Remove(filepath.Join(root, "absent.go"))

	_, err := LoadBundle(root, []ed25519.PublicKey{kp.Public})
	if !errors.Is(err, ErrBundleInvalid) {
		t.Errorf("err = %v, want ErrBundleInvalid", err)
	}
}

func TestLoadBundle_WrongSize(t *testing.T) {
	t.Parallel()
	kp, _ := GenerateKeypair()
	// Build a bundle whose manifest claims a wrong size for the file —
	// we'd need to bypass the helper to forge this. Instead, mutate
	// the manifest before signing so the manifest is internally
	// consistent (signs cleanly) but disagrees with disk truth.
	root := t.TempDir()
	content := []byte("# hello")
	_ = os.WriteFile(filepath.Join(root, "README.md"), content, 0o644)

	sum := sha256.Sum256(content)
	m := &Manifest{
		Schema: SchemaSkillV1, Name: "test", Version: "1.0.0",
		SignedAt: 1700000000,
		Files: []ManifestFile{
			{Path: "README.md", SHA256: hex.EncodeToString(sum[:]), Size: 9999}, // wrong size
		},
	}
	if err := Sign(m, kp); err != nil {
		t.Fatalf("Sign: %v", err)
	}
	mb, _ := json.MarshalIndent(m, "", "  ")
	_ = os.WriteFile(filepath.Join(root, ManifestFilename), mb, 0o644)

	_, err := LoadBundle(root, []ed25519.PublicKey{kp.Public})
	if !errors.Is(err, ErrBundleInvalid) {
		t.Errorf("err = %v, want ErrBundleInvalid", err)
	}
}

func TestLoadBundle_UntrustedPublisher(t *testing.T) {
	t.Parallel()
	signer, _ := GenerateKeypair()
	other, _ := GenerateKeypair()
	root := makeBundle(t, signer, []fixtureFile{
		{Path: "x.md", Content: []byte("x")},
	}, nil)

	_, err := LoadBundle(root, []ed25519.PublicKey{other.Public})
	if !errors.Is(err, ErrUntrustedPublisher) {
		t.Errorf("err = %v, want ErrUntrustedPublisher", err)
	}
}

func TestLoadBundle_NilTrustSetPasses(t *testing.T) {
	t.Parallel()
	kp, _ := GenerateKeypair()
	root := makeBundle(t, kp, []fixtureFile{
		{Path: "x.md", Content: []byte("x")},
	}, nil)
	if _, err := LoadBundle(root, nil); err != nil {
		t.Errorf("nil trust set: %v", err)
	}
}

func TestLoadBundle_NestedDirsHandled(t *testing.T) {
	t.Parallel()
	kp, _ := GenerateKeypair()
	root := makeBundle(t, kp, []fixtureFile{
		{Path: "deeply/nested/dir/file.txt", Content: []byte("hi")},
		{Path: "src/cmd/main.go", Content: []byte("package main")},
	}, nil)

	if _, err := LoadBundle(root, []ed25519.PublicKey{kp.Public}); err != nil {
		t.Errorf("nested: %v", err)
	}
}

func TestLoadBundle_OversizedFile(t *testing.T) {
	t.Parallel()
	kp, _ := GenerateKeypair()
	// Forge a manifest with a Size value that exceeds MaxBundleFileSize.
	root := t.TempDir()
	huge := make([]byte, MaxBundleFileSize+1)
	for i := range huge {
		huge[i] = 'A'
	}
	_ = os.WriteFile(filepath.Join(root, "big.bin"), huge, 0o644)

	sum := sha256.Sum256(huge)
	m := &Manifest{
		Schema: SchemaSkillV1, Name: "test", Version: "1.0.0",
		SignedAt: 1700000000,
		Files: []ManifestFile{
			{Path: "big.bin", SHA256: hex.EncodeToString(sum[:]), Size: int64(len(huge))},
		},
	}
	_ = Sign(m, kp)
	mb, _ := json.MarshalIndent(m, "", "  ")
	_ = os.WriteFile(filepath.Join(root, ManifestFilename), mb, 0o644)

	_, err := LoadBundle(root, []ed25519.PublicKey{kp.Public})
	if !errors.Is(err, ErrBundleInvalid) {
		t.Errorf("err = %v, want ErrBundleInvalid (size cap)", err)
	}
}

func TestLoadBundle_DirectoryWhereFileExpected(t *testing.T) {
	t.Parallel()
	kp, _ := GenerateKeypair()
	root := t.TempDir()
	// Manifest claims "thing" is a file; create a directory there instead.
	if err := os.MkdirAll(filepath.Join(root, "thing"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	m := &Manifest{
		Schema: SchemaSkillV1, Name: "test", Version: "1.0.0",
		SignedAt: 1700000000,
		Files: []ManifestFile{
			{Path: "thing", SHA256: hex.EncodeToString(make([]byte, 32)), Size: 0},
		},
	}
	_ = Sign(m, kp)
	mb, _ := json.MarshalIndent(m, "", "  ")
	_ = os.WriteFile(filepath.Join(root, ManifestFilename), mb, 0o644)

	_, err := LoadBundle(root, []ed25519.PublicKey{kp.Public})
	if !errors.Is(err, ErrBundleInvalid) {
		t.Errorf("err = %v, want ErrBundleInvalid (file-vs-dir)", err)
	}
}

func TestHashFile_MatchesSha256(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	content := []byte("the quick brown fox")
	p := filepath.Join(root, "f.txt")
	_ = os.WriteFile(p, content, 0o644)

	got, err := hashFile(p)
	if err != nil {
		t.Fatalf("hashFile: %v", err)
	}
	expected := sha256.Sum256(content)
	if got != hex.EncodeToString(expected[:]) {
		t.Errorf("hash = %s, want %s", got, hex.EncodeToString(expected[:]))
	}
}
