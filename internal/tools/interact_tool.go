package tools

import (
	"context"
	"encoding/json"

	"github.com/euraika-labs/pan-agent/internal/tools/interact"
)

func init() {
	Register(&interactTool{router: interact.NewRouter()})
}

type interactTool struct {
	router *interact.Router
}

func (t *interactTool) Name() string { return "interact" }

func (t *interactTool) Description() string {
	return "Interact with desktop applications using the best available method " +
		"(direct API, accessibility, vision, or coordinates). Describe what you " +
		"want to accomplish and the router selects the optimal approach."
}

func (t *interactTool) Parameters() json.RawMessage {
	return interact.ToolParameters()
}

func (t *interactTool) Execute(ctx context.Context, raw json.RawMessage) (*Result, error) {
	var req interact.Request
	if err := json.Unmarshal(raw, &req); err != nil {
		return &Result{Error: "invalid parameters: " + err.Error()}, nil
	}

	resp := t.router.Route(ctx, req)

	if resp.Error != "" {
		return &Result{Error: resp.Error}, nil
	}

	out, err := json.Marshal(resp)
	if err != nil {
		return &Result{Error: "marshal response: " + err.Error()}, nil
	}

	return &Result{Output: string(out)}, nil
}
