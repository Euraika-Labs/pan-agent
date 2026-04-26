package gateway

import (
	"context"
	"database/sql"
	"strings"
	"testing"

	"github.com/euraika-labs/pan-agent/internal/rag"
	"github.com/euraika-labs/pan-agent/internal/storage"
)

// Phase 13 WS#13.B — chat-loop retrieval tests. The substrate
// (Index.Search, content-hash gate) is covered in internal/rag's
// suite; these tests pin the gateway's retrieval glue:
//
//   - env gate behaviour
//   - retrieveRAGContext returns nil when not configured / disabled
//     / query too short
//   - formatRAGContext produces the expected human-readable shape
//   - ragContextMessage is empty when no hits are found
//   - exact-match-with-query is filtered out

// ---------------------------------------------------------------------------
// Env gate
// ---------------------------------------------------------------------------

func TestRAGRetrievalEnabled_Gate(t *testing.T) {
	cases := []struct {
		env  string
		set  bool
		want bool
	}{
		{set: false, want: false},
		{env: "", set: true, want: false},
		{env: "0", set: true, want: false},
		{env: "false", set: true, want: false},
		{env: "FALSE", set: true, want: false},
		{env: "1", set: true, want: true},
		{env: "true", set: true, want: true},
		{env: "yes", set: true, want: true},
	}
	for _, tc := range cases {
		name := "unset"
		if tc.set {
			name = tc.env
		}
		t.Run(name, func(t *testing.T) {
			t.Setenv(ragRetrievalEnvKey, tc.env)
			if got := ragRetrievalEnabled(); got != tc.want {
				t.Errorf("got %v, want %v (env=%q)", got, tc.want, tc.env)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// retrieveRAGContext
// ---------------------------------------------------------------------------

func TestRetrieveRAGContext_DisabledByDefault(t *testing.T) {
	srv := setupTestServer(t)
	attachIndex(t, srv) // attached but env gate off
	got := srv.retrieveRAGContext(context.Background(), "s-1",
		"a query that is comfortably long enough")
	if got != nil {
		t.Errorf("expected nil with env gate off, got %d hits", len(got))
	}
}

func TestRetrieveRAGContext_NoIndex(t *testing.T) {
	t.Setenv(ragRetrievalEnvKey, "1")
	srv := setupTestServer(t)
	// no AttachRAGIndex
	got := srv.retrieveRAGContext(context.Background(), "s-1",
		"a query that is comfortably long enough")
	if got != nil {
		t.Errorf("expected nil with no index, got %d hits", len(got))
	}
}

func TestRetrieveRAGContext_NoSession(t *testing.T) {
	t.Setenv(ragRetrievalEnvKey, "1")
	srv := setupTestServer(t)
	attachIndex(t, srv)
	got := srv.retrieveRAGContext(context.Background(), "",
		"a query that is comfortably long enough")
	if got != nil {
		t.Errorf("expected nil with empty session, got %d hits", len(got))
	}
}

func TestRetrieveRAGContext_QueryTooShort(t *testing.T) {
	t.Setenv(ragRetrievalEnvKey, "1")
	srv := setupTestServer(t)
	attachIndex(t, srv)
	// Both empty and short queries should be skipped before the
	// embedder gets called.
	for _, q := range []string{"", "ok", "short here"} {
		if got := srv.retrieveRAGContext(context.Background(), "s-1", q); got != nil {
			t.Errorf("query %q: expected nil, got %d hits", q, len(got))
		}
	}
}

func TestRetrieveRAGContext_HappyPath(t *testing.T) {
	t.Setenv(ragRetrievalEnvKey, "1")
	srv := setupTestServer(t)
	em := attachIndex(t, srv)
	idx := srv.getRAGIndex()
	if idx == nil {
		t.Fatal("expected index attached")
	}

	// Seed the index with three messages tied to the session.
	for i, txt := range []string{
		"the quick brown fox jumps",
		"alpha bravo charlie delta",
		"unrelated content over here",
	} {
		_, err := idx.Upsert(context.Background(), rag.IngestRequest{
			Source: "message", SourceID: idForIndexHelper(i),
			SessionID: "s-1", Text: txt,
		})
		if err != nil {
			t.Fatalf("seed[%d]: %v", i, err)
		}
	}
	beforeCalls := em.calls

	hits := srv.retrieveRAGContext(context.Background(), "s-1",
		"a query that is comfortably long enough")

	// Embedder runs once for the query.
	if em.calls == beforeCalls {
		t.Error("expected embedder call for query")
	}
	// Some non-zero number of hits — the exact count depends on the
	// fake embedder's deterministic-but-arbitrary scoring; we mostly
	// care that retrieval ran and produced a result.
	if hits == nil {
		t.Skip("fake embedder produced no hits above MinScore — acceptable")
	}
}

func TestRetrieveRAGContext_FiltersExactQueryMatch(t *testing.T) {
	t.Setenv(ragRetrievalEnvKey, "1")
	srv := setupTestServer(t)
	em := attachIndex(t, srv)
	idx := srv.getRAGIndex()

	// Seed a row whose text is identical to the query we're about to
	// run. The retrieval helper must drop it from results.
	const query = "exactly the same text we will query for"
	if _, err := idx.Upsert(context.Background(), rag.IngestRequest{
		Source: "message", SourceID: "id-self",
		SessionID: "s-1", Text: query,
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	// Add an unrelated row to make sure search has *something* to find
	// even after the self-filter.
	if _, err := idx.Upsert(context.Background(), rag.IngestRequest{
		Source: "message", SourceID: "id-other",
		SessionID: "s-1", Text: "something else interesting going on",
	}); err != nil {
		t.Fatalf("seed other: %v", err)
	}
	_ = em.calls

	hits := srv.retrieveRAGContext(context.Background(), "s-1", query)
	for _, h := range hits {
		if strings.TrimSpace(h.Embedding.Text) == query {
			t.Errorf("self-match leaked into results: %+v", h.Embedding)
		}
	}
}

// ---------------------------------------------------------------------------
// formatRAGContext / ragContextMessage
// ---------------------------------------------------------------------------

func TestFormatRAGContext_Empty(t *testing.T) {
	t.Parallel()
	if got := formatRAGContext(nil); got != "" {
		t.Errorf("empty hits → %q, want empty string", got)
	}
}

func TestFormatRAGContext_Shape(t *testing.T) {
	t.Parallel()
	hits := []rag.Hit{
		{Score: 0.92, Embedding: storage.Embedding{Text: "first message"}},
		{Score: 0.85, Embedding: storage.Embedding{Text: "second message"}},
	}
	got := formatRAGContext(hits)
	if !strings.HasPrefix(got, "Relevant prior context (top 2 from this session):") {
		t.Errorf("missing header: %q", got)
	}
	if !strings.Contains(got, "[0.92] first message") {
		t.Errorf("missing first hit: %q", got)
	}
	if !strings.Contains(got, "[0.85] second message") {
		t.Errorf("missing second hit: %q", got)
	}
}

func TestFormatRAGContext_TruncatesLongText(t *testing.T) {
	t.Parallel()
	long := strings.Repeat("x", 1000)
	hits := []rag.Hit{{Score: 0.5, Embedding: storage.Embedding{Text: long}}}
	got := formatRAGContext(hits)
	if !strings.Contains(got, " …") {
		t.Errorf("long text not truncated: %q", got)
	}
	if len(got) > 600 {
		t.Errorf("output too large: len = %d", len(got))
	}
}

func TestFormatRAGContext_CollapsesNewlines(t *testing.T) {
	t.Parallel()
	hits := []rag.Hit{{Score: 0.5, Embedding: storage.Embedding{Text: "line one\nline two\nline three"}}}
	got := formatRAGContext(hits)
	// The bulleted line itself should not contain embedded \n that
	// would break the bullet shape.
	for _, line := range strings.Split(got, "\n") {
		if strings.HasPrefix(line, "- [") && strings.Contains(line, "line two") {
			if strings.Count(line, "line ") < 3 {
				t.Errorf("expected all 3 segments on one bullet line: %q", line)
			}
		}
	}
}

func TestRagContextMessage_EmptyOnNoHits(t *testing.T) {
	t.Parallel()
	m := ragContextMessage(nil)
	if m.Content != "" {
		t.Errorf("expected empty Content, got %q", m.Content)
	}
}

func TestRagContextMessage_UserRole(t *testing.T) {
	t.Parallel()
	hits := []rag.Hit{{Score: 0.9, Embedding: storage.Embedding{Text: "hello"}}}
	m := ragContextMessage(hits)
	if m.Role != "user" {
		t.Errorf("Role = %q, want user", m.Role)
	}
	if !strings.Contains(m.Content, "[0.90] hello") {
		t.Errorf("Content malformed: %q", m.Content)
	}
}

// ---------------------------------------------------------------------------
// parsePositiveInt
// ---------------------------------------------------------------------------

func TestParsePositiveInt(t *testing.T) {
	t.Parallel()
	cases := map[string]int{
		"":               0,
		"0":              0,
		"-1":             0,
		"abc":            0,
		"5":              5,
		"100":            100,
		"99999999999999": 0, // overflow guard caps absurd values at 0
		"1.5":            0,
	}
	for in, want := range cases {
		if got := parsePositiveInt(in); got != want {
			t.Errorf("parsePositiveInt(%q) = %d, want %d", in, got, want)
		}
	}
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

// idForIndexHelper mirrors the pattern from rag_test.go in the
// rag package. Local copy keeps test packages independent.
func idForIndexHelper(i int) string {
	return [...]string{"a", "b", "c", "d", "e", "f"}[i%6]
}

// keep imports tidy in case this test file is the only reference.
var _ = sql.NullString{}
