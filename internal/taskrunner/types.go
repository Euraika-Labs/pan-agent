// Package taskrunner provides the durable task execution engine for
// Phase 12 WS#4. Tasks are persisted in SQLite with step memoization,
// heartbeat-based zombie detection, and pause-not-terminate budget
// semantics.
package taskrunner

// TaskStatus represents the lifecycle state of a task.
type TaskStatus string

const (
	StatusQueued    TaskStatus = "queued"
	StatusRunning   TaskStatus = "running"
	StatusPaused    TaskStatus = "paused"
	StatusZombie    TaskStatus = "zombie"
	StatusSucceeded TaskStatus = "succeeded"
	StatusFailed    TaskStatus = "failed"
	StatusCancelled TaskStatus = "cancelled"
)

// Task represents a durable task row.
type Task struct {
	ID                string     `json:"id"`
	PlanJSON          string     `json:"plan_json,omitempty"`
	Status            TaskStatus `json:"status"`
	SessionID         string     `json:"session_id"`
	CreatedAt         int64      `json:"created_at"`
	LastHeartbeatAt   *int64     `json:"last_heartbeat_at,omitempty"`
	NextPlanStepIndex int        `json:"next_plan_step_index"`
	TokenBudgetCap    int        `json:"token_budget_cap"`
	CostCapUSD        float64    `json:"cost_cap_usd"`
}

// EventKind classifies a task event.
type EventKind string

const (
	EventToolCall       EventKind = "tool_call"
	EventApproval       EventKind = "approval"
	EventJournalReceipt EventKind = "journal_receipt"
	EventArtifact       EventKind = "artifact"
	EventCost           EventKind = "cost"
	EventError          EventKind = "error"
	EventHeartbeat      EventKind = "heartbeat"
	EventStepCompleted  EventKind = "step_completed"
)

// TaskEvent represents a single event in a task's execution history.
type TaskEvent struct {
	ID          int64     `json:"id"`
	TaskID      string    `json:"task_id"`
	StepID      string    `json:"step_id"`
	Attempt     int       `json:"attempt"`
	Sequence    int       `json:"sequence"`
	Kind        EventKind `json:"kind"`
	PayloadJSON string    `json:"payload_json,omitempty"`
	CreatedAt   int64     `json:"created_at"`
}
