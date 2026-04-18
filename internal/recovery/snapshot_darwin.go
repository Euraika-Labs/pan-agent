//go:build darwin

package recovery

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"syscall"
)

// cowBinary returns the absolute path to the cp binary on darwin.
func cowBinary() string { return "/bin/cp" }

// cowArgs returns the argv for a CoW clone on darwin (cp -c src dst).
func cowArgs(src, dst string) []string { return []string{"-c", src, dst} }

// cowCopy performs an APFS copy-on-write clone via `cp -c`.
// Absolute path, LC_ALL=C, argv-not-string — mirrors internal/secret/keyring_darwin.go.
func cowCopy(src, dst string) error {
	cmd := exec.Command("/bin/cp", "-c", src, dst)
	cmd.Env = append(os.Environ(), "LC_ALL=C")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("recovery: cp -c: %s: %w", stderr.String(), err)
	}
	return nil
}

// probeCow checks whether the filesystem at root supports APFS CoW clones.
// It writes a 1-byte temp file, attempts cp -c, then removes both.
func probeCow(root string) bool {
	if err := os.MkdirAll(root, 0o700); err != nil {
		return false
	}
	src, err := os.CreateTemp(root, ".rprobe-src-*")
	if err != nil {
		return false
	}
	srcName := src.Name()
	_, _ = src.Write([]byte{0})
	src.Close()
	defer os.Remove(srcName)

	dstName := srcName + ".dst"
	defer os.Remove(dstName)

	cmd := exec.Command("/bin/cp", "-c", srcName, dstName)
	cmd.Env = append(os.Environ(), "LC_ALL=C")
	return cmd.Run() == nil
}

// deviceID extracts the device number from a FileInfo.
// Checks fakeDevProvider first so test hooks work without syscall.Stat_t.
func deviceID(fi os.FileInfo) uint64 {
	if f, ok := fi.Sys().(fakeDevProvider); ok {
		return f.FakeDev()
	}
	if st, ok := fi.Sys().(*syscall.Stat_t); ok {
		return uint64(st.Dev)
	}
	return 0
}

// inodeID extracts the inode number for use as a mount-id stand-in.
func inodeID(fi os.FileInfo) uint64 {
	if st, ok := fi.Sys().(*syscall.Stat_t); ok {
		return uint64(st.Ino)
	}
	return 0
}
