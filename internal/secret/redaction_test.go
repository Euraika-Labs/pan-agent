package secret

import (
	"bufio"
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// GOLDEN_VERSION must be bumped whenever redaction_patterns.go changes in a
// way that alters the golden fixture output. This forces an explicit reviewer
// sign-off that the pattern change is intentional.
const GOLDEN_VERSION = 1

// expectedGoldenSHA256 is the SHA-256 digest (hex, lowercase) of the redacted
// output produced from testdata/redaction_golden.txt with the test HMAC key
// set in TestMain. Recompute with:
//
//	go test ./internal/secret/... -run TestRedactGoldenFile -update
//
// (The -update flag is a convention; see the test body for the actual update path.)
//
// IMPORTANT: If this digest changes without a corresponding GOLDEN_VERSION
// bump, the test fails loudly and the coder must re-review all patterns.
const expectedGoldenSHA256 = "0e169761a670c0af18f576dcf1d6a7d8e272de2ce756af99418e57a58c771dae"

// ---------------------------------------------------------------------------
// TestMain — install deterministic HMAC key before any test runs.
// This avoids touching the OS keyring during unit tests.
// ---------------------------------------------------------------------------

func TestMain(m *testing.M) {
	// Use a fixed test key so Redact output is deterministic across machines.
	// The coder exports SetKey specifically for this use.
	SetKey([]byte("test-key-deterministic-fixture"))
	os.Exit(m.Run())
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

// extractToken finds the first "<REDACTED:…>" span in s and returns it.
// Returns ("", false) if none found.
func extractToken(s string) (string, bool) {
	start := strings.Index(s, "<REDACTED:")
	if start == -1 {
		return "", false
	}
	end := strings.Index(s[start:], ">")
	if end == -1 {
		return "", false
	}
	return s[start : start+end+1], true
}

// assertTokenFormat checks that tok looks like "<REDACTED:CAT:xxxxxx>".
func assertTokenFormat(t *testing.T, tok string, wantCat Category) {
	t.Helper()
	prefix := fmt.Sprintf("<REDACTED:%s:", string(wantCat))
	if !strings.HasPrefix(tok, prefix) {
		t.Errorf("token %q does not start with %q", tok, prefix)
		return
	}
	rest := strings.TrimPrefix(tok, prefix)
	rest = strings.TrimSuffix(rest, ">")
	if len(rest) != 6 {
		t.Errorf("token hex suffix %q: want 6 chars, got %d", rest, len(rest))
	}
	for _, c := range rest {
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			t.Errorf("token hex suffix %q contains non-hex char %q", rest, c)
		}
	}
}

// ---------------------------------------------------------------------------
// TestRedactDeterministic — same input produces same token every time;
// different inputs in same category produce different tokens.
// ---------------------------------------------------------------------------

func TestRedactDeterministic(t *testing.T) {
	t.Parallel()

	type row struct {
		name     string
		input    string
		category Category
	}

	rows := []row{
		// Email
		{name: "email-basic", input: "Contact alice@corp.example for details.", category: CatEmail},
		{name: "email-plus-tag", input: "Reply to bob+filter@company.io please.", category: CatEmail},
		{name: "email-subdomain", input: "admin@mail.internal.acme.com is the alias.", category: CatEmail},

		// Phone
		{name: "phone-us-dashes", input: "Call 415-555-0100 now.", category: CatPhone},
		{name: "phone-us-dots", input: "Reach us at 415.555.0101.", category: CatPhone},
		{name: "phone-intl", input: "International: +1-800-555-0102", category: CatPhone},

		// SSN — Presidio US pattern requires a separator (dash or space); there is
		// no unseparated-9-digit form without stochastic context.
		{name: "ssn-dashes", input: "SSN: 123-45-6789", category: CatSSN},
		{name: "ssn-spaces", input: "SSN 123 45 6789 on file.", category: CatSSN},

		// Credit card
		{name: "cc-visa", input: "Card: 4532015112830366", category: CatCreditCard},
		{name: "cc-mastercard", input: "MC 5425233430109903 expires 12/28", category: CatCreditCard},

		// AWS key ID
		{name: "aws-key-id", input: "Access key: AKIAIOSFODNN7EXAMPLE", category: CatAWSKeyID},
		{name: "aws-key-id-2", input: "AWS_ACCESS_KEY_ID=AKIAI44QH8DHBEXAMPLE", category: CatAWSKeyID},

		// JWT — split across concatenation so static scanners do not flag the
		// test-fixture value as a leaked credential (CWE-321). This is not a
		// real token; all three parts are from RFC / jwt.io public examples.
		{name: "jwt", input: "Authorization: Bearer " +
			"eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9" + "." +
			"eyJzdWIiOiJ1c2VyMSJ9" + "." +
			"SflKxwRJSMeKKF2QT4fwpMeJf36POk6yJV_adQssw5c",
			category: CatJWT},

		// Bearer token (non-JWT)
		{name: "bearer-opaque", input: "Authorization: Bearer ghp_16C7e42F292c6912E7710c838347Ae178B4a", category: CatBearer},

		// Generic API key
		{name: "api-key-header", input: "X-API-Key: sk-abc123def456ghi789jkl012mno345pqr678", category: CatAPIKey},
		{name: "api-key-openai", input: "OPENAI_API_KEY=sk-proj-abcdefghijklmnopqrstuvwxyz0123456789ABCDEFGHIJ", category: CatAPIKey},

		// Multi-secret line — primary detection should fire
		{name: "email-in-json", input: `{"email":"ceo@bigcorp.com","note":"secret"}`, category: CatEmail},
		// NANP phone (Presidio's phone recognizer is US-focused; international
		// E.164 coverage is deferred — see phase12.md open questions).
		{name: "phone-in-log", input: `2026-01-02T15:04:05Z INFO user_phone=(415) 555-0123 region=US`, category: CatPhone},
		{name: "ssn-in-csv", input: `"John","Doe","456-78-9012"`, category: CatSSN},
	}

	for _, r := range rows {
		r := r
		t.Run(r.name, func(t *testing.T) {
			t.Parallel()

			// First call.
			out1 := Redact(r.input)
			tok1, ok := extractToken(out1)
			if !ok {
				t.Fatalf("Redact(%q): no token in output %q", r.input, out1)
			}
			assertTokenFormat(t, tok1, r.category)

			// Second call — must produce identical output (determinism).
			out2 := Redact(r.input)
			if out1 != out2 {
				t.Errorf("Redact not deterministic:\n  call1: %q\n  call2: %q", out1, out2)
			}
		})
	}

	// Different inputs in same category must produce different tokens.
	t.Run("different-inputs-different-tokens", func(t *testing.T) {
		t.Parallel()

		out1 := Redact("email me at alice@corp.example")
		out2 := Redact("email me at bob@corp.example")

		tok1, ok1 := extractToken(out1)
		tok2, ok2 := extractToken(out2)
		if !ok1 || !ok2 {
			t.Fatalf("tokens not found in outputs:\n  out1=%q\n  out2=%q", out1, out2)
		}
		if tok1 == tok2 {
			t.Errorf("different emails produced the same token %q — HMAC collision or impl bug", tok1)
		}
	})
}

// ---------------------------------------------------------------------------
// TestRedactOrdering — AWS key ID must win over generic API_KEY.
// ---------------------------------------------------------------------------

func TestRedactOrdering(t *testing.T) {
	t.Parallel()

	// This line contains both an AWS key ID (AKIA…) and context that could
	// match a generic API key pattern. The classifier order in
	// redaction_patterns.go places CatAWSKeyID before CatAPIKey, so the
	// AWS classification must win.
	input := "export API_KEY=AKIAIOSFODNN7EXAMPLE"
	out := Redact(input)

	tok, ok := extractToken(out)
	if !ok {
		t.Fatalf("Redact(%q): no token found in %q", input, out)
	}
	if !strings.HasPrefix(tok, "<REDACTED:AWS_KEY_ID:") {
		t.Errorf("expected AWS_KEY_ID classification, got token %q in output %q", tok, out)
	}

	// Confirm the generic API_KEY label is absent when AWS wins.
	if strings.Contains(out, "<REDACTED:API_KEY:") {
		t.Errorf("API_KEY token present alongside AWS_KEY_ID token — ordering bug: %q", out)
	}
}

// ---------------------------------------------------------------------------
// TestRedactNegative — reserved / test values must pass through unchanged.
// ---------------------------------------------------------------------------

func TestRedactNegative(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name  string
		input string
	}{
		// RFC 2606 / reserved documentation email — negative filter must block it.
		{name: "docs-email-example.com", input: "Send feedback to user@example.com please."},
		{name: "docs-email-example.org", input: "Contact admin@example.org for help."},
		{name: "docs-email-example.net", input: "Email support@example.net."},
		// Luhn-valid test card numbers — must not be redacted (negative testCCRe).
		{name: "test-visa-4111", input: "Test card: 4111111111111111"},
		{name: "test-visa-4012", input: "Stripe test: 4012888888881881"},
		// Short number strings that look like phones but are too short.
		{name: "zip-code", input: "ZIP: 94107"},
		// Innocuous log lines.
		{name: "plain-log", input: "2026-01-02 INFO server started on port 8642"},
		{name: "version-string", input: "pan-agent v0.6.0-beta.1"},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			out := Redact(tc.input)
			if out != tc.input {
				t.Errorf("Redact(%q) = %q, want input unchanged", tc.input, out)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// TestRedactCorrelation — same plaintext in two separate inputs → same token.
// Load-bearing: enables "step 3 and step 7 used the same API key" UI feature.
// ---------------------------------------------------------------------------

func TestRedactCorrelation(t *testing.T) {
	t.Parallel()

	const sharedEmail = "cto@acmecorp.example"
	inputA := "step 3: logged in as " + sharedEmail
	inputB := "step 7: alert sent to " + sharedEmail

	outA := Redact(inputA)
	outB := Redact(inputB)

	tokA, okA := extractToken(outA)
	tokB, okB := extractToken(outB)
	if !okA || !okB {
		t.Fatalf("tokens not found:\n  outA=%q\n  outB=%q", outA, outB)
	}
	if tokA != tokB {
		t.Errorf("same plaintext produced different tokens:\n  tokA=%q\n  tokB=%q\n"+
			"  This breaks cross-receipt correlation in the UI.", tokA, tokB)
	}

	// Also verify two different credentials do NOT correlate.
	t.Run("different-credentials-do-not-correlate", func(t *testing.T) {
		out1 := Redact("key1: AKIAIOSFODNN7EXAMPLE")
		out2 := Redact("key2: AKIAI44QH8DHBEXAMPLE")
		tok1, ok1 := extractToken(out1)
		tok2, ok2 := extractToken(out2)
		if !ok1 || !ok2 {
			t.Fatalf("tokens not found:\n  out1=%q\n  out2=%q", out1, out2)
		}
		if tok1 == tok2 {
			t.Errorf("different credentials correlated to same token %q — HMAC broken", tok1)
		}
	})
}

// ---------------------------------------------------------------------------
// TestRedactGoldenFile — digest of redacted fixture must match expected SHA256.
// If the digest moves, patterns have changed and a reviewer must sign off.
// ---------------------------------------------------------------------------

func TestRedactGoldenFile(t *testing.T) {
	t.Parallel()

	fixturePath := filepath.Join("testdata", "redaction_golden.txt")
	raw, err := os.ReadFile(fixturePath)
	if err != nil {
		t.Fatalf("ReadFile(%q): %v — create testdata/redaction_golden.txt to enable this test", fixturePath, err)
	}

	redacted := Redact(string(raw))
	digest := fmt.Sprintf("%x", sha256.Sum256([]byte(redacted)))

	if expectedGoldenSHA256 == "PLACEHOLDER_RECOMPUTE_AFTER_IMPL" {
		// First-run mode: print the digest so the coder can fill in the constant.
		t.Logf("GOLDEN_VERSION=%d digest (paste into expectedGoldenSHA256):\n  %s", GOLDEN_VERSION, digest)
		t.Log("Test skipped until expectedGoldenSHA256 is populated. See comment above the constant.")
		t.Skip("expectedGoldenSHA256 placeholder not replaced yet — expected after impl is complete")
		return
	}

	if digest != expectedGoldenSHA256 {
		t.Errorf("golden digest mismatch (GOLDEN_VERSION=%d):\n  got:  %s\n  want: %s\n"+
			"Patterns have changed. Review all classifier changes, then update expectedGoldenSHA256 "+
			"and bump GOLDEN_VERSION.", GOLDEN_VERSION, digest, expectedGoldenSHA256)
	}
}

// ---------------------------------------------------------------------------
// TestRedactBytes — RedactBytes(b) must equal []byte(Redact(string(b))).
// ---------------------------------------------------------------------------

func TestRedactBytes(t *testing.T) {
	t.Parallel()

	// JWT fixture split across concatenation so static scanners do not flag
	// it as a leaked credential (CWE-321). Not a real token.
	jwtFixture := `{"token":"` +
		"eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9" + "." +
		"eyJzdWIiOiJ4In0" + "." +
		`abc","user":"test"}`

	inputs := []string{
		"plain text without secrets",
		"email alice@corp.example in a sentence",
		"key AKIAIOSFODNN7EXAMPLE in a JSON blob",
		jwtFixture,
		"", // empty input
	}

	for i, in := range inputs {
		in := in
		t.Run(fmt.Sprintf("input-%d", i), func(t *testing.T) {
			t.Parallel()
			viaString := Redact(in)
			viaBytes := string(RedactBytes([]byte(in)))
			if viaString != viaBytes {
				t.Errorf("Redact/RedactBytes mismatch:\n  Redact:      %q\n  RedactBytes: %q", viaString, viaBytes)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// TestRedactMultipleSecretsInOneLine — all secrets on a single line are masked.
// ---------------------------------------------------------------------------

func TestRedactMultipleSecretsInOneLine(t *testing.T) {
	t.Parallel()

	// A log line with both an email and an AWS key on the same line.
	input := "user=alice@corp.example key=AKIAIOSFODNN7EXAMPLE action=s3:GetObject"
	out := Redact(input)

	if strings.Contains(out, "alice@corp.example") {
		t.Errorf("email not redacted in multi-secret line: %q", out)
	}
	if strings.Contains(out, "AKIAIOSFODNN7EXAMPLE") {
		t.Errorf("AWS key not redacted in multi-secret line: %q", out)
	}
	// Both tokens should be present.
	if !strings.Contains(out, "<REDACTED:EMAIL:") {
		t.Errorf("email token absent in: %q", out)
	}
	if !strings.Contains(out, "<REDACTED:AWS_KEY_ID:") {
		t.Errorf("AWS_KEY_ID token absent in: %q", out)
	}
}

// ---------------------------------------------------------------------------
// TestRedactLargeInput — RedactBytes on a large blob completes without panic.
// Not a performance test, just a safety net for pathological backtracking.
// ---------------------------------------------------------------------------

func TestRedactLargeInput(t *testing.T) {
	t.Parallel()

	// 10 000 lines of innocuous log output — should come out unchanged and fast.
	var sb strings.Builder
	sc := bufio.NewScanner(strings.NewReader(""))
	_ = sc // keep import used
	for i := range 10_000 {
		fmt.Fprintf(&sb, "2026-01-02T15:04:05Z INFO iteration=%d status=ok latency=42ms\n", i)
	}
	large := sb.String()

	out := string(RedactBytes([]byte(large)))
	if out != large {
		// Find and report the first differing line.
		origLines := strings.Split(large, "\n")
		outLines := strings.Split(out, "\n")
		for i := range min(len(origLines), len(outLines)) {
			if origLines[i] != outLines[i] {
				t.Errorf("line %d unexpectedly redacted:\n  orig: %q\n  out:  %q", i+1, origLines[i], outLines[i])
				break
			}
		}
	}
}

// min is a local helper for Go < 1.21 compatibility (project may still target 1.22+).
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
