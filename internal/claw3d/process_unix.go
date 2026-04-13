//go:build !windows

package claw3d

import (
	"os"
	"syscall"
)

// signalZero sends signal 0 to the process, which returns nil iff the process
// exists and the caller has permission to signal it.
func signalZero(p *os.Process) error {
	return p.Signal(syscall.Signal(0))
}

// killProcess kills a process. On Unix we send SIGKILL for immediate
// termination. A graceful SIGTERM first would be nicer but the TS implementation
// uses a kill-tree approach; for simplicity we SIGKILL here.
func killProcess(p *os.Process) error {
	return p.Kill()
}
