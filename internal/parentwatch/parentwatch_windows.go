//go:build windows

package parentwatch

import (
	"golang.org/x/sys/windows"
)

// isAlive returns true if the process with the given PID is running.
//
// On Windows we open a handle with SYNCHRONIZE rights and check the wait
// state without blocking. A signaled handle (WAIT_OBJECT_0) means the
// process has exited; WAIT_TIMEOUT means it's still running.
//
// If OpenProcess fails (access denied, PID reused, etc.) we conservatively
// return true — we'd rather leave the sidecar running than kill it based on
// a transient permission error.
func isAlive(pid int) bool {
	h, err := windows.OpenProcess(windows.SYNCHRONIZE, false, uint32(pid))
	if err != nil {
		// ERROR_INVALID_PARAMETER specifically means "no such PID" — that's
		// the only case where we know the parent is definitely gone.
		if err == windows.ERROR_INVALID_PARAMETER {
			return false
		}
		return true
	}
	defer windows.CloseHandle(h)

	ev, err := windows.WaitForSingleObject(h, 0)
	if err != nil {
		return true
	}
	return ev != windows.WAIT_OBJECT_0
}
