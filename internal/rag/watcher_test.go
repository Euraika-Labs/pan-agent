package rag

import (
	"context"
	"errors"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/euraika-labs/pan-agent/internal/storage"
)

// Phase 13 WS#13.B — watcher tests. Cover Tick semantics in isolation
// (cursor advance, role filter, min-length filter, error handling)
// plus a small Start/Stop liveness test.

// fakeMessageStore implements MessageStore without SQLite.
type fakeMessageStore struct {
	mu       sync.Mutex
	messages []storage.Message
	state    map[string]string

	// failOnSet, when true, makes SetRAGState return an error so we
	// can verify Tick handles persistence failures cleanly.
	failOnSet bool
}

func newFakeMessageStore() *fakeMessageStore {
	return &fakeMessageStore{state: map[string]string{}}
}

func (s *fakeMessageStore) ListMessagesSince(after int64, limit int) ([]storage.Message, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []storage.Message
	for _, m := range s.messages {
		if m.ID > after {
			out = append(out, m)
			if len(out) >= limit {
				break
			}
		}
	}
	return out, nil
}

func (s *fakeMessageStore) GetRAGState(key string) (string, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	v, ok := s.state[key]
	return v, ok, nil
}

func (s *fakeMessageStore) SetRAGState(key, value string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.failOnSet {
		return errors.New("simulated cursor write failure")
	}
	s.state[key] = value
	return nil
}

func (s *fakeMessageStore) addMsg(t *testing.T, role, content string) int64 {
	t.Helper()
	s.mu.Lock()
	defer s.mu.Unlock()
	id := int64(len(s.messages) + 1)
	s.messages = append(s.messages, storage.Message{
		ID: id, SessionID: "s-1", Role: role, Content: content,
		Timestamp: time.Now().Unix(),
	})
	return id
}

// newTestWatcher builds a Watcher over fake collaborators sharing the
// same fake store across the Index and the Watcher (so the Index's
// content-hash dedup is observable through the same backing rows).
func newTestWatcher(t *testing.T, opts WatcherOptions) (*Watcher, *fakeMessageStore, *fakeEmbedder) {
	t.Helper()
	em := &fakeEmbedder{model: "watcher-test", dim: 4}
	storeFake := &fakeStore{}
	idx, err := NewIndex(em, storeFake)
	if err != nil {
		t.Fatalf("NewIndex: %v", err)
	}
	msgs := newFakeMessageStore()
	w, err := NewWatcher(idx, msgs, opts)
	if err != nil {
		t.Fatalf("NewWatcher: %v", err)
	}
	return w, msgs, em
}

// ---------------------------------------------------------------------------
// Tick — cursor + role + length semantics
// ---------------------------------------------------------------------------

func TestNewWatcher_Validation(t *testing.T) {
	t.Parallel()
	em := &fakeEmbedder{model: "m", dim: 2}
	idx, _ := NewIndex(em, &fakeStore{})
	if _, err := NewWatcher(nil, newFakeMessageStore(), WatcherOptions{}); err == nil {
		t.Error("nil idx: expected error")
	}
	if _, err := NewWatcher(idx, nil, WatcherOptions{}); err == nil {
		t.Error("nil store: expected error")
	}
}

func TestWatcher_DefaultOptions(t *testing.T) {
	t.Parallel()
	w, _, _ := newTestWatcher(t, WatcherOptions{})
	if w.interval != 5*time.Second {
		t.Errorf("interval default = %v, want 5s", w.interval)
	}
	if w.batchSize != 100 {
		t.Errorf("batchSize default = %d, want 100", w.batchSize)
	}
	if w.cursorKey != DefaultCursorKey {
		t.Errorf("cursorKey default = %q, want %q", w.cursorKey, DefaultCursorKey)
	}
	if w.minLen != 1 {
		t.Errorf("minLen default = %d, want 1", w.minLen)
	}
}

func TestWatcher_Tick_FreshState(t *testing.T) {
	t.Parallel()
	w, msgs, em := newTestWatcher(t, WatcherOptions{BatchSize: 50})

	msgs.addMsg(t, "user", "hello world")
	msgs.addMsg(t, "assistant", "hi back")
	msgs.addMsg(t, "user", "how are you")

	n, err := w.Tick(context.Background())
	if err != nil {
		t.Fatalf("Tick: %v", err)
	}
	if n != 3 {
		t.Errorf("indexed = %d, want 3", n)
	}
	if em.callCount() != 3 {
		t.Errorf("embedder called %d times, want 3", em.callCount())
	}
	v, ok, _ := msgs.GetRAGState(w.cursorKey)
	if !ok || v != "3" {
		t.Errorf("cursor = %q (ok=%v), want \"3\"", v, ok)
	}
}

func TestWatcher_Tick_Idempotent(t *testing.T) {
	t.Parallel()
	w, msgs, em := newTestWatcher(t, WatcherOptions{BatchSize: 50})

	msgs.addMsg(t, "user", "msg-A")
	msgs.addMsg(t, "user", "msg-B")

	if _, err := w.Tick(context.Background()); err != nil {
		t.Fatalf("first Tick: %v", err)
	}
	first := em.callCount()

	// Second tick with no new messages: no embed calls, cursor unchanged.
	n, err := w.Tick(context.Background())
	if err != nil {
		t.Fatalf("second Tick: %v", err)
	}
	if n != 0 {
		t.Errorf("indexed = %d, want 0 on idle tick", n)
	}
	if em.callCount() != first {
		t.Errorf("embedder called extra times: %d vs %d", em.callCount(), first)
	}
}

func TestWatcher_Tick_OnlyNewMessages(t *testing.T) {
	t.Parallel()
	w, msgs, em := newTestWatcher(t, WatcherOptions{BatchSize: 50})

	msgs.addMsg(t, "user", "first")
	msgs.addMsg(t, "user", "second")
	if _, err := w.Tick(context.Background()); err != nil {
		t.Fatalf("first Tick: %v", err)
	}
	beforeNew := em.callCount()

	msgs.addMsg(t, "user", "third")
	msgs.addMsg(t, "assistant", "fourth")

	n, err := w.Tick(context.Background())
	if err != nil {
		t.Fatalf("second Tick: %v", err)
	}
	if n != 2 {
		t.Errorf("indexed = %d, want 2 (only new)", n)
	}
	delta := em.callCount() - beforeNew
	if delta != 2 {
		t.Errorf("embedder delta = %d, want 2", delta)
	}
}

func TestWatcher_Tick_SkipsToolAndSystem(t *testing.T) {
	t.Parallel()
	w, msgs, em := newTestWatcher(t, WatcherOptions{BatchSize: 50})

	msgs.addMsg(t, "user", "real one")
	msgs.addMsg(t, "tool", "command output")
	msgs.addMsg(t, "system", "system prompt")
	msgs.addMsg(t, "assistant", "real reply")

	n, err := w.Tick(context.Background())
	if err != nil {
		t.Fatalf("Tick: %v", err)
	}
	if n != 2 {
		t.Errorf("indexed = %d, want 2 (user + assistant only)", n)
	}
	if em.callCount() != 2 {
		t.Errorf("embedder calls = %d, want 2", em.callCount())
	}
	// Cursor must advance past skipped rows so they don't get re-checked.
	v, _, _ := msgs.GetRAGState(w.cursorKey)
	if v != "4" {
		t.Errorf("cursor = %q, want \"4\" (advanced past tool/system)", v)
	}
}

func TestWatcher_Tick_MinLengthFilter(t *testing.T) {
	t.Parallel()
	w, msgs, _ := newTestWatcher(t, WatcherOptions{MinTextLength: 10})

	msgs.addMsg(t, "user", "ok")                  // < 10 chars: skipped
	msgs.addMsg(t, "user", "this is long enough") // ≥ 10: indexed

	n, err := w.Tick(context.Background())
	if err != nil {
		t.Fatalf("Tick: %v", err)
	}
	if n != 1 {
		t.Errorf("indexed = %d, want 1", n)
	}
}

func TestWatcher_Tick_BatchSizeCaps(t *testing.T) {
	t.Parallel()
	w, msgs, _ := newTestWatcher(t, WatcherOptions{BatchSize: 2})

	for i := 0; i < 5; i++ {
		msgs.addMsg(t, "user", "msg-"+strconv.Itoa(i))
	}

	n, err := w.Tick(context.Background())
	if err != nil {
		t.Fatalf("Tick: %v", err)
	}
	if n != 2 {
		t.Errorf("first Tick: indexed = %d, want 2 (BatchSize cap)", n)
	}

	n, err = w.Tick(context.Background())
	if err != nil {
		t.Fatalf("second Tick: %v", err)
	}
	if n != 2 {
		t.Errorf("second Tick: indexed = %d, want 2", n)
	}

	n, err = w.Tick(context.Background())
	if err != nil {
		t.Fatalf("third Tick: %v", err)
	}
	if n != 1 {
		t.Errorf("third Tick: indexed = %d, want 1 (drain)", n)
	}
}

func TestWatcher_Tick_EmptyContent(t *testing.T) {
	t.Parallel()
	w, msgs, em := newTestWatcher(t, WatcherOptions{})

	msgs.addMsg(t, "user", "")
	msgs.addMsg(t, "user", "real")

	n, err := w.Tick(context.Background())
	if err != nil {
		t.Fatalf("Tick: %v", err)
	}
	if n != 1 {
		t.Errorf("indexed = %d, want 1 (empty skipped)", n)
	}
	if em.callCount() != 1 {
		t.Errorf("embedder calls = %d, want 1", em.callCount())
	}
}

func TestWatcher_Tick_CorruptCursorResets(t *testing.T) {
	t.Parallel()
	w, msgs, _ := newTestWatcher(t, WatcherOptions{})

	// Pre-seed a corrupt cursor value.
	_ = msgs.SetRAGState(w.cursorKey, "not-a-number")
	msgs.addMsg(t, "user", "alpha")

	n, err := w.Tick(context.Background())
	if err != nil {
		t.Fatalf("Tick: %v", err)
	}
	if n != 1 {
		t.Errorf("indexed = %d, want 1 (cursor should reset to 0 on corrupt)", n)
	}
}

// ---------------------------------------------------------------------------
// Start / Stop liveness
// ---------------------------------------------------------------------------

func TestWatcher_StartStop(t *testing.T) {
	t.Parallel()
	w, msgs, em := newTestWatcher(t, WatcherOptions{Interval: 10 * time.Millisecond})

	msgs.addMsg(t, "user", "before-start")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := w.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if !w.Running() {
		t.Error("Running() = false after Start")
	}

	// Wait for the initial drain tick to complete.
	deadline := time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(deadline) && atomic.LoadInt64(&em.calls) == 0 {
		time.Sleep(5 * time.Millisecond)
	}
	if em.callCount() < 1 {
		t.Errorf("watcher did not drain initial messages: calls = %d", em.callCount())
	}

	if err := w.Stop(); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	if w.Running() {
		t.Error("Running() = true after Stop")
	}
	// Idempotent Stop.
	if err := w.Stop(); err != nil {
		t.Errorf("second Stop: %v", err)
	}
}

func TestWatcher_StartTwiceErrors(t *testing.T) {
	t.Parallel()
	w, _, _ := newTestWatcher(t, WatcherOptions{Interval: time.Hour})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := w.Start(ctx); err != nil {
		t.Fatalf("first Start: %v", err)
	}
	defer w.Stop()
	if err := w.Start(ctx); err == nil {
		t.Error("second Start: expected error")
	}
}

func TestWatcher_ContextCancelStops(t *testing.T) {
	t.Parallel()
	w, _, _ := newTestWatcher(t, WatcherOptions{Interval: 10 * time.Millisecond})

	ctx, cancel := context.WithCancel(context.Background())
	if err := w.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	cancel()

	// Loop should exit promptly. Stop() waits for it.
	done := make(chan struct{})
	go func() { _ = w.Stop(); close(done) }()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("watcher did not stop within 2s of ctx cancel")
	}
}
