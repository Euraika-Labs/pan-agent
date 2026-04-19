//go:build linux

package recovery

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"syscall"
)

// cowBinary returns the absolute path to the cp binary on linux.
func cowBinary() string { return "/bin/cp" }

// cowArgs returns the argv for a reflink copy on linux (cp --reflink=always src dst).
func cowArgs(src, dst string) []string { return []string{"--reflink=always", src, dst} }

// cowCopy performs a reflink copy via `cp --reflink=always`.
// Absolute path, LC_ALL=C, argv-not-string.
func cowCopy(src, dst string) error {
	cmd := exec.Command("/bin/cp", "--reflink=always", src, dst)
	cmd.Env = append(os.Environ(), "LC_ALL=C")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("recovery: cp --reflink=always: %s: %w", stderr.String(), err)
	}
	return nil
}

// probeCow checks whether the filesystem at root supports reflinks.
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

	cmd := exec.Command("/bin/cp", "--reflink=always", srcName, dstName)
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
