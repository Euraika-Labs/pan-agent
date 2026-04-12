// Package approval manages pending tool-approval requests.
// When the agent loop encounters a "dangerous" tool call, it creates an
// Approval record and blocks the goroutine until the user resolves it via the
// HTTP API (POST /v1/approvals/{id}).
package approval

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"sync"
	"time"
)

// Status represents the lifecycle state of an approval request.
type Status string

const (
	StatusPending  Status = "pending"
	StatusApproved Status = "approved"
	StatusRejected Status = "rejected"
)

// Approval is a single pending (or resolved) tool-approval request.
type Approval struct {
	ID         string  `json:"id"`
	SessionID  string  `json:"session_id"`
	ToolName   string  `json:"tool_name"`
	Arguments  string  `json:"arguments"` // raw JSON string
	Status     Status  `json:"status"`
	CreatedAt  int64   `json:"created_at"`
	ResolvedAt *int64  `json:"resolved_at,omitempty"`

	// ch is signalled when the approval is resolved. Not exposed in JSON.
	ch chan struct{}
}

// ErrNotFound is returned when an approval ID does not exist in the store.
var ErrNotFound = errors.New("approval: not found")

// ErrAlreadyResolved is returned when a resolve attempt is made on an
// approval that has already been approved or rejected.
var ErrAlreadyResolved = errors.New("approval: already resolved")

// Store is an in-memory registry of approval requests.
type Store struct {
	mu      sync.RWMutex
	pending map[string]*Approval
}

// NewStore returns an initialised Store.
func NewStore() *Store {
	return &Store{pending: make(map[string]*Approval)}
}

// Create registers a new approval request and returns it.
// The returned Approval is owned by the Store; callers must not modify it.
func (s *Store) Create(sessionID, toolName, arguments string) *Approval {
	a := &Approval{
		ID:        newID(),
		SessionID: sessionID,
		ToolName:  toolName,
		Arguments: arguments,
		Status:    StatusPending,
		CreatedAt: time.Now().UnixMilli(),
		ch:        make(chan struct{}),
	}

	s.mu.Lock()
	s.pending[a.ID] = a
	s.mu.Unlock()

	return a
}

// Wait blocks until the approval is resolved or done is closed.
// Returns the resolved Status or an error if done fires first.
func (s *Store) Wait(id string, done <-chan struct{}) (Status, error) {
	s.mu.RLock()
	a, ok := s.pending[id]
	s.mu.RUnlock()
	if !ok {
		return "", ErrNotFound
	}

	select {
	case <-a.ch:
		return a.Status, nil
	case <-done:
		return "", errors.New("approval: context cancelled while waiting")
	}
}

// Resolve sets the approval status to approved or rejected and signals any
// waiting goroutine.
func (s *Store) Resolve(id string, approved bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	a, ok := s.pending[id]
	if !ok {
		return ErrNotFound
	}
	if a.Status != StatusPending {
		return ErrAlreadyResolved
	}

	now := time.Now().UnixMilli()
	a.ResolvedAt = &now
	if approved {
		a.Status = StatusApproved
	} else {
		a.Status = StatusRejected
	}
	close(a.ch)
	return nil
}

// Get returns a snapshot copy of the approval identified by id.
func (s *Store) Get(id string) (Approval, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	a, ok := s.pending[id]
	if !ok {
		return Approval{}, ErrNotFound
	}
	// Return a value copy so callers never hold a pointer into the store.
	out := *a
	out.ch = nil
	return out, nil
}

// ListPending returns all approvals that are still in StatusPending, ordered
// by creation time (oldest first).
func (s *Store) ListPending() []Approval {
	s.mu.RLock()
	defer s.mu.RUnlock()

	out := make([]Approval, 0, len(s.pending))
	for _, a := range s.pending {
		if a.Status == StatusPending {
			cp := *a
			cp.ch = nil
			out = append(out, cp)
		}
	}
	// Insertion sort by CreatedAt ascending (expected N is tiny).
	for i := 1; i < len(out); i++ {
		key := out[i]
		j := i - 1
		for j >= 0 && out[j].CreatedAt > key.CreatedAt {
			out[j+1] = out[j]
			j--
		}
		out[j+1] = key
	}
	return out
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

// newID generates a random 16-byte hex string suitable for use as an ID.
func newID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
