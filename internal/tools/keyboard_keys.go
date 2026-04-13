package tools

import (
	"encoding/json"
	"strings"
)

// keyboardParams is the JSON parameter shape for the keyboard tool.
type keyboardParams struct {
	Operation string   `json:"operation"`
	Text      string   `json:"text,omitempty"`
	Key       string   `json:"key,omitempty"`
	Modifiers []string `json:"modifiers,omitempty"`
}

// keyboardParametersJSON is the JSON Schema shared across all platforms.
var keyboardParametersJSON = json.RawMessage(`{
  "type": "object",
  "required": ["operation"],
  "properties": {
    "operation": {
      "type": "string",
      "enum": ["type", "press", "hotkey"],
      "description": "type: send text; press: single key; hotkey: modifier combo"
    },
    "text": {
      "type": "string",
      "description": "Text to type (used with operation=type)"
    },
    "key": {
      "type": "string",
      "description": "Key name to press (enter, tab, escape, a-z, f1-f12, etc.)"
    },
    "modifiers": {
      "type": "array",
      "items": {"type": "string"},
      "description": "Modifier keys to hold: ctrl, alt, shift, win/super"
    }
  }
}`)

// namedKeysList maps friendly names to a platform-neutral key identifier.
// Platform files map these to OS-specific codes.
var namedKeysList = []string{
	"enter", "return", "tab", "escape", "esc", "backspace", "delete", "del",
	"left", "up", "right", "down", "home", "end", "pageup", "pagedown",
	"insert", "space",
	"f1", "f2", "f3", "f4", "f5", "f6", "f7", "f8", "f9", "f10", "f11", "f12",
}

// modifierKeysList is the set of valid modifier names.
var modifierKeysList = []string{"ctrl", "alt", "shift", "win", "super"}

// normalizeKeyName lowercases and trims a key name.
func normalizeKeyName(name string) string {
	return strings.ToLower(strings.TrimSpace(name))
}
