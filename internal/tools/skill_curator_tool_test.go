package tools

import (
	"encoding/json"
	"testing"
)

// TestParseStringArrayAcceptsNativeArray covers the well-behaved case: the
// LLM sends skill_ids as a JSON array as the schema requests.
func TestParseStringArrayAcceptsNativeArray(t *testing.T) {
	raw := json.RawMessage(`["coding/refactor-go","coding/refactor-rust"]`)
	got, err := parseStringArray(raw)
	if err != nil {
		t.Fatalf("parseStringArray: %v", err)
	}
	want := []string{"coding/refactor-go", "coding/refactor-rust"}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Errorf("got %v, want %v", got, want)
	}
}

// TestParseStringArrayAcceptsStringifiedArray is the regression test for the
// bug caught during the curator e2e smoke: Llama-3.3-70B-Instruct sends array
// parameters as JSON-encoded strings instead of native arrays.
func TestParseStringArrayAcceptsStringifiedArray(t *testing.T) {
	raw := json.RawMessage(`"[\"coding/refactor-go\", \"coding/refactor-rust\"]"`)
	got, err := parseStringArray(raw)
	if err != nil {
		t.Fatalf("parseStringArray stringified: %v", err)
	}
	if len(got) != 2 || got[0] != "coding/refactor-go" {
		t.Errorf("got %v, want [coding/refactor-go coding/refactor-rust]", got)
	}
}

// TestParseStringArrayEmpty — missing or empty raw payload returns nil, no error.
func TestParseStringArrayEmpty(t *testing.T) {
	got, err := parseStringArray(nil)
	if err != nil || got != nil {
		t.Errorf("empty: got (%v, %v), want (nil, nil)", got, err)
	}
	got, err = parseStringArray(json.RawMessage{})
	if err != nil || got != nil {
		t.Errorf("zero-len: got (%v, %v), want (nil, nil)", got, err)
	}
}

// TestParseStringArrayRejectsGarbage — an integer or object is not a valid
// skill_ids payload, and we want a descriptive error.
func TestParseStringArrayRejectsGarbage(t *testing.T) {
	bad := []json.RawMessage{
		json.RawMessage(`42`),
		json.RawMessage(`{"not":"an array"}`),
		json.RawMessage(`"not a json array inside a string"`),
	}
	for _, raw := range bad {
		if _, err := parseStringArray(raw); err == nil {
			t.Errorf("parseStringArray(%s): want error, got nil", string(raw))
		}
	}
}

// TestParseSplitProposalsAcceptsNativeArray — well-behaved native-array input.
func TestParseSplitProposalsAcceptsNativeArray(t *testing.T) {
	raw := json.RawMessage(`[
		{"category":"writing","name":"intro","description":"intros","content":"# intro"},
		{"category":"writing","name":"outro","description":"outros","content":"# outro"}
	]`)
	got, err := parseSplitProposals(raw)
	if err != nil {
		t.Fatalf("parseSplitProposals: %v", err)
	}
	if len(got) != 2 || got[0].Name != "intro" || got[1].Name != "outro" {
		t.Errorf("got %+v, want [intro, outro]", got)
	}
}

// TestParseSplitProposalsAcceptsStringifiedArray — the LLM-quirk regression,
// nested objects inside a stringified array.
func TestParseSplitProposalsAcceptsStringifiedArray(t *testing.T) {
	// JSON-encoded string whose body is a JSON array of objects.
	raw := json.RawMessage(`"[{\"category\":\"writing\",\"name\":\"intro\",\"description\":\"intros\",\"content\":\"# intro\"},{\"category\":\"writing\",\"name\":\"outro\",\"description\":\"outros\",\"content\":\"# outro\"}]"`)
	got, err := parseSplitProposals(raw)
	if err != nil {
		t.Fatalf("parseSplitProposals stringified: %v", err)
	}
	if len(got) != 2 || got[0].Name != "intro" {
		t.Errorf("got %+v, want [intro, outro]", got)
	}
}
