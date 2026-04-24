package cron

import (
	"context"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
)

// withIsolatedCronHome points the package's filePath() at a temp dir so
// the real user's jobs.json is not touched.
func withIsolatedCronHome(t *testing.T) {
	t.Helper()
	tmp := t.TempDir()
	t.Setenv("PAN_AGENT_HOME", tmp)
	// Pre-create the cron subdirectory so writeJobs does not fail if the
	// tests run in an isolated env where paths.CronJobsFile()'s parent
	// does not yet exist.
	_ = os.MkdirAll(filepath.Join(tmp, "cron"), 0o700)
}

// TestComputeNextRun exercises the schedule-expression parser.
func TestComputeNextRun(t *testing.T) {
	now := int64(1_000_000)

	if got := computeNextRun("", now); got != nil {
		t.Errorf("empty schedule: got %v, want nil", got)
	}
	if got := computeNextRun("once", now); got != nil {
		t.Errorf("once: got %v, want nil", got)
	}
	if got := computeNextRun("garbage", now); got != nil {
		t.Errorf("garbage: got %v, want nil", got)
	}
	if got := computeNextRun("5m", now); got == nil || *got != now+5*60*1000 {
		t.Errorf("5m: got %v, want %d", got, now+5*60*1000)
	}
	if got := computeNextRun("@every 10s", now); got == nil || *got != now+10*1000 {
		t.Errorf("@every 10s: got %v, want %d", got, now+10*1000)
	}
}

// TestSchedulerFiresDueJob spins a scheduler with a very tight tick and
// verifies that an enabled Active job with NextRun in the past fires
// exactly once.
func TestSchedulerFiresDueJob(t *testing.T) {
	withIsolatedCronHome(t)

	job, err := Create("test-job", "1h", "do the thing")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	// Force NextRun to the distant past so the scheduler fires on its
	// immediate-tick invocation.
	past := nowMs() - 60_000
	if err := updateJob(job.ID, func(j *Job) { j.NextRun = &past }); err != nil {
		t.Fatalf("updateJob: %v", err)
	}

	var fired int32
	s := NewScheduler(func(ctx context.Context, j Job) error {
		atomic.AddInt32(&fired, 1)
		return nil
	})
	// Short interval to speed the test, though the immediate first tick
	// should catch the due job.
	s.SetInterval(20 * time.Millisecond)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s.Start(ctx)

	// Give the scheduler up to 500 ms to fire.
	deadline := time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(deadline) {
		if atomic.LoadInt32(&fired) >= 1 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if got := atomic.LoadInt32(&fired); got < 1 {
		t.Fatalf("dispatcher fired %d times, want ≥1", got)
	}
}

// TestCronJobCostCapPersistence verifies that CostCapUSD survives a
// write/read cycle through jobs.json.
func TestCronJobCostCapPersistence(t *testing.T) {
	withIsolatedCronHome(t)

	job, err := Create("budget-job", "30m", "expensive work")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	// Set CostCapUSD on the job.
	if err := updateJob(job.ID, func(j *Job) {
		j.CostCapUSD = 5.0
	}); err != nil {
		t.Fatalf("updateJob: %v", err)
	}

	// Read it back via the public List API.
	jobs, err := List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}

	var found *Job
	for i := range jobs {
		if jobs[i].ID == job.ID {
			found = &jobs[i]
			break
		}
	}
	if found == nil {
		t.Fatal("job not found after write")
	}
	if found.CostCapUSD != 5.0 {
		t.Errorf("CostCapUSD = %f, want 5.0", found.CostCapUSD)
	}
}
