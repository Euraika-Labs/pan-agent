//go:build windows

package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"syscall"
	"unsafe"
)

var (
	user32DLLWM            = syscall.NewLazyDLL("user32.dll")
	procEnumWindows        = user32DLLWM.NewProc("EnumWindows")
	procGetWindowTextW     = user32DLLWM.NewProc("GetWindowTextW")
	procGetWindowTextLenW  = user32DLLWM.NewProc("GetWindowTextLengthW")
	procIsWindowVisible    = user32DLLWM.NewProc("IsWindowVisible")
	procSetForegroundWindow = user32DLLWM.NewProc("SetForegroundWindow")
	procMoveWindow         = user32DLLWM.NewProc("MoveWindow")
	procSendMessageW       = user32DLLWM.NewProc("SendMessageW")
	procGetWindowRect      = user32DLLWM.NewProc("GetWindowRect")
)

const wmClose = 0x0010

// RECT mirrors the Windows RECT structure.
type windowRect struct {
	Left, Top, Right, Bottom int32
}

// windowEntry holds a discovered window's handle and title.
type windowEntry struct {
	HWND  uintptr
	Title string
}

// enumVisibleWindows returns all visible top-level windows that have a non-empty title.
func enumVisibleWindows() ([]windowEntry, error) {
	var windows []windowEntry

	cb := syscall.NewCallback(func(hwnd uintptr, _ uintptr) uintptr {
		// Skip invisible windows.
		vis, _, _ := procIsWindowVisible.Call(hwnd)
		if vis == 0 {
			return 1 // continue enumeration
		}
		// Get title length.
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
		return 1 // continue enumeration
	})

	ret, _, err := procEnumWindows.Call(cb, 0)
	if ret == 0 {
		return nil, fmt.Errorf("EnumWindows failed: %w", err)
	}
	return windows, nil
}

// findWindow returns the first visible window whose title contains substr (case-insensitive).
func findWindow(substr string) (*windowEntry, error) {
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

// WindowManagerTool manages top-level windows.
type WindowManagerTool struct{}

type windowManagerParams struct {
	Operation string `json:"operation"`
	Title     string `json:"title,omitempty"`
	X         int32  `json:"x,omitempty"`
	Y         int32  `json:"y,omitempty"`
	Width     int32  `json:"width,omitempty"`
	Height    int32  `json:"height,omitempty"`
}

func (WindowManagerTool) Name() string { return "window_manager" }

func (WindowManagerTool) Description() string {
	return "Manage top-level Windows windows: list visible windows, find by title, " +
		"focus, move, resize, or close a window."
}

func (WindowManagerTool) Parameters() json.RawMessage {
	return json.RawMessage(`{
  "type": "object",
  "required": ["operation"],
  "properties": {
    "operation": {
      "type": "string",
      "enum": ["list", "find", "focus", "move", "resize", "close"],
      "description": "Operation to perform"
    },
    "title": {
      "type": "string",
      "description": "Window title substring to match (used by find/focus/move/resize/close)"
    },
    "x": {
      "type": "integer",
      "description": "Target X position (move)"
    },
    "y": {
      "type": "integer",
      "description": "Target Y position (move)"
    },
    "width": {
      "type": "integer",
      "description": "Target width in pixels (resize)"
    },
    "height": {
      "type": "integer",
      "description": "Target height in pixels (resize)"
    }
  }
}`)
}

func (t WindowManagerTool) Execute(_ context.Context, params json.RawMessage) (*Result, error) {
	var p windowManagerParams
	if err := json.Unmarshal(params, &p); err != nil {
		return &Result{Error: fmt.Sprintf("invalid parameters: %v", err)}, nil
	}

	switch p.Operation {
	case "list":
		windows, err := enumVisibleWindows()
		if err != nil {
			return &Result{Error: err.Error()}, nil
		}
		var lines []string
		for _, w := range windows {
			lines = append(lines, fmt.Sprintf("hwnd=0x%X  %s", w.HWND, w.Title))
		}
		return &Result{Output: strings.Join(lines, "\n")}, nil

	case "find":
		if p.Title == "" {
			return &Result{Error: "title must not be empty for operation=find"}, nil
		}
		w, err := findWindow(p.Title)
		if err != nil {
			return &Result{Error: err.Error()}, nil
		}
		return &Result{Output: fmt.Sprintf("hwnd=0x%X  %s", w.HWND, w.Title)}, nil

	case "focus":
		if p.Title == "" {
			return &Result{Error: "title must not be empty for operation=focus"}, nil
		}
		w, err := findWindow(p.Title)
		if err != nil {
			return &Result{Error: err.Error()}, nil
		}
		ret, _, e := procSetForegroundWindow.Call(w.HWND)
		if ret == 0 {
			return &Result{Error: fmt.Sprintf("SetForegroundWindow failed: %v", e)}, nil
		}
		return &Result{Output: fmt.Sprintf("focused: %s", w.Title)}, nil

	case "move":
		if p.Title == "" {
			return &Result{Error: "title must not be empty for operation=move"}, nil
		}
		w, err := findWindow(p.Title)
		if err != nil {
			return &Result{Error: err.Error()}, nil
		}
		// Get current size so we only change position.
		var rect windowRect
		procGetWindowRect.Call(w.HWND, uintptr(unsafe.Pointer(&rect)))
		width := rect.Right - rect.Left
		height := rect.Bottom - rect.Top
		ret, _, e := procMoveWindow.Call(
			w.HWND,
			uintptr(p.X), uintptr(p.Y),
			uintptr(width), uintptr(height),
			1, // bRepaint
		)
		if ret == 0 {
			return &Result{Error: fmt.Sprintf("MoveWindow failed: %v", e)}, nil
		}
		return &Result{Output: fmt.Sprintf("moved %q to (%d, %d)", w.Title, p.X, p.Y)}, nil

	case "resize":
		if p.Title == "" {
			return &Result{Error: "title must not be empty for operation=resize"}, nil
		}
		w, err := findWindow(p.Title)
		if err != nil {
			return &Result{Error: err.Error()}, nil
		}
		// Get current position so we only change size.
		var rect windowRect
		procGetWindowRect.Call(w.HWND, uintptr(unsafe.Pointer(&rect)))
		ret, _, e := procMoveWindow.Call(
			w.HWND,
			uintptr(rect.Left), uintptr(rect.Top),
			uintptr(p.Width), uintptr(p.Height),
			1, // bRepaint
		)
		if ret == 0 {
			return &Result{Error: fmt.Sprintf("MoveWindow failed: %v", e)}, nil
		}
		return &Result{Output: fmt.Sprintf("resized %q to %dx%d", w.Title, p.Width, p.Height)}, nil

	case "close":
		if p.Title == "" {
			return &Result{Error: "title must not be empty for operation=close"}, nil
		}
		w, err := findWindow(p.Title)
		if err != nil {
			return &Result{Error: err.Error()}, nil
		}
		procSendMessageW.Call(w.HWND, wmClose, 0, 0)
		return &Result{Output: fmt.Sprintf("sent WM_CLOSE to %q", w.Title)}, nil

	default:
		return &Result{Error: fmt.Sprintf("unknown operation: %q (want list|find|focus|move|resize|close)", p.Operation)}, nil
	}
}

var _ Tool = WindowManagerTool{}

func init() {
	Register(WindowManagerTool{})
}
