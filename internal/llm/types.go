package llm

import "encoding/json"

// Message represents a single chat message exchanged with the model.
type Message struct {
	Role       string     `json:"role"` // "system", "user", "assistant", "tool"
	Content    string     `json:"content"`
	Name       string     `json:"name,omitempty"`
	ToolCallID string     `json:"tool_call_id,omitempty"`
	ToolCalls  []ToolCall `json:"tool_calls,omitempty"`
}

// ToolCall is a request from the model to invoke a tool.
type ToolCall struct {
	ID       string `json:"id"`
	Type     string `json:"type"` // "function"
	Function FnCall `json:"function"`
}

// FnCall holds the name and JSON-encoded arguments of a function call.
type FnCall struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"` // JSON string
}

// StreamEvent is emitted on the channel returned by ChatStream.
// Only the fields relevant to the event Type are populated.
type StreamEvent struct {
	// Type is one of: "chunk", "tool_call", "done", "error", "usage"
	Type string

	// chunk
	Content string

	// tool_call
	ToolCall *ToolCall

	// usage
	Usage *Usage

	// error
	Error string

	// done
	SessionID string
}

// Usage reports token consumption for a request.
type Usage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

// ToolDef describes a tool that the model may call.
type ToolDef struct {
	Type     string    `json:"type"` // "function"
	Function ToolFnDef `json:"function"`
}

// ToolFnDef holds the schema for a single function tool.
type ToolFnDef struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Parameters  json.RawMessage `json:"parameters"`
}

// ModelInfo holds the identifier and metadata returned by the /models endpoint.
type ModelInfo struct {
	ID      string `json:"id"`
	Object  string `json:"object"`
	Created int64  `json:"created"`
	OwnedBy string `json:"owned_by"`
}
