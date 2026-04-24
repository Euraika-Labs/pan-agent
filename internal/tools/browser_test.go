package tools

import (
	"os"
	"path/filepath"
	"testing"
)

func TestBrowserProfileDir_Default(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("PAN_AGENT_HOME", tmp)

	browserProfileOverride = ""
	defer func() { browserProfileOverride = "" }()

	dir := browserProfileDir()
	want := filepath.Join(tmp, "browser-profile")
	if dir != want {
		t.Errorf("browserProfileDir() = %q, want %q", dir, want)
	}
	info, err := os.Stat(dir)
	if err != nil {
		t.Fatalf("browser-profile dir not created: %v", err)
	}
	if !info.IsDir() {
		t.Fatalf("browser-profile is not a directory")
	}
}

func TestBrowserProfileDir_Override(t *testing.T) {
	tmp := t.TempDir()
	override := filepath.Join(tmp, "custom-profile")
	if err := os.MkdirAll(override, 0o700); err != nil {
		t.Fatal(err)
	}

	browserProfileOverride = override
	defer func() { browserProfileOverride = "" }()

	dir := browserProfileDir()
	if dir != override {
		t.Errorf("browserProfileDir() = %q, want %q", dir, override)
	}
}

func TestCloseBrowser_NeverLaunched(t *testing.T) {
	browserMu.Lock()
	saved := browserLaunched
	browserLaunched = false
	browserMu.Unlock()

	defer func() {
		browserMu.Lock()
		browserLaunched = saved
		browserMu.Unlock()
	}()

	// Must not panic when browser was never launched.
	CloseBrowser()
}

func TestCheckNavigableURL(t *testing.T) {
	tests := []struct {
		url     string
		wantErr bool
	}{
		{"https://example.com", false},
		{"http://example.com/path", false},
		{"file:///etc/passwd", true},
		{"ftp://example.com", true},
		{"javascript:alert(1)", true},
		{"http://localhost:8642", true},
		{"http://127.0.0.1:8642", true},
		{"http://192.168.1.1", true},
		{"http://10.0.0.1", true},
		{"", true},
		{"not-a-url", true},
	}

	t.Setenv("PAN_AGENT_BROWSER_ALLOW_LOCAL", "false")

	for _, tt := range tests {
		err := checkNavigableURL(tt.url)
		if (err != nil) != tt.wantErr {
			t.Errorf("checkNavigableURL(%q) err=%v, wantErr=%v", tt.url, err, tt.wantErr)
		}
	}
}

func TestCheckNavigableURL_AllowLocal(t *testing.T) {
	t.Setenv("PAN_AGENT_BROWSER_ALLOW_LOCAL", "true")

	if err := checkNavigableURL("http://localhost:8642"); err != nil {
		t.Errorf("with ALLOW_LOCAL=true, localhost should be allowed: %v", err)
	}
}
