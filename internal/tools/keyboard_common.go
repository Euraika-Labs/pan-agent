package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// KeyboardTool simulates keyboard input.
type KeyboardTool struct{}

func (KeyboardTool) Name() string { return "keyboard" }

func (KeyboardTool) Description() string {
	return "Simulate keyboard input: type text, press a key, or send a hotkey combination. " +
		"Operations: type (sends text character by character), " +
		"press (presses and releases a single key), " +
		"hotkey (presses modifier+key combo, e.g. ctrl+c)."
}

func (KeyboardTool) Parameters() json.RawMessage { return keyboardParametersJSON }

func (t KeyboardTool) Execute(_ context.Context, params json.RawMessage) (*Result, error) {
	var p keyboardParams
	if err := json.Unmarshal(params, &p); err != nil {
		return &Result{Error: fmt.Sprintf("invalid parameters: %v", err)}, nil
	}

	switch p.Operation {
	case "type":
		if p.Text == "" {
			return &Result{Error: "text must not be empty for operation=type"}, nil
		}
		if err := keyboardTypeText(p.Text); err != nil {
			return &Result{Error: err.Error()}, nil
		}
		return &Result{Output: fmt.Sprintf("typed %d character(s)", len([]rune(p.Text)))}, nil

	case "press":
		if p.Key == "" {
			return &Result{Error: "key must not be empty for operation=press"}, nil
		}
		if err := keyboardPressKey(p.Key); err != nil {
			return &Result{Error: err.Error()}, nil
		}
		return &Result{Output: fmt.Sprintf("pressed key: %s", p.Key)}, nil

	case "hotkey":
		if p.Key == "" {
			return &Result{Error: "key must not be empty for operation=hotkey"}, nil
		}
		if err := keyboardHotkey(p.Modifiers, p.Key); err != nil {
			return &Result{Error: err.Error()}, nil
		}
		return &Result{Output: fmt.Sprintf("hotkey: %s+%s", strings.Join(p.Modifiers, "+"), p.Key)}, nil

	default:
		return &Result{Error: fmt.Sprintf("unknown operation: %q (want type|press|hotkey)", p.Operation)}, nil
	}
}

var _ Tool = KeyboardTool{}

func init() {
	Register(KeyboardTool{})
}
