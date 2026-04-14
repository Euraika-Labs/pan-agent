package tools

import "encoding/json"

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
