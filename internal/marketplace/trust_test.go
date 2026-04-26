package marketplace

import (
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeTrustFile(t *testing.T, body string) string {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, "trusted.json")
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	return p
}

func TestLoadTrustSet_HappyPath(t *testing.T) {
	t.Parallel()
	kp, _ := GenerateKeypair()
	body := `{
  "version": 1,
  "publishers": [
    {
      "fingerprint": "` + kp.Fingerprint() + `",
      "public_key":  "` + kp.PublicKeyHex() + `",
      "name":        "Test Publisher",
      "added_at":    1700000000
    }
  ]
}`
	p := writeTrustFile(t, body)
	ts, pubs, err := LoadTrustSet(p)
	if err != nil {
		t.Fatalf("LoadTrustSet: %v", err)
	}
	if len(ts.Publishers) != 1 {
		t.Errorf("Publishers len = %d, want 1", len(ts.Publishers))
	}
	if len(pubs) != 1 {
		t.Errorf("pubs len = %d, want 1", len(pubs))
	}
	if string(pubs[0]) != string(kp.Public) {
		t.Errorf("pub bytes mismatch")
	}
}

func TestLoadTrustSet_FileNotFound(t *testing.T) {
	t.Parallel()
	ts, pubs, err := LoadTrustSet("/nonexistent/path.json")
	if err != nil {
		t.Fatalf("missing file should not error, got %v", err)
	}
	if ts == nil {
		t.Fatal("ts nil for missing file")
	}
	if len(pubs) != 0 {
		t.Errorf("pubs len = %d, want 0 on missing file", len(pubs))
	}
}

func TestLoadTrustSet_EmptyPublishers(t *testing.T) {
	t.Parallel()
	p := writeTrustFile(t, `{"version": 1, "publishers": []}`)
	_, pubs, err := LoadTrustSet(p)
	if err != nil {
		t.Fatalf("LoadTrustSet: %v", err)
	}
	if len(pubs) != 0 {
		t.Errorf("pubs len = %d, want 0", len(pubs))
	}
}

func TestLoadTrustSet_ImpliedVersion(t *testing.T) {
	t.Parallel()
	kp, _ := GenerateKeypair()
	// Hand-edited file with no "version" field — accepted as current.
	body := `{"publishers": [{"public_key": "` + kp.PublicKeyHex() + `"}]}`
	p := writeTrustFile(t, body)
	_, pubs, err := LoadTrustSet(p)
	if err != nil {
		t.Fatalf("LoadTrustSet: %v", err)
	}
	if len(pubs) != 1 {
		t.Errorf("pubs len = %d, want 1", len(pubs))
	}
}

func TestLoadTrustSet_WrongVersion(t *testing.T) {
	t.Parallel()
	body := `{"version": 999, "publishers": []}`
	p := writeTrustFile(t, body)
	_, _, err := LoadTrustSet(p)
	if !errors.Is(err, ErrTrustSetInvalid) {
		t.Errorf("err = %v, want ErrTrustSetInvalid", err)
	}
}

func TestLoadTrustSet_MalformedJSON(t *testing.T) {
	t.Parallel()
	p := writeTrustFile(t, "not json {")
	_, _, err := LoadTrustSet(p)
	if !errors.Is(err, ErrTrustSetInvalid) {
		t.Errorf("err = %v, want ErrTrustSetInvalid", err)
	}
}

func TestLoadTrustSet_BadPublicKey(t *testing.T) {
	t.Parallel()
	body := `{"version": 1, "publishers": [{"public_key": "zzznothex"}]}`
	p := writeTrustFile(t, body)
	_, _, err := LoadTrustSet(p)
	if !errors.Is(err, ErrTrustSetInvalid) {
		t.Errorf("err = %v, want ErrTrustSetInvalid", err)
	}
}

func TestLoadTrustSet_FingerprintMismatch(t *testing.T) {
	t.Parallel()
	kp, _ := GenerateKeypair()
	body := `{"version": 1, "publishers": [{
		"fingerprint": "0000000000000000",
		"public_key": "` + kp.PublicKeyHex() + `"
	}]}`
	p := writeTrustFile(t, body)
	_, _, err := LoadTrustSet(p)
	if !errors.Is(err, ErrTrustSetInvalid) {
		t.Errorf("err = %v, want ErrTrustSetInvalid (fingerprint mismatch)", err)
	}
}

func TestLoadTrustSet_DuplicateKeys(t *testing.T) {
	t.Parallel()
	kp, _ := GenerateKeypair()
	hex := kp.PublicKeyHex()
	body := `{"version": 1, "publishers": [
		{"public_key": "` + hex + `"},
		{"public_key": "` + hex + `"}
	]}`
	p := writeTrustFile(t, body)
	_, _, err := LoadTrustSet(p)
	if !errors.Is(err, ErrTrustSetInvalid) {
		t.Errorf("err = %v, want ErrTrustSetInvalid (duplicate)", err)
	}
}

func TestLoadTrustSet_EmptyPublicKey(t *testing.T) {
	t.Parallel()
	body := `{"version": 1, "publishers": [{"public_key": ""}]}`
	p := writeTrustFile(t, body)
	_, _, err := LoadTrustSet(p)
	if !errors.Is(err, ErrTrustSetInvalid) {
		t.Errorf("err = %v, want ErrTrustSetInvalid", err)
	}
}

func TestSaveAndReload_RoundTrip(t *testing.T) {
	t.Parallel()
	kp1, _ := GenerateKeypair()
	kp2, _ := GenerateKeypair()
	dir := t.TempDir()
	path := filepath.Join(dir, "trust.json")

	ts := &TrustSet{
		Version: TrustSetVersion,
		Publishers: []TrustEntry{
			{PublicKey: kp1.PublicKeyHex(), Name: "A"},
			{PublicKey: kp2.PublicKeyHex(), Name: "B", AddedAt: 1700000000},
		},
	}
	if err := SaveTrustSet(path, ts); err != nil {
		t.Fatalf("SaveTrustSet: %v", err)
	}

	// Reloaded set must match (and have fingerprints filled in).
	got, pubs, err := LoadTrustSet(path)
	if err != nil {
		t.Fatalf("LoadTrustSet: %v", err)
	}
	if len(got.Publishers) != 2 {
		t.Fatalf("publisher count = %d, want 2", len(got.Publishers))
	}
	for _, p := range got.Publishers {
		if p.Fingerprint == "" {
			t.Errorf("Save did not fill fingerprint: %+v", p)
		}
	}
	if len(pubs) != 2 {
		t.Errorf("pubs len = %d, want 2", len(pubs))
	}
}

func TestSaveTrustSet_NilErrors(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if err := SaveTrustSet(filepath.Join(dir, "x.json"), nil); err == nil {
		t.Error("nil ts: expected error")
	}
}

func TestSaveTrustSet_StableFormatting(t *testing.T) {
	t.Parallel()
	kp, _ := GenerateKeypair()
	dir := t.TempDir()
	path := filepath.Join(dir, "trust.json")
	ts := &TrustSet{
		Version: TrustSetVersion,
		Publishers: []TrustEntry{
			{PublicKey: kp.PublicKeyHex(), Name: "X"},
		},
	}
	if err := SaveTrustSet(path, ts); err != nil {
		t.Fatalf("Save: %v", err)
	}
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	// Re-parse to JSON map, expect well-formed.
	var dec map[string]any
	if err := json.Unmarshal(body, &dec); err != nil {
		t.Errorf("re-parse: %v\nbody: %s", err, body)
	}
	if !strings.Contains(string(body), "  ") {
		t.Errorf("expected indented output: %s", body)
	}
}

func TestPinPublisher_NewEntry(t *testing.T) {
	t.Parallel()
	kp, _ := GenerateKeypair()
	ts := &TrustSet{Version: TrustSetVersion}
	entry, added := PinPublisher(ts, kp.Public, "Alice", 1700000000)
	if !added {
		t.Error("expected added=true on fresh entry")
	}
	if entry == nil || entry.Name != "Alice" {
		t.Errorf("entry malformed: %+v", entry)
	}
	if entry.Fingerprint != kp.Fingerprint() {
		t.Errorf("fingerprint mismatch: %q vs %q", entry.Fingerprint, kp.Fingerprint())
	}
	if len(ts.Publishers) != 1 {
		t.Errorf("ts.Publishers len = %d, want 1", len(ts.Publishers))
	}
}

func TestPinPublisher_Idempotent(t *testing.T) {
	t.Parallel()
	kp, _ := GenerateKeypair()
	ts := &TrustSet{Version: TrustSetVersion}
	_, _ = PinPublisher(ts, kp.Public, "Alice", 100)
	entry, added := PinPublisher(ts, kp.Public, "Alice (renamed)", 200)
	if added {
		t.Error("second pin: added should be false")
	}
	if entry == nil || entry.Name != "Alice" {
		t.Errorf("expected original entry, got %+v", entry)
	}
	if len(ts.Publishers) != 1 {
		t.Errorf("publisher count = %d, want 1 (idempotent)", len(ts.Publishers))
	}
}

func TestPinPublisher_NilSafe(t *testing.T) {
	t.Parallel()
	if e, added := PinPublisher(nil, ed25519.PublicKey(make([]byte, PublicKeySize)), "x", 0); e != nil || added {
		t.Error("nil ts: should return (nil, false)")
	}
	ts := &TrustSet{}
	if e, added := PinPublisher(ts, []byte{1, 2, 3}, "x", 0); e != nil || added {
		t.Error("short key: should return (nil, false)")
	}
}

// stripFile is package-private; quick sanity check that it returns
// the dir without error on common cases.
func TestStripFile(t *testing.T) {
	t.Parallel()
	cases := map[string]string{
		"/a/b/c.txt":      "/a/b",
		"a/b/c":           "a/b",
		"file.txt":        ".",
		"":                ".",
		"/a":              "/",
		`C:\path\to\file`: `C:\path\to`,
	}
	for in, want := range cases {
		if got := stripFile(in); got != want {
			t.Errorf("stripFile(%q) = %q, want %q", in, got, want)
		}
	}
}
