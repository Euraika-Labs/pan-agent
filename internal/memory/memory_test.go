package memory

import (
	"os"
	"strings"
	"testing"

	"github.com/euraika-labs/pan-agent/internal/paths"
)

// isolateAgentHome points paths.AgentHome() at t.TempDir() via the
// PAN_AGENT_HOME env override. Every test that touches MemoryFile /
// UserFile / ProfileHome must call this first — otherwise writes land
// in the real %LOCALAPPDATA%\pan-agent\profiles\ and leak across runs
// (we discovered 12 test_* directories sitting in a dev install after
// the previous setup strategy only cleaned up the MEMORY.md file but
// not the profile dir itself). Uses t.Setenv so restoration is
// automatic at the end of the test.
func isolateAgentHome(t *testing.T) {
	t.Helper()
	t.Setenv("PAN_AGENT_HOME", t.TempDir())
}

// writeMemoryFile writes raw content directly to the MEMORY.md path for a profile.
func writeMemoryFile(t *testing.T, profile, content string) {
	t.Helper()
	p := paths.MemoryFile(profile)
	if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
		t.Fatalf("writeMemoryFile: %v", err)
	}
}

// readRawMemory reads the raw MEMORY.md bytes.
func readRawMemory(t *testing.T, profile string) string {
	t.Helper()
	data, err := os.ReadFile(paths.MemoryFile(profile))
	if os.IsNotExist(err) {
		return ""
	}
	if err != nil {
		t.Fatalf("readRawMemory: %v", err)
	}
	return string(data)
}

// uniqueProfile isolates AgentHome to a per-test temp dir and returns a
// stable profile name. Every caller of AddEntry/UpdateEntry/ReadMemory
// goes through this path, so wiring isolation in here ensures no memory
// test can leak writes into the real %LOCALAPPDATA%\pan-agent directory.
func uniqueProfile(t *testing.T) string {
	t.Helper()
	isolateAgentHome(t)
	// Replace slashes in the test name that would create subdirectories.
	safe := strings.NewReplacer("/", "_", " ", "_").Replace(t.Name())
	return "test_" + safe
}

// cleanProfile removes the MEMORY.md file so tests start fresh.
func cleanProfile(t *testing.T, profile string) {
	t.Helper()
	_ = os.Remove(paths.MemoryFile(profile))
	_ = os.Remove(paths.UserFile(profile))
}

// ---------------------------------------------------------------------------
// AddEntry
// ---------------------------------------------------------------------------

func TestAddEntryCreatesEntry(t *testing.T) {
	profile := uniqueProfile(t)
	cleanProfile(t, profile)
	t.Cleanup(func() { cleanProfile(t, profile) })

	if err := AddEntry("first entry", profile); err != nil {
		t.Fatalf("AddEntry: %v", err)
	}

	raw := readRawMemory(t, profile)
	if !strings.Contains(raw, "first entry") {
		t.Errorf("MEMORY.md does not contain 'first entry': %q", raw)
	}
}

func TestAddEntryUsesDelimiter(t *testing.T) {
	profile := uniqueProfile(t)
	cleanProfile(t, profile)
	t.Cleanup(func() { cleanProfile(t, profile) })

	if err := AddEntry("alpha", profile); err != nil {
		t.Fatalf("AddEntry alpha: %v", err)
	}
	if err := AddEntry("beta", profile); err != nil {
		t.Fatalf("AddEntry beta: %v", err)
	}

	raw := readRawMemory(t, profile)
	if !strings.Contains(raw, entryDelimiter) {
		t.Errorf("expected delimiter %q in %q", entryDelimiter, raw)
	}
}

func TestAddEntryMultiple(t *testing.T) {
	profile := uniqueProfile(t)
	cleanProfile(t, profile)
	t.Cleanup(func() { cleanProfile(t, profile) })

	entries := []string{"one", "two", "three"}
	for _, e := range entries {
		if err := AddEntry(e, profile); err != nil {
			t.Fatalf("AddEntry(%q): %v", e, err)
		}
	}

	state, err := ReadMemory(profile)
	if err != nil {
		t.Fatalf("ReadMemory: %v", err)
	}
	if len(state.Entries) != 3 {
		t.Errorf("want 3 entries, got %d: %v", len(state.Entries), state.Entries)
	}
}

func TestAddEntryCharLimitEnforced(t *testing.T) {
	profile := uniqueProfile(t)
	cleanProfile(t, profile)
	t.Cleanup(func() { cleanProfile(t, profile) })

	// Fill memory to just below the limit with a large entry.
	big := strings.Repeat("x", memoryCharLimit-5)
	if err := AddEntry(big, profile); err != nil {
		t.Fatalf("AddEntry big: %v", err)
	}

	// Adding any non-empty entry now should fail.
	err := AddEntry("overflow", profile)
	if err == nil {
		t.Error("expected error when exceeding char limit, got nil")
	}
}

// ---------------------------------------------------------------------------
// UpdateEntry
// ---------------------------------------------------------------------------

func TestUpdateEntry(t *testing.T) {
	profile := uniqueProfile(t)
	cleanProfile(t, profile)
	t.Cleanup(func() { cleanProfile(t, profile) })

	_ = AddEntry("original", profile)

	if err := UpdateEntry(0, "updated", profile); err != nil {
		t.Fatalf("UpdateEntry: %v", err)
	}

	state, _ := ReadMemory(profile)
	if len(state.Entries) == 0 || state.Entries[0] != "updated" {
		t.Errorf("UpdateEntry: want 'updated', got %v", state.Entries)
	}
}

func TestUpdateEntryOutOfRange(t *testing.T) {
	profile := uniqueProfile(t)
	cleanProfile(t, profile)
	t.Cleanup(func() { cleanProfile(t, profile) })

	_ = AddEntry("only entry", profile)

	err := UpdateEntry(5, "x", profile)
	if err == nil {
		t.Error("expected error for out-of-range index, got nil")
	}
}

// ---------------------------------------------------------------------------
// RemoveEntry
// ---------------------------------------------------------------------------

func TestRemoveEntry(t *testing.T) {
	profile := uniqueProfile(t)
	cleanProfile(t, profile)
	t.Cleanup(func() { cleanProfile(t, profile) })

	_ = AddEntry("keep", profile)
	_ = AddEntry("remove me", profile)

	if err := RemoveEntry(1, profile); err != nil {
		t.Fatalf("RemoveEntry: %v", err)
	}

	state, _ := ReadMemory(profile)
	if len(state.Entries) != 1 {
		t.Errorf("want 1 entry after remove, got %d: %v", len(state.Entries), state.Entries)
	}
	if state.Entries[0] != "keep" {
		t.Errorf("remaining entry = %q, want %q", state.Entries[0], "keep")
	}
}

func TestRemoveEntryOutOfRange(t *testing.T) {
	profile := uniqueProfile(t)
	cleanProfile(t, profile)
	t.Cleanup(func() { cleanProfile(t, profile) })

	_ = AddEntry("only", profile)

	err := RemoveEntry(99, profile)
	if err == nil {
		t.Error("expected error for out-of-range remove index, got nil")
	}
}

// ---------------------------------------------------------------------------
// ReadMemory
// ---------------------------------------------------------------------------

func TestReadMemoryEmpty(t *testing.T) {
	profile := uniqueProfile(t)
	cleanProfile(t, profile)
	t.Cleanup(func() { cleanProfile(t, profile) })

	state, err := ReadMemory(profile)
	if err != nil {
		t.Fatalf("ReadMemory on fresh profile: %v", err)
	}
	if len(state.Entries) != 0 {
		t.Errorf("want 0 entries for empty profile, got %d", len(state.Entries))
	}
	if state.CharLimit != memoryCharLimit {
		t.Errorf("CharLimit = %d, want %d", state.CharLimit, memoryCharLimit)
	}
	if state.UserCharLimit != userCharLimit {
		t.Errorf("UserCharLimit = %d, want %d", state.UserCharLimit, userCharLimit)
	}
}

func TestReadMemoryWithFile(t *testing.T) {
	profile := uniqueProfile(t)
	cleanProfile(t, profile)
	t.Cleanup(func() { cleanProfile(t, profile) })

	raw := "hello" + entryDelimiter + "world"
	writeMemoryFile(t, profile, raw)

	state, err := ReadMemory(profile)
	if err != nil {
		t.Fatalf("ReadMemory: %v", err)
	}
	if len(state.Entries) != 2 {
		t.Errorf("want 2 entries, got %d: %v", len(state.Entries), state.Entries)
	}
	if state.Entries[0] != "hello" {
		t.Errorf("Entries[0] = %q, want %q", state.Entries[0], "hello")
	}
	if state.Entries[1] != "world" {
		t.Errorf("Entries[1] = %q, want %q", state.Entries[1], "world")
	}
}

func TestWriteUserProfile(t *testing.T) {
	profile := uniqueProfile(t)
	cleanProfile(t, profile)
	t.Cleanup(func() { cleanProfile(t, profile) })

	if err := WriteUserProfile("I am a test user", profile); err != nil {
		t.Fatalf("WriteUserProfile: %v", err)
	}

	state, err := ReadMemory(profile)
	if err != nil {
		t.Fatalf("ReadMemory: %v", err)
	}
	if state.UserProfile != "I am a test user" {
		t.Errorf("UserProfile = %q, want %q", state.UserProfile, "I am a test user")
	}
}

func TestWriteUserProfileCharLimitEnforced(t *testing.T) {
	profile := uniqueProfile(t)
	cleanProfile(t, profile)
	t.Cleanup(func() { cleanProfile(t, profile) })

	oversized := strings.Repeat("y", userCharLimit+1)
	err := WriteUserProfile(oversized, profile)
	if err == nil {
		t.Error("expected error for oversized user profile, got nil")
	}
}

// ---------------------------------------------------------------------------
// parseEntries / serialize round-trip (internal helpers)
// ---------------------------------------------------------------------------

func TestParseEntriesEmpty(t *testing.T) {
	entries := parseEntries("")
	if len(entries) != 0 {
		t.Errorf("parseEntries(\"\") = %v, want []", entries)
	}
}

func TestSerializeRoundTrip(t *testing.T) {
	in := []string{"alpha", "beta", "gamma"}
	serialized := serialize(in)
	out := parseEntries(serialized)
	if len(out) != len(in) {
		t.Fatalf("round-trip length mismatch: %d != %d", len(out), len(in))
	}
	for i := range in {
		if out[i] != in[i] {
			t.Errorf("round-trip[%d]: got %q, want %q", i, out[i], in[i])
		}
	}
}
