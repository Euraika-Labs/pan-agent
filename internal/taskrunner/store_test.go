package taskrunner

import (
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

func openTestDB(t *testing.T) *sql.DB {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "test.db")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { db.Close() })

	schema := `
PRAGMA journal_mode = WAL;
PRAGMA foreign_keys = ON;

CREATE TABLE IF NOT EXISTS tasks (
    id                   TEXT    PRIMARY KEY,
    plan_json            TEXT,
    status               TEXT    NOT NULL DEFAULT 'queued',
    session_id           TEXT    NOT NULL,
    created_at           INTEGER NOT NULL,
    last_heartbeat_at    INTEGER,
    next_plan_step_index INTEGER DEFAULT 0,
    token_budget_cap     INTEGER DEFAULT 0,
    cost_cap_usd         REAL    DEFAULT 0
);

CREATE TABLE IF NOT EXISTS task_events (
    id           INTEGER PRIMARY KEY AUTOINCREMENT,
    task_id      TEXT    NOT NULL,
    step_id      TEXT    NOT NULL,
    attempt      INTEGER NOT NULL DEFAULT 1,
    sequence     INTEGER NOT NULL,
    kind         TEXT    NOT NULL,
    payload_json TEXT,
    created_at   INTEGER NOT NULL,
    FOREIGN KEY (task_id) REFERENCES tasks(id),
    UNIQUE (task_id, step_id, kind, attempt)
);`

	if _, err := db.Exec(schema); err != nil {
		t.Fatal(err)
	}
	return db
}

func TestCreateTask(t *testing.T) {
	s := NewStore(openTestDB(t))

	task, err := s.CreateTask("sess-1", `{"steps":[]}`, 5.0)
	if err != nil {
		t.Fatal(err)
	}
	if task.ID == "" {
		t.Error("ID is empty")
	}
	if task.Status != StatusQueued {
		t.Errorf("status = %q, want %q", task.Status, StatusQueued)
	}
	if task.SessionID != "sess-1" {
		t.Errorf("session_id = %q, want %q", task.SessionID, "sess-1")
	}
}

func TestGetTask(t *testing.T) {
	s := NewStore(openTestDB(t))

	created, _ := s.CreateTask("sess-1", `{}`, 10.0)
	got, err := s.GetTask(created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != created.ID {
		t.Errorf("ID = %q, want %q", got.ID, created.ID)
	}
	if got.CostCapUSD != 10.0 {
		t.Errorf("CostCapUSD = %f, want 10.0", got.CostCapUSD)
	}
}

func TestListTasks(t *testing.T) {
	s := NewStore(openTestDB(t))

	s.CreateTask("sess-1", `{}`, 0)
	s.CreateTask("sess-1", `{}`, 0)
	s.CreateTask("sess-2", `{}`, 0)

	tasks, err := s.ListTasks("sess-1", 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(tasks) != 2 {
		t.Errorf("got %d tasks, want 2", len(tasks))
	}
}

func TestUpdateStatus(t *testing.T) {
	s := NewStore(openTestDB(t))
	task, _ := s.CreateTask("s", `{}`, 0)

	ok, err := s.UpdateStatus(task.ID, StatusQueued, StatusRunning)
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Error("UpdateStatus returned false")
	}

	got, _ := s.GetTask(task.ID)
	if got.Status != StatusRunning {
		t.Errorf("status = %q, want %q", got.Status, StatusRunning)
	}

	// CAS: wrong from-state should fail
	ok, err = s.UpdateStatus(task.ID, StatusQueued, StatusPaused)
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Error("expected CAS failure when from-state doesn't match")
	}
}

func TestHeartbeatAndZombies(t *testing.T) {
	db := openTestDB(t)
	s := NewStore(db)
	task, _ := s.CreateTask("s", `{}`, 0)
	s.UpdateStatus(task.ID, StatusQueued, StatusRunning)

	// Set heartbeat to 2 minutes ago so it's definitely stale.
	pastHB := time.Now().Add(-2 * time.Minute).UnixMilli()
	db.Exec(`UPDATE tasks SET last_heartbeat_at = ? WHERE id = ?`, pastHB, task.ID)

	n, err := s.MarkZombies(60 * time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Errorf("marked %d zombies, want 1", n)
	}

	got, _ := s.GetTask(task.ID)
	if got.Status != StatusZombie {
		t.Errorf("status = %q, want %q", got.Status, StatusZombie)
	}
}

func TestHeartbeatFresh(t *testing.T) {
	s := NewStore(openTestDB(t))
	task, _ := s.CreateTask("s", `{}`, 0)
	s.UpdateStatus(task.ID, StatusQueued, StatusRunning)
	s.Heartbeat(task.ID)

	// Fresh heartbeat should NOT be marked zombie.
	n, err := s.MarkZombies(60 * time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Errorf("marked %d zombies, want 0 (heartbeat is fresh)", n)
	}
}

func TestMarkZombies_NullHeartbeat(t *testing.T) {
	db := openTestDB(t)
	s := NewStore(db)
	task, _ := s.CreateTask("s", `{}`, 0)
	s.UpdateStatus(task.ID, StatusQueued, StatusRunning)

	// Backdate created_at to 2 minutes ago; never sent a heartbeat.
	past := time.Now().Add(-2 * time.Minute).UnixMilli()
	db.Exec(`UPDATE tasks SET created_at = ? WHERE id = ?`, past, task.ID)

	n, err := s.MarkZombies(60 * time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Errorf("marked %d zombies, want 1 (null heartbeat, stale created_at)", n)
	}

	got, _ := s.GetTask(task.ID)
	if got.Status != StatusZombie {
		t.Errorf("status = %q, want %q", got.Status, StatusZombie)
	}
}

func TestAddAndListEvents(t *testing.T) {
	s := NewStore(openTestDB(t))
	task, _ := s.CreateTask("s", `{}`, 0)

	err := s.AddEvent(task.ID, "step-1", 1, 1, EventToolCall, `{"tool":"browser"}`)
	if err != nil {
		t.Fatal(err)
	}
	err = s.AddEvent(task.ID, "step-1", 1, 2, EventStepCompleted, "")
	if err != nil {
		t.Fatal(err)
	}

	events, err := s.ListEvents(task.ID, 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 2 {
		t.Fatalf("got %d events, want 2", len(events))
	}
	if events[0].Kind != EventToolCall {
		t.Errorf("events[0].Kind = %q, want %q", events[0].Kind, EventToolCall)
	}
	if events[1].Kind != EventStepCompleted {
		t.Errorf("events[1].Kind = %q, want %q", events[1].Kind, EventStepCompleted)
	}
}

func TestUpdateStatus_CAS_WrongFromState(t *testing.T) {
	s := NewStore(openTestDB(t))
	task, _ := s.CreateTask("s", `{}`, 0)
	s.UpdateStatus(task.ID, StatusQueued, StatusRunning)

	// CAS with wrong from-state should fail.
	ok, err := s.UpdateStatus(task.ID, StatusQueued, StatusCancelled)
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Error("CAS with wrong from-state should return false")
	}

	// Verify task is still running.
	got, _ := s.GetTask(task.ID)
	if got.Status != StatusRunning {
		t.Errorf("status = %q, want running", got.Status)
	}
}

func TestListAllTasks(t *testing.T) {
	s := NewStore(openTestDB(t))

	s.CreateTask("sess-1", `{}`, 0)
	s.CreateTask("sess-2", `{}`, 0)

	tasks, err := s.ListAllTasks(100)
	if err != nil {
		t.Fatal(err)
	}
	if len(tasks) != 2 {
		t.Errorf("got %d tasks, want 2", len(tasks))
	}
}

func TestEventUniqueness(t *testing.T) {
	s := NewStore(openTestDB(t))
	task, _ := s.CreateTask("s", `{}`, 0)

	err := s.AddEvent(task.ID, "step-1", 1, 1, EventToolCall, `{}`)
	if err != nil {
		t.Fatal(err)
	}
	// Duplicate (same task_id, step_id, kind, attempt) should fail
	err = s.AddEvent(task.ID, "step-1", 1, 2, EventToolCall, `{}`)
	if err == nil {
		t.Error("expected uniqueness violation, got nil")
	}
}
