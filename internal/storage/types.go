package storage

// Session represents a conversation session stored in the database.
type Session struct {
	ID           string
	Source       string
	StartedAt    int64
	EndedAt      *int64
	MessageCount int
	Model        string
	Title        string
}

// Message represents a single message within a session.
type Message struct {
	ID        int64
	SessionID string
	Role      string
	Content   string
	Timestamp int64
}

// SearchResult is returned by SearchSessions and includes a content snippet
// produced by the FTS5 snippet() function.
type SearchResult struct {
	SessionID    string
	Title        string
	StartedAt    int64
	Source       string
	MessageCount int
	Model        string
	Snippet      string
}

// SkillUsage records a single invocation of a skill by a session.
// Used for usage tracking and feeding the curator agent.
type SkillUsage struct {
	ID          int64  `json:"id"`
	SessionID   string `json:"session_id"`
	SkillID     string `json:"skill_id"` // "<category>/<name>"
	MessageID   int64  `json:"message_id,omitempty"`
	UsedAt      int64  `json:"used_at"`
	Outcome     string `json:"outcome"` // "success"|"error"|"abandoned"|"unknown"
	ContextHint string `json:"context_hint,omitempty"`
}
