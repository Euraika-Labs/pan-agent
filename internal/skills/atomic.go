package skills

import (
	"fmt"
	"os"
	"path/filepath"
)

// atomicWrite writes data to path via a temp-file + rename dance so a crash
// mid-write does not leave a truncated file. Creates parent directories.
func atomicWrite(path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("atomicWrite mkdir: %w", err)
	}
	tmp, err := os.CreateTemp(dir, ".skill.*.tmp")
	if err != nil {
		return fmt.Errorf("atomicWrite tempfile: %w", err)
	}
	tmpName := tmp.Name()
	defer func() {
		// If we did not rename, clean up the temp file.
		_, statErr := os.Stat(tmpName)
		if statErr == nil {
			_ = os.Remove(tmpName)
		}
	}()

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return fmt.Errorf("atomicWrite write: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("atomicWrite close: %w", err)
	}
	if err := os.Chmod(tmpName, perm); err != nil {
		return fmt.Errorf("atomicWrite chmod: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("atomicWrite rename: %w", err)
	}
	return nil
}

// snapshotFile reads a file's contents for rollback. Returns nil content and
// a false bool if the file does not exist (nothing to roll back to).
func snapshotFile(path string) ([]byte, bool) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, false
	}
	return data, true
}

// restoreFile writes previously-captured bytes back to path. If snapshot had
// no prior content (existed=false), the file is removed.
func restoreFile(path string, data []byte, existed bool, perm os.FileMode) error {
	if !existed {
		_ = os.Remove(path)
		return nil
	}
	return atomicWrite(path, data, perm)
}
