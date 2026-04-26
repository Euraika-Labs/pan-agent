package llm

import (
	"github.com/euraika-labs/pan-agent/internal/secret"
)

// Phase 13 WS#13.G — LLM-client wiring for prompt-history redaction.
//
// The redaction primitives live in internal/secret. This file is the
// thin bridge that knows about llm.Message / llm.StreamEvent shapes so
// the gateway's chat loop can pass redacted prompts to the provider
// and un-redact streaming responses before they reach the user.
//
// The contract is asymmetric on purpose:
//
//   Outbound:  Message.Content + ToolCalls[].Function.Arguments are
//              rewritten through secret.RedactForLLM, sharing one
//              ReversibleMap for the whole turn so identical
//              plaintexts get the same counter token across the
//              system prompt, history, and the new user turn.
//
//   Inbound:   chunk content from ChatStream gets the counter tokens
//              swapped back to plaintext (visible to the user) and
//              the same swap runs on tool_call argument bodies (so
//              the tool executor sees plaintext, not the placeholder).
//
// Mapping lifetime is one chat turn — caller allocates with
// NewReversibleMap, passes through Redact*/UnRedact*, drops the
// pointer when the stream closes (Clear() optional).

// RedactMessages returns a copy of msgs with every redactable string
// rewritten through secret.RedactForLLM, sharing the supplied mapping
// so identical plaintexts across the slice reuse the same counter
// token. Tool-call arguments are JSON strings and we redact them as
// raw text — the model sees `{"to":"<REDACTED:EMAIL:1>"}` which it
// parses without trouble; the un-redact pass on the response converts
// back to plaintext before the tool runs.
//
// Original msgs is not mutated. Pass-through fields (Role, Name,
// ToolCallID, ToolCall.ID, ToolCall.Type) carry over unchanged.
//
// mapping must be non-nil. policy follows secret.Policy semantics
// (zero-value redacts every category; non-nil Categories restricts).
func RedactMessages(msgs []Message, mapping *secret.ReversibleMap, policy secret.Policy) []Message {
	if mapping == nil {
		panic("llm: RedactMessages called with nil mapping")
	}
	if len(msgs) == 0 {
		return msgs
	}
	out := make([]Message, len(msgs))
	for i, m := range msgs {
		nm := Message{
			Role:       m.Role,
			Content:    secret.RedactForLLM(m.Content, mapping, policy),
			Name:       m.Name,
			ToolCallID: m.ToolCallID,
		}
		if len(m.ToolCalls) > 0 {
			nm.ToolCalls = make([]ToolCall, len(m.ToolCalls))
			for j, tc := range m.ToolCalls {
				nm.ToolCalls[j] = ToolCall{
					ID:   tc.ID,
					Type: tc.Type,
					Function: FnCall{
						Name:      tc.Function.Name,
						Arguments: secret.RedactForLLM(tc.Function.Arguments, mapping, policy),
					},
				}
			}
		}
		out[i] = nm
	}
	return out
}

// UnRedactChunk substitutes counter tokens in a streaming chunk with
// the plaintext from mapping. Designed to run on every "chunk"
// StreamEvent before it's relayed to the client.
//
// Streaming caveat: when the provider splits a token literal across
// two chunks (e.g. "…<REDACT" + "ED:EMAIL:1> …"), neither half can be
// un-redacted in isolation — the regex won't match. Callers that need
// strict per-chunk fidelity should buffer until either a full token
// is visible or `done` arrives. For practical use the model rarely
// splits a 20-byte token across deltas, and the closing line passes
// through UnRedactString one final time on the assembled message,
// so the persisted/displayed text always renders correctly.
//
// mapping may be nil; in that case content is returned unchanged.
func UnRedactChunk(content string, mapping *secret.ReversibleMap) string {
	return secret.UnRedactResponse(content, mapping)
}

// UnRedactToolCall rewrites the Function.Arguments string of tc in
// place so the tool executor receives plaintext instead of counter
// tokens. The model sees the redacted token shape, decides on a tool
// invocation, the gateway un-redacts before the tool runs.
//
// Idempotent — calling twice with the same mapping produces the same
// result (UnRedactResponse leaves unknown tokens alone).
//
// mapping may be nil; in that case tc is left untouched.
func UnRedactToolCall(tc *ToolCall, mapping *secret.ReversibleMap) {
	if tc == nil || mapping == nil {
		return
	}
	tc.Function.Arguments = secret.UnRedactResponse(tc.Function.Arguments, mapping)
}
