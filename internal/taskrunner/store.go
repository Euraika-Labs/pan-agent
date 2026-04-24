package taskrunner

import (
	"database/sql"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// Store provides CRUD operations for tasks and task events.
type Store struct {
	db *sql.DB
}

// NewStore wraps an existing *sql.DB (shared with storage.DB).
func NewStore(db *sql.DB) *Store {
	return &Store{db: db}
}

// CreateTask inserts a new task in queued status.
func (s *Store) CreateTask(sessionID, planJSON string, costCap float64) (*Task, error) {
	t := &Task{
		ID:         uuid.New().String(),
		PlanJSON:   planJSON,
		Status:     StatusQueued,
		SessionID:  sessionID,
		CreatedAt:  time.Now().UnixMilli(),
		CostCapUSD: costCap,
	}

	_, err := s.db.Exec(
		`INSERT INTO tasks (id, plan_json, status, session_id, created_at, cost_cap_usd)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		t.ID, t.PlanJSON, t.Status, t.SessionID, t.CreatedAt, t.CostCapUSD,
	)
	if err != nil {
		return nil, fmt.Errorf("CreateTask: %w", err)
	}
	return t, nil
}

// GetTask returns a task by ID.
func (s *Store) GetTask(id string) (*Task, error) {
	var t Task
	var planJSON sql.NullString
	var lastHBInt sql.NullInt64
	err := s.db.QueryRow(
		`SELECT id, plan_json, status, session_id, created_at,
		        last_heartbeat_at, next_plan_step_index,
		        token_budget_cap, cost_cap_usd
		 FROM tasks WHERE id = ?`, id,
	).Scan(
		&t.ID, &planJSON, &t.Status, &t.SessionID, &t.CreatedAt,
		&lastHBInt, &t.NextPlanStepIndex,
		&t.TokenBudgetCap, &t.CostCapUSD,
	)
	if err != nil {
		return nil, fmt.Errorf("GetTask: %w", err)
	}
	t.PlanJSON = planJSON.String
	if lastHBInt.Valid {
		v := lastHBInt.Int64
		t.LastHeartbeatAt = &v
	}
	return &t, nil
}

// ListTasks returns tasks for a session, newest first.
func (s *Store) ListTasks(sessionID string, limit int) ([]Task, error) {
	rows, err := s.db.Query(
		`SELECT id, plan_json, status, session_id, created_at,
		        last_heartbeat_at, next_plan_step_index,
		        token_budget_cap, cost_cap_usd
		 FROM tasks
		 WHERE session_id = ?
		 ORDER BY created_at DESC
		 LIMIT ?`,
		sessionID, limit,
	)
	if err != nil {
		return nil, fmt.Errorf("ListTasks: %w", err)
	}
	defer rows.Close()

	var tasks []Task
	for rows.Next() {
		var t Task
		var planJSON sql.NullString
		var lastHBInt sql.NullInt64
		if err := rows.Scan(
			&t.ID, &planJSON, &t.Status, &t.SessionID, &t.CreatedAt,
			&lastHBInt, &t.NextPlanStepIndex,
			&t.TokenBudgetCap, &t.CostCapUSD,
		); err != nil {
			return nil, fmt.Errorf("ListTasks scan: %w", err)
		}
		t.PlanJSON = planJSON.String
		if lastHBInt.Valid {
			v := lastHBInt.Int64
			t.LastHeartbeatAt = &v
		}
		tasks = append(tasks, t)
	}
	return tasks, rows.Err()
}

// ListAllTasks returns all tasks, newest first.
func (s *Store) ListAllTasks(limit int) ([]Task, error) {
	rows, err := s.db.Query(
		`SELECT id, plan_json, status, session_id, created_at,
		        last_heartbeat_at, next_plan_step_index,
		        token_budget_cap, cost_cap_usd
		 FROM tasks
		 ORDER BY created_at DESC
		 LIMIT ?`,
		limit,
	)
	if err != nil {
		return nil, fmt.Errorf("ListAllTasks: %w", err)
	}
	defer rows.Close()

	var tasks []Task
	for rows.Next() {
		var t Task
		var planJSON sql.NullString
		var lastHBInt sql.NullInt64
		if err := rows.Scan(
			&t.ID, &planJSON, &t.Status, &t.SessionID, &t.CreatedAt,
			&lastHBInt, &t.NextPlanStepIndex,
			&t.TokenBudgetCap, &t.CostCapUSD,
		); err != nil {
			return nil, fmt.Errorf("ListAllTasks scan: %w", err)
		}
		t.PlanJSON = planJSON.String
		if lastHBInt.Valid {
			v := lastHBInt.Int64
			t.LastHeartbeatAt = &v
		}
		tasks = append(tasks, t)
	}
	return tasks, rows.Err()
}

// UpdateStatus transitions a task to a new status. Uses compare-and-swap
// to prevent re-transitioning a task that another goroutine already moved.
func (s *Store) UpdateStatus(id string, from, to TaskStatus) (bool, error) {
	res, err := s.db.Exec(
		`UPDATE tasks SET status = ? WHERE id = ? AND status = ?`,
		to, id, from,
	)
	if err != nil {
		return false, fmt.Errorf("UpdateStatus: %w", err)
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

// Heartbeat updates the last_heartbeat_at timestamp for a running task.
func (s *Store) Heartbeat(id string) error {
	_, err := s.db.Exec(
		`UPDATE tasks SET last_heartbeat_at = ? WHERE id = ? AND status = 'running'`,
		time.Now().UnixMilli(), id,
	)
	return err
}

// MarkZombies transitions tasks that have been running without a heartbeat
// for longer than staleThreshold to zombie status. Returns the count.
func (s *Store) MarkZombies(staleThreshold time.Duration) (int, error) {
	cutoff := time.Now().Add(-staleThreshold).UnixMilli()
	res, err := s.db.Exec(
		`UPDATE tasks SET status = 'zombie'
		 WHERE status = 'running'
		   AND (
		     (last_heartbeat_at IS NOT NULL AND last_heartbeat_at < ?)
		     OR (last_heartbeat_at IS NULL AND created_at < ?)
		   )`,
		cutoff, cutoff,
	)
	if err != nil {
		return 0, fmt.Errorf("MarkZombies: %w", err)
	}
	n, _ := res.RowsAffected()
	return int(n), nil
}

// AdvanceStep increments next_plan_step_index for a task.
func (s *Store) AdvanceStep(id string, stepIndex int) error {
	_, err := s.db.Exec(
		`UPDATE tasks SET next_plan_step_index = ? WHERE id = ?`,
		stepIndex, id,
	)
	return err
}

// AddEvent inserts a task event.
func (s *Store) AddEvent(taskID, stepID string, attempt, sequence int, kind EventKind, payloadJSON string) error {
	_, err := s.db.Exec(
		`INSERT INTO task_events (task_id, step_id, attempt, sequence, kind, payload_json, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		taskID, stepID, attempt, sequence, kind, payloadJSON, time.Now().UnixMilli(),
	)
	if err != nil {
		return fmt.Errorf("AddEvent: %w", err)
	}
	return nil
}

// ListEvents returns events for a task, ordered by sequence.
func (s *Store) ListEvents(taskID string, limit int) ([]TaskEvent, error) {
	rows, err := s.db.Query(
		`SELECT id, task_id, step_id, attempt, sequence, kind, payload_json, created_at
		 FROM task_events
		 WHERE task_id = ?
		 ORDER BY sequence ASC
		 LIMIT ?`,
		taskID, limit,
	)
	if err != nil {
		return nil, fmt.Errorf("ListEvents: %w", err)
	}
	defer rows.Close()

	var events []TaskEvent
	for rows.Next() {
		var e TaskEvent
		var payload sql.NullString
		if err := rows.Scan(
			&e.ID, &e.TaskID, &e.StepID, &e.Attempt, &e.Sequence,
			&e.Kind, &payload, &e.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("ListEvents scan: %w", err)
		}
		e.PayloadJSON = payload.String
		events = append(events, e)
	}
	return events, rows.Err()
}
