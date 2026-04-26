package secret

import (
	"regexp"
	"strings"
	"testing"
)

// Direct tests for the helpers extracted from redactInternal in Phase 13
// WS#13.G prep — applyClassifier, protectNegatives, restoreProtected.
//
// The end-to-end invariant (byte-for-byte equivalent output to the
// pre-refactor implementation) is guarded by TestRedactGoldenFile in
// redaction_test.go. These tests target each helper in isolation so a
// future regression points the reviewer at the exact broken layer
// rather than just a different SHA on the golden fixture.

// ---------------------------------------------------------------------------
// applyClassifier
// ---------------------------------------------------------------------------

func TestApplyClassifier_NoCaptureGroup_ReplacesWholeMatch(t *testing.T) {
	t.Parallel()
	c := classifier{
		category: "TEST",
		re:       regexp.MustCompile(`AKIA[A-Z0-9]{16}`),
		minLen:   20,
		maxLen:   20,
	}
	out := applyClassifier(
		"key=AKIAIOSFODNN7EXAMPLE end",
		c,
		func(span string) string { return "<TOK:" + span[:4] + ">" },
	)
	if out != "key=<TOK:AKIA> end" {
		t.Errorf("got %q", out)
	}
}

func TestApplyClassifier_CaptureGroup_ReplacesOnlyTheGroup(t *testing.T) {
	t.Parallel()
	// Mimics the bearer-token regex shape: prefix + captured value.
	c := classifier{
		category: "BEARER",
		re:       regexp.MustCompile(`(?i)bearer\s+([A-Za-z0-9]+)`),
		minLen:   1,
		maxLen:   4096,
	}
	out := applyClassifier(
		"Authorization: Bearer abc123 next",
		c,
		func(span string) string { return "<TOK:" + span + ">" },
	)
	// "Bearer " surrounding context is preserved; only "abc123" is replaced.
	if out != "Authorization: Bearer <TOK:abc123> next" {
		t.Errorf("got %q", out)
	}
}

func TestApplyClassifier_LengthBounds_RejectTooShort(t *testing.T) {
	t.Parallel()
	c := classifier{
		category: "TEST",
		re:       regexp.MustCompile(`\d+`),
		minLen:   5,
	}
	out := applyClassifier(
		"id 42 vs 1234567",
		c,
		func(span string) string { return "<TOK>" },
	)
	// "42" is too short; "1234567" is long enough.
	if out != "id 42 vs <TOK>" {
		t.Errorf("got %q", out)
	}
}

func TestApplyClassifier_LengthBounds_RejectTooLong(t *testing.T) {
	t.Parallel()
	c := classifier{
		category: "TEST",
		re:       regexp.MustCompile(`\d+`),
		minLen:   1,
		maxLen:   3,
	}
	out := applyClassifier(
		"short 42 long 1234567",
		c,
		func(span string) string { return "<TOK>" },
	)
	// "42" passes; "1234567" exceeds maxLen and is left alone.
	if out != "short <TOK> long 1234567" {
		t.Errorf("got %q", out)
	}
}

func TestApplyClassifier_NegativeRegex_RejectsMatch(t *testing.T) {
	t.Parallel()
	c := classifier{
		category: "EMAIL",
		re:       regexp.MustCompile(`(?i)[a-z0-9.]+@[a-z0-9.]+`),
		negative: regexp.MustCompile(`(?i)@example\.(com|org|net)$`),
		minLen:   3,
		maxLen:   254,
	}
	out := applyClassifier(
		"real alice@corp.io and docs bob@example.com",
		c,
		func(span string) string { return "<TOK>" },
	)
	// Real email gets redacted; docs example does not.
	if !strings.Contains(out, "<TOK>") {
		t.Errorf("real email not redacted: %q", out)
	}
	if !strings.Contains(out, "bob@example.com") {
		t.Errorf("negative-matching email was redacted: %q", out)
	}
}

func TestApplyClassifier_EmitTokenIsByteIdentical(t *testing.T) {
	t.Parallel()
	// Asserts emit() output is interpolated verbatim — the helper must
	// not transform whatever the caller returned. Critical for the
	// future LLM pipeline whose tokens differ in shape from HMAC ones.
	c := classifier{
		category: "TEST",
		re:       regexp.MustCompile(`secret`),
		minLen:   1,
	}
	emitted := "<REDACTED:test:42>"
	out := applyClassifier("a secret here", c, func(span string) string { return emitted })
	if out != "a "+emitted+" here" {
		t.Errorf("got %q, want %q", out, "a "+emitted+" here")
	}
}

// ---------------------------------------------------------------------------
// protectNegatives + restoreProtected — round-trip + isolation
// ---------------------------------------------------------------------------

func TestProtectAndRestore_RoundTrip(t *testing.T) {
	t.Parallel()
	patterns := []classifier{
		{
			category: "EMAIL",
			re:       regexp.MustCompile(`(?i)[a-z0-9.]+@[a-z0-9.]+`),
			negative: regexp.MustCompile(`(?i)@example\.com$`),
			minLen:   3,
			maxLen:   254,
		},
	}
	original := "alice@corp.io and bob@example.com"
	working, guards := protectNegatives(original, patterns)

	// alice@corp.io must remain intact (its email regex matched but
	// negative did not). bob@example.com must be replaced with a NUL
	// placeholder.
	if !strings.Contains(working, "alice@corp.io") {
		t.Errorf("real email mangled by protect pass: %q", working)
	}
	if strings.Contains(working, "bob@example.com") {
		t.Errorf("docs email was not replaced: %q", working)
	}
	if len(guards) != 1 {
		t.Errorf("guard count = %d, want 1", len(guards))
	}

	// Restoration recovers the original byte-for-byte.
	restored := restoreProtected(working, guards)
	if restored != original {
		t.Errorf("round-trip mismatch:\n  original:  %q\n  restored: %q", original, restored)
	}
}

func TestProtectNegatives_NoNegativeRegex_IsNoOp(t *testing.T) {
	t.Parallel()
	patterns := []classifier{
		{
			category: "TEST",
			re:       regexp.MustCompile(`secret`),
			negative: nil,
			minLen:   1,
		},
	}
	in := "a secret value"
	working, guards := protectNegatives(in, patterns)
	if working != in {
		t.Errorf("protect pass mutated text without negative regex: %q", working)
	}
	if len(guards) != 0 {
		t.Errorf("guard count = %d, want 0", len(guards))
	}
}

func TestProtectNegatives_PlaceholdersAreImmuneToReclassification(t *testing.T) {
	t.Parallel()
	// The whole point of pass 1 → pass 2: a span protected by pass 1
	// must NOT be re-matched by ANY classifier in pass 2. Verify by
	// running applyClassifier against the placeholdered output and
	// asserting the placeholder survives unchanged.
	patterns := []classifier{
		{
			category: "EMAIL",
			re:       regexp.MustCompile(`(?i)[a-z0-9.]+@[a-z0-9.]+`),
			negative: regexp.MustCompile(`(?i)@example\.com$`),
			minLen:   3,
			maxLen:   254,
		},
	}
	working, guards := protectNegatives("docs bob@example.com", patterns)
	// Now run a hypothetical aggressive classifier that would match
	// any "bob" substring.
	aggro := classifier{
		category: "AGG",
		re:       regexp.MustCompile(`bob`),
		minLen:   1,
	}
	out := applyClassifier(working, aggro, func(span string) string { return "<HIT>" })
	// Restore — final output must STILL contain the original
	// bob@example.com because protectNegatives shielded it.
	final := restoreProtected(out, guards)
	if !strings.Contains(final, "bob@example.com") {
		t.Errorf("protected span was mutated by later classifier: %q", final)
	}
	if strings.Contains(final, "<HIT>") {
		t.Errorf("aggressive classifier should not have matched inside placeholder: %q", final)
	}
}

func TestRestoreProtected_EmptyGuards_IsNoOp(t *testing.T) {
	t.Parallel()
	in := "no guards here"
	out := restoreProtected(in, nil)
	if out != in {
		t.Errorf("empty guards mutated text: got %q, want %q", out, in)
	}
}
