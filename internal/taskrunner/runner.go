package taskrunner

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"time"

	"github.com/euraika-labs/pan-agent/internal/tools"
)

// ---------------------------------------------------------------------------
// Plan types
// ---------------------------------------------------------------------------

// Step describes a single action within a task plan.
type Step struct {
	ID          string          `json:"id"`
	Tool        string          `json:"tool"`
	Params      json.RawMessage `json:"params"`
	Description string          `json:"description,omitempty"`
}

// Plan is the JSON structure persisted in task.plan_json.
type Plan struct {
	Steps []Step `json:"steps"`
}

// ---------------------------------------------------------------------------
// GatedTools — tools that require user approval before execution.
// Mirrors the set in internal/gateway/chat.go.
// ---------------------------------------------------------------------------

// GatedTools maps tool names that require approval to true.
var GatedTools = map[string]bool{
	"terminal":       true,
	"filesystem":     true,
	"code_execution": true,
	"browser":        true,
	"interact":       true,
}

// ---------------------------------------------------------------------------
// ValidatePlan
// ---------------------------------------------------------------------------

// ValidatePlan parses planJSON and rejects empty step lists or references
// to tools that are not registered in the provided registry.
func ValidatePlan(planJSON string, registry *tools.Registry) error {
	var plan Plan
	if err := json.Unmarshal([]byte(planJSON), &plan); err != nil {
		return fmt.Errorf("invalid plan JSON: %w", err)
	}
	if len(plan.Steps) == 0 {
		return fmt.Errorf("plan has no steps")
	}
	for _, step := range plan.Steps {
		if step.Tool == "" {
			return fmt.Errorf("step %q has no tool", step.ID)
		}
		if _, ok := registry.Get(step.Tool); !ok {
			return fmt.Errorf("unknown tool %q in step %q", step.Tool, step.ID)
		}
	}
	return nil
}

// ---------------------------------------------------------------------------
// Runner
// ---------------------------------------------------------------------------

// Runner executes task plans step by step, persisting progress so that
// tasks survive process restarts (resume support).
type Runner struct {
	store    *Store
	registry *tools.Registry
	gated    map[string]bool
}

// NewRunner constructs a Runner backed by the given store and tool registry.
func NewRunner(store *Store, registry *tools.Registry) *Runner {
	return &Runner{
		store:    store,
		registry: registry,
		gated:    GatedTools,
	}
}

// Execute runs the task identified by taskID. It CAS-transitions the task
// from queued to running and then iterates through the plan steps. On
// completion it transitions to succeeded; on budget, gated tool, or
// context cancellation it transitions to paused; on error it transitions
// to failed.
func (r *Runner) Execute(ctx context.Context, taskID string) error {
	// CAS queued → running. If another worker beat us, bail out silently.
	ok, err := r.store.UpdateStatus(taskID, StatusQueued, StatusRunning)
	if err != nil {
		return fmt.Errorf("runner: CAS queued→running: %w", err)
	}
	if !ok {
		return nil // another worker claimed it
	}

	task, err := r.store.GetTask(taskID)
	if err != nil {
		return fmt.Errorf("runner: get task: %w", err)
	}

	var plan Plan
	if err := json.Unmarshal([]byte(task.PlanJSON), &plan); err != nil {
		_ = r.emitError(taskID, "", "invalid plan JSON: "+err.Error())
		r.transition(taskID, StatusRunning, StatusFailed)
		return fmt.Errorf("runner: parse plan: %w", err)
	}

	// Background heartbeat goroutine.
	hbCtx, hbCancel := context.WithCancel(ctx)
	defer hbCancel()
	go r.heartbeatLoop(hbCtx, taskID)

	sequence := 0

	for i := task.NextPlanStepIndex; i < len(plan.Steps); i++ {
		// Check for context cancellation before executing the next step.
		if err := ctx.Err(); err != nil {
			r.transition(taskID, StatusRunning, StatusPaused)
			return err
		}

		step := plan.Steps[i]

		// Gated tool check — pause for approval.
		if r.gated[step.Tool] {
			sequence++
			_ = r.store.AddEvent(taskID, step.ID, 1, sequence, EventApproval,
				fmt.Sprintf(`{"tool":%q,"reason":"gated"}`, step.Tool))
			r.transition(taskID, StatusRunning, StatusPaused)
			return nil
		}

		// Look up the tool.
		tool, found := r.registry.Get(step.Tool)
		if !found {
			sequence++
			_ = r.store.AddEvent(taskID, step.ID, 1, sequence, EventError,
				fmt.Sprintf(`{"error":"unknown tool %q"}`, step.Tool))
			r.transition(taskID, StatusRunning, StatusFailed)
			return fmt.Errorf("unknown tool %q", step.Tool)
		}

		// Execute the tool.
		result, execErr := tool.Execute(ctx, step.Params)

		// If the context was cancelled during execution, treat it as a
		// pause (not a failure) — the caller asked us to stop.
		if ctx.Err() != nil {
			r.transition(taskID, StatusRunning, StatusPaused)
			return ctx.Err()
		}

		// Record the tool call event.
		sequence++
		payload := "{}"
		if result != nil {
			if b, err := json.Marshal(result); err == nil {
				payload = string(b)
			}
		}
		_ = r.store.AddEvent(taskID, step.ID, 1, sequence, EventToolCall, payload)

		if execErr != nil {
			sequence++
			_ = r.store.AddEvent(taskID, step.ID, 1, sequence, EventError,
				fmt.Sprintf(`{"error":%q}`, execErr.Error()))
			r.transition(taskID, StatusRunning, StatusFailed)
			return execErr
		}

		// Check if the tool denied execution (approval rejected at the
		// tool level).
		if result != nil && result.Denied {
			sequence++
			_ = r.store.AddEvent(taskID, step.ID, 1, sequence, EventApproval,
				fmt.Sprintf(`{"tool":%q,"reason":"denied"}`, step.Tool))
			r.transition(taskID, StatusRunning, StatusPaused)
			return nil
		}

		// Check for tool-level error string.
		if result != nil && result.Error != "" {
			sequence++
			_ = r.store.AddEvent(taskID, step.ID, 1, sequence, EventError,
				fmt.Sprintf(`{"error":%q}`, result.Error))
			r.transition(taskID, StatusRunning, StatusFailed)
			return fmt.Errorf("tool %q error: %s", step.Tool, result.Error)
		}

		// Step completed.
		sequence++
		_ = r.store.AddEvent(taskID, step.ID, 1, sequence, EventStepCompleted, "")
		_ = r.store.AdvanceStep(taskID, i+1)

		// Budget check: sum accumulated costs from EventCost events.
		if task.CostCapUSD > 0 {
			cost := r.accumulatedCost(taskID)
			if cost >= task.CostCapUSD {
				sequence++
				_ = r.store.AddEvent(taskID, step.ID, 1, sequence, EventCost,
					fmt.Sprintf(`{"amount":%.4f,"cap":%.4f}`, cost, task.CostCapUSD))
				r.transition(taskID, StatusRunning, StatusPaused)
				return nil
			}
		}

		// Recheck context after each step.
		if err := ctx.Err(); err != nil {
			r.transition(taskID, StatusRunning, StatusPaused)
			return err
		}
	}

	// All steps completed.
	r.transition(taskID, StatusRunning, StatusSucceeded)
	return nil
}

// Start polls for queued tasks every 2s and executes them one at a time.
// It blocks until ctx is cancelled.
func (r *Runner) Start(ctx context.Context) {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			r.pollOnce(ctx)
		}
	}
}

// pollOnce finds one queued task and runs it with panic recovery.
func (r *Runner) pollOnce(ctx context.Context) {
	defer func() {
		if rec := recover(); rec != nil {
			log.Printf("[runner] panic recovered: %v", rec)
		}
	}()

	tasks, err := r.store.ListQueuedTasks(1)
	if err != nil {
		log.Printf("[runner] list queued tasks: %v", err)
		return
	}

	if len(tasks) > 0 {
		r.safeExecute(ctx, tasks[0].ID)
	}
}

// safeExecute wraps Execute in a recover() so a panicking tool does not
// crash the polling loop.
func (r *Runner) safeExecute(ctx context.Context, taskID string) {
	defer func() {
		if rec := recover(); rec != nil {
			log.Printf("[runner] panic executing task %s: %v", taskID, rec)
			ok, _ := r.store.UpdateStatus(taskID, StatusRunning, StatusFailed)
			if !ok {
				r.transition(taskID, StatusQueued, StatusFailed)
			}
		}
	}()

	if err := r.Execute(ctx, taskID); err != nil {
		log.Printf("[runner] task %s error: %v", taskID, err)
	}
}

// heartbeatLoop sends a heartbeat every 10s until the context is cancelled.
func (r *Runner) heartbeatLoop(ctx context.Context, taskID string) {
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			_ = r.store.Heartbeat(taskID)
		}
	}
}

// accumulatedCost sums the cost values from EventCost events for a task.
func (r *Runner) accumulatedCost(taskID string) float64 {
	events, err := r.store.ListEvents(taskID, 10000)
	if err != nil {
		return 0
	}
	var total float64
	for _, e := range events {
		if e.Kind != EventCost {
			continue
		}
		var payload struct {
			Amount float64 `json:"amount"`
		}
		if err := json.Unmarshal([]byte(e.PayloadJSON), &payload); err == nil {
			total += payload.Amount
		}
	}
	return total
}

// transition logs a warning if the status CAS fails (DB error or no-op).
func (r *Runner) transition(taskID string, from, to TaskStatus) {
	ok, err := r.store.UpdateStatus(taskID, from, to)
	if err != nil {
		log.Printf("[runner] task %s status %s→%s failed: %v", taskID, from, to, err)
	} else if !ok {
		log.Printf("[runner] task %s status %s→%s: no rows affected", taskID, from, to)
	}
}

// emitError is a convenience wrapper for AddEvent with EventError kind.
func (r *Runner) emitError(taskID, stepID, msg string) error {
	return r.store.AddEvent(taskID, stepID, 1, 0, EventError,
		fmt.Sprintf(`{"error":%q}`, msg))
}
