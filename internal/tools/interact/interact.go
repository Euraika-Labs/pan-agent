// Package interact provides a single canonical "interact" tool that routes
// desktop-automation intents through an internal layer hierarchy:
//
//   - direct_api: AppleScript/osascript (macOS), PowerShell+UIAutomation (Win), AT-SPI (Linux)
//   - accessibility: ARIA-YAML observation (Playwright locator.ariaSnapshot pattern)
//   - vision: base64 screenshot → LLM, or provider computer_use passthrough
//   - coordinate: last-resort pixel click
//
// The LLM sees ONE tool — "interact" — and never picks the layer directly.
// The router selects the best layer based on platform capabilities and intent.
package interact

import (
	"context"
	"encoding/json"
	"fmt"
	"runtime"
)

// Layer identifies which interaction backend handled a request.
type Layer string

const (
	LayerDirectAPI     Layer = "direct_api"
	LayerAccessibility Layer = "accessibility"
	LayerVision        Layer = "vision"
	LayerCoordinate    Layer = "coordinate"
	LayerUnsupported   Layer = "unsupported"
)

// Request is the input to the interact tool.
type Request struct {
	Intent      string            `json:"intent"`
	App         string            `json:"app,omitempty"`
	Text        string            `json:"text,omitempty"`
	X           *int              `json:"x,omitempty"`
	Y           *int              `json:"y,omitempty"`
	Key         string            `json:"key,omitempty"`
	Constraints map[string]string `json:"constraints,omitempty"`
}

// Response is the output of the interact tool.
type Response struct {
	Layer      Layer   `json:"layer"`
	Output     string  `json:"output"`
	Confidence float64 `json:"confidence"`
	Error      string  `json:"error,omitempty"`
}

// Router selects the best interaction layer for a given request.
type Router struct {
	directAPI *DirectAPI
	vision    *Vision
}

// NewRouter creates a Router with platform-appropriate backends.
func NewRouter() *Router {
	return &Router{
		directAPI: NewDirectAPI(),
		vision:    NewVision(),
	}
}

// Route dispatches an interaction request to the best available layer.
func (r *Router) Route(ctx context.Context, req Request) Response {
	if req.Intent == "" {
		return Response{Layer: LayerUnsupported, Error: "intent is required"}
	}

	if result, ok := r.tryDirectAPI(ctx, req); ok {
		return result
	}

	if result, ok := r.tryVision(ctx, req); ok {
		return result
	}

	return Response{
		Layer:  LayerUnsupported,
		Output: fmt.Sprintf("no interaction layer available for intent %q on %s", req.Intent, runtime.GOOS),
	}
}

func (r *Router) tryDirectAPI(ctx context.Context, req Request) (Response, bool) {
	if r.directAPI == nil || !r.directAPI.Available() {
		return Response{}, false
	}

	result, err := r.directAPI.Execute(ctx, req)
	if err != nil {
		return Response{}, false
	}

	return Response{
		Layer:      LayerDirectAPI,
		Output:     result,
		Confidence: 0.9,
	}, true
}

func (r *Router) tryVision(ctx context.Context, req Request) (Response, bool) {
	if r.vision == nil {
		return Response{}, false
	}

	result, err := r.vision.Capture(ctx)
	if err != nil {
		return Response{}, false
	}

	return Response{
		Layer:      LayerVision,
		Output:     result,
		Confidence: 0.6,
	}, true
}

// ToolParameters returns the JSON Schema for the interact tool, registered
// as the single LLM-facing surface for all desktop interaction.
func ToolParameters() json.RawMessage {
	return json.RawMessage(`{
  "type": "object",
  "properties": {
    "intent": {
      "type": "string",
      "description": "What you want to accomplish (e.g., 'open Safari and navigate to example.com', 'click the Submit button', 'take a screenshot')"
    },
    "app": {
      "type": "string",
      "description": "Target application name (optional, helps routing)"
    },
    "text": {
      "type": "string",
      "description": "Text to type (for 'type' intent)"
    },
    "x": {
      "type": "integer",
      "description": "X coordinate for click/right_click intents"
    },
    "y": {
      "type": "integer",
      "description": "Y coordinate for click/right_click intents"
    },
    "key": {
      "type": "string",
      "description": "Key combination for 'key' intent (e.g., 'ctrl+c', 'super', 'alt+F4')"
    },
    "constraints": {
      "type": "object",
      "description": "Additional constraints (e.g., {\"timeout\": \"5s\", \"retry\": \"true\"})",
      "additionalProperties": {"type": "string"}
    }
  },
  "required": ["intent"]
}`)
}
