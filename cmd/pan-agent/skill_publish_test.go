package main

import (
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/euraika-labs/pan-agent/internal/marketplace"
	"github.com/euraika-labs/pan-agent/internal/paths"
)

// Phase 13 WS#13.C — producer-side CLI tests. Cover keygen + build
// + the seed-file round-trip, including the keygen → build → verify
// chain that exercises the full publish-and-consume pipeline.

func TestSkillKeygen_HappyPath(t *testing.T) {
	t.Setenv("PAN_AGENT_HOME", t.TempDir())
	if err := cmdSkillKeygen(nil); err != nil {
		t.Fatalf("keygen: %v", err)
	}
	// Seed file should exist; on POSIX systems it must be mode 0o600.
	// Windows doesn't honour Unix file permissions (NTFS ACLs map back
	// through Stat as -rw-rw-rw-), so the perm assertion is POSIX-only.
	path := paths.MarketplacePublisherSeedFile("")
	st, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat seed: %v", err)
	}
	if runtime.GOOS != "windows" && st.Mode().Perm() != 0o600 {
		t.Errorf("seed perm = %v, want 0600", st.Mode().Perm())
	}
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read seed: %v", err)
	}
	if _, err := hex.DecodeString(string(body)); err != nil {
		t.Errorf("seed not hex: %v", err)
	}
}

func TestSkillKeygen_RefusesOverwrite(t *testing.T) {
	t.Setenv("PAN_AGENT_HOME", t.TempDir())
	if err := cmdSkillKeygen(nil); err != nil {
		t.Fatalf("first keygen: %v", err)
	}

	// Re-running without --force should refuse.
	err := cmdSkillKeygen(nil)
	if err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Errorf("expected refusal, got %v", err)
	}
}

func TestSkillKeygen_ForceOverwrites(t *testing.T) {
	t.Setenv("PAN_AGENT_HOME", t.TempDir())
	if err := cmdSkillKeygen(nil); err != nil {
		t.Fatalf("first keygen: %v", err)
	}
	first, _ := os.ReadFile(paths.MarketplacePublisherSeedFile(""))

	if err := cmdSkillKeygen([]string{"--force"}); err != nil {
		t.Fatalf("force keygen: %v", err)
	}
	second, _ := os.ReadFile(paths.MarketplacePublisherSeedFile(""))
	if string(first) == string(second) {
		t.Error("--force did not regenerate the seed")
	}
}

func TestSkillBuild_NoSeed(t *testing.T) {
	t.Setenv("PAN_AGENT_HOME", t.TempDir())
	srcDir := t.TempDir()
	_ = os.WriteFile(filepath.Join(srcDir, "SKILL.md"), []byte("# x"), 0o644)

	err := cmdSkillBuild([]string{"--version", "1.0.0", srcDir})
	if err == nil || !strings.Contains(err.Error(), "skill keygen") {
		t.Errorf("expected 'run keygen' error, got %v", err)
	}
}

func TestSkillBuild_RequiresVersion(t *testing.T) {
	t.Setenv("PAN_AGENT_HOME", t.TempDir())
	if err := cmdSkillKeygen(nil); err != nil {
		t.Fatalf("keygen: %v", err)
	}
	srcDir := t.TempDir()
	_ = os.WriteFile(filepath.Join(srcDir, "SKILL.md"), []byte("# x"), 0o644)

	err := cmdSkillBuild([]string{srcDir})
	if err == nil || !strings.Contains(err.Error(), "version") {
		t.Errorf("expected version-required error, got %v", err)
	}
}

func TestSkillBuild_MissingSourceDir(t *testing.T) {
	t.Setenv("PAN_AGENT_HOME", t.TempDir())
	if err := cmdSkillKeygen(nil); err != nil {
		t.Fatalf("keygen: %v", err)
	}
	err := cmdSkillBuild([]string{"--version", "1.0.0"})
	if err == nil {
		t.Error("expected missing-dir error")
	}
}

func TestSkillBuild_RoundTripWithVerify(t *testing.T) {
	t.Setenv("PAN_AGENT_HOME", t.TempDir())
	if err := cmdSkillKeygen(nil); err != nil {
		t.Fatalf("keygen: %v", err)
	}

	srcDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(srcDir, "SKILL.md"),
		[]byte("---\nname: x\ncategory: y\ndescription: z\n---\nbody"),
		0o644); err != nil {
		t.Fatalf("write SKILL.md: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(srcDir, "templates"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(srcDir, "templates", "a.txt"),
		[]byte("template"), 0o644); err != nil {
		t.Fatalf("write template: %v", err)
	}

	// Build.
	if err := cmdSkillBuild([]string{
		"--version", "1.0.0", "--name", "test-skill",
		"--description", "test fixture", srcDir,
	}); err != nil {
		t.Fatalf("build: %v", err)
	}

	// Manifest exists + parses.
	mb, err := os.ReadFile(filepath.Join(srcDir, marketplace.ManifestFilename))
	if err != nil {
		t.Fatalf("read manifest: %v", err)
	}
	var m marketplace.Manifest
	if err := json.Unmarshal(mb, &m); err != nil {
		t.Fatalf("parse manifest: %v", err)
	}
	if m.Name != "test-skill" || m.Version != "1.0.0" {
		t.Errorf("manifest fields wrong: %+v", m)
	}
	if m.Signature == "" || m.PublicKeyHex == "" {
		t.Error("manifest not signed")
	}

	// Verify (permissive mode — no trust set required).
	if err := cmdSkillVerify([]string{srcDir}); err != nil {
		t.Errorf("verify after build: %v", err)
	}

	// Verify --strict without pin should fail.
	if err := cmdSkillVerify([]string{"--strict", srcDir}); err == nil {
		t.Error("strict verify without pin should fail")
	}

	// Pin our own publisher → strict verify succeeds.
	if err := cmdSkillTrustPin([]string{m.PublicKeyHex}); err != nil {
		t.Fatalf("trust pin: %v", err)
	}
	if err := cmdSkillVerify([]string{"--strict", srcDir}); err != nil {
		t.Errorf("strict verify after pin: %v", err)
	}
}

func TestSkillBuild_NameInferredFromDir(t *testing.T) {
	t.Setenv("PAN_AGENT_HOME", t.TempDir())
	if err := cmdSkillKeygen(nil); err != nil {
		t.Fatalf("keygen: %v", err)
	}
	parent := t.TempDir()
	srcDir := filepath.Join(parent, "weather-tool")
	if err := os.MkdirAll(srcDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(srcDir, "SKILL.md"),
		[]byte("---\nname: x\ncategory: y\ndescription: z\n---\nbody"),
		0o644); err != nil {
		t.Fatalf("write SKILL.md: %v", err)
	}
	if err := cmdSkillBuild([]string{"--version", "1.0.0", srcDir}); err != nil {
		t.Fatalf("build: %v", err)
	}
	mb, _ := os.ReadFile(filepath.Join(srcDir, marketplace.ManifestFilename))
	var m marketplace.Manifest
	_ = json.Unmarshal(mb, &m)
	if m.Name != "weather-tool" {
		t.Errorf("inferred name = %q, want weather-tool", m.Name)
	}
}

func TestSkillBuild_CorruptSeedFile(t *testing.T) {
	t.Setenv("PAN_AGENT_HOME", t.TempDir())
	// Write a corrupt seed.
	path := paths.MarketplacePublisherSeedFile("")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte("not-hex-data"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	srcDir := t.TempDir()
	_ = os.WriteFile(filepath.Join(srcDir, "SKILL.md"), []byte("# x"), 0o644)

	err := cmdSkillBuild([]string{"--version", "1.0.0", "--name", "x", srcDir})
	if err == nil {
		t.Error("expected corrupt-seed error")
	}
}

func TestFilepathBase(t *testing.T) {
	cases := map[string]string{
		"/a/b/c":         "c",
		"a/b/c":          "c",
		"c":              "c",
		"":               "",
		`C:\foo\bar`:     "bar",
		"/path/with/sl/": "",
	}
	for in, want := range cases {
		if got := filepathBase(in); got != want {
			t.Errorf("filepathBase(%q) = %q, want %q", in, got, want)
		}
	}
}
