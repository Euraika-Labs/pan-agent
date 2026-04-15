// Package parentwatch lets a process self-terminate when its parent dies.
//
// Useful when pan-agent is launched as a sidecar by a GUI launcher (Tauri,
// Electron, etc.). If the launcher is force-killed with TerminateProcess (or
// otherwise exits without signaling its children), Go's signal handlers never
// fire and the sidecar is orphaned — keeping the TCP port bound. This package
// polls the parent PID and invokes an onExit callback when the parent is gone.
//
// Usage: call Watch with the parent's PID and a callback that triggers a
// graceful shutdown. Activation is typically gated on an env var like
// PAN_AGENT_PARENT_PID so CLI usage is unaffected.
package parentwatch

import (
	"context"
	"time"
)

// pollInterval is how often we check whether the parent is still alive.
// 2 seconds is a good trade-off: fast enough that an orphaned sidecar
// releases its port quickly, slow enough to be effectively free.
const pollInterval = 2 * time.Second

// Watch blocks until ctx is cancelled OR the process with the given PID exits.
// When the parent exits, onExit is invoked exactly once (synchronously on the
// caller's goroutine). Intended to be run in its own goroutine.
//
// If pid <= 0 Watch returns immediately without calling onExit — callers use
// this to cheaply no-op when the env var is unset.
func Watch(ctx context.Context, pid int, onExit func()) {
	if pid <= 0 {
		return
	}

	t := time.NewTicker(pollInterval)
	defer t.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if !isAlive(pid) {
				onExit()
				return
			}
		}
	}
}
