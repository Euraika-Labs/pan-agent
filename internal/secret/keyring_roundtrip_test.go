//go:build keyring_live

package secret

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"runtime"
	"testing"
)

// TestRoundtripDarwin exercises Set → Get → Delete on the macOS login keychain.
// Run with: go test ./internal/secret/... -tags=keyring_live
// Requires: macOS with an unlocked login keychain (true for all interactive sessions).
func TestRoundtripDarwin(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skipf("keyring_live darwin roundtrip: skipping on %s", runtime.GOOS)
	}

	key, value := roundtripKey(), "darwin-roundtrip-value"
	runRoundtrip(t, key, value)
}

// TestRoundtripWindows exercises Set → Get → Delete via Windows Credential Manager (wincred).
// Run with: go test ./internal/secret/... -tags=keyring_live
// Requires: Windows with user credentials available (always true for interactive logon).
func TestRoundtripWindows(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skipf("keyring_live windows roundtrip: skipping on %s", runtime.GOOS)
	}

	key, value := roundtripKey(), "windows-roundtrip-value"
	runRoundtrip(t, key, value)
}

// TestRoundtripLinux exercises Set → Get → Delete via Secret Service / GNOME Keyring.
// Run with: go test ./internal/secret/... -tags=keyring_live
// Requires: a running Secret Service daemon. In CI use:
//
//	dbus-launch gnome-keyring-daemon --start --components=secrets
func TestRoundtripLinux(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skipf("keyring_live linux roundtrip: skipping on %s", runtime.GOOS)
	}

	key, value := roundtripKey(), "linux-roundtrip-value"
	runRoundtrip(t, key, value)
}

// ---------------------------------------------------------------------------
// shared roundtrip logic
// ---------------------------------------------------------------------------

// runRoundtrip performs the canonical Set → Get → Delete → Get(expect ErrNotFound) sequence.
func runRoundtrip(t *testing.T, key, value string) {
	t.Helper()

	// Ensure cleanup even if the test panics midway.
	t.Cleanup(func() {
		// Best-effort: ignore errors on cleanup delete (key may already be gone).
		_ = Delete(key)
	})

	// 1. Set
	if err := Set(key, value); err != nil {
		t.Fatalf("Set(%q, %q): %v", key, value, err)
	}

	// 2. Get — must return the exact value stored.
	got, err := Get(key)
	if err != nil {
		t.Fatalf("Get(%q) after Set: %v", key, err)
	}
	if got != value {
		t.Errorf("Get(%q) = %q, want %q", key, got, value)
	}

	// 3. Delete.
	if err := Delete(key); err != nil {
		t.Fatalf("Delete(%q): %v", key, err)
	}

	// 4. Get after Delete — must return ErrNotFound.
	_, err = Get(key)
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("Get(%q) after Delete: want ErrNotFound, got %v", key, err)
	}
}

// roundtripKey returns a unique key name safe for use with the real keyring.
// Uses crypto/rand so the suffix is unpredictable and safe from CWE-338.
// A random suffix avoids collisions when tests run in parallel or are
// interrupted before cleanup.
func roundtripKey() string {
	buf := make([]byte, 8)
	if _, err := rand.Read(buf); err != nil {
		// crypto/rand.Read only fails on catastrophic OS entropy failures.
		panic(fmt.Sprintf("crypto/rand.Read: %v", err))
	}
	return "pan-agent-test-roundtrip-" + hex.EncodeToString(buf)
}
