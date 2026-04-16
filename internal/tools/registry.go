// Package tools defines the Tool interface and an in-process registry that
// maps tool names to their implementations.
package tools

import (
	"context"
	"encoding/json"
	"sync"
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

// Registry holds a named set of tools. The package exposes a default
// global Registry plus constructor helpers so integration tests can
// stand up an isolated tool set per test (previously the package-global
// map made it impossible to run two Server instances in one process).
type Registry struct {
	mu sync.RWMutex
	m  map[string]Tool
}

// NewRegistry returns an empty Registry. For integration tests that want
// to pin the exact tools available without touching the global default.
func NewRegistry() *Registry {
	return &Registry{m: make(map[string]Tool)}
}

// Register adds t to this registry. A second call with the same name
// overwrites the previous entry (same semantics as the prior global).
func (r *Registry) Register(t Tool) {
	r.mu.Lock()
	r.m[t.Name()] = t
	r.mu.Unlock()
}

// Get returns the tool registered under name and a boolean indicating
// whether it was found.
func (r *Registry) Get(name string) (Tool, bool) {
	r.mu.RLock()
	t, ok := r.m[name]
	r.mu.RUnlock()
	return t, ok
}

// All returns a shallow copy of the registry map. Callers may iterate
// without holding any lock; tool values are immutable after registration.
func (r *Registry) All() map[string]Tool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make(map[string]Tool, len(r.m))
	for k, v := range r.m {
		out[k] = v
	}
	return out
}

// Default is the process-wide registry every tool's init() writes into.
// Package-level Register/Get/All delegate here for backward compatibility;
// tests that need isolation can allocate a fresh Registry via NewRegistry
// and pass it through their Server construction.
var Default = NewRegistry()

// Register adds t to the default (package-global) registry.
func Register(t Tool) { Default.Register(t) }

// Get returns the tool registered in the default registry.
func Get(name string) (Tool, bool) { return Default.Get(name) }

// All returns a shallow copy of the default registry's map.
func All() map[string]Tool { return Default.All() }
