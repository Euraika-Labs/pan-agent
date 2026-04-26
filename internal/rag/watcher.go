package rag

import (
	"context"
	"fmt"
	"log"
	"strconv"
	"sync"
	"time"

	"github.com/euraika-labs/pan-agent/internal/storage"
)

// Phase 13 WS#13.B — message watcher. Polls the messages table for
// new entries and feeds them through Index.Upsert so the semantic
// index stays in sync with the chat history without each AddMessage
// call blocking on the embedder.
//
// Why polling instead of an AddMessage hook:
//
//   1. Decouples the chat hot path from the embedder. AddMessage is
//      called on every streaming token finalisation; embedding takes
//      a network round-trip. A polling watcher absorbs latency
//      without holding up the writer.
//   2. Crash-safe by construction. The cursor is persisted in
//      rag_state, so a crash mid-tick re-processes the unfinished
//      batch on restart. Index.Upsert's content-hash gate makes the
//      re-processing a no-op for already-embedded rows.
//   3. Backpressure-friendly. If the embedder is slow or the rate
//      limit kicks in, the queue grows on the messages table side
//      (which is fine — messages persist regardless) instead of
//      stalling chat.
//
// Trade-offs accepted:
//   - Up to `interval` of indexing latency (default 5s).
//   - Tool-role messages are skipped because they are usually
//     command output that pollutes search results.

// MessageStore is the subset of *storage.DB methods Watcher needs.
// Defined as an interface so unit tests inject in-memory fakes.
type MessageStore interface {
	ListMessagesSince(afterID int64, limit int) ([]storage.Message, error)
	GetRAGState(key string) (string, bool, error)
	SetRAGState(key, value string) error
}

// WatcherOptions configures Watcher behaviour. Zero-value fields fall
// back to the documented defaults.
type WatcherOptions struct {
	// Interval between polling ticks. 0 → 5s.
	Interval time.Duration
	// BatchSize caps the number of messages drained per tick. 0 → 100.
	// Smaller batches keep the embedder's per-tick latency bounded.
	BatchSize int
	// CursorKey under which the watcher records its progress in
	// rag_state. 0-length → "watcher:last_message_id". Tests
	// override to isolate state across parallel runs.
	CursorKey string
	// MinTextLength skips messages whose content is shorter than this
	// after trim. 0 → 1 (only literal-empty messages are skipped).
	// Set higher to skip "ok"/"yes"-style replies that don't carry
	// search-worthy information.
	MinTextLength int
}

// Watcher is the polling auto-indexer. Construct with NewWatcher,
// run with Start, halt with Stop. Tick is exposed for tests + manual
// catch-up calls; production code typically only calls Start/Stop.
type Watcher struct {
	idx       *Index
	store     MessageStore
	interval  time.Duration
	batchSize int
	cursorKey string
	minLen    int

	mu      sync.Mutex
	running bool
	stopCh  chan struct{}
	doneCh  chan struct{}
}

// DefaultCursorKey is the rag_state key the watcher uses to record
// progress when WatcherOptions.CursorKey is empty.
const DefaultCursorKey = "watcher:last_message_id"

// NewWatcher constructs a Watcher. idx + store must be non-nil.
// Defaults applied in this order:
//
//	Interval      → 5s
//	BatchSize     → 100
//	CursorKey     → "watcher:last_message_id"
//	MinTextLength → 1 (only literally-empty messages skipped)
func NewWatcher(idx *Index, store MessageStore, opts WatcherOptions) (*Watcher, error) {
	if idx == nil {
		return nil, fmt.Errorf("rag: NewWatcher: idx required")
	}
	if store == nil {
		return nil, fmt.Errorf("rag: NewWatcher: store required")
	}
	w := &Watcher{
		idx:       idx,
		store:     store,
		interval:  opts.Interval,
		batchSize: opts.BatchSize,
		cursorKey: opts.CursorKey,
		minLen:    opts.MinTextLength,
	}
	if w.interval == 0 {
		w.interval = 5 * time.Second
	}
	if w.batchSize == 0 {
		w.batchSize = 100
	}
	if w.cursorKey == "" {
		w.cursorKey = DefaultCursorKey
	}
	if w.minLen == 0 {
		w.minLen = 1
	}
	return w, nil
}

// Start runs the watcher loop on a goroutine. Returns immediately
// after kicking it off; the loop runs until ctx is cancelled or
// Stop() is called. Calling Start while already running returns an
// error to surface the misuse.
func (w *Watcher) Start(ctx context.Context) error {
	w.mu.Lock()
	if w.running {
		w.mu.Unlock()
		return fmt.Errorf("rag: Watcher already running")
	}
	w.stopCh = make(chan struct{})
	w.doneCh = make(chan struct{})
	w.running = true
	w.mu.Unlock()

	go w.run(ctx)
	return nil
}

// Stop signals the watcher to halt and waits for the goroutine to
// finish. Idempotent: calling Stop on a stopped watcher is a no-op.
func (w *Watcher) Stop() error {
	w.mu.Lock()
	if !w.running {
		w.mu.Unlock()
		return nil
	}
	close(w.stopCh)
	done := w.doneCh
	w.running = false
	w.mu.Unlock()

	<-done
	return nil
}

// Running reports whether Start has been called and Stop has not.
func (w *Watcher) Running() bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.running
}

func (w *Watcher) run(ctx context.Context) {
	defer close(w.doneCh)

	// Drain once on startup so a freshly-launched server catches up
	// to the existing message log without waiting for the first tick.
	if _, err := w.Tick(ctx); err != nil {
		log.Printf("rag: watcher initial tick: %v", err)
	}

	ticker := time.NewTicker(w.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-w.stopCh:
			return
		case <-ticker.C:
			if _, err := w.Tick(ctx); err != nil {
				log.Printf("rag: watcher tick: %v", err)
			}
		}
	}
}

// Tick processes one batch of unprocessed messages. Returns the
// number of messages successfully indexed (cache hits count too —
// they don't trigger an embedder call but they do advance progress).
//
// Errors mid-batch fail the whole tick; the cursor is NOT advanced
// past the failure point so retries pick up where it stopped. The
// rag_state cursor is only persisted after the entire batch
// succeeds, which keeps the at-least-once guarantee tight.
func (w *Watcher) Tick(ctx context.Context) (int, error) {
	cursor, err := w.readCursor()
	if err != nil {
		return 0, fmt.Errorf("rag: Tick read cursor: %w", err)
	}

	msgs, err := w.store.ListMessagesSince(cursor, w.batchSize)
	if err != nil {
		return 0, fmt.Errorf("rag: Tick list: %w", err)
	}
	if len(msgs) == 0 {
		return 0, nil
	}

	indexed := 0
	maxID := cursor
	for _, m := range msgs {
		if ctx.Err() != nil {
			return indexed, ctx.Err()
		}
		if !w.shouldIndex(m) {
			// Skipped messages still advance the cursor so we don't
			// re-evaluate them on every tick.
			if m.ID > maxID {
				maxID = m.ID
			}
			continue
		}
		_, err := w.idx.Upsert(ctx, IngestRequest{
			Source:    "message",
			SourceID:  strconv.FormatInt(m.ID, 10),
			SessionID: m.SessionID,
			Text:      m.Content,
		})
		if err != nil {
			// Persist any progress made so far, then surface the
			// error. The next tick retries from the failure point.
			if maxID > cursor {
				_ = w.writeCursor(maxID)
			}
			return indexed, fmt.Errorf("rag: Tick upsert msg %d: %w", m.ID, err)
		}
		indexed++
		if m.ID > maxID {
			maxID = m.ID
		}
	}

	if maxID > cursor {
		if err := w.writeCursor(maxID); err != nil {
			return indexed, fmt.Errorf("rag: Tick write cursor: %w", err)
		}
	}
	return indexed, nil
}

// shouldIndex returns true when m carries enough signal to be worth
// embedding. Skips: tool messages (typically command output), system
// messages, role-less rows, and content shorter than minLen.
func (w *Watcher) shouldIndex(m storage.Message) bool {
	if m.Role != "user" && m.Role != "assistant" {
		return false
	}
	if len(m.Content) < w.minLen {
		return false
	}
	return true
}

func (w *Watcher) readCursor() (int64, error) {
	v, ok, err := w.store.GetRAGState(w.cursorKey)
	if err != nil {
		return 0, err
	}
	if !ok {
		return 0, nil
	}
	id, err := strconv.ParseInt(v, 10, 64)
	if err != nil {
		// Corrupt cursor — treat as zero. The next tick will re-read
		// from the start; the content-hash gate prevents duplicate
		// embeddings even after the rewind.
		log.Printf("rag: watcher cursor corrupt (%q), resetting to 0", v)
		return 0, nil
	}
	return id, nil
}

func (w *Watcher) writeCursor(id int64) error {
	return w.store.SetRAGState(w.cursorKey, strconv.FormatInt(id, 10))
}
