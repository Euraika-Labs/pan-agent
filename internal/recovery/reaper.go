package recovery

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

const (
	defaultReaperInterval = 10 * time.Second
	defaultStaleAfter     = 60 * time.Second
	defaultPurgeAge       = 7 * 24 * time.Hour
)

// Reaper runs on a fixed interval and performs:
//  1. Snapshot purge — removes snapshot trees older than purgeAge.
//  2. Orphan cleanup — removes snapshot dirs with no matching action_receipt.
//
// TODO(WS4): heartbeat-demote-to-zombie path goes here. The Reaper struct
// already accepts a heartbeatWriter *sql.DB so WS#4 can add the UPDATE without
// architectural change. For Phase 12 WS#2 scope, that UPDATE is omitted.
type Reaper struct {
	// db is the main reader pool — used for orphan ID lookups.
	db *sql.DB
	// heartbeatWriter is a SEPARATE *sql.DB instance opened against the same
	// WAL-mode file. It must not share SetMaxOpenConns(1) with the main pool
	// so that a long main-writer transaction cannot starve heartbeat writes
	// past the staleAfter threshold. See architect spec C3.
	// For WS#2 scope this field is declared but the demote-to-zombie path is
	// not yet implemented — see TODO(WS4) above.
	heartbeatWriter *sql.DB

	snap       *Snapshotter
	interval   time.Duration
	staleAfter time.Duration
	purgeAge   time.Duration
	quit       chan struct{}
	clock      func() time.Time
}

// NewReaper constructs a Reaper. db is the main pool; heartbeatWriter is a
// second *sql.DB opened against the same file (WAL mode, no conn limit).
// Pass nil for heartbeatWriter until WS#4 wires it up.
func NewReaper(db *sql.DB, heartbeatWriter *sql.DB, s *Snapshotter) *Reaper {
	return &Reaper{
		db:              db,
		heartbeatWriter: heartbeatWriter,
		snap:            s,
		interval:        defaultReaperInterval,
		staleAfter:      defaultStaleAfter,
		purgeAge:        defaultPurgeAge,
		quit:            make(chan struct{}),
		clock:           time.Now,
	}
}

// SetInterval overrides the tick interval. Exposed for tests.
func (r *Reaper) SetInterval(d time.Duration) { r.interval = d }

// SetPurgeAge overrides the snapshot retention age. Exposed for tests.
func (r *Reaper) SetPurgeAge(d time.Duration) { r.purgeAge = d }

// SetClock replaces the clock. Exposed for tests.
func (r *Reaper) SetClock(fn func() time.Time) { r.clock = fn }

// Start begins the reaper loop. It runs until ctx is cancelled or Stop is called.
// Intended to be run in a goroutine: go reaper.Start(ctx).
func (r *Reaper) Start(ctx context.Context) error {
	ticker := time.NewTicker(r.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-r.quit:
			return nil
		case <-ticker.C:
			r.tick(ctx)
		}
	}
}

// Stop signals the reaper to exit after the current tick completes.
func (r *Reaper) Stop() {
	select {
	case r.quit <- struct{}{}:
	default:
	}
}

// tick runs one reaper cycle.
func (r *Reaper) tick(ctx context.Context) {
	now := r.clock()

	// TODO(WS4): heartbeat sweep goes here.
	// UPDATE tasks SET status='zombie'
	// WHERE status='running' AND last_heartbeat_at < (now - staleAfter).Unix()
	// Must run on r.heartbeatWriter to avoid starving the main pool.

	// Snapshot purge: remove trees older than purgeAge.
	cutoff := now.Add(-r.purgeAge).Unix()
	if err := r.snap.Purge(ctx, cutoff); err != nil {
		// Non-fatal: log and continue.
		_ = fmt.Errorf("reaper: purge: %w", err)
	}

	// Orphan cleanup: collect known receipt IDs, remove unknown snapshot dirs.
	known, err := r.loadReceiptIDs(ctx)
	if err == nil {
		_ = r.snap.PurgeOrphans(ctx, known)
	}
}

// loadReceiptIDs returns the set of all receipt IDs currently in action_receipts.
func (r *Reaper) loadReceiptIDs(ctx context.Context) (map[string]bool, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT id FROM action_receipts`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	ids := make(map[string]bool)
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids[id] = true
	}
	return ids, rows.Err()
}
