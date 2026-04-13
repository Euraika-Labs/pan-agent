package tools

import (
	"context"
	"encoding/json"
	"fmt"
)

// MouseTool controls the mouse cursor and sends mouse button events.
type MouseTool struct{}

func (MouseTool) Name() string { return "mouse" }

func (MouseTool) Description() string {
	return "Control the mouse: move cursor, click, double-click, right-click, or scroll. " +
		"Coordinates are absolute screen pixels."
}

func (MouseTool) Parameters() json.RawMessage { return mouseParametersJSON }

func (t MouseTool) Execute(_ context.Context, params json.RawMessage) (*Result, error) {
	var p mouseParams
	if err := json.Unmarshal(params, &p); err != nil {
		return &Result{Error: fmt.Sprintf("invalid parameters: %v", err)}, nil
	}

	switch p.Operation {
	case "move":
		if err := mouseMove(p.X, p.Y); err != nil {
			return &Result{Error: err.Error()}, nil
		}
		return &Result{Output: fmt.Sprintf("moved cursor to (%d, %d)", p.X, p.Y)}, nil

	case "click":
		if err := mouseClick(p.X, p.Y); err != nil {
			return &Result{Error: err.Error()}, nil
		}
		return &Result{Output: fmt.Sprintf("left-clicked at (%d, %d)", p.X, p.Y)}, nil

	case "double_click":
		if err := mouseDoubleClick(p.X, p.Y); err != nil {
			return &Result{Error: err.Error()}, nil
		}
		return &Result{Output: fmt.Sprintf("double-clicked at (%d, %d)", p.X, p.Y)}, nil

	case "right_click":
		if err := mouseRightClick(p.X, p.Y); err != nil {
			return &Result{Error: err.Error()}, nil
		}
		return &Result{Output: fmt.Sprintf("right-clicked at (%d, %d)", p.X, p.Y)}, nil

	case "scroll":
		delta := p.Delta
		if delta == 0 {
			delta = 120
		}
		if err := mouseScroll(p.X, p.Y, delta); err != nil {
			return &Result{Error: err.Error()}, nil
		}
		return &Result{Output: fmt.Sprintf("scrolled %d at (%d, %d)", delta, p.X, p.Y)}, nil

	default:
		return &Result{Error: fmt.Sprintf("unknown operation: %q (want move|click|double_click|right_click|scroll)", p.Operation)}, nil
	}
}

var _ Tool = MouseTool{}
