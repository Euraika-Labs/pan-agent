//go:build !windows && !darwin && !linux

package tools

import (
	"fmt"
	"runtime"
)

func mouseMove(_, _ int32) error {
	return fmt.Errorf("mouse: not supported on %s", runtime.GOOS)
}

func mouseClick(_, _ int32) error {
	return fmt.Errorf("mouse: not supported on %s", runtime.GOOS)
}

func mouseDoubleClick(_, _ int32) error {
	return fmt.Errorf("mouse: not supported on %s", runtime.GOOS)
}

func mouseRightClick(_, _ int32) error {
	return fmt.Errorf("mouse: not supported on %s", runtime.GOOS)
}

func mouseScroll(_, _, _ int32) error {
	return fmt.Errorf("mouse: not supported on %s", runtime.GOOS)
}
