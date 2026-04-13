//go:build !windows && !darwin && !linux

package tools

import (
	"fmt"
	"runtime"
)

func keyboardTypeText(_ string) error {
	return fmt.Errorf("keyboard: not supported on %s (X11/macOS implementation pending)", runtime.GOOS)
}

func keyboardPressKey(_ string) error {
	return fmt.Errorf("keyboard: not supported on %s", runtime.GOOS)
}

func keyboardHotkey(_ []string, _ string) error {
	return fmt.Errorf("keyboard: not supported on %s", runtime.GOOS)
}
