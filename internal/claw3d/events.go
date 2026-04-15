package claw3d

import (
	"encoding/json"
	"sync"
	"time"
)

// ---------------------------------------------------------------------------
// Event emission helpers.
//
// Gate-2 refinement (from Codex): presence is modelled as latest-state-wins
// with a monotonic sequence number. The server never drops a presence event
// silently — instead it overwrites the pending snapshot and increments a
// `dropped` counter. Clients that observe a gap in `seq` call the resync
// method (sessions.list, filtered) to rebuild state deterministically.
//
// chat deltas are NOT coalesced — losing one corrupts the stream. They go
// through the critical path in client.send() and close the connection if the
// outbox is full.
// ---------------------------------------------------------------------------

// presenceCoalescer keeps at most one pending snapshot per agent id and emits
// it to the hub when it either (a) is set while the outbox has capacity, or
// (b) is flushed by a heartbeat tick. It is safe for concurrent callers.
type presenceCoalescer struct {
	mu       sync.Mutex
	pending  map[string]json.RawMessage
	dropped  uint64
	lastEmit time.Time
}

func newPresenceCoalescer() *presenceCoalescer {
	return &presenceCoalescer{pending: map[string]json.RawMessage{}}
}

// Update merges a snapshot into the pending map. Call Emit afterwards — or
// rely on the caller's heartbeat loop — to push to the hub.
func (p *presenceCoalescer) Update(agentID string, snapshot any) {
	raw, _ := json.Marshal(snapshot)
	p.mu.Lock()
	if _, exists := p.pending[agentID]; exists {
		p.dropped++
	}
	p.pending[agentID] = raw
	p.mu.Unlock()
}

// Drain returns all pending snapshots keyed by agent id plus the accumulated
// dropped count, and resets both. The caller wraps the result into a single
// `presence` event with the current monotonic seq.
func (p *presenceCoalescer) Drain() (map[string]json.RawMessage, uint64) {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := p.pending
	drp := p.dropped
	p.pending = map[string]json.RawMessage{}
	p.dropped = 0
	p.lastEmit = time.Now()
	return out, drp
}

// EmitPresence serialises the current pending state into a presence event and
// broadcasts it through the supplied hub. Returns the seq assigned to the
// frame so clients using sessions.list can reason about gaps.
func (p *presenceCoalescer) EmitPresence(h *Hub) uint64 {
	pending, dropped := p.Drain()
	if len(pending) == 0 {
		return 0 // nothing to emit
	}
	agents := make(map[string]any, len(pending))
	for id, raw := range pending {
		var v any
		_ = json.Unmarshal(raw, &v)
		agents[id] = v
	}
	payload := map[string]any{
		"agents":  agents,
		"dropped": dropped,
	}
	frame := marshalEventFrame("presence", payload)
	h.Broadcast(frame)
	// marshalEventFrame consumed the next seq; we need to return it so
	// callers can log / test against it.
	return seqCounter
}

// EmitHeartbeat pushes a liveness event to all connected clients. The Claw3D
// UI uses this to detect that the adapter is alive even when no chat or
// presence traffic is flowing.
func EmitHeartbeat(h *Hub) {
	frame := marshalEventFrame("heartbeat", map[string]any{
		"ts": time.Now().UnixMilli(),
	})
	h.Broadcast(frame)
}
