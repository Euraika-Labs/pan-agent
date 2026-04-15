package claw3d

import (
	"context"
	"encoding/json"
	"time"

	"github.com/gorilla/websocket"
)

// outboxCap bounds per-connection write buffering. 128 is a trade-off: large
// enough that a brief UI stall doesn't drop events, small enough that a hung
// client doesn't inflate memory indefinitely.
const outboxCap = 128

// adapterClient is the per-connection actor. One reader goroutine + one
// writer goroutine coordinate via the outbox channel. The struct is
// intentionally unexported — handlers reach the hub via the registered
// methods, not by poking client internals.
type adapterClient struct {
	conn    *websocket.Conn
	out     chan []byte
	ctx     context.Context
	cancel  context.CancelFunc
	hub     *Hub
	adapter *Adapter // back-reference so handlers can reach the DB and publicHost
}

// send enqueues a frame. Critical frames (rpc responses and chat deltas) must
// not be silently dropped — if the outbox is full, close the connection so
// the caller observes 1006 and reconnects. Events are expendable and get the
// drop-oldest treatment.
//
// Gate-2 refinement (from Sonnet + Codex): isCritical peeks the type/event
// discriminator before deciding; a fuller resync-via-seq scheme for presence
// lands at M2 once events actually flow.
func (c *adapterClient) send(frame []byte) {
	select {
	case c.out <- frame:
		return
	default:
	}
	if isCritical(frame) {
		c.cancel()
		return
	}
	// Drop-oldest then retry.
	select {
	case <-c.out:
	default:
	}
	select {
	case c.out <- frame:
	default:
		c.cancel()
	}
}

// isCritical inspects a serialized frame's discriminator fields without
// unmarshalling the full body. Any res frame, any req echo, and any chat
// event are considered unloseable; presence/heartbeat/cron are droppable.
func isCritical(frame []byte) bool {
	var head struct {
		Type  string `json:"type"`
		Event string `json:"event"`
	}
	_ = json.Unmarshal(frame, &head)
	if head.Type == "res" || head.Type == "req" {
		return true
	}
	if head.Type == "event" && head.Event == "chat" {
		return true
	}
	return false
}

// reader loops reading inbound frames and dispatching them. Read deadline is
// reset on every pong so idle clients don't get reaped mid-session.
func (c *adapterClient) reader() {
	defer c.cancel()
	_ = c.conn.SetReadDeadline(time.Now().Add(30 * time.Second))
	c.conn.SetPongHandler(func(string) error {
		return c.conn.SetReadDeadline(time.Now().Add(30 * time.Second))
	})
	for {
		_, raw, err := c.conn.ReadMessage()
		if err != nil {
			return
		}
		dispatch(c.ctx, c, raw)
	}
}

// writer loops draining the outbox. All writes to c.conn MUST go through this
// goroutine — gorilla/websocket is not safe for concurrent writes.
func (c *adapterClient) writer() {
	defer c.cancel()
	ping := time.NewTicker(15 * time.Second)
	defer ping.Stop()
	for {
		select {
		case <-c.ctx.Done():
			return
		case <-ping.C:
			_ = c.conn.WriteControl(websocket.PingMessage, nil,
				time.Now().Add(5*time.Second))
		case msg := <-c.out:
			_ = c.conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if err := c.conn.WriteMessage(websocket.TextMessage, msg); err != nil {
				return
			}
		}
	}
}
