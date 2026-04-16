package claw3d

import "time"

// EmitHeartbeat pushes a liveness event to all connected clients. The Claw3D
// UI uses this to detect that the adapter is alive even when no chat or
// presence traffic is flowing.
//
// Note: the previous file shipped a presenceCoalescer type + four methods
// that were never wired into the hub (scaffolded for a future "presence"
// event but no caller ever adopted them). Removed in 0.4.2 after the
// golangci-lint `unused` check surfaced the dead code — the pattern can
// come back if/when a concrete use case lands.
func EmitHeartbeat(h *Hub) {
	frame := marshalEventFrame("heartbeat", map[string]any{
		"ts": time.Now().UnixMilli(),
	})
	h.Broadcast(frame)
}
