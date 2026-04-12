package approval

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"
)

const pendingTimeout = 5 * time.Minute

// Response is the user's answer to a pending approval request.
type Response struct {
	Approved bool
	// Phrase is required for Level 2 (Catastrophic) commands:
	// the user must type "YES-I-UNDERSTAND-THE-RISK" exactly.
	Phrase string
}

// pendingEntry holds state for one outstanding approval request.
type pendingEntry struct {
	check   ApprovalCheck
	command string
	ch      chan Response
	cancel  context.CancelFunc
}

var (
	pendingMap sync.Map // map[string]*pendingEntry
	idCounter  atomic.Uint64
)

// newPendingID generates a unique, monotonically increasing approval ID for
// the pending registry. Named distinctly from the existing newID helper in
// approval.go which generates random hex IDs for the Store-based flow.
func newPendingID() string {
	return fmt.Sprintf("pending-%d", idCounter.Add(1))
}

// Request registers a pending approval and returns the approval ID and a
// channel that will receive exactly one Response when the request is resolved
// or times out.
//
// The channel is closed (with zero Response) on a 5-minute context timeout.
// Callers must always drain the channel to avoid goroutine leaks.
func Request(ctx context.Context, check ApprovalCheck, command string) (string, <-chan Response) {
	id := newPendingID()
	ch := make(chan Response, 1)

	timeoutCtx, cancel := context.WithTimeout(ctx, pendingTimeout)

	entry := &pendingEntry{
		check:   check,
		command: command,
		ch:      ch,
		cancel:  cancel,
	}
	pendingMap.Store(id, entry)

	go func() {
		<-timeoutCtx.Done()
		// Remove from the map so HasPending / ListPending stay accurate.
		if _, loaded := pendingMap.LoadAndDelete(id); loaded {
			// Timed out before Resolve was called: send a zero Response
			// (Approved=false) and close the channel.
			ch <- Response{}
			close(ch)
		}
		cancel()
	}()

	return id, ch
}

// Resolve delivers response to the waiting caller and removes the entry from
// the pending registry. Returns false if the ID is not found (already resolved
// or timed out).
func Resolve(id string, response Response) bool {
	val, loaded := pendingMap.LoadAndDelete(id)
	if !loaded {
		return false
	}
	entry := val.(*pendingEntry)
	// Cancel the timeout goroutine so it does not also send a Response.
	entry.cancel()
	entry.ch <- response
	close(entry.ch)
	return true
}

// HasPending reports whether an approval request with the given ID is still
// outstanding (not yet resolved or timed out).
func HasPending(id string) bool {
	_, ok := pendingMap.Load(id)
	return ok
}

// ListPending returns the IDs of all currently outstanding approval requests.
// The order is not guaranteed.
func ListPending() []string {
	var ids []string
	pendingMap.Range(func(key, _ any) bool {
		ids = append(ids, key.(string))
		return true
	})
	return ids
}
