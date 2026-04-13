//go:build windows

package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"syscall"
	"unsafe"
)

var (
	user32DLLMouse     = syscall.NewLazyDLL("user32.dll")
	procSendInputMouse = user32DLLMouse.NewProc("SendInput")
	procSetCursorPos   = user32DLLMouse.NewProc("SetCursorPos")
)

// Mouse event flags for SendInput.
const (
	mouseeventfMove        = 0x0001
	mouseeventfLeftDown    = 0x0002
	mouseeventfLeftUp      = 0x0004
	mouseeventfRightDown   = 0x0008
	mouseeventfRightUp     = 0x0010
	mouseeventfWheel       = 0x0800
	mouseeventfAbsolute    = 0x8000

	inputMouse = 0
)

// mouseInput mirrors the MOUSEINPUT structure.
type mouseInput struct {
	dx          int32
	dy          int32
	mouseData   uint32
	dwFlags     uint32
	time        uint32
	dwExtraInfo uintptr
}

// mouseInputUnion is INPUT { DWORD type; union { MOUSEINPUT; ... } }
// padded to match the size of the keyboard inputUnion (28 bytes on 64-bit).
type mouseInputUnion struct {
	dwType  uint32
	_       uint32 // padding
	mi      mouseInput
}

func sendMouseEvent(flags uint32, x, y int32, data uint32) error {
	inp := mouseInputUnion{
		dwType: inputMouse,
		mi: mouseInput{
			dx:        x,
			dy:        y,
			mouseData: data,
			dwFlags:   flags,
		},
	}
	ret, _, err := procSendInputMouse.Call(
		1,
		uintptr(unsafe.Pointer(&inp)),
		uintptr(unsafe.Sizeof(inp)),
	)
	if ret == 0 {
		return fmt.Errorf("SendInput failed: %w", err)
	}
	return nil
}

func leftClick(x, y int32) error {
	if err := sendMouseEvent(mouseeventfLeftDown, x, y, 0); err != nil {
		return err
	}
	return sendMouseEvent(mouseeventfLeftUp, x, y, 0)
}

// MouseTool controls the mouse cursor and sends mouse button events.
type MouseTool struct{}

type mouseParams struct {
	Operation string `json:"operation"`
	X         int32  `json:"x,omitempty"`
	Y         int32  `json:"y,omitempty"`
	Delta     int32  `json:"delta,omitempty"`
}

func (MouseTool) Name() string { return "mouse" }

func (MouseTool) Description() string {
	return "Control the mouse: move cursor, click, double-click, right-click, or scroll. " +
		"Coordinates are absolute screen pixels."
}

func (MouseTool) Parameters() json.RawMessage {
	return json.RawMessage(`{
  "type": "object",
  "required": ["operation"],
  "properties": {
    "operation": {
      "type": "string",
      "enum": ["move", "click", "double_click", "right_click", "scroll"],
      "description": "Mouse operation to perform"
    },
    "x": {
      "type": "integer",
      "description": "Absolute X screen coordinate"
    },
    "y": {
      "type": "integer",
      "description": "Absolute Y screen coordinate"
    },
    "delta": {
      "type": "integer",
      "description": "Scroll amount for scroll operation (positive=up, negative=down). Default 120."
    }
  }
}`)
}

func (t MouseTool) Execute(_ context.Context, params json.RawMessage) (*Result, error) {
	var p mouseParams
	if err := json.Unmarshal(params, &p); err != nil {
		return &Result{Error: fmt.Sprintf("invalid parameters: %v", err)}, nil
	}

	switch p.Operation {
	case "move":
		ret, _, err := procSetCursorPos.Call(uintptr(p.X), uintptr(p.Y))
		if ret == 0 {
			return &Result{Error: fmt.Sprintf("SetCursorPos failed: %v", err)}, nil
		}
		return &Result{Output: fmt.Sprintf("moved cursor to (%d, %d)", p.X, p.Y)}, nil

	case "click":
		if err := leftClick(p.X, p.Y); err != nil {
			return &Result{Error: err.Error()}, nil
		}
		return &Result{Output: fmt.Sprintf("left-clicked at (%d, %d)", p.X, p.Y)}, nil

	case "double_click":
		if err := leftClick(p.X, p.Y); err != nil {
			return &Result{Error: err.Error()}, nil
		}
		if err := leftClick(p.X, p.Y); err != nil {
			return &Result{Error: err.Error()}, nil
		}
		return &Result{Output: fmt.Sprintf("double-clicked at (%d, %d)", p.X, p.Y)}, nil

	case "right_click":
		if err := sendMouseEvent(mouseeventfRightDown, p.X, p.Y, 0); err != nil {
			return &Result{Error: err.Error()}, nil
		}
		if err := sendMouseEvent(mouseeventfRightUp, p.X, p.Y, 0); err != nil {
			return &Result{Error: err.Error()}, nil
		}
		return &Result{Output: fmt.Sprintf("right-clicked at (%d, %d)", p.X, p.Y)}, nil

	case "scroll":
		delta := p.Delta
		if delta == 0 {
			delta = 120
		}
		if err := sendMouseEvent(mouseeventfWheel, p.X, p.Y, uint32(delta)); err != nil {
			return &Result{Error: err.Error()}, nil
		}
		return &Result{Output: fmt.Sprintf("scrolled %d at (%d, %d)", delta, p.X, p.Y)}, nil

	default:
		return &Result{Error: fmt.Sprintf("unknown operation: %q (want move|click|double_click|right_click|scroll)", p.Operation)}, nil
	}
}

var _ Tool = MouseTool{}

func init() {
	Register(MouseTool{})
}
