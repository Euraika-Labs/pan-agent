package config

import (
	"os"
	"path/filepath"
	"testing"
)

func writeTemp(t *testing.T, dir, name, content string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
		t.Fatalf("writeTemp: %v", err)
	}
	return p
}

// TestReadEnvBasic verifies that key=value pairs are parsed correctly.
func TestReadEnvBasic(t *testing.T) {
	dir := t.TempDir()
	p := writeTemp(t, dir, ".env", "FOO=bar\nBAZ=qux\n")
	// Invalidate any cache that might exist.
	invalidatePrefix("env:" + p)

	got, err := ReadEnv(p)
	if err != nil {
		t.Fatalf("ReadEnv: %v", err)
	}
	if got["FOO"] != "bar" {
		t.Errorf("FOO = %q, want %q", got["FOO"], "bar")
	}
	if got["BAZ"] != "qux" {
		t.Errorf("BAZ = %q, want %q", got["BAZ"], "qux")
	}
}

// TestReadEnvCommentSkipped verifies that comment lines are ignored.
func TestReadEnvCommentSkipped(t *testing.T) {
	dir := t.TempDir()
	p := writeTemp(t, dir, ".env", "# this is a comment\nKEY=value\n")
	invalidatePrefix("env:" + p)

	got, err := ReadEnv(p)
	if err != nil {
		t.Fatalf("ReadEnv: %v", err)
	}
	if _, exists := got["# this is a comment"]; exists {
		t.Error("comment line should not appear as a key")
	}
	if got["KEY"] != "value" {
		t.Errorf("KEY = %q, want %q", got["KEY"], "value")
	}
}

// TestReadEnvDoubleQuotes verifies that double-quoted values are unquoted.
func TestReadEnvDoubleQuotes(t *testing.T) {
	dir := t.TempDir()
	p := writeTemp(t, dir, ".env", `QUOTED="hello world"`+"\n")
	invalidatePrefix("env:" + p)

	got, err := ReadEnv(p)
	if err != nil {
		t.Fatalf("ReadEnv: %v", err)
	}
	if got["QUOTED"] != "hello world" {
		t.Errorf("QUOTED = %q, want %q", got["QUOTED"], "hello world")
	}
}

// TestReadEnvSingleQuotes verifies that single-quoted values are unquoted.
func TestReadEnvSingleQuotes(t *testing.T) {
	dir := t.TempDir()
	p := writeTemp(t, dir, ".env", "QUOTED='single quoted'\n")
	invalidatePrefix("env:" + p)

	got, err := ReadEnv(p)
	if err != nil {
		t.Fatalf("ReadEnv: %v", err)
	}
	if got["QUOTED"] != "single quoted" {
		t.Errorf("QUOTED = %q, want %q", got["QUOTED"], "single quoted")
	}
}

// TestReadEnvMissingFile verifies that a missing file returns an empty map (no error).
func TestReadEnvMissingFile(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "nonexistent.env")
	invalidatePrefix("env:" + p)

	got, err := ReadEnv(p)
	if err != nil {
		t.Fatalf("ReadEnv on missing file: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("expected empty map for missing file, got %v", got)
	}
}

// TestReadEnvEmptyValueOmitted verifies that keys with empty values are omitted.
func TestReadEnvEmptyValueOmitted(t *testing.T) {
	dir := t.TempDir()
	p := writeTemp(t, dir, ".env", "EMPTY=\nPRESENT=yes\n")
	invalidatePrefix("env:" + p)

	got, err := ReadEnv(p)
	if err != nil {
		t.Fatalf("ReadEnv: %v", err)
	}
	if _, exists := got["EMPTY"]; exists {
		t.Error("empty-value key should be omitted from result")
	}
	if got["PRESENT"] != "yes" {
		t.Errorf("PRESENT = %q, want %q", got["PRESENT"], "yes")
	}
}

// TestSetEnvValueCreatesFile verifies that SetEnvValue creates the file when it doesn't exist.
func TestSetEnvValueCreatesFile(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, ".env")

	if err := SetEnvValue(p, "NEWKEY", "newval"); err != nil {
		t.Fatalf("SetEnvValue: %v", err)
	}
	invalidatePrefix("env:" + p)

	got, err := ReadEnv(p)
	if err != nil {
		t.Fatalf("ReadEnv after SetEnvValue: %v", err)
	}
	if got["NEWKEY"] != "newval" {
		t.Errorf("NEWKEY = %q, want %q", got["NEWKEY"], "newval")
	}
}

// TestSetEnvValueUpdatesExisting verifies that SetEnvValue replaces an existing entry.
func TestSetEnvValueUpdatesExisting(t *testing.T) {
	dir := t.TempDir()
	p := writeTemp(t, dir, ".env", "KEY=oldval\n")
	invalidatePrefix("env:" + p)

	if err := SetEnvValue(p, "KEY", "newval"); err != nil {
		t.Fatalf("SetEnvValue: %v", err)
	}
	invalidatePrefix("env:" + p)

	got, err := ReadEnv(p)
	if err != nil {
		t.Fatalf("ReadEnv: %v", err)
	}
	if got["KEY"] != "newval" {
		t.Errorf("KEY = %q, want %q", got["KEY"], "newval")
	}
}

// TestSetEnvValueAppends verifies that SetEnvValue appends a new key.
func TestSetEnvValueAppends(t *testing.T) {
	dir := t.TempDir()
	p := writeTemp(t, dir, ".env", "EXISTING=yes\n")
	invalidatePrefix("env:" + p)

	if err := SetEnvValue(p, "BRAND_NEW", "added"); err != nil {
		t.Fatalf("SetEnvValue: %v", err)
	}
	invalidatePrefix("env:" + p)

	got, err := ReadEnv(p)
	if err != nil {
		t.Fatalf("ReadEnv: %v", err)
	}
	if got["EXISTING"] != "yes" {
		t.Errorf("EXISTING = %q, want %q", got["EXISTING"], "yes")
	}
	if got["BRAND_NEW"] != "added" {
		t.Errorf("BRAND_NEW = %q, want %q", got["BRAND_NEW"], "added")
	}
}

// TestSetEnvValueUncommentCommented verifies that SetEnvValue replaces commented-out entries.
func TestSetEnvValueUncommentCommented(t *testing.T) {
	dir := t.TempDir()
	p := writeTemp(t, dir, ".env", "# COMMENTED=old\n")
	invalidatePrefix("env:" + p)

	if err := SetEnvValue(p, "COMMENTED", "active"); err != nil {
		t.Fatalf("SetEnvValue: %v", err)
	}
	invalidatePrefix("env:" + p)

	got, err := ReadEnv(p)
	if err != nil {
		t.Fatalf("ReadEnv: %v", err)
	}
	if got["COMMENTED"] != "active" {
		t.Errorf("COMMENTED = %q, want %q", got["COMMENTED"], "active")
	}
}
