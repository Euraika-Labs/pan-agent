// Package tools defines the Tool interface and an in-process registry that
// maps tool names to their implementations.
package tools

import (
	"context"
	"encoding/json"
)

// Tool is the contract every agent tool must satisfy.
type Tool interface {
	// Name returns the unique identifier used to dispatch this tool.
	Name() string

	// Description is a human-readable summary shown to the LLM.
	Description() string

	// Parameters returns the JSON Schema that describes the tool's input object.
	Parameters() json.RawMessage

	// Execute runs the tool with the given JSON-encoded parameters.
	Execute(ctx context.Context, params json.RawMessage) (*Result, error)
}

// Result is the structured response returned from every tool execution.
type Result struct {
	Output string `json:"output"`
	Error  string `json:"error,omitempty"`
	Denied bool   `json:"denied,omitempty"` // true when approval was rejected
}

// registry holds all registered tools, keyed by Tool.Name().
var registry = map[string]Tool{}

// Register adds t to the global registry.
// A second call with the same name overwrites the previous entry.
func Register(t Tool) { registry[t.Name()] = t }

// Get returns the tool registered under name and a boolean indicating whether
// it was found.
func Get(name string) (Tool, bool) {
	t, ok := registry[name]
	return t, ok
}

// All returns a shallow copy of the registry map.
// Callers may iterate over it without holding any lock; tool values are
// immutable after registration.
func All() map[string]Tool {
	out := make(map[string]Tool, len(registry))
	for k, v := range registry {
		out[k] = v
	}
	return out
}
