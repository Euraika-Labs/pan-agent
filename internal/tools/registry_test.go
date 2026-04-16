package tools

import (
	"context"
	"encoding/json"
	"testing"
)

// stubTool is a minimal Tool for registry-isolation tests.
type stubTool struct{ name string }

func (s stubTool) Name() string                   { return s.name }
func (s stubTool) Description() string            { return "test stub" }
func (s stubTool) Parameters() json.RawMessage    { return json.RawMessage(`{}`) }
func (s stubTool) Execute(_ context.Context, _ json.RawMessage) (*Result, error) {
	return &Result{Output: s.name}, nil
}

// TestRegistryIsolation proves M8: two independent Registry instances do
// not share state. Previously a package-global map made this impossible,
// which in turn prevented integration tests from standing up two Server
// instances in a single test process.
func TestRegistryIsolation(t *testing.T) {
	a := NewRegistry()
	b := NewRegistry()

	a.Register(stubTool{name: "only_in_a"})
	b.Register(stubTool{name: "only_in_b"})

	if _, ok := a.Get("only_in_a"); !ok {
		t.Error("only_in_a should be in registry a")
	}
	if _, ok := a.Get("only_in_b"); ok {
		t.Error("only_in_b leaked into registry a")
	}
	if _, ok := b.Get("only_in_a"); ok {
		t.Error("only_in_a leaked into registry b")
	}
	if _, ok := b.Get("only_in_b"); !ok {
		t.Error("only_in_b should be in registry b")
	}

	// The global Default registry should not be affected either.
	if _, ok := Default.Get("only_in_a"); ok {
		t.Error("only_in_a leaked into Default registry")
	}
}

// TestRegistryConcurrentAccess covers the RWMutex hedge around the map
// (previously a bare map that would race under -race).
func TestRegistryConcurrentAccess(t *testing.T) {
	r := NewRegistry()
	done := make(chan struct{})

	go func() {
		for i := 0; i < 100; i++ {
			r.Register(stubTool{name: "a"})
		}
		close(done)
	}()
	for i := 0; i < 100; i++ {
		_, _ = r.Get("a")
		_ = r.All()
	}
	<-done
}
