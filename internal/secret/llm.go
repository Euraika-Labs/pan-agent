package secret

import (
	"fmt"
	"regexp"
)

// Phase 13 WS#13.G — LLM-bound prompt redaction.
//
// The HMAC pipeline (Redact / RedactWithMap) is the right tool for
// storage paths: tokens are deterministic across calls, so a receipt
// referencing `<REDACTED:EMAIL:a1b2c3>` correlates with another
// receipt redacting the same email. That's exactly what the receipts
// + task_events + plan_json columns need.
//
// For LLM consumption the requirements differ:
//
//   - Tokens must be human-readable so the model can reason about them
//     ("respond to the first email" vs "respond to <REDACTED:EMAIL:a1b2c3>").
//     Counter-based tokens (`<REDACTED:EMAIL:1>`) read much better.
//   - Tokens must NOT be deterministic across conversations — a token
//     in conversation A leaking into conversation B's mapping would
//     un-redact to the wrong plaintext. Per-mapping counters reset
//     and are scoped to a single ReversibleMap.
//   - Identical plaintext within ONE mapping gets the SAME token so
//     the model sees consistency turn-to-turn (otherwise "the email
//     I mentioned earlier" drifts from `:1` to `:7`).
//   - The mapping must be reversible — UnRedactResponse turns assistant
//     output that mentions tokens back into plaintext for the user, so
//     redaction is invisible at the UX layer.
//
// Threat model — closed:
//   - Provider-side prompt logging (provider only sees tokens).
//   - Network-tap exposure (TLS terminates at the provider).
//   - Cross-conversation correlation by the provider (counter tokens
//     are per-mapping, deterministic ONLY within one mapping).
//
// Threat model — explicitly NOT closed:
//   - Provider runtime exposure to the model itself (the model still
//     reasons about plaintext indirectly via counter tokens; we only
//     change the wire/log shape).
//   - Provider log retention (procurement / contract control).
//   - RAG embedding privacy (out of scope; embeddings stay local).
//   - Side channels (token count, response time, response length).

// Policy controls which categories RedactForLLM redacts. An empty /
// zero-value Policy redacts every built-in category (default-on for
// safety). Callers that want narrower coverage pass a non-nil
// Categories map listing the specific categories to act on.
type Policy struct {
	// Categories — when non-nil, only categories whose key is true
	// are redacted. nil/empty = redact everything (the safe default).
	Categories map[Category]bool
}

// allows reports whether category c should be redacted under p.
func (p Policy) allows(c Category) bool {
	if len(p.Categories) == 0 {
		return true
	}
	return p.Categories[c]
}

// ReversibleMap is the per-conversation side-table mapping LLM-format
// redaction tokens back to their original plaintext. Allocated by the
// caller via NewReversibleMap, populated by RedactForLLM, consumed by
// UnRedactResponse.
//
// Lifetime is intentionally short: the mapping holds plaintext in
// memory, so callers must drop the pointer once the request stream
// closes. Never persist a ReversibleMap; never share one across
// conversations.
type ReversibleMap struct {
	byToken    map[string]string // token text → original plaintext
	byCategory map[Category]int  // category → highest counter assigned
	// dedupKey holds (category, plaintext) → token so identical
	// plaintexts within one mapping reuse a token.
	dedupKey map[string]string
}

// NewReversibleMap allocates an empty mapping. Callers typically do
// `mapping := secret.NewReversibleMap(); defer mapping.Clear()` so
// plaintext doesn't outlive the request.
func NewReversibleMap() *ReversibleMap {
	return &ReversibleMap{
		byToken:    make(map[string]string),
		byCategory: make(map[Category]int),
		dedupKey:   make(map[string]string),
	}
}

// Size returns the number of distinct tokens in the mapping. Useful
// for the redaction-diff UI banner ("3 secrets redacted this turn").
func (m *ReversibleMap) Size() int {
	if m == nil {
		return 0
	}
	return len(m.byToken)
}

// Lookup returns the plaintext for token, or ("", false) if the token
// is not in this mapping.
func (m *ReversibleMap) Lookup(token string) (string, bool) {
	if m == nil {
		return "", false
	}
	plain, ok := m.byToken[token]
	return plain, ok
}

// Categories returns the set of categories that have at least one
// token in the mapping. Order is unspecified.
func (m *ReversibleMap) Categories() []Category {
	if m == nil {
		return nil
	}
	out := make([]Category, 0, len(m.byCategory))
	for c := range m.byCategory {
		out = append(out, c)
	}
	return out
}

// Clear zeroes the mapping. Call when the request scope ends so
// plaintext doesn't linger in memory longer than necessary.
func (m *ReversibleMap) Clear() {
	if m == nil {
		return
	}
	for k := range m.byToken {
		delete(m.byToken, k)
	}
	for k := range m.byCategory {
		delete(m.byCategory, k)
	}
	for k := range m.dedupKey {
		delete(m.dedupKey, k)
	}
}

// RedactForLLM rewrites text by replacing every detected secret with a
// per-mapping counter token of the shape `<REDACTED:CATEGORY:N>`.
//
// Identical plaintext within one mapping reuses the same token —
// "respond to <REDACTED:EMAIL:1> from corp.example" stays consistent
// even if the same email appears five times in the prompt history.
//
// `mapping` must be non-nil. Multiple calls with the same mapping
// continue the per-category counter, so iterating over a Message slice
// produces a globally-consistent token-set across the whole prompt.
//
// On redaction-subsystem init failure (rare — keyring fundamentally
// unavailable), RedactForLLM passes text through unchanged. Callers
// that want fail-closed behaviour should check Ready() first and
// refuse to send the prompt; the default fail-open posture is chosen
// to avoid nuking the prompt over a transient failure.
func RedactForLLM(text string, mapping *ReversibleMap, policy Policy) string {
	if mapping == nil {
		// Defensive — callers that misuse this should crash visibly
		// rather than silently leak plaintext. Phase 13 design
		// prefers explicit failure here.
		panic("secret: RedactForLLM called with nil mapping")
	}

	global.initKey()
	global.mu.RLock()
	initErr := global.keyInitErr
	patterns := global.patterns
	global.mu.RUnlock()
	if initErr != nil {
		// Fail-open: passing the un-redacted prompt through is no
		// worse than the pre-WS#13.G state (which always sent
		// un-redacted prompts to the provider). A subsequent Ready()
		// check at the gateway can refuse new requests.
		return text
	}

	// Pass 1: protect negative-regex matches (mirrors HMAC pipeline
	// — the same negative-regex policy applies regardless of which
	// token format we end up emitting).
	working, guards := protectNegatives(text, patterns)

	// Pass 2: per-classifier emit using counter tokens, with per-
	// (category, plaintext) deduplication so identical secrets reuse
	// a token within this mapping.
	for _, c := range patterns {
		c := c
		if !policy.allows(c.category) {
			continue
		}
		working = applyClassifier(working, c, func(span string) string {
			key := string(c.category) + "\x00" + span
			if existing, ok := mapping.dedupKey[key]; ok {
				return existing
			}
			mapping.byCategory[c.category]++
			n := mapping.byCategory[c.category]
			token := fmt.Sprintf("<REDACTED:%s:%d>", c.category, n)
			mapping.byToken[token] = span
			mapping.dedupKey[key] = token
			return token
		})
	}

	// Pass 3: restore protected spans.
	return restoreProtected(working, guards)
}

// llmTokenRe matches the per-mapping counter-token shape produced by
// RedactForLLM. The HMAC pipeline emits 6-char hex suffixes which
// don't match this pattern, so UnRedactResponse won't touch HMAC
// tokens that may have leaked into a response.
var llmTokenRe = regexp.MustCompile(`<REDACTED:[A-Z_]+:\d+>`)

// UnRedactResponse replaces every counter-token literal in text with
// its original plaintext from the mapping. Unknown tokens (not in the
// mapping) are left as literal text — fail-closed: we never invent a
// substitution.
//
// Designed for assistant-message post-processing: the model may quote
// a token verbatim when discussing the redacted entity, and the user
// expects to see the original value rendered. Per Phase 13 D6 / Q4,
// callers gate this on `cfg.PromptRedaction.ShowRedacted == false`
// (the default) — when ShowRedacted is true, callers leave the token
// visible.
//
// `mapping` may be nil; in that case text is returned unchanged.
func UnRedactResponse(text string, mapping *ReversibleMap) string {
	if mapping == nil || mapping.Size() == 0 {
		return text
	}
	return llmTokenRe.ReplaceAllStringFunc(text, func(token string) string {
		if plain, ok := mapping.byToken[token]; ok {
			return plain
		}
		return token
	})
}
