package claw3d

import (
	"context"
	"encoding/json"
	"runtime"
	"time"
)

// M1 implements just two methods end-to-end: status (liveness) and wake
// (no-op keepalive). The remaining 24 protocol-v3 methods are stubbed at M2
// behind handler files split by resource (handlers_agents.go, etc.).
func init() {
	registerMethod("status", handleStatus)
	registerMethod("wake", handleWake)
}

// StatusPayload is the public shape returned by the "status" method. The
// schema for this payload is published in internal/claw3d/protocol.md and
// frozen at M2 as part of Gate-2 dual-oracle testing.
type StatusPayload struct {
	ProtocolVersion int    `json:"protocolVersion"`
	AdapterType     string `json:"adapterType"`
	AdapterVersion  string `json:"adapterVersion"`
	OS              string `json:"os"`
	UptimeMS        int64  `json:"uptimeMs"`
}

// adapterStartedAt is initialised at package load so Uptime is measured from
// server start, not from first status call.
var adapterStartedAt = time.Now()

func handleStatus(_ context.Context, _ *adapterClient, _ json.RawMessage) (any, error) {
	return StatusPayload{
		ProtocolVersion: 3,
		AdapterType:     "hermes",
		AdapterVersion:  "0.4.0-alpha",
		OS:              runtime.GOOS,
		UptimeMS:        time.Since(adapterStartedAt).Milliseconds(),
	}, nil
}

func handleWake(_ context.Context, _ *adapterClient, _ json.RawMessage) (any, error) {
	return map[string]bool{"awake": true}, nil
}
