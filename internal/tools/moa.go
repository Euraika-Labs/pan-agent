package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// MoaTool implements a Mixture-of-Agents pattern, routing a prompt to multiple
// models and aggregating their responses.
// In v0.1 this is a placeholder; full MOA support is planned for v0.2.
type MoaTool struct{}

// moaParams is the JSON-decoded parameter bag for MoaTool.
type moaParams struct {
	Prompt string   `json:"prompt"`
	Models []string `json:"models"`
}

func (MoaTool) Name() string { return "moa" }

func (MoaTool) Description() string {
	return "Run a prompt through multiple models (Mixture of Agents) and aggregate results. " +
		"Specify the prompt and a list of model identifiers. " +
		"Note: MOA requires multiple model configs and will be fully implemented in v0.2; " +
		"this is currently a placeholder."
}

func (MoaTool) Parameters() json.RawMessage {
	return json.RawMessage(`{
  "type": "object",
  "required": ["prompt", "models"],
  "properties": {
    "prompt": {
      "type": "string",
      "description": "The prompt to send to all models."
    },
    "models": {
      "type": "array",
      "items": { "type": "string" },
      "description": "List of model identifiers to include in the mixture (e.g. [\"claude-3-5-sonnet\", \"gpt-4o\"])."
    }
  }
}`)
}

func (MoaTool) Execute(_ context.Context, params json.RawMessage) (*Result, error) {
	var p moaParams
	if err := json.Unmarshal(params, &p); err != nil {
		return &Result{Error: fmt.Sprintf("invalid parameters: %v", err)}, nil
	}
	if p.Prompt == "" {
		return &Result{Error: "prompt must not be empty"}, nil
	}
	if len(p.Models) == 0 {
		return &Result{Error: "models must contain at least one model identifier"}, nil
	}

	return &Result{
		Output: fmt.Sprintf(
			"MOA_PENDING: prompt=%q models=[%s] — Mixture of Agents requires multiple model configs and will be implemented in v0.2",
			p.Prompt, strings.Join(p.Models, ", "),
		),
	}, nil
}

// Ensure MoaTool satisfies the Tool interface at compile time.
var _ Tool = MoaTool{}

func init() {
	Register(MoaTool{})
}
