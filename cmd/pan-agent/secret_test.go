package main

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/euraika-labs/pan-agent/internal/secret"
)

// Phase 13 — `pan-agent secret` CLI tests.

// captureSecretStdout — local copy of the concurrent-drain helper
// (the duplication in tests across cmd/pan-agent dedups in a
// follow-up; right now each test file ships its own to avoid
// cross-PR coupling).
func captureSecretStdout(t *testing.T, fn func() error) (string, error) {
	t.Helper()
	old := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	os.Stdout = w

	var buf bytes.Buffer
	done := make(chan struct{})
	go func() {
		_, _ = io.Copy(&buf, r)
		close(done)
	}()

	runErr := fn()
	_ = w.Close()
	<-done
	os.Stdout = old
	return buf.String(), runErr
}

// withSecretKey ensures the redaction pipeline has a key for tests.
// Without this, the pipeline initialises from the OS keyring which
// may not be available in CI.
func withSecretKey(t *testing.T) {
	t.Helper()
	secret.SetKey([]byte("test-redaction-key-pan-agent-secret-cli"))
}

// ---------------------------------------------------------------------------
// dispatch
// ---------------------------------------------------------------------------

func TestCmdSecret_NoAction(t *testing.T) {
	if err := cmdSecret(nil); err == nil {
		t.Error("expected missing-action error")
	}
}

func TestCmdSecret_UnknownAction(t *testing.T) {
	if err := cmdSecret([]string{"banana"}); err == nil ||
		!strings.Contains(err.Error(), "unknown") {
		t.Errorf("got %v, want unknown-action error", err)
	}
}

// ---------------------------------------------------------------------------
// patterns
// ---------------------------------------------------------------------------

func TestCmdSecretPatterns_Plain(t *testing.T) {
	out, err := captureSecretStdout(t, func() error {
		return cmdSecretPatterns(nil)
	})
	if err != nil {
		t.Fatalf("patterns: %v", err)
	}
	for _, want := range []string{
		"EMAIL", "API_KEY", "JWT", "AWS_KEY_ID", "BEARER_TOKEN",
		"SLACK_TOKEN", "STRIPE_KEY", "GITHUB_TOKEN", "GCP_PRIVATE_KEY",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q\n%s", want, out)
		}
	}
}

func TestCmdSecretPatterns_JSON(t *testing.T) {
	out, err := captureSecretStdout(t, func() error {
		return cmdSecretPatterns([]string{"--json"})
	})
	if err != nil {
		t.Fatalf("patterns --json: %v", err)
	}
	var arr []string
	if err := json.Unmarshal([]byte(out), &arr); err != nil {
		t.Fatalf("output not valid JSON: %v\n%s", err, out)
	}
	if len(arr) < 8 {
		t.Errorf("len = %d, want at least 8 categories", len(arr))
	}
	// Every entry must be uppercase, non-empty, ASCII.
	for _, c := range arr {
		if c == "" {
			t.Error("empty category in list")
		}
		if c != strings.ToUpper(c) {
			t.Errorf("category %q is not uppercase", c)
		}
	}
}

func TestSecretCategories_NoDuplicates(t *testing.T) {
	cats := secretCategories()
	seen := map[string]bool{}
	for _, c := range cats {
		if seen[c] {
			t.Errorf("duplicate category %q", c)
		}
		seen[c] = true
	}
}

// ---------------------------------------------------------------------------
// scan
// ---------------------------------------------------------------------------

func TestCmdSecretScan_FromArg(t *testing.T) {
	withSecretKey(t)
	out, err := captureSecretStdout(t, func() error {
		return cmdSecretScan([]string{"contact alice@example.com please"})
	})
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	// example.com is in the docs/example domain set, which Redact
	// excludes — use a non-reserved address.
	if !strings.Contains(out, "@example.com") && !strings.Contains(out, "<REDACTED:") {
		t.Errorf("expected either redaction or pass-through (example.com is reserved), got %q", out)
	}
}

func TestCmdSecretScan_RedactsRealEmail(t *testing.T) {
	withSecretKey(t)
	out, err := captureSecretStdout(t, func() error {
		return cmdSecretScan([]string{"contact alice@corp.acme please"})
	})
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	if !strings.Contains(out, "<REDACTED:EMAIL:") {
		t.Errorf("expected EMAIL redaction, got %q", out)
	}
	if strings.Contains(out, "alice@corp.acme") {
		t.Errorf("plaintext leaked: %q", out)
	}
}

func TestCmdSecretScan_RedactsAWSKey(t *testing.T) {
	withSecretKey(t)
	akia := "AKIAIOSFODNN7EXAMPLE"
	out, err := captureSecretStdout(t, func() error {
		return cmdSecretScan([]string{"AWS_ACCESS_KEY_ID=" + akia})
	})
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	if !strings.Contains(out, "<REDACTED:AWS_KEY_ID:") {
		t.Errorf("expected AWS_KEY_ID redaction, got %q", out)
	}
	if strings.Contains(out, akia) {
		t.Errorf("AKIA leaked: %q", out)
	}
}

func TestCmdSecretScan_FromStdin(t *testing.T) {
	withSecretKey(t)
	// Pipe input via stdin redirect.
	r, w, _ := os.Pipe()
	go func() {
		_, _ = w.WriteString("send report to bob@corp.acme\n")
		_ = w.Close()
	}()
	old := os.Stdin
	os.Stdin = r
	defer func() { os.Stdin = old }()

	out, err := captureSecretStdout(t, func() error {
		return cmdSecretScan(nil)
	})
	if err != nil {
		t.Fatalf("scan stdin: %v", err)
	}
	if !strings.Contains(out, "<REDACTED:EMAIL:") {
		t.Errorf("expected EMAIL redaction from stdin: %q", out)
	}
}

func TestCmdSecretScan_EmptyArg(t *testing.T) {
	withSecretKey(t)
	// Empty stdin too.
	r, w, _ := os.Pipe()
	w.Close()
	old := os.Stdin
	os.Stdin = r
	defer func() { os.Stdin = old }()

	if err := cmdSecretScan(nil); err == nil {
		t.Error("expected empty-input error")
	}
}

func TestCmdSecretScan_PassThroughCleanText(t *testing.T) {
	withSecretKey(t)
	out, err := captureSecretStdout(t, func() error {
		return cmdSecretScan([]string{"This is just normal prose with no secrets."})
	})
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	if strings.Contains(out, "<REDACTED:") {
		t.Errorf("clean text should pass through, got %q", out)
	}
	if !strings.Contains(out, "normal prose") {
		t.Errorf("clean text mangled: %q", out)
	}
}
