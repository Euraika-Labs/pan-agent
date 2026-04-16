package cron

import (
	"context"
	"log"
	"strings"
	"sync"
	"time"
)

// Dispatch is called with a due job's metadata. Implementations should kick
// off execution of job.Prompt however the host prefers (HTTP self-call,
// direct LLM client, etc.) — the scheduler stays policy-free.
//
// The callback is invoked synchronously from the scheduler goroutine, so
// long-running dispatchers should hand off to their own worker pool and
// return promptly. A non-nil error is recorded on the job as LastError.
type Dispatch func(ctx context.Context, job Job) error

// defaultTickInterval is the scheduler poll period. Deliberately chunky
// (30s) because cron jobs are minute-granularity and the alternative is
// a very chatty goroutine burning battery.
const defaultTickInterval = 30 * time.Second

// Scheduler polls the jobs file, finds due jobs, calls Dispatch for each,
// and updates NextRun according to the schedule expression.
//
// Start once at server boot; Stop it during graceful shutdown.
type Scheduler struct {
	dispatch Dispatch
	interval time.Duration

	mu      sync.Mutex
	running map[string]bool // job IDs currently being dispatched
}

// NewScheduler returns an initialised Scheduler.
func NewScheduler(dispatch Dispatch) *Scheduler {
	return &Scheduler{
		dispatch: dispatch,
		interval: defaultTickInterval,
		running:  make(map[string]bool),
	}
}

// SetInterval overrides the default tick period. Primarily for tests.
func (s *Scheduler) SetInterval(d time.Duration) {
	if d > 0 {
		s.interval = d
	}
}

// Start runs the scheduler loop until ctx is cancelled. It returns
// immediately; the loop runs in a goroutine.
func (s *Scheduler) Start(ctx context.Context) {
	go s.loop(ctx)
}

func (s *Scheduler) loop(ctx context.Context) {
	// Tick once immediately at boot so jobs with NextRun already in the
	// past don't have to wait for the first interval.
	s.tick(ctx)
	t := time.NewTicker(s.interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			s.tick(ctx)
		}
	}
}

func (s *Scheduler) tick(ctx context.Context) {
	defer func() {
		if r := recover(); r != nil {
			// One bad dispatcher must not kill the scheduler.
			log.Printf("[cron] scheduler tick panic recovered: %v", r)
		}
	}()

	jobs, err := List()
	if err != nil {
		log.Printf("[cron] scheduler list: %v", err)
		return
	}
	now := nowMs()
	for _, j := range jobs {
		if !j.Enabled || j.State != StateActive {
			continue
		}
		if j.NextRun == nil || *j.NextRun > now {
			continue
		}
		// Skip if a previous tick's dispatch is still in flight — prevents
		// pile-up on a slow job.
		s.mu.Lock()
		if s.running[j.ID] {
			s.mu.Unlock()
			continue
		}
		s.running[j.ID] = true
		s.mu.Unlock()

		go s.runOne(ctx, j)
	}
}

func (s *Scheduler) runOne(ctx context.Context, j Job) {
	defer func() {
		s.mu.Lock()
		delete(s.running, j.ID)
		s.mu.Unlock()
		if r := recover(); r != nil {
			_ = updateJob(j.ID, func(j *Job) {
				j.LastError = "dispatcher panic"
				j.LastStatus = "error"
			})
		}
	}()

	// Dispatch. Errors are recorded on the job but do not stop the
	// scheduler.
	var errMsg string
	status := "ok"
	if err := s.dispatch(ctx, j); err != nil {
		errMsg = err.Error()
		status = "error"
	}

	next := computeNextRun(j.Schedule, nowMs())
	_ = updateJob(j.ID, func(j *Job) {
		lr := nowMs()
		j.LastRun = &lr
		j.LastStatus = status
		j.LastError = errMsg
		if next != nil {
			j.NextRun = next
		} else {
			// Non-repeating one-shot completed.
			j.NextRun = nil
			j.State = StateCompleted
			j.Enabled = false
		}
	})
}

// computeNextRun parses the Schedule string and returns the next Unix-ms
// timestamp, or nil if this job should not repeat.
//
// Supported formats (intentionally minimal):
//   - duration like "30m", "2h", "24h" → repeats at that interval
//   - "@every 5m" (systemd / quartz style) → same as plain duration
//   - "once" / empty → do not repeat
//
// Full cron-expression parsing (`0 0 * * *`) is out of scope for this
// minimal scheduler; add a real expression parser when the product
// needs day-of-week scheduling.
func computeNextRun(schedule string, nowMs int64) *int64 {
	s := strings.TrimSpace(schedule)
	s = strings.TrimPrefix(s, "@every ")
	if s == "" || strings.EqualFold(s, "once") {
		return nil
	}
	d, err := time.ParseDuration(s)
	if err != nil || d <= 0 {
		return nil
	}
	next := nowMs + d.Milliseconds()
	return &next
}
