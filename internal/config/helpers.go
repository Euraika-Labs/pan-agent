package config

import (
	"fmt"
	"os"
	"path/filepath"
)

// safeWriteFile writes content to path, creating any missing parent directories
// first.  This mirrors the TypeScript safeWriteFile utility.
func safeWriteFile(path, content string) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("config.safeWriteFile mkdir %s: %w", dir, err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		return fmt.Errorf("config.safeWriteFile write %s: %w", path, err)
	}
	return nil
}
