package llm

import (
	"strings"
	"testing"

	"github.com/euraika-labs/pan-agent/internal/secret"
)

// Phase 13 WS#13.G — bridge-layer tests for the llm.Redact* helpers.
// The underlying redaction logic is exercised in internal/secret; these
// tests verify the message-shape preservation and round-trip semantics
// that the chat loop will rely on.

func init() {
	// Deterministic HMAC key so a stray Redact() call (which uses HMAC
	// shape) doesn't depend on keyring availability inside CI.
	secret.SetKey([]byte("test-redaction-key-ws13g-bridge"))
}

func TestRedactMessages_PreservesShape(t *testing.T) {
	t.Parallel()
	mapping := secret.NewReversibleMap()
	msgs := []Message{
		{Role: "system", Content: "you are an assistant"},
		{Role: "user", Content: "email me at alice@corp.example"},
		{Role: "assistant", Content: "", ToolCalls: []ToolCall{{
			ID: "call_1", Type: "function",
			Function: FnCall{Name: "send_email",
				Arguments: `{"to":"alice@corp.example"}`},
		}}},
		{Role: "tool", ToolCallID: "call_1", Name: "send_email",
			Content: "sent to alice@corp.example"},
	}
	out := RedactMessages(msgs, mapping, secret.Policy{})

	if len(out) != len(msgs) {
		t.Fatalf("len = %d, want %d", len(out), len(msgs))
	}

	// Roles, names, IDs preserved unchanged.
	for i := range msgs {
		if out[i].Role != msgs[i].Role {
			t.Errorf("Role[%d] = %q, want %q", i, out[i].Role, msgs[i].Role)
		}
		if out[i].Name != msgs[i].Name {
			t.Errorf("Name[%d] = %q, want %q", i, out[i].Name, msgs[i].Name)
		}
		if out[i].ToolCallID != msgs[i].ToolCallID {
			t.Errorf("ToolCallID[%d] = %q, want %q", i,
				out[i].ToolCallID, msgs[i].ToolCallID)
		}
	}

	// User-message email got tokenized.
	if strings.Contains(out[1].Content, "alice@corp.example") {
		t.Errorf("plaintext leaked in user msg: %q", out[1].Content)
	}
	if !strings.Contains(out[1].Content, "<REDACTED:EMAIL:") {
		t.Errorf("expected EMAIL token in user msg: %q", out[1].Content)
	}

	// Tool-call arguments redacted.
	if strings.Contains(out[2].ToolCalls[0].Function.Arguments,
		"alice@corp.example") {
		t.Errorf("plaintext leaked in tool args: %q",
			out[2].ToolCalls[0].Function.Arguments)
	}

	// Tool-call ID, Type, Function.Name carried over.
	if out[2].ToolCalls[0].ID != "call_1" {
		t.Errorf("ToolCall.ID changed: %q", out[2].ToolCalls[0].ID)
	}
	if out[2].ToolCalls[0].Function.Name != "send_email" {
		t.Errorf("Function.Name changed: %q",
			out[2].ToolCalls[0].Function.Name)
	}
}

func TestRedactMessages_DedupesAcrossMessages(t *testing.T) {
	t.Parallel()
	mapping := secret.NewReversibleMap()
	msgs := []Message{
		{Role: "user", Content: "ping bob@corp.example"},
		{Role: "assistant", Content: "noted bob@corp.example"},
		{Role: "user", Content: "again bob@corp.example"},
	}
	out := RedactMessages(msgs, mapping, secret.Policy{})

	// All three references must collapse to ONE token (counter == 1).
	for i, m := range out {
		if !strings.Contains(m.Content, "<REDACTED:EMAIL:1>") {
			t.Errorf("msg[%d] missing :1 token: %q", i, m.Content)
		}
		if strings.Contains(m.Content, "<REDACTED:EMAIL:2>") {
			t.Errorf("msg[%d] dedupe failed, got :2: %q", i, m.Content)
		}
	}
	if mapping.Size() != 1 {
		t.Errorf("mapping.Size() = %d, want 1", mapping.Size())
	}
}

func TestRedactMessages_DoesNotMutateInput(t *testing.T) {
	t.Parallel()
	mapping := secret.NewReversibleMap()
	original := "see carol@corp.example"
	msgs := []Message{{Role: "user", Content: original}}
	_ = RedactMessages(msgs, mapping, secret.Policy{})
	if msgs[0].Content != original {
		t.Errorf("input mutated: %q != %q", msgs[0].Content, original)
	}
}

func TestRedactMessages_NilMappingPanics(t *testing.T) {
	t.Parallel()
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic on nil mapping")
		}
	}()
	_ = RedactMessages([]Message{{Role: "user", Content: "x"}}, nil, secret.Policy{})
}

func TestRedactMessages_Empty(t *testing.T) {
	t.Parallel()
	mapping := secret.NewReversibleMap()
	out := RedactMessages(nil, mapping, secret.Policy{})
	if len(out) != 0 {
		t.Errorf("expected empty out, got %d", len(out))
	}
	out = RedactMessages([]Message{}, mapping, secret.Policy{})
	if len(out) != 0 {
		t.Errorf("expected empty out, got %d", len(out))
	}
}

func TestRedactMessages_PolicyRestricts(t *testing.T) {
	t.Parallel()
	mapping := secret.NewReversibleMap()
	policy := secret.Policy{Categories: map[secret.Category]bool{
		secret.CatEmail: true,
	}}
	msgs := []Message{
		{Role: "user", Content: "email d@corp.example phone 415-555-1234"},
	}
	out := RedactMessages(msgs, mapping, policy)
	if !strings.Contains(out[0].Content, "<REDACTED:EMAIL:") {
		t.Errorf("EMAIL not redacted under policy: %q", out[0].Content)
	}
	if strings.Contains(out[0].Content, "<REDACTED:PHONE:") {
		t.Errorf("PHONE redacted despite policy excluding it: %q", out[0].Content)
	}
	if !strings.Contains(out[0].Content, "415-555-1234") {
		t.Errorf("PHONE plaintext should remain under restrictive policy: %q",
			out[0].Content)
	}
}

func TestUnRedactChunk_RoundTrip(t *testing.T) {
	t.Parallel()
	mapping := secret.NewReversibleMap()
	redacted := secret.RedactForLLM("ping eve@corp.example", mapping, secret.Policy{})

	// Model echoes the token in a chunk → un-redact restores plaintext.
	got := UnRedactChunk(redacted, mapping)
	if got != "ping eve@corp.example" {
		t.Errorf("UnRedactChunk = %q, want %q", got, "ping eve@corp.example")
	}
}

func TestUnRedactChunk_NilMappingPassthrough(t *testing.T) {
	t.Parallel()
	got := UnRedactChunk("<REDACTED:EMAIL:1> hi", nil)
	if got != "<REDACTED:EMAIL:1> hi" {
		t.Errorf("nil mapping should pass through: got %q", got)
	}
}

func TestUnRedactChunk_UnknownTokenLeftAlone(t *testing.T) {
	t.Parallel()
	mapping := secret.NewReversibleMap()
	// No redaction performed yet → mapping is empty → unknown token
	// in chunk must NOT be replaced (fail-closed).
	got := UnRedactChunk("hello <REDACTED:EMAIL:99> there", mapping)
	if got != "hello <REDACTED:EMAIL:99> there" {
		t.Errorf("unknown token mutated: %q", got)
	}
}

func TestUnRedactToolCall_InPlace(t *testing.T) {
	t.Parallel()
	mapping := secret.NewReversibleMap()
	args := secret.RedactForLLM(`{"to":"frank@corp.example"}`, mapping, secret.Policy{})
	tc := &ToolCall{
		ID: "call_1", Type: "function",
		Function: FnCall{Name: "send_email", Arguments: args},
	}
	UnRedactToolCall(tc, mapping)
	if !strings.Contains(tc.Function.Arguments, "frank@corp.example") {
		t.Errorf("UnRedactToolCall failed: %q", tc.Function.Arguments)
	}
	if strings.Contains(tc.Function.Arguments, "<REDACTED:EMAIL:") {
		t.Errorf("token should be gone after un-redact: %q",
			tc.Function.Arguments)
	}
}

func TestUnRedactToolCall_NilSafe(t *testing.T) {
	t.Parallel()
	// Both nil cases must not panic.
	UnRedactToolCall(nil, secret.NewReversibleMap())
	UnRedactToolCall(&ToolCall{}, nil)
}

func TestRedactAndUnRedact_FullRoundTrip(t *testing.T) {
	t.Parallel()
	mapping := secret.NewReversibleMap()
	originalText := "contact us at support@corp.example or 415-555-9999"
	originalArgs := `{"emails":["a@corp.example","b@corp.example"]}`

	// Outbound to the model.
	msgs := []Message{
		{Role: "user", Content: originalText},
		{Role: "assistant", ToolCalls: []ToolCall{{
			ID: "x", Type: "function",
			Function: FnCall{Name: "f", Arguments: originalArgs},
		}}},
	}
	redacted := RedactMessages(msgs, mapping, secret.Policy{})
	if strings.Contains(redacted[0].Content, "support@corp.example") ||
		strings.Contains(redacted[0].Content, "415-555-9999") {
		t.Errorf("outbound leak: %q", redacted[0].Content)
	}

	// Inbound — simulate model echoing the user's redacted text back.
	echoedChunk := redacted[0].Content
	restored := UnRedactChunk(echoedChunk, mapping)
	if restored != originalText {
		t.Errorf("round-trip mismatch:\n got: %q\nwant: %q", restored, originalText)
	}

	// Inbound — simulate model emitting a tool call with redacted args.
	tc := &ToolCall{Function: FnCall{
		Name: "f", Arguments: redacted[1].ToolCalls[0].Function.Arguments,
	}}
	UnRedactToolCall(tc, mapping)
	if tc.Function.Arguments != originalArgs {
		t.Errorf("tool-call round-trip mismatch:\n got: %q\nwant: %q",
			tc.Function.Arguments, originalArgs)
	}
}
