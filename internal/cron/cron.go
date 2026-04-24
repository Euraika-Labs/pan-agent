// Package cron manages scheduled jobs stored in cron/jobs.json.
//
// Each Job has a cron schedule expression, a prompt, state (active/paused/
// completed), and optional metadata.  The file format is a JSON array so that
// it is human-editable and compatible with the TypeScript jobs.json produced by
// the pan-agent process.
//
// All mutations are protected by an in-process mutex.  The file is the source
// of truth for cross-process coordination.
package cron

import (
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/euraika-labs/pan-agent/internal/paths"
)

// ---------------------------------------------------------------------------
// Types
// ---------------------------------------------------------------------------

// State represents the lifecycle state of a Job.
type State string

const (
	StateActive    State = "active"
	StatePaused    State = "paused"
	StateCompleted State = "completed"
)

// Repeat tracks repeat-count metadata for a job.
type Repeat struct {
	Times     *int `json:"times"` // nil means unlimited
	Completed int  `json:"completed"`
}

// Job is a single scheduled task.
type Job struct {
	ID         string   `json:"id"`
	Name       string   `json:"name"`
	Schedule   string   `json:"schedule"`
	Prompt     string   `json:"prompt"`
	State      State    `json:"state"`
	Enabled    bool     `json:"enabled"`
	NextRun    *int64   `json:"next_run_at,omitempty"` // Unix ms; nil = not yet scheduled
	LastRun    *int64   `json:"last_run_at,omitempty"` // Unix ms; nil = never run
	LastStatus string   `json:"last_status,omitempty"`
	LastError  string   `json:"last_error,omitempty"`
	Repeat     *Repeat  `json:"repeat,omitempty"`
	Deliver    []string `json:"deliver,omitempty"`
	Skills     []string `json:"skills,omitempty"`
	Script     string   `json:"script,omitempty"`
	CostCapUSD float64  `json:"cost_cap_usd,omitempty"`
}

// ---------------------------------------------------------------------------
// File I/O
// ---------------------------------------------------------------------------

var mu sync.Mutex

func filePath() string {
	return paths.CronJobsFile()
}

// rawJob mirrors the on-disk shape which may differ slightly from Job.
// Specifically, schedule may be stored as a nested object with a "value" key
// (pan-agent legacy format) or as a plain string.  We normalise on read.
type rawJob struct {
	ID              string          `json:"id"`
	Name            string          `json:"name"`
	Schedule        json.RawMessage `json:"schedule"` // string OR {"value":"..."}
	ScheduleDisplay string          `json:"schedule_display"`
	Prompt          string          `json:"prompt"`
	State           string          `json:"state"`
	Enabled         *bool           `json:"enabled"`
	NextRun         *int64          `json:"next_run_at"`
	LastRun         *int64          `json:"last_run_at"`
	LastStatus      string          `json:"last_status"`
	LastError       string          `json:"last_error"`
	Repeat          *Repeat         `json:"repeat"`
	Deliver         json.RawMessage `json:"deliver"` // string OR []string
	Skills          json.RawMessage `json:"skills"`  // string OR []string
	Script          string          `json:"script"`
	CostCapUSD      float64         `json:"cost_cap_usd,omitempty"`
}

// scheduleValue is the nested-object schedule format used by pan-agent legacy jobs.
type scheduleValue struct {
	Value string `json:"value"`
}

func decodeSchedule(raw json.RawMessage) string {
	if len(raw) == 0 {
		return "?"
	}
	// Try plain string first.
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return s
	}
	// Try {"value":"..."}.
	var sv scheduleValue
	if err := json.Unmarshal(raw, &sv); err == nil && sv.Value != "" {
		return sv.Value
	}
	return "?"
}

func decodeStringSlice(raw json.RawMessage) []string {
	if len(raw) == 0 {
		return nil
	}
	var arr []string
	if err := json.Unmarshal(raw, &arr); err == nil {
		return arr
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil && s != "" {
		return []string{s}
	}
	return nil
}

func fromRaw(r rawJob) Job {
	enabled := true
	if r.Enabled != nil {
		enabled = *r.Enabled
	}

	var state State
	switch {
	case r.State == "paused" || !enabled:
		state = StatePaused
	case r.State == "completed":
		state = StateCompleted
	default:
		state = StateActive
	}

	name := r.Name
	if name == "" {
		name = "(unnamed)"
	}

	schedule := r.ScheduleDisplay
	if schedule == "" {
		schedule = decodeSchedule(r.Schedule)
	}

	deliver := decodeStringSlice(r.Deliver)
	if len(deliver) == 0 {
		deliver = []string{"local"}
	}

	skills := decodeStringSlice(r.Skills)

	return Job{
		ID:         r.ID,
		Name:       name,
		Schedule:   schedule,
		Prompt:     r.Prompt,
		State:      state,
		Enabled:    enabled,
		NextRun:    r.NextRun,
		LastRun:    r.LastRun,
		LastStatus: r.LastStatus,
		LastError:  r.LastError,
		Repeat:     r.Repeat,
		Deliver:    deliver,
		Skills:     skills,
		Script:     r.Script,
		CostCapUSD: r.CostCapUSD,
	}
}

func readJobs() ([]Job, error) {
	data, err := os.ReadFile(filePath())
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("cron: read jobs file: %w", err)
	}

	// The file may be a JSON array or an object with a "jobs" key.
	var rawArray []rawJob
	if err := json.Unmarshal(data, &rawArray); err != nil {
		var wrapped struct {
			Jobs []rawJob `json:"jobs"`
		}
		if err2 := json.Unmarshal(data, &wrapped); err2 != nil {
			return nil, fmt.Errorf("cron: parse jobs file: %w", err)
		}
		rawArray = wrapped.Jobs
	}

	jobs := make([]Job, 0, len(rawArray))
	for _, r := range rawArray {
		if r.ID == "" {
			continue // skip malformed entries
		}
		jobs = append(jobs, fromRaw(r))
	}
	return jobs, nil
}

// writeJobs persists jobs as a plain JSON array using a temp-file + rename
// pattern so a crash mid-write cannot leave a truncated jobs file (the
// previous os.WriteFile truncates-then-writes and lost every job on any
// crash during the ~ms write window).
func writeJobs(jobs []Job) error {
	data, err := json.MarshalIndent(jobs, "", "  ")
	if err != nil {
		return fmt.Errorf("cron: marshal jobs: %w", err)
	}
	finalPath := filePath()
	dir := filepath.Dir(finalPath)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("cron: mkdir: %w", err)
	}
	tmp, err := os.CreateTemp(dir, "jobs.*.json.tmp")
	if err != nil {
		return fmt.Errorf("cron: create temp: %w", err)
	}
	tmpName := tmp.Name()
	// Best-effort cleanup on any failure path.
	defer func() { _ = os.Remove(tmpName) }()
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("cron: write temp: %w", err)
	}
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("cron: chmod temp: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("cron: fsync temp: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("cron: close temp: %w", err)
	}
	if err := os.Rename(tmpName, finalPath); err != nil {
		return fmt.Errorf("cron: rename jobs file: %w", err)
	}
	return nil
}

// ---------------------------------------------------------------------------
// UUID helper (same implementation as models package — no external dep)
// ---------------------------------------------------------------------------

func newUUID() string {
	var b [16]byte
	if _, err := io.ReadFull(rand.Reader, b[:]); err != nil {
		return fmt.Sprintf("fallback-%d", time.Now().UnixNano())
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x",
		b[0:4], b[4:6], b[6:8], b[8:10], b[10:])
}

func nowMs() int64 {
	return time.Now().UnixMilli()
}

func ptrInt64(v int64) *int64 { return &v }

// ---------------------------------------------------------------------------
// Public API
// ---------------------------------------------------------------------------

// List returns all cron jobs (enabled and disabled).
func List() ([]Job, error) {
	mu.Lock()
	defer mu.Unlock()
	return readJobs()
}

// Create adds a new job with the given schedule expression, prompt, and
// optional name.  The job starts in the active/enabled state.
func Create(name, schedule, prompt string) (*Job, error) {
	mu.Lock()
	defer mu.Unlock()

	jobs, err := readJobs()
	if err != nil {
		return nil, err
	}

	if name == "" {
		name = "(unnamed)"
	}

	now := nowMs()
	job := Job{
		ID:       newUUID(),
		Name:     name,
		Schedule: schedule,
		Prompt:   prompt,
		State:    StateActive,
		Enabled:  true,
		NextRun:  ptrInt64(now), // start as soon as possible
		Deliver:  []string{"local"},
	}

	jobs = append(jobs, job)
	if err := writeJobs(jobs); err != nil {
		return nil, err
	}
	return &job, nil
}

// Remove deletes the job with the given ID.
func Remove(id string) error {
	if id == "" {
		return fmt.Errorf("cron: remove: empty id")
	}
	mu.Lock()
	defer mu.Unlock()

	jobs, err := readJobs()
	if err != nil {
		return err
	}

	filtered := jobs[:0]
	found := false
	for _, j := range jobs {
		if j.ID == id {
			found = true
			continue
		}
		filtered = append(filtered, j)
	}
	if !found {
		return fmt.Errorf("cron: job %q not found", id)
	}

	return writeJobs(filtered)
}

// Pause sets a job's state to paused and disabled=false.
func Pause(id string) error {
	if id == "" {
		return fmt.Errorf("cron: pause: empty id")
	}
	return updateJob(id, func(j *Job) {
		j.State = StatePaused
		j.Enabled = false
	})
}

// Resume re-enables a paused job and sets its state back to active.
func Resume(id string) error {
	if id == "" {
		return fmt.Errorf("cron: resume: empty id")
	}
	return updateJob(id, func(j *Job) {
		j.State = StateActive
		j.Enabled = true
	})
}

// Trigger sets the job's next_run_at to now, causing the scheduler to pick it
// up on its next tick.
func Trigger(id string) error {
	if id == "" {
		return fmt.Errorf("cron: trigger: empty id")
	}
	now := nowMs()
	return updateJob(id, func(j *Job) {
		j.NextRun = ptrInt64(now)
	})
}

// ---------------------------------------------------------------------------
// Internal helpers
// ---------------------------------------------------------------------------

// updateJob applies a mutation function to the job with the given ID and
// persists the result.  Caller must not hold mu.
func updateJob(id string, fn func(*Job)) error {
	mu.Lock()
	defer mu.Unlock()

	jobs, err := readJobs()
	if err != nil {
		return err
	}

	idx := -1
	for i := range jobs {
		if jobs[i].ID == id {
			idx = i
			break
		}
	}
	if idx == -1 {
		return fmt.Errorf("cron: job %q not found", id)
	}

	fn(&jobs[idx])
	return writeJobs(jobs)
}
