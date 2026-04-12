package storage

import (
	"path/filepath"
	"testing"
)

// openTestDB opens a fresh in-memory-equivalent SQLite database in a temp dir.
// Using a real file in t.TempDir() because ":memory:" behaves oddly with
// MaxOpenConns(1) and the modernc driver.
func openTestDB(t *testing.T) *DB {
	t.Helper()
	dir := t.TempDir()
	db, err := Open(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

// ---------------------------------------------------------------------------
// CreateSession
// ---------------------------------------------------------------------------

func TestCreateSession(t *testing.T) {
	db := openTestDB(t)

	s, err := db.CreateSession("gpt-4")
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	if s.ID == "" {
		t.Error("CreateSession: ID is empty")
	}
	if s.Source != "pan-agent" {
		t.Errorf("Source = %q, want %q", s.Source, "pan-agent")
	}
	if s.Model != "gpt-4" {
		t.Errorf("Model = %q, want %q", s.Model, "gpt-4")
	}
	if s.StartedAt == 0 {
		t.Error("StartedAt is zero")
	}
}

func TestCreateSessionMultiple(t *testing.T) {
	db := openTestDB(t)

	s1, _ := db.CreateSession("m1")
	s2, _ := db.CreateSession("m2")
	if s1.ID == s2.ID {
		t.Error("two sessions should have different IDs")
	}
}

// ---------------------------------------------------------------------------
// AddMessage
// ---------------------------------------------------------------------------

func TestAddMessage(t *testing.T) {
	db := openTestDB(t)

	s, _ := db.CreateSession("test-model")
	if err := db.AddMessage(s.ID, "user", "Hello, world!"); err != nil {
		t.Fatalf("AddMessage: %v", err)
	}
}

func TestAddMessageIncrementsCount(t *testing.T) {
	db := openTestDB(t)

	s, _ := db.CreateSession("test-model")
	_ = db.AddMessage(s.ID, "user", "msg 1")
	_ = db.AddMessage(s.ID, "assistant", "reply")

	sessions, err := db.ListSessions(10, 0)
	if err != nil {
		t.Fatalf("ListSessions: %v", err)
	}
	if len(sessions) == 0 {
		t.Fatal("expected at least one session")
	}
	if sessions[0].MessageCount != 2 {
		t.Errorf("MessageCount = %d, want 2", sessions[0].MessageCount)
	}
}

func TestGetMessages(t *testing.T) {
	db := openTestDB(t)

	s, _ := db.CreateSession("test-model")
	_ = db.AddMessage(s.ID, "user", "question")
	_ = db.AddMessage(s.ID, "assistant", "answer")

	msgs, err := db.GetMessages(s.ID)
	if err != nil {
		t.Fatalf("GetMessages: %v", err)
	}
	if len(msgs) != 2 {
		t.Fatalf("want 2 messages, got %d", len(msgs))
	}
	if msgs[0].Role != "user" || msgs[0].Content != "question" {
		t.Errorf("msgs[0] = {%q %q}, want {user question}", msgs[0].Role, msgs[0].Content)
	}
	if msgs[1].Role != "assistant" || msgs[1].Content != "answer" {
		t.Errorf("msgs[1] = {%q %q}, want {assistant answer}", msgs[1].Role, msgs[1].Content)
	}
}

// ---------------------------------------------------------------------------
// ListSessions
// ---------------------------------------------------------------------------

func TestListSessions(t *testing.T) {
	db := openTestDB(t)

	_, _ = db.CreateSession("m1")
	_, _ = db.CreateSession("m2")
	_, _ = db.CreateSession("m3")

	sessions, err := db.ListSessions(10, 0)
	if err != nil {
		t.Fatalf("ListSessions: %v", err)
	}
	if len(sessions) != 3 {
		t.Errorf("want 3 sessions, got %d", len(sessions))
	}
}

func TestListSessionsPagination(t *testing.T) {
	db := openTestDB(t)

	for i := 0; i < 5; i++ {
		_, _ = db.CreateSession("model")
	}

	page1, _ := db.ListSessions(3, 0)
	if len(page1) != 3 {
		t.Errorf("page1: want 3, got %d", len(page1))
	}

	page2, _ := db.ListSessions(3, 3)
	if len(page2) != 2 {
		t.Errorf("page2: want 2, got %d", len(page2))
	}
}

func TestListSessionsEmpty(t *testing.T) {
	db := openTestDB(t)

	sessions, err := db.ListSessions(10, 0)
	if err != nil {
		t.Fatalf("ListSessions on empty db: %v", err)
	}
	if len(sessions) != 0 {
		t.Errorf("want 0 sessions, got %d", len(sessions))
	}
}

// ---------------------------------------------------------------------------
// SearchSessions
// ---------------------------------------------------------------------------

func TestSearchSessions(t *testing.T) {
	db := openTestDB(t)

	s, _ := db.CreateSession("model")
	_ = db.AddMessage(s.ID, "user", "the quick brown fox")
	_ = db.AddMessage(s.ID, "assistant", "jumps over the lazy dog")

	results, err := db.SearchSessions("quick fox", 10)
	if err != nil {
		t.Fatalf("SearchSessions: %v", err)
	}
	if len(results) == 0 {
		t.Error("expected at least one search result")
	}
	if results[0].SessionID != s.ID {
		t.Errorf("SessionID = %q, want %q", results[0].SessionID, s.ID)
	}
}

func TestSearchSessionsNoMatch(t *testing.T) {
	db := openTestDB(t)

	s, _ := db.CreateSession("model")
	_ = db.AddMessage(s.ID, "user", "hello world")

	results, err := db.SearchSessions("zyxwvutsrqponmlkjihgfedcba", 10)
	if err != nil {
		t.Fatalf("SearchSessions: %v", err)
	}
	if len(results) != 0 {
		t.Errorf("expected 0 results for non-existent term, got %d", len(results))
	}
}

func TestSearchSessionsEmptyQuery(t *testing.T) {
	db := openTestDB(t)

	results, err := db.SearchSessions("", 10)
	if err != nil {
		t.Fatalf("SearchSessions empty query: %v", err)
	}
	if results != nil {
		t.Errorf("expected nil for empty query, got %v", results)
	}
}

// ---------------------------------------------------------------------------
// UpdateTitle
// ---------------------------------------------------------------------------

func TestUpdateTitle(t *testing.T) {
	db := openTestDB(t)

	s, _ := db.CreateSession("model")
	if err := db.UpdateTitle(s.ID, "My Test Session"); err != nil {
		t.Fatalf("UpdateTitle: %v", err)
	}

	sessions, _ := db.ListSessions(10, 0)
	if len(sessions) == 0 {
		t.Fatal("no sessions returned")
	}
	if sessions[0].Title != "My Test Session" {
		t.Errorf("Title = %q, want %q", sessions[0].Title, "My Test Session")
	}
}

// ---------------------------------------------------------------------------
// sanitizeFTS (internal helper)
// ---------------------------------------------------------------------------

func TestSanitizeFTS(t *testing.T) {
	cases := []struct {
		input string
		want  string
	}{
		{"hello world", `"hello"* "world"*`},
		{"", ""},
		{"  spaces  ", `"spaces"*`},
		{`quote"injection`, `"quoteinjection"*`},
	}
	for _, c := range cases {
		got := sanitizeFTS(c.input)
		if got != c.want {
			t.Errorf("sanitizeFTS(%q) = %q, want %q", c.input, got, c.want)
		}
	}
}
