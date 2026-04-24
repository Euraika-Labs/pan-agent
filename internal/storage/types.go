package storage

// Session represents a conversation session stored in the database.
//
// JSON tags use camelCase to match the React UI's field expectations (see
// desktop/src/screens/Sessions/Sessions.tsx). Without these, Go's default
// marshaller emits PascalCase ("SessionID"), which the UI silently treats
// as undefined — the root cause of the "undefined session card" and missing
// React key warnings seen pre-audit.
type Session struct {
	ID              string  `json:"id"`
	Source          string  `json:"source"`
	StartedAt       int64   `json:"startedAt"`
	EndedAt         *int64  `json:"endedAt,omitempty"`
	MessageCount    int     `json:"messageCount"`
	Model           string  `json:"model"`
	Title           string  `json:"title"`
	TokenBudgetUsed int     `json:"tokenBudgetUsed"`
	TokenBudgetCap  int     `json:"tokenBudgetCap"`
	CostUsedUSD     float64 `json:"costUsedUsd"`
	CostCapUSD      float64 `json:"costCapUsd"`
}

// Message represents a single message within a session.
type Message struct {
	ID        int64  `json:"id"`
	SessionID string `json:"sessionId"`
	Role      string `json:"role"`
	Content   string `json:"content"`
	Timestamp int64  `json:"timestamp"`
}

// SearchResult is returned by SearchSessions and includes a content snippet
// produced by the FTS5 snippet() function.
type SearchResult struct {
	SessionID    string `json:"sessionId"`
	Title        string `json:"title"`
	StartedAt    int64  `json:"startedAt"`
	Source       string `json:"source"`
	MessageCount int    `json:"messageCount"`
	Model        string `json:"model"`
	Snippet      string `json:"snippet"`
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
