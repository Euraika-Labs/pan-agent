// Package recovery — snapshot_copy.go provides the tier-2 os.CopyFS fallback
// and the Windows-only stubs for cowCopy / probeCow / deviceID / inodeID.
// This file is compiled on all platforms. The platform files (snapshot_darwin.go,
// snapshot_linux.go) provide the real cowCopy / probeCow / deviceID / inodeID
// implementations for their respective OSes; the stubs below compile only when
// neither of those build tags is satisfied (i.e. Windows and any other OS).
package recovery

import (
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
)

// copyFSFallback copies src to dst using pure Go I/O (tier-2 fallback).
// Works for both files and directories. Called by captureInto when CoW is
// unavailable or has failed, and by Restore.
func copyFSFallback(src, dst string) error {
	fi, err := os.Stat(src)
	if err != nil {
		return fmt.Errorf("recovery: copyFSFallback stat: %w", err)
	}
	if fi.IsDir() {
		return copyDir(src, dst)
	}
	return copyFile(src, dst, fi.Mode())
}

func copyFile(src, dst string, mode fs.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	if err := os.MkdirAll(filepath.Dir(dst), 0o700); err != nil {
		return err
	}
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
	if err != nil {
		return err
	}
	defer out.Close()
	_, err = io.Copy(out, in)
	return err
}

func copyDir(src, dst string) error {
	return filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil // skip unreadable
		}
		rel, relErr := filepath.Rel(src, path)
		if relErr != nil {
			return nil
		}
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o700)
		}
		fi, infoErr := d.Info()
		if infoErr != nil {
			return nil
		}
		return copyFile(path, target, fi.Mode())
	})
}
