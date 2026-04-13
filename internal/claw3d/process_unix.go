//go:build !windows

package claw3d

import (
	"os"
	"syscall"
)

// probeAlive returns true if the process exists and is still running.
// Uses kill(pid, 0) which is a no-op signal that only checks existence.
func probeAlive(p *os.Process) bool {
	return p.Signal(syscall.Signal(0)) == nil
}

// killProcess kills a process. On Unix we send SIGKILL for immediate
// termination. A graceful SIGTERM first would be nicer but the TS implementation
// uses a kill-tree approach; for simplicity we SIGKILL here.
func killProcess(p *os.Process) error {
	return p.Kill()
}
