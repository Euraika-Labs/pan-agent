//go:build !windows

package parentwatch

import (
	"errors"
	"syscall"
)

// isAlive returns true if the process with the given PID is running.
//
// On Unix we send signal 0 — the kernel does permission + existence checks
// without actually delivering anything. ESRCH means no such process.
// EPERM means it exists but we can't signal it — still alive, so return true.
func isAlive(pid int) bool {
	err := syscall.Kill(pid, 0)
	if err == nil {
		return true
	}
	return !errors.Is(err, syscall.ESRCH)
}
