package taskrunner

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/euraika-labs/pan-agent/internal/tools"
)

// ---------------------------------------------------------------------------
// Mock tool
// ---------------------------------------------------------------------------

type mockTool struct {
	name   string
	result *tools.Result
	err    error
	delay  time.Duration
}

func (m *mockTool) Name() string                { return m.name }
func (m *mockTool) Description() string         { return "mock tool " + m.name }
func (m *mockTool) Parameters() json.RawMessage { return json.RawMessage(`{}`) }
func (m *mockTool) Execute(ctx context.Context, params json.RawMessage) (*tools.Result, error) {
	if m.delay > 0 {
		select {
		case <-time.After(m.delay):
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	return m.result, m.err
}

// ---------------------------------------------------------------------------
// Helper: build a plan JSON from step descriptors
// ---------------------------------------------------------------------------

func makePlanJSON(t *testing.T, steps []Step) string {
	t.Helper()
	p := Plan{Steps: steps}
	b, err := json.Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func newRegistry(tt ...*mockTool) *tools.Registry {
	r := tools.NewRegistry()
	for _, t := range tt {
		r.Register(t)
	}
	return r
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

func TestRunnerExecutesSteps(t *testing.T) {
	db := openTestDB(t)
	store := NewStore(db)

	mt1 := &mockTool{name: "echo", result: &tools.Result{Output: "ok1"}}
	mt2 := &mockTool{name: "noop", result: &tools.Result{Output: "ok2"}}
	mt3 := &mockTool{name: "ping", result: &tools.Result{Output: "ok3"}}
	reg := newRegistry(mt1, mt2, mt3)

	planJSON := makePlanJSON(t, []Step{
		{ID: "s1", Tool: "echo", Params: json.RawMessage(`{}`)},
		{ID: "s2", Tool: "noop", Params: json.RawMessage(`{}`)},
		{ID: "s3", Tool: "ping", Params: json.RawMessage(`{}`)},
	})

	task, err := store.CreateTask("sess-1", planJSON, 0)
	if err != nil {
		t.Fatal(err)
	}

	runner := NewRunner(store, reg)
	if err := runner.Execute(context.Background(), task.ID); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	got, _ := store.GetTask(task.ID)
	if got.Status != StatusSucceeded {
		t.Errorf("status = %q, want %q", got.Status, StatusSucceeded)
	}

	events, _ := store.ListEvents(task.ID, 100)
	// Each step produces a tool_call + step_completed = 6 events total.
	if len(events) != 6 {
		t.Errorf("got %d events, want 6", len(events))
	}
}

func TestRunnerResumeSkipsCompleted(t *testing.T) {
	db := openTestDB(t)
	store := NewStore(db)

	mt1 := &mockTool{name: "a", result: &tools.Result{Output: "ok"}}
	mt2 := &mockTool{name: "b", result: &tools.Result{Output: "ok"}}
	mt3 := &mockTool{name: "c", result: &tools.Result{Output: "ok"}}
	reg := newRegistry(mt1, mt2, mt3)

	planJSON := makePlanJSON(t, []Step{
		{ID: "s1", Tool: "a", Params: json.RawMessage(`{}`)},
		{ID: "s2", Tool: "b", Params: json.RawMessage(`{}`)},
		{ID: "s3", Tool: "c", Params: json.RawMessage(`{}`)},
	})

	task, err := store.CreateTask("sess-1", planJSON, 0)
	if err != nil {
		t.Fatal(err)
	}
	// Simulate that steps 0 and 1 are already completed.
	store.AdvanceStep(task.ID, 2)

	runner := NewRunner(store, reg)
	if err := runner.Execute(context.Background(), task.ID); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	got, _ := store.GetTask(task.ID)
	if got.Status != StatusSucceeded {
		t.Errorf("status = %q, want %q", got.Status, StatusSucceeded)
	}

	// Only step 3 (index 2) should have been executed.
	events, _ := store.ListEvents(task.ID, 100)
	// 1 tool_call + 1 step_completed = 2
	if len(events) != 2 {
		t.Errorf("got %d events, want 2 (only step 3 executed)", len(events))
	}
}

func TestRunnerBudgetExceeded(t *testing.T) {
	db := openTestDB(t)
	store := NewStore(db)

	mt := &mockTool{name: "work", result: &tools.Result{Output: "done"}}
	reg := newRegistry(mt)

	planJSON := makePlanJSON(t, []Step{
		{ID: "s1", Tool: "work", Params: json.RawMessage(`{}`)},
		{ID: "s2", Tool: "work", Params: json.RawMessage(`{}`)},
	})

	task, err := store.CreateTask("sess-1", planJSON, 1.0) // cap = $1.0
	if err != nil {
		t.Fatal(err)
	}

	// Inject a cost event that will be visible after step 1 completes.
	// The runner accumulates EventCost events. We insert one before
	// execution so the budget check after step 1 finds it.
	_ = store.AddEvent(task.ID, "pre", 1, 0, EventCost, `{"amount":1.5}`)

	runner := NewRunner(store, reg)
	if err := runner.Execute(context.Background(), task.ID); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	got, _ := store.GetTask(task.ID)
	if got.Status != StatusPaused {
		t.Errorf("status = %q, want %q (budget exceeded)", got.Status, StatusPaused)
	}
}

func TestRunnerCancelViaContext(t *testing.T) {
	db := openTestDB(t)
	store := NewStore(db)

	// A slow tool that takes 5 seconds — we cancel before it finishes.
	mt := &mockTool{name: "slow", result: &tools.Result{Output: "done"}, delay: 5 * time.Second}
	reg := newRegistry(mt)

	planJSON := makePlanJSON(t, []Step{
		{ID: "s1", Tool: "slow", Params: json.RawMessage(`{}`)},
	})

	task, err := store.CreateTask("sess-1", planJSON, 0)
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())

	// Cancel after a short delay.
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()

	runner := NewRunner(store, reg)
	err = runner.Execute(ctx, task.ID)
	if err == nil {
		t.Fatal("expected context cancellation error")
	}

	got, _ := store.GetTask(task.ID)
	if got.Status != StatusPaused {
		t.Errorf("status = %q, want %q (context cancelled)", got.Status, StatusPaused)
	}
}

func TestRunnerToolNotFound(t *testing.T) {
	db := openTestDB(t)
	store := NewStore(db)

	reg := newRegistry() // empty registry

	planJSON := makePlanJSON(t, []Step{
		{ID: "s1", Tool: "nonexistent", Params: json.RawMessage(`{}`)},
	})

	task, err := store.CreateTask("sess-1", planJSON, 0)
	if err != nil {
		t.Fatal(err)
	}

	runner := NewRunner(store, reg)
	err = runner.Execute(context.Background(), task.ID)
	if err == nil {
		t.Fatal("expected error for unknown tool")
	}

	got, _ := store.GetTask(task.ID)
	if got.Status != StatusFailed {
		t.Errorf("status = %q, want %q", got.Status, StatusFailed)
	}
}

func TestRunnerGatedToolPauses(t *testing.T) {
	db := openTestDB(t)
	store := NewStore(db)

	// Register "terminal" so it exists in the registry, but it should
	// still be caught by the gated check before execution.
	mt := &mockTool{name: "terminal", result: &tools.Result{Output: "ok"}}
	reg := newRegistry(mt)

	planJSON := makePlanJSON(t, []Step{
		{ID: "s1", Tool: "terminal", Params: json.RawMessage(`{}`)},
	})

	task, err := store.CreateTask("sess-1", planJSON, 0)
	if err != nil {
		t.Fatal(err)
	}

	runner := NewRunner(store, reg)
	if err := runner.Execute(context.Background(), task.ID); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	got, _ := store.GetTask(task.ID)
	if got.Status != StatusPaused {
		t.Errorf("status = %q, want %q (gated tool)", got.Status, StatusPaused)
	}

	// Should have an approval event.
	events, _ := store.ListEvents(task.ID, 100)
	found := false
	for _, e := range events {
		if e.Kind == EventApproval {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected EventApproval event for gated tool")
	}
}

func TestValidatePlan(t *testing.T) {
	reg := newRegistry(
		&mockTool{name: "echo", result: &tools.Result{Output: "ok"}},
	)

	tests := []struct {
		name    string
		input   string
		wantErr bool
	}{
		{
			name:    "valid",
			input:   `{"steps":[{"id":"s1","tool":"echo","params":{}}]}`,
			wantErr: false,
		},
		{
			name:    "empty steps",
			input:   `{"steps":[]}`,
			wantErr: true,
		},
		{
			name:    "unknown tool",
			input:   `{"steps":[{"id":"s1","tool":"unknown","params":{}}]}`,
			wantErr: true,
		},
		{
			name:    "invalid JSON",
			input:   `not json`,
			wantErr: true,
		},
		{
			name:    "missing tool field",
			input:   `{"steps":[{"id":"s1","params":{}}]}`,
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidatePlan(tc.input, reg)
			if (err != nil) != tc.wantErr {
				t.Errorf("ValidatePlan() error = %v, wantErr %v", err, tc.wantErr)
			}
		})
	}
}

func TestRunnerToolDenied(t *testing.T) {
	db := openTestDB(t)
	store := NewStore(db)

	mt := &mockTool{name: "fs", result: &tools.Result{Output: "", Denied: true}}
	reg := newRegistry(mt)

	planJSON := makePlanJSON(t, []Step{
		{ID: "s1", Tool: "fs", Params: json.RawMessage(`{}`)},
	})

	task, err := store.CreateTask("sess-1", planJSON, 0)
	if err != nil {
		t.Fatal(err)
	}

	runner := NewRunner(store, reg)
	if err := runner.Execute(context.Background(), task.ID); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	got, _ := store.GetTask(task.ID)
	if got.Status != StatusPaused {
		t.Errorf("status = %q, want %q (tool denied)", got.Status, StatusPaused)
	}
}

func TestRunnerToolError(t *testing.T) {
	db := openTestDB(t)
	store := NewStore(db)

	mt := &mockTool{name: "fail", result: nil, err: fmt.Errorf("boom")}
	reg := newRegistry(mt)

	planJSON := makePlanJSON(t, []Step{
		{ID: "s1", Tool: "fail", Params: json.RawMessage(`{}`)},
	})

	task, err := store.CreateTask("sess-1", planJSON, 0)
	if err != nil {
		t.Fatal(err)
	}

	runner := NewRunner(store, reg)
	err = runner.Execute(context.Background(), task.ID)
	if err == nil {
		t.Fatal("expected error from failing tool")
	}

	got, _ := store.GetTask(task.ID)
	if got.Status != StatusFailed {
		t.Errorf("status = %q, want %q", got.Status, StatusFailed)
	}
}
