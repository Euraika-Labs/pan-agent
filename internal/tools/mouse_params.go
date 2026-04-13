package tools

import "encoding/json"

// mouseParams is the JSON parameter shape for the mouse tool.
type mouseParams struct {
	Operation string `json:"operation"`
	X         int32  `json:"x,omitempty"`
	Y         int32  `json:"y,omitempty"`
	Delta     int32  `json:"delta,omitempty"`
}

// mouseParametersJSON is the JSON Schema shared across all platforms.
var mouseParametersJSON = json.RawMessage(`{
  "type": "object",
  "required": ["operation"],
  "properties": {
    "operation": {
      "type": "string",
      "enum": ["move", "click", "double_click", "right_click", "scroll"],
      "description": "Mouse operation to perform"
    },
    "x": {
      "type": "integer",
      "description": "Absolute X screen coordinate"
    },
    "y": {
      "type": "integer",
      "description": "Absolute Y screen coordinate"
    },
    "delta": {
      "type": "integer",
      "description": "Scroll amount for scroll operation (positive=up, negative=down). Default 120."
    }
  }
}`)
