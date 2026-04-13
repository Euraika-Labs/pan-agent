//go:build windows

package tools

import (
	"fmt"
	"strings"
	"syscall"
	"unsafe"
)

var (
	user32DLLWM             = syscall.NewLazyDLL("user32.dll")
	procEnumWindows         = user32DLLWM.NewProc("EnumWindows")
	procGetWindowTextW      = user32DLLWM.NewProc("GetWindowTextW")
	procGetWindowTextLenW   = user32DLLWM.NewProc("GetWindowTextLengthW")
	procIsWindowVisible     = user32DLLWM.NewProc("IsWindowVisible")
	procSetForegroundWindow = user32DLLWM.NewProc("SetForegroundWindow")
	procMoveWindow          = user32DLLWM.NewProc("MoveWindow")
	procSendMessageW        = user32DLLWM.NewProc("SendMessageW")
	procGetWindowRect       = user32DLLWM.NewProc("GetWindowRect")
)

const wmClose = 0x0010

type windowRect struct {
	Left, Top, Right, Bottom int32
}

type windowEntry struct {
	HWND  uintptr
	Title string
}

func enumVisibleWindows() ([]windowEntry, error) {
	var windows []windowEntry
	cb := syscall.NewCallback(func(hwnd uintptr, _ uintptr) uintptr {
		vis, _, _ := procIsWindowVisible.Call(hwnd)
		if vis == 0 {
			return 1
		}
		length, _, _ := procGetWindowTextLenW.Call(hwnd)
		if length == 0 {
			return 1
		}
		buf := make([]uint16, length+1)
		procGetWindowTextW.Call(hwnd, uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)))
		title := syscall.UTF16ToString(buf)
		if title != "" {
			windows = append(windows, windowEntry{HWND: hwnd, Title: title})
		}
		return 1
	})
	ret, _, err := procEnumWindows.Call(cb, 0)
	if ret == 0 {
		return nil, fmt.Errorf("EnumWindows failed: %w", err)
	}
	return windows, nil
}

func findWindowByTitle(substr string) (*windowEntry, error) {
	windows, err := enumVisibleWindows()
	if err != nil {
		return nil, err
	}
	lower := strings.ToLower(substr)
	for i := range windows {
		if strings.Contains(strings.ToLower(windows[i].Title), lower) {
			return &windows[i], nil
		}
	}
	return nil, fmt.Errorf("no window found matching %q", substr)
}

func wmListWindows() (string, error) {
	windows, err := enumVisibleWindows()
	if err != nil {
		return "", err
	}
	var lines []string
	for _, w := range windows {
		lines = append(lines, fmt.Sprintf("hwnd=0x%X  %s", w.HWND, w.Title))
	}
	return strings.Join(lines, "\n"), nil
}

func wmFindWindow(title string) (string, error) {
	w, err := findWindowByTitle(title)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("hwnd=0x%X  %s", w.HWND, w.Title), nil
}

func wmFocusWindow(title string) (string, error) {
	w, err := findWindowByTitle(title)
	if err != nil {
		return "", err
	}
	ret, _, e := procSetForegroundWindow.Call(w.HWND)
	if ret == 0 {
		return "", fmt.Errorf("SetForegroundWindow failed: %v", e)
	}
	return fmt.Sprintf("focused: %s", w.Title), nil
}

func wmMoveWindow(title string, x, y int32) (string, error) {
	w, err := findWindowByTitle(title)
	if err != nil {
		return "", err
	}
	var rect windowRect
	procGetWindowRect.Call(w.HWND, uintptr(unsafe.Pointer(&rect)))
	width := rect.Right - rect.Left
	height := rect.Bottom - rect.Top
	ret, _, e := procMoveWindow.Call(w.HWND, uintptr(x), uintptr(y), uintptr(width), uintptr(height), 1)
	if ret == 0 {
		return "", fmt.Errorf("MoveWindow failed: %v", e)
	}
	return fmt.Sprintf("moved %q to (%d, %d)", w.Title, x, y), nil
}

func wmResizeWindow(title string, width, height int32) (string, error) {
	w, err := findWindowByTitle(title)
	if err != nil {
		return "", err
	}
	var rect windowRect
	procGetWindowRect.Call(w.HWND, uintptr(unsafe.Pointer(&rect)))
	ret, _, e := procMoveWindow.Call(w.HWND, uintptr(rect.Left), uintptr(rect.Top), uintptr(width), uintptr(height), 1)
	if ret == 0 {
		return "", fmt.Errorf("MoveWindow failed: %v", e)
	}
	return fmt.Sprintf("resized %q to %dx%d", w.Title, width, height), nil
}

func wmCloseWindow(title string) (string, error) {
	w, err := findWindowByTitle(title)
	if err != nil {
		return "", err
	}
	procSendMessageW.Call(w.HWND, wmClose, 0, 0)
	return fmt.Sprintf("sent WM_CLOSE to %q", w.Title), nil
}
