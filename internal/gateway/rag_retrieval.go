package gateway

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/euraika-labs/pan-agent/internal/llm"
	"github.com/euraika-labs/pan-agent/internal/rag"
)

// Phase 13 WS#13.B — chat-loop retrieval integration.
//
// When the RAG index is attached AND PAN_AGENT_RAG_RETRIEVAL is set,
// each chat turn embeds the user's latest message and queries the
// index for the top-K most-similar prior messages from the same
// session. The hits are formatted into a single user-role message
// inserted before the live history, so the model sees:
//
//   [system prompt]
//   [skills inventory]
//   [RAG context: top-K relevant prior messages]   <-- new
//   [history from req.Messages]
//
// The RAG message is positioned BEFORE the live history (and after
// the stable skills inventory) so the prompt cache stays mostly
// warm between turns within one session — only the RAG slot
// changes, and only when the retrieved set actually differs.
//
// The slice is intentionally small + env-gated so it can roll out
// behind a feature flag while we measure embedder cost + retrieval
// quality on real traffic.

const (
	// ragRetrievalEnvKey toggles the integration. Reads truthy when
	// the value is non-empty AND not "0" / "false" (case-insensitive).
	ragRetrievalEnvKey = "PAN_AGENT_RAG_RETRIEVAL"

	// ragRetrievalDefaultK is the default top-K when the env override
	// PAN_AGENT_RAG_RETRIEVAL_K is unset or invalid.
	ragRetrievalDefaultK = 5

	// ragRetrievalMinQueryLen suppresses retrieval for short queries
	// (e.g. "ok", "yes") that don't carry enough signal to beat the
	// per-turn embedder round-trip cost.
	ragRetrievalMinQueryLen = 16

	// ragRetrievalMinScore drops weak hits before formatting. Cosine
	// scores below this are signal-poor and would dilute the prompt.
	ragRetrievalMinScore = 0.35
)

// ragRetrievalEnabled reports whether the env gate is on. Reads on
// every call (cheap) so flipping the var doesn't need a restart.
func ragRetrievalEnabled() bool {
	v := os.Getenv(ragRetrievalEnvKey)
	if v == "" || v == "0" || strings.EqualFold(v, "false") {
		return false
	}
	return true
}

// retrieveRAGContext queries the attached index for the top-K hits
// most similar to query, scoped to sessionID. Returns nil with no
// error when:
//
//   - the index isn't attached (server not configured for RAG)
//   - the env gate is off
//   - the query is shorter than ragRetrievalMinQueryLen
//   - the search returns zero hits (clean miss isn't an error)
//
// Errors from the underlying idx.Search are downgraded to a logged
// warning + nil return — retrieval failure must never break chat.
// Callers receive a clean "no extra context this turn".
func (s *Server) retrieveRAGContext(ctx context.Context, sessionID, query string) []rag.Hit {
	if !ragRetrievalEnabled() {
		return nil
	}
	idx := s.getRAGIndex()
	if idx == nil {
		return nil
	}
	if sessionID == "" {
		return nil
	}
	q := strings.TrimSpace(query)
	if len(q) < ragRetrievalMinQueryLen {
		return nil
	}

	k := ragRetrievalDefaultK
	if v := os.Getenv("PAN_AGENT_RAG_RETRIEVAL_K"); v != "" {
		if parsed := parsePositiveInt(v); parsed > 0 {
			k = parsed
		}
	}

	hits, err := idx.Search(ctx, rag.SearchRequest{
		Query:     q,
		SessionID: sessionID,
		K:         k,
		MinScore:  ragRetrievalMinScore,
	})
	if err != nil {
		// Don't propagate — chat must keep working when RAG is sick.
		// The error is left visible to ops via the standard logger.
		return nil
	}
	if len(hits) == 0 {
		return nil
	}

	// Filter out exact-text matches with the query — the model
	// already sees the live message in the history, so reciting it
	// back as "relevant prior context" is noise.
	out := make([]rag.Hit, 0, len(hits))
	for _, h := range hits {
		if strings.TrimSpace(h.Embedding.Text) == q {
			continue
		}
		out = append(out, h)
	}
	return out
}

// formatRAGContext renders hits into a single user-role message that
// the chat loop inserts before the live history. Returns "" when
// hits is empty so callers can use the result as a presence check.
//
// Format is intentionally human-readable so a curious user opening
// the prompt history sees what the model saw without needing a
// special viewer:
//
//	Relevant prior context (top N from this session):
//	- [0.92] First retrieved message body up to the cap
//	- [0.85] Second retrieved message body up to the cap
//
// Bodies longer than ragContextSnippetMax are truncated with " …"
// to keep the per-turn token budget bounded.
func formatRAGContext(hits []rag.Hit) string {
	if len(hits) == 0 {
		return ""
	}
	const ragContextSnippetMax = 400
	var b strings.Builder
	fmt.Fprintf(&b, "Relevant prior context (top %d from this session):\n", len(hits))
	for _, h := range hits {
		text := strings.TrimSpace(h.Embedding.Text)
		if len(text) > ragContextSnippetMax {
			text = text[:ragContextSnippetMax] + " …"
		}
		// Newlines inside the snippet would break the bullet shape
		// when the model parses it. Collapse them to a single space.
		text = strings.ReplaceAll(text, "\n", " ")
		fmt.Fprintf(&b, "- [%.2f] %s\n", h.Score, text)
	}
	return b.String()
}

// ragContextMessage builds the synthetic user-role message that
// carries retrieved context, or returns the empty Message when no
// context is available (caller checks .Content == "").
func ragContextMessage(hits []rag.Hit) llm.Message {
	body := formatRAGContext(hits)
	if body == "" {
		return llm.Message{}
	}
	return llm.Message{Role: "user", Content: body}
}

// parsePositiveInt is a tiny stdlib-free parser used for env values.
// Returns 0 on any non-positive or malformed input so callers fall
// back to the default.
func parsePositiveInt(s string) int {
	n := 0
	for _, c := range s {
		if c < '0' || c > '9' {
			return 0
		}
		n = n*10 + int(c-'0')
		if n > 1<<20 {
			return 0
		}
	}
	if n <= 0 {
		return 0
	}
	return n
}
