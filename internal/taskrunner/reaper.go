package taskrunner

import (
	"context"
	"log"
	"time"
)

const (
	reaperCadence  = 10 * time.Second
	staleThreshold = 60 * time.Second
)

// Reaper periodically scans for running tasks that have missed their
// heartbeat deadline and transitions them to zombie status.
type Reaper struct {
	store *Store
}

// NewReaper creates a Reaper backed by the given Store.
func NewReaper(store *Store) *Reaper {
	return &Reaper{store: store}
}

// Run starts the reaper loop. It blocks until ctx is cancelled.
func (r *Reaper) Run(ctx context.Context) {
	ticker := time.NewTicker(reaperCadence)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			n, err := r.store.MarkZombies(staleThreshold)
			if err != nil {
				log.Printf("[reaper] error marking zombies: %v", err)
				continue
			}
			if n > 0 {
				log.Printf("[reaper] marked %d stale tasks as zombie", n)
			}
		}
	}
}
