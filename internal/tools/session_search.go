package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/euraika-labs/pan-agent/internal/paths"
	"github.com/euraika-labs/pan-agent/internal/storage"
)

// SessionSearchTool performs a full-text search over historical chat sessions
// stored in the pan-agent SQLite database.
type SessionSearchTool struct{}

// sessionSearchParams is the JSON-decoded parameter bag for SessionSearchTool.
type sessionSearchParams struct {
	Query string `json:"query"`
	Limit int    `json:"limit,omitempty"`
}

const sessionSearchDefaultLimit = 20

func (SessionSearchTool) Name() string { return "session_search" }

func (SessionSearchTool) Description() string {
	return "Full-text search over historical pan-agent chat sessions. " +
		"Returns matching sessions with a content snippet showing where the query was found."
}

func (SessionSearchTool) Parameters() json.RawMessage {
	return json.RawMessage(`{
  "type": "object",
  "required": ["query"],
  "properties": {
    "query": {
      "type": "string",
      "description": "Search query. Words are prefix-matched across all session messages."
    },
    "limit": {
      "type": "integer",
      "description": "Maximum number of sessions to return. Defaults to 20."
    }
  }
}`)
}

func (SessionSearchTool) Execute(_ context.Context, params json.RawMessage) (*Result, error) {
	var p sessionSearchParams
	if err := json.Unmarshal(params, &p); err != nil {
		return &Result{Error: fmt.Sprintf("invalid parameters: %v", err)}, nil
	}
	if p.Query == "" {
		return &Result{Error: "query must not be empty"}, nil
	}
	if p.Limit <= 0 {
		p.Limit = sessionSearchDefaultLimit
	}

	db, err := storage.Open(paths.StateDB())
	if err != nil {
		return &Result{Error: fmt.Sprintf("open database: %v", err)}, nil
	}
	defer db.Close()

	results, err := db.SearchSessions(p.Query, p.Limit)
	if err != nil {
		return &Result{Error: fmt.Sprintf("search: %v", err)}, nil
	}

	if len(results) == 0 {
		return &Result{Output: "no sessions found"}, nil
	}

	var sb strings.Builder
	fmt.Fprintf(&sb, "%d session(s) found:\n\n", len(results))
	for _, r := range results {
		title := r.Title
		if title == "" {
			title = "(untitled)"
		}
		ts := time.UnixMilli(r.StartedAt).Format("2006-01-02 15:04")
		fmt.Fprintf(&sb, "Session: %s\n", r.SessionID)
		fmt.Fprintf(&sb, "  Title:    %s\n", title)
		fmt.Fprintf(&sb, "  Date:     %s\n", ts)
		fmt.Fprintf(&sb, "  Source:   %s\n", r.Source)
		fmt.Fprintf(&sb, "  Messages: %d\n", r.MessageCount)
		if r.Model != "" {
			fmt.Fprintf(&sb, "  Model:    %s\n", r.Model)
		}
		fmt.Fprintf(&sb, "  Snippet:  %s\n\n", r.Snippet)
	}
	return &Result{Output: sb.String()}, nil
}

// Ensure SessionSearchTool satisfies the Tool interface at compile time.
var _ Tool = SessionSearchTool{}

func init() {
	Register(SessionSearchTool{})
}
