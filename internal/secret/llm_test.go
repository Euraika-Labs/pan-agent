package secret

import (
	"strings"
	"testing"
)

// Tests for the Phase 13 WS#13.G LLM-redaction pipeline. The HMAC
// pipeline already has comprehensive coverage in redaction_test.go;
// these tests focus on the surface that's specific to RedactForLLM /
// UnRedactResponse / ReversibleMap.

// ---------------------------------------------------------------------------
// RedactForLLM — counter scoping, dedup, policy filter
// ---------------------------------------------------------------------------

func TestRedactForLLM_HappyPath(t *testing.T) {
	t.Parallel()
	mapping := NewReversibleMap()
	out := RedactForLLM("contact alice@corp.example for details", mapping, Policy{})
	if !strings.Contains(out, "<REDACTED:EMAIL:1>") {
		t.Errorf("expected email:1 token in output, got %q", out)
	}
	if mapping.Size() != 1 {
		t.Errorf("Size = %d, want 1", mapping.Size())
	}
	plain, ok := mapping.Lookup("<REDACTED:EMAIL:1>")
	if !ok || plain != "alice@corp.example" {
		t.Errorf("Lookup returned (%q, %v), want (\"alice@corp.example\", true)", plain, ok)
	}
}

func TestRedactForLLM_CountersScopePerCategory(t *testing.T) {
	t.Parallel()
	mapping := NewReversibleMap()
	out := RedactForLLM(
		"alice@corp.example bob@corp.example carol@corp.example",
		mapping,
		Policy{},
	)
	if !strings.Contains(out, "<REDACTED:EMAIL:1>") ||
		!strings.Contains(out, "<REDACTED:EMAIL:2>") ||
		!strings.Contains(out, "<REDACTED:EMAIL:3>") {
		t.Errorf("expected email:1/2/3 tokens, got %q", out)
	}
}

func TestRedactForLLM_DedupesIdenticalPlaintext(t *testing.T) {
	t.Parallel()
	// Same email mentioned twice — must reuse the same counter token
	// so the LLM sees consistency ("the email I mentioned").
	mapping := NewReversibleMap()
	out := RedactForLLM(
		"reply to alice@corp.example and cc alice@corp.example",
		mapping,
		Policy{},
	)
	count := strings.Count(out, "<REDACTED:EMAIL:1>")
	if count != 2 {
		t.Errorf("expected email:1 to appear twice (dedup), got %d in %q", count, out)
	}
	if strings.Contains(out, "<REDACTED:EMAIL:2>") {
		t.Errorf("dedup failed — same plaintext got two tokens: %q", out)
	}
	if mapping.Size() != 1 {
		t.Errorf("Size = %d, want 1 (deduped)", mapping.Size())
	}
}

func TestRedactForLLM_CountersContinueAcrossCalls(t *testing.T) {
	t.Parallel()
	// One mapping shared across two calls — counters must continue.
	mapping := NewReversibleMap()
	out1 := RedactForLLM("alice@corp.example", mapping, Policy{})
	out2 := RedactForLLM("bob@corp.example", mapping, Policy{})
	if !strings.Contains(out1, "<REDACTED:EMAIL:1>") {
		t.Errorf("first call: expected email:1, got %q", out1)
	}
	if !strings.Contains(out2, "<REDACTED:EMAIL:2>") {
		t.Errorf("second call: expected email:2 (counter continued), got %q", out2)
	}
	if mapping.Size() != 2 {
		t.Errorf("Size = %d, want 2", mapping.Size())
	}
}

func TestRedactForLLM_DistinctCategoriesGetIndependentCounters(t *testing.T) {
	t.Parallel()
	mapping := NewReversibleMap()
	out := RedactForLLM(
		"call 415-555-0100 or email alice@corp.example",
		mapping,
		Policy{},
	)
	// Both should be :1 since each category counts independently.
	if !strings.Contains(out, "<REDACTED:PHONE:1>") {
		t.Errorf("expected phone:1, got %q", out)
	}
	if !strings.Contains(out, "<REDACTED:EMAIL:1>") {
		t.Errorf("expected email:1, got %q", out)
	}
}

func TestRedactForLLM_PolicyFilter(t *testing.T) {
	t.Parallel()
	// Only redact emails. Phone in the same input should pass through.
	mapping := NewReversibleMap()
	policy := Policy{Categories: map[Category]bool{CatEmail: true}}
	out := RedactForLLM(
		"call 415-555-0100 or email alice@corp.example",
		mapping,
		policy,
	)
	if strings.Contains(out, "<REDACTED:PHONE") {
		t.Errorf("phone redacted despite policy filter: %q", out)
	}
	if !strings.Contains(out, "<REDACTED:EMAIL:1>") {
		t.Errorf("email not redacted: %q", out)
	}
	if !strings.Contains(out, "415-555-0100") {
		t.Errorf("phone plaintext missing: %q", out)
	}
}

func TestRedactForLLM_NegativeRegexHonored(t *testing.T) {
	t.Parallel()
	// docs-email negative regex should keep example.com addresses intact.
	mapping := NewReversibleMap()
	out := RedactForLLM(
		"real alice@corp.io and docs bob@example.com",
		mapping,
		Policy{},
	)
	if !strings.Contains(out, "<REDACTED:EMAIL:1>") {
		t.Errorf("real email not redacted: %q", out)
	}
	if !strings.Contains(out, "bob@example.com") {
		t.Errorf("docs email was redacted despite negative regex: %q", out)
	}
}

func TestRedactForLLM_NilMappingPanics(t *testing.T) {
	t.Parallel()
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic on nil mapping; recovered nil")
		}
	}()
	_ = RedactForLLM("alice@corp.example", nil, Policy{})
}

func TestRedactForLLM_EmptyText(t *testing.T) {
	t.Parallel()
	mapping := NewReversibleMap()
	if out := RedactForLLM("", mapping, Policy{}); out != "" {
		t.Errorf("empty input → %q, want empty", out)
	}
	if mapping.Size() != 0 {
		t.Errorf("Size = %d, want 0", mapping.Size())
	}
}

// ---------------------------------------------------------------------------
// UnRedactResponse — round-trip + safety
// ---------------------------------------------------------------------------

func TestUnRedactResponse_RoundTrip(t *testing.T) {
	t.Parallel()
	// Property: for any input text, un-redacting the redacted output
	// yields back the original text byte-for-byte.
	cases := []string{
		"contact alice@corp.example",
		"call 415-555-0100 then email alice@corp.example",
		"three emails: a@x.io b@y.io c@z.io",
		"plain text without any secrets",
		"",
		`{"email":"alice@corp.example","phone":"415-555-0100"}`,
		"AKIAIOSFODNN7EXAMPLE",
	}
	for _, in := range cases {
		t.Run(in, func(t *testing.T) {
			mapping := NewReversibleMap()
			redacted := RedactForLLM(in, mapping, Policy{})
			unredacted := UnRedactResponse(redacted, mapping)
			if unredacted != in {
				t.Errorf("round-trip failed:\n  in:        %q\n  redacted:  %q\n  unredact:  %q", in, redacted, unredacted)
			}
		})
	}
}

func TestUnRedactResponse_AssistantMentionsToken(t *testing.T) {
	t.Parallel()
	// Realistic: model receives prompt with `<REDACTED:EMAIL:1>` and
	// echoes the token in its response. UnRedactResponse must
	// substitute back to plaintext for the user.
	mapping := NewReversibleMap()
	_ = RedactForLLM("contact alice@corp.example", mapping, Policy{})
	assistantOutput := "I'll send a follow-up to <REDACTED:EMAIL:1> later today."
	out := UnRedactResponse(assistantOutput, mapping)
	if out != "I'll send a follow-up to alice@corp.example later today." {
		t.Errorf("unredacted assistant output wrong: %q", out)
	}
}

func TestUnRedactResponse_UnknownTokenPassthrough(t *testing.T) {
	t.Parallel()
	// A token that's not in the mapping (different conversation, hand-
	// crafted by the model, etc.) must be left as-is rather than
	// silently substituted with a guess.
	mapping := NewReversibleMap()
	_ = RedactForLLM("alice@corp.example", mapping, Policy{}) // populates email:1
	out := UnRedactResponse(
		"Reference <REDACTED:PHONE:99> from earlier",
		mapping,
	)
	if !strings.Contains(out, "<REDACTED:PHONE:99>") {
		t.Errorf("unknown token was substituted: %q", out)
	}
}

func TestUnRedactResponse_DoesNotTouchHMACTokens(t *testing.T) {
	t.Parallel()
	// HMAC tokens have hex suffixes (`<REDACTED:EMAIL:a1b2c3>`) which
	// the LLM-token regex (`<REDACTED:[A-Z_]+:\d+>`) doesn't match.
	// Verify a leaked HMAC token in an assistant response doesn't get
	// mangled.
	mapping := NewReversibleMap()
	_ = RedactForLLM("alice@corp.example", mapping, Policy{})
	hmacToken := "<REDACTED:EMAIL:a1b2c3>" // hex, not LLM-format
	out := UnRedactResponse("see "+hmacToken+" for context", mapping)
	if !strings.Contains(out, hmacToken) {
		t.Errorf("HMAC token mangled: %q", out)
	}
}

func TestUnRedactResponse_NilMappingNoOp(t *testing.T) {
	t.Parallel()
	in := "contact <REDACTED:EMAIL:1>"
	if out := UnRedactResponse(in, nil); out != in {
		t.Errorf("nil mapping → %q, want %q (no-op)", out, in)
	}
}

func TestUnRedactResponse_EmptyMappingNoOp(t *testing.T) {
	t.Parallel()
	mapping := NewReversibleMap()
	in := "contact <REDACTED:EMAIL:1>"
	if out := UnRedactResponse(in, mapping); out != in {
		t.Errorf("empty mapping → %q, want %q (no-op)", out, in)
	}
}

// ---------------------------------------------------------------------------
// Mapping isolation — A and B don't leak
// ---------------------------------------------------------------------------

func TestReversibleMap_IsolationBetweenMappings(t *testing.T) {
	t.Parallel()
	mapA := NewReversibleMap()
	mapB := NewReversibleMap()
	_ = RedactForLLM("alice@corp.example", mapA, Policy{})
	_ = RedactForLLM("bob@corp.example", mapB, Policy{})
	// Both have :1 internally — but un-redacting A's text against B's
	// mapping must NOT yield A's plaintext (counters collide → must miss).
	got := UnRedactResponse("see <REDACTED:EMAIL:1>", mapB)
	if strings.Contains(got, "alice") {
		t.Errorf("mapping A's plaintext leaked through mapping B: %q", got)
	}
	// And B's mapping correctly returns bob.
	gotB := UnRedactResponse("see <REDACTED:EMAIL:1>", mapB)
	if !strings.Contains(gotB, "bob@corp.example") {
		t.Errorf("mapping B failed to un-redact its own token: %q", gotB)
	}
}

// ---------------------------------------------------------------------------
// ReversibleMap accessor methods
// ---------------------------------------------------------------------------

func TestReversibleMap_Categories(t *testing.T) {
	t.Parallel()
	m := NewReversibleMap()
	_ = RedactForLLM("alice@corp.example and 415-555-0100", m, Policy{})
	cats := m.Categories()
	if len(cats) != 2 {
		t.Errorf("Categories len = %d, want 2", len(cats))
	}
	seen := map[Category]bool{}
	for _, c := range cats {
		seen[c] = true
	}
	if !seen[CatEmail] || !seen[CatPhone] {
		t.Errorf("Categories missing entries: %v", cats)
	}
}

func TestReversibleMap_Clear(t *testing.T) {
	t.Parallel()
	m := NewReversibleMap()
	_ = RedactForLLM("alice@corp.example", m, Policy{})
	if m.Size() != 1 {
		t.Fatalf("pre-clear Size = %d, want 1", m.Size())
	}
	m.Clear()
	if m.Size() != 0 {
		t.Errorf("post-clear Size = %d, want 0", m.Size())
	}
	// After Clear, counters reset — next RedactForLLM call should
	// emit email:1 again, not email:2.
	_ = RedactForLLM("bob@corp.example", m, Policy{})
	if _, ok := m.Lookup("<REDACTED:EMAIL:1>"); !ok {
		t.Error("counter did not reset after Clear")
	}
	if _, ok := m.Lookup("<REDACTED:EMAIL:2>"); ok {
		t.Error("counter incorrectly continued past Clear")
	}
}

func TestReversibleMap_NilSafety(t *testing.T) {
	t.Parallel()
	var m *ReversibleMap // nil
	if m.Size() != 0 {
		t.Errorf("nil Size = %d, want 0", m.Size())
	}
	if _, ok := m.Lookup("<REDACTED:EMAIL:1>"); ok {
		t.Error("nil Lookup returned ok=true")
	}
	if cats := m.Categories(); cats != nil {
		t.Errorf("nil Categories = %v, want nil", cats)
	}
	m.Clear() // must not panic
}

// ---------------------------------------------------------------------------
// Policy
// ---------------------------------------------------------------------------

func TestPolicy_AllowsAllByDefault(t *testing.T) {
	t.Parallel()
	p := Policy{}
	for _, c := range []Category{
		CatEmail, CatPhone, CatSSN, CatCreditCard,
		CatAPIKey, CatJWT, CatAWSKeyID, CatBearer,
	} {
		if !p.allows(c) {
			t.Errorf("default Policy disallowed %s", c)
		}
	}
}

func TestPolicy_NonEmptyMapFiltersExactly(t *testing.T) {
	t.Parallel()
	p := Policy{Categories: map[Category]bool{CatEmail: true}}
	if !p.allows(CatEmail) {
		t.Error("explicitly allowed category was disallowed")
	}
	if p.allows(CatPhone) {
		t.Error("non-listed category was allowed")
	}
}

// ---------------------------------------------------------------------------
// Cross-pipeline isolation — RedactForLLM and Redact don't interfere
// ---------------------------------------------------------------------------

func TestRedactForLLM_DoesNotInterfereWithHMACPipeline(t *testing.T) {
	t.Parallel()
	in := "contact alice@corp.example"

	// HMAC pipeline output (deterministic across calls, hex suffix).
	hmac1 := Redact(in)
	if !strings.Contains(hmac1, "<REDACTED:EMAIL:") {
		t.Fatalf("HMAC pipeline didn't redact: %q", hmac1)
	}

	// LLM pipeline output (counter, distinct shape).
	mapping := NewReversibleMap()
	llm1 := RedactForLLM(in, mapping, Policy{})
	if !strings.Contains(llm1, "<REDACTED:EMAIL:1>") {
		t.Fatalf("LLM pipeline didn't redact: %q", llm1)
	}

	// HMAC and LLM tokens must NOT be identical strings.
	if hmac1 == llm1 {
		t.Errorf("HMAC and LLM redactions produced identical output: %q", hmac1)
	}

	// Calling Redact again still returns the same HMAC output —
	// LLM redaction doesn't mutate the global redactor.
	hmac2 := Redact(in)
	if hmac1 != hmac2 {
		t.Errorf("HMAC pipeline became non-deterministic after LLM call:\n  first: %q\n  second: %q", hmac1, hmac2)
	}
}
