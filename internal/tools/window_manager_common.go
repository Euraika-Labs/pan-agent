package tools

import (
	"context"
	"encoding/json"
	"fmt"
)

// WindowManagerTool manages top-level desktop windows.
type WindowManagerTool struct{}

func (WindowManagerTool) Name() string { return "window_manager" }

func (WindowManagerTool) Description() string {
	return "Manage desktop windows: list visible windows, find by title, " +
		"focus, move, resize, or close a window."
}

func (WindowManagerTool) Parameters() json.RawMessage { return windowManagerParametersJSON }

func (t WindowManagerTool) Execute(_ context.Context, params json.RawMessage) (*Result, error) {
	var p windowManagerParams
	if err := json.Unmarshal(params, &p); err != nil {
		return &Result{Error: fmt.Sprintf("invalid parameters: %v", err)}, nil
	}

	switch p.Operation {
	case "list":
		out, err := wmListWindows()
		if err != nil {
			return &Result{Error: err.Error()}, nil
		}
		return &Result{Output: out}, nil

	case "find":
		if p.Title == "" {
			return &Result{Error: "title must not be empty for operation=find"}, nil
		}
		out, err := wmFindWindow(p.Title)
		if err != nil {
			return &Result{Error: err.Error()}, nil
		}
		return &Result{Output: out}, nil

	case "focus":
		if p.Title == "" {
			return &Result{Error: "title must not be empty for operation=focus"}, nil
		}
		out, err := wmFocusWindow(p.Title)
		if err != nil {
			return &Result{Error: err.Error()}, nil
		}
		return &Result{Output: out}, nil

	case "move":
		if p.Title == "" {
			return &Result{Error: "title must not be empty for operation=move"}, nil
		}
		out, err := wmMoveWindow(p.Title, p.X, p.Y)
		if err != nil {
			return &Result{Error: err.Error()}, nil
		}
		return &Result{Output: out}, nil

	case "resize":
		if p.Title == "" {
			return &Result{Error: "title must not be empty for operation=resize"}, nil
		}
		out, err := wmResizeWindow(p.Title, p.Width, p.Height)
		if err != nil {
			return &Result{Error: err.Error()}, nil
		}
		return &Result{Output: out}, nil

	case "close":
		if p.Title == "" {
			return &Result{Error: "title must not be empty for operation=close"}, nil
		}
		out, err := wmCloseWindow(p.Title)
		if err != nil {
			return &Result{Error: err.Error()}, nil
		}
		return &Result{Output: out}, nil

	default:
		return &Result{Error: fmt.Sprintf("unknown operation: %q (want list|find|focus|move|resize|close)", p.Operation)}, nil
	}
}

var _ Tool = WindowManagerTool{}

func init() {
	Register(WindowManagerTool{})
}
