package tools

import (
	"context"
	"encoding/json"
	"fmt"
)

// DelegationTool spawns a sub-agent to handle a delegated task.
// In v0.1 this is a placeholder; full sub-agent delegation is planned for v0.2.
type DelegationTool struct{}

// delegationParams is the JSON-decoded parameter bag for DelegationTool.
type delegationParams struct {
	Task  string `json:"task"`
	Model string `json:"model,omitempty"`
}

func (DelegationTool) Name() string { return "delegation" }

func (DelegationTool) Description() string {
	return "Delegate a task to a sub-agent. " +
		"Specify the task description and optionally a model to use. " +
		"Note: full sub-agent delegation is planned for v0.2; this is currently a placeholder."
}

func (DelegationTool) Parameters() json.RawMessage {
	return json.RawMessage(`{
  "type": "object",
  "required": ["task"],
  "properties": {
    "task": {
      "type": "string",
      "description": "Description of the task to delegate to a sub-agent."
    },
    "model": {
      "type": "string",
      "description": "Optional model identifier for the sub-agent (e.g. \"claude-3-5-sonnet\")."
    }
  }
}`)
}

func (DelegationTool) Execute(_ context.Context, params json.RawMessage) (*Result, error) {
	var p delegationParams
	if err := json.Unmarshal(params, &p); err != nil {
		return &Result{Error: fmt.Sprintf("invalid parameters: %v", err)}, nil
	}
	if p.Task == "" {
		return &Result{Error: "task must not be empty"}, nil
	}

	model := p.Model
	if model == "" {
		model = "default"
	}

	return &Result{
		Output: fmt.Sprintf(
			"DELEGATION_PENDING: task=%q model=%s — sub-agent delegation will be implemented in v0.2",
			p.Task, model,
		),
	}, nil
}

// Ensure DelegationTool satisfies the Tool interface at compile time.
var _ Tool = DelegationTool{}

func init() {
	Register(DelegationTool{})
}
