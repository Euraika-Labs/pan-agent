//go:build !darwin && !linux

package recovery

import (
	"errors"
	"os"
)

// cowBinary returns a stub path — CoW is not available on Windows.
func cowBinary() string { return "/bin/cp" }

// cowArgs returns stub args — never actually called since probeCow returns false.
func cowArgs(src, dst string) []string { return []string{src, dst} }

// cowCopy is not supported on Windows — always returns an error so captureInto
// falls through to tier-2 (os.CopyFS).
func cowCopy(_, _ string) error {
	return errors.New("recovery: CoW clone not supported on this platform")
}

// probeCow always returns false on platforms without reflink support.
func probeCow(_ string) bool { return false }

// deviceID returns 0 or the fake dev ID from fakeDevProvider on Windows.
func deviceID(fi os.FileInfo) uint64 {
	if f, ok := fi.Sys().(fakeDevProvider); ok {
		return f.FakeDev()
	}
	return 0
}

// inodeID returns 0 on platforms where os.FileInfo.Sys() is not *syscall.Stat_t.
func inodeID(_ os.FileInfo) uint64 { return 0 }
