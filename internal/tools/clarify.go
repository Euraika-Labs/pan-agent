package tools

import (
	"context"
	"encoding/json"
	"fmt"
)

// ClarifyTool surfaces a clarification question to the user via the agent loop.
type ClarifyTool struct{}

// clarifyParams is the JSON-decoded parameter bag for ClarifyTool.
type clarifyParams struct {
	Question string `json:"question"`
}

func (ClarifyTool) Name() string { return "clarify" }

func (ClarifyTool) Description() string {
	return "Ask the user a clarification question before proceeding. " +
		"Use this when the task is ambiguous and proceeding without clarification " +
		"could lead to incorrect results. The agent loop surfaces the question to the user."
}

func (ClarifyTool) Parameters() json.RawMessage {
	return json.RawMessage(`{
  "type": "object",
  "required": ["question"],
  "properties": {
    "question": {
      "type": "string",
      "description": "The clarification question to present to the user."
    }
  }
}`)
}

func (ClarifyTool) Execute(_ context.Context, params json.RawMessage) (*Result, error) {
	var p clarifyParams
	if err := json.Unmarshal(params, &p); err != nil {
		return &Result{Error: fmt.Sprintf("invalid parameters: %v", err)}, nil
	}
	if p.Question == "" {
		return &Result{Error: "question must not be empty"}, nil
	}
	return &Result{Output: fmt.Sprintf("CLARIFICATION_NEEDED: %s", p.Question)}, nil
}

// Ensure ClarifyTool satisfies the Tool interface at compile time.
var _ Tool = ClarifyTool{}

func init() {
	Register(ClarifyTool{})
}
