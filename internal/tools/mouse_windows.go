//go:build windows

package tools

import (
	"fmt"
	"syscall"
	"unsafe"
)

var (
	user32DLLMouse     = syscall.NewLazyDLL("user32.dll")
	procSendInputMouse = user32DLLMouse.NewProc("SendInput")
	procSetCursorPos   = user32DLLMouse.NewProc("SetCursorPos")
)

const (
	mouseeventfLeftDown  = 0x0002
	mouseeventfLeftUp    = 0x0004
	mouseeventfRightDown = 0x0008
	mouseeventfRightUp   = 0x0010
	mouseeventfWheel     = 0x0800
	inputMouse           = 0
)

type mouseInput struct {
	dx, dy      int32
	mouseData   uint32
	dwFlags     uint32
	time        uint32
	dwExtraInfo uintptr
}

type mouseInputUnion struct {
	dwType uint32
	_      uint32
	mi     mouseInput
}

func sendMouseEvent(flags uint32, x, y int32, data uint32) error {
	inp := mouseInputUnion{dwType: inputMouse, mi: mouseInput{dx: x, dy: y, mouseData: data, dwFlags: flags}}
	ret, _, err := procSendInputMouse.Call(1, uintptr(unsafe.Pointer(&inp)), uintptr(unsafe.Sizeof(inp)))
	if ret == 0 {
		return fmt.Errorf("SendInput failed: %w", err)
	}
	return nil
}

func mouseMove(x, y int32) error {
	ret, _, err := procSetCursorPos.Call(uintptr(x), uintptr(y))
	if ret == 0 {
		return fmt.Errorf("SetCursorPos failed: %v", err)
	}
	return nil
}

func mouseClick(x, y int32) error {
	if err := mouseMove(x, y); err != nil {
		return err
	}
	if err := sendMouseEvent(mouseeventfLeftDown, x, y, 0); err != nil {
		return err
	}
	return sendMouseEvent(mouseeventfLeftUp, x, y, 0)
}

func mouseDoubleClick(x, y int32) error {
	if err := mouseClick(x, y); err != nil {
		return err
	}
	return mouseClick(x, y)
}

func mouseRightClick(x, y int32) error {
	if err := mouseMove(x, y); err != nil {
		return err
	}
	if err := sendMouseEvent(mouseeventfRightDown, x, y, 0); err != nil {
		return err
	}
	return sendMouseEvent(mouseeventfRightUp, x, y, 0)
}

func mouseScroll(x, y, delta int32) error {
	return sendMouseEvent(mouseeventfWheel, x, y, uint32(delta))
}

func init() { Register(MouseTool{}) }
