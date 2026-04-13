package tools

import "encoding/json"

// windowManagerParams is the JSON parameter shape for the window manager tool.
type windowManagerParams struct {
	Operation string `json:"operation"`
	Title     string `json:"title,omitempty"`
	X         int32  `json:"x,omitempty"`
	Y         int32  `json:"y,omitempty"`
	Width     int32  `json:"width,omitempty"`
	Height    int32  `json:"height,omitempty"`
}

// windowManagerParametersJSON is the JSON Schema shared across all platforms.
var windowManagerParametersJSON = json.RawMessage(`{
  "type": "object",
  "required": ["operation"],
  "properties": {
    "operation": {
      "type": "string",
      "enum": ["list", "find", "focus", "move", "resize", "close"],
      "description": "Operation to perform"
    },
    "title": {
      "type": "string",
      "description": "Window title substring to match (used by find/focus/move/resize/close)"
    },
    "x": {
      "type": "integer",
      "description": "Target X position (move)"
    },
    "y": {
      "type": "integer",
      "description": "Target Y position (move)"
    },
    "width": {
      "type": "integer",
      "description": "Target width in pixels (resize)"
    },
    "height": {
      "type": "integer",
      "description": "Target height in pixels (resize)"
    }
  }
}`)
