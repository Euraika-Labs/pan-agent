//go:build !windows && !darwin

package tools

import (
	"fmt"
	"runtime"
)

func wmListWindows() (string, error) {
	return "", fmt.Errorf("window_manager: not supported on %s", runtime.GOOS)
}

func wmFindWindow(_ string) (string, error) {
	return "", fmt.Errorf("window_manager: not supported on %s", runtime.GOOS)
}

func wmFocusWindow(_ string) (string, error) {
	return "", fmt.Errorf("window_manager: not supported on %s", runtime.GOOS)
}

func wmMoveWindow(_ string, _, _ int32) (string, error) {
	return "", fmt.Errorf("window_manager: not supported on %s", runtime.GOOS)
}

func wmResizeWindow(_ string, _, _ int32) (string, error) {
	return "", fmt.Errorf("window_manager: not supported on %s", runtime.GOOS)
}

func wmCloseWindow(_ string) (string, error) {
	return "", fmt.Errorf("window_manager: not supported on %s", runtime.GOOS)
}
