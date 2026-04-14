package skills

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// withIsolatedAgentHome points the platform-specific data dir at t.TempDir()
// so paths.AgentHome resolves there. It must be called once per test BEFORE
// the first call to paths.AgentHome — paths uses sync.Once. For that reason
// callers should funnel through this helper in TestMain.
func withIsolatedAgentHome(t *testing.T) string {
	t.Helper()
	tmp := t.TempDir()
	switch runtime.GOOS {
	case "windows":
		t.Setenv("LOCALAPPDATA", tmp)
	case "linux":
		t.Setenv("XDG_DATA_HOME", tmp)
	default:
		t.Setenv("HOME", tmp)
	}
	return tmp
}

// TestResolveActiveDirRejectsTraversal locks down the new sanitiser:
// path-traversal characters in category or name must be refused.
func TestResolveActiveDirRejectsTraversal(t *testing.T) {
	withIsolatedAgentHome(t)
	bad := []struct {
		cat, name string
	}{
		{"..", "x"},
		{"x", ".."},
		{"../etc", "passwd"},
		{"foo/bar", "x"}, // slash inside category
		{"foo", "bar/baz"},
		{"FOO", "x"}, // uppercase rejected by ValidateCategory
		{"", "x"},
		{"x", ""},
	}
	for _, c := range bad {
		if _, err := resolveActiveDir("default", c.cat, c.name); err == nil {
			t.Errorf("resolveActiveDir(%q,%q): want error, got nil", c.cat, c.name)
		}
	}
}

// TestResolveActiveDirContainment confirms that a *valid* (cat, name) resolves
// to a path strictly inside ProfileSkillsDir.
func TestResolveActiveDirContainment(t *testing.T) {
	withIsolatedAgentHome(t)
	got, err := resolveActiveDir("default", "coding", "fizz")
	if err != nil {
		t.Fatalf("resolveActiveDir: %v", err)
	}
	if !strings.HasSuffix(filepath.ToSlash(got), "skills/coding/fizz") {
		t.Errorf("resolved path = %q, want suffix skills/coding/fizz", got)
	}
}

// TestResolveProposalDirRejectsBadID checks the proposal-id sanitiser.
func TestResolveProposalDirRejectsBadID(t *testing.T) {
	withIsolatedAgentHome(t)
	bad := []string{"", "..", "../x", "a/b", "a\\b", "with space and:", strings.Repeat("a", 200)}
	for _, id := range bad {
		if _, err := resolveProposalDir("default", id); err == nil {
			t.Errorf("resolveProposalDir(%q): want error, got nil", id)
		}
	}
}

// TestResolveProposalDirAcceptsUUID confirms a real uuid-shaped id works.
func TestResolveProposalDirAcceptsUUID(t *testing.T) {
	withIsolatedAgentHome(t)
	got, err := resolveProposalDir("default", "11111111-2222-3333-4444-555555555555")
	if err != nil {
		t.Fatalf("resolveProposalDir: %v", err)
	}
	if !strings.Contains(filepath.ToSlash(got), "/_proposed/") {
		t.Errorf("resolved path = %q, want to contain /_proposed/", got)
	}
}

// TestSplitAndResolveActiveID rejects malformed ids before they reach disk.
func TestSplitAndResolveActiveID(t *testing.T) {
	withIsolatedAgentHome(t)
	bad := []string{"", "no-slash", "../escape/x", "a/", "/x", "a/b/c"} // a/b/c parses as cat=a, name=b/c → bad
	for _, id := range bad {
		if _, _, _, err := splitAndResolveActiveID("default", id); err == nil {
			// "a/b/c" is special: SplitN gives ("a", "b/c") which fails ValidateName.
			// Either way we expect an error.
			t.Errorf("splitAndResolveActiveID(%q): want error, got nil", id)
		}
	}
	// Smoke a happy case.
	cat, name, dir, err := splitAndResolveActiveID("default", "coding/fizz")
	if err != nil {
		t.Fatalf("splitAndResolveActiveID: %v", err)
	}
	if cat != "coding" || name != "fizz" {
		t.Errorf("got cat=%q name=%q, want coding/fizz", cat, name)
	}
	if !filepath.IsAbs(dir) {
		t.Errorf("returned dir is not absolute: %q", dir)
	}
}

// Sanity: the helper should never let a containment-failure pass even with
// an explicit absolute path attempt.
func TestResolveActiveDirAbsoluteCategoryRejected(t *testing.T) {
	withIsolatedAgentHome(t)
	// Absolute paths fail ValidateCategory because of the leading slash on
	// non-Windows; on Windows the colon is in the regex deny list.
	abs := "/etc"
	if runtime.GOOS == "windows" {
		abs = "C:\\windows"
	}
	if _, err := resolveActiveDir("default", abs, "x"); err == nil {
		t.Errorf("resolveActiveDir(%q): want error, got nil", abs)
	}
}

// TestIsolatedAgentHomeUsesTempDir confirms that the resolved skill paths
// live somewhere under the OS temp dir — i.e. that the env-var override
// caught hold before paths.AgentHome cached. Because AgentHome uses
// sync.Once, only the *first* test in this binary actually picks up the
// env var; subsequent tests inherit the cache. That's still enough to
// keep the user's real LOCALAPPDATA untouched.
func TestIsolatedAgentHomeUsesTempDir(t *testing.T) {
	withIsolatedAgentHome(t)
	dir, err := resolveActiveDir("default", "x", "y")
	if err != nil {
		t.Fatalf("resolveActiveDir: %v", err)
	}
	tempRoot := filepath.ToSlash(os.TempDir())
	got := filepath.ToSlash(dir)
	if !strings.HasPrefix(got, tempRoot) && !strings.Contains(got, "/Temp/") {
		t.Errorf("resolved skill path %q is NOT inside tempdir %q — "+
			"sync.Once may have cached the real LOCALAPPDATA. Risk of "+
			"polluting user data; investigate before shipping.", got, tempRoot)
	}
}
