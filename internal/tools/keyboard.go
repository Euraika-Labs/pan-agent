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
	user32DLLKeyboard     = syscall.NewLazyDLL("user32.dll")
	procSendInputKeyboard = user32DLLKeyboard.NewProc("SendInput")
)

// Virtual key codes for common keys.
const (
	vkReturn   = 0x0D
	vkTab      = 0x09
	vkEscape   = 0x1B
	vkBack     = 0x08
	vkDelete   = 0x2E
	vkLeft     = 0x25
	vkUp       = 0x26
	vkRight    = 0x27
	vkDown     = 0x28
	vkHome     = 0x24
	vkEnd      = 0x23
	vkPageUp   = 0x21
	vkPageDown = 0x22
	vkInsert   = 0x2D
	vkF1       = 0x70
	vkF2       = 0x71
	vkF3       = 0x72
	vkF4       = 0x73
	vkF5       = 0x74
	vkF6       = 0x75
	vkF7       = 0x76
	vkF8       = 0x77
	vkF9       = 0x78
	vkF10      = 0x79
	vkF11      = 0x7A
	vkF12      = 0x7B
	vkSpace    = 0x20
	vkControl  = 0x11
	vkMenu     = 0x12 // Alt
	vkShift    = 0x10
	vkWin      = 0x5B
	vkA        = 0x41
	// B-Z follow sequentially from vkA
)

// namedKeys maps friendly names to virtual key codes.
var namedKeys = map[string]uint16{
	"enter":     vkReturn,
	"return":    vkReturn,
	"tab":       vkTab,
	"escape":    vkEscape,
	"esc":       vkEscape,
	"backspace": vkBack,
	"delete":    vkDelete,
	"del":       vkDelete,
	"left":      vkLeft,
	"up":        vkUp,
	"right":     vkRight,
	"down":      vkDown,
	"home":      vkHome,
	"end":       vkEnd,
	"pageup":    vkPageUp,
	"pagedown":  vkPageDown,
	"insert":    vkInsert,
	"space":     vkSpace,
	"f1":        vkF1,
	"f2":        vkF2,
	"f3":        vkF3,
	"f4":        vkF4,
	"f5":        vkF5,
	"f6":        vkF6,
	"f7":        vkF7,
	"f8":        vkF8,
	"f9":        vkF9,
	"f10":       vkF10,
	"f11":       vkF11,
	"f12":       vkF12,
}

// modifierKeys maps modifier names to their virtual key codes.
var modifierKeys = map[string]uint16{
	"ctrl":  vkControl,
	"alt":   vkMenu,
	"shift": vkShift,
	"win":   vkWin,
}

// INPUT structure for SendInput (keyboard variant).
// https://docs.microsoft.com/en-us/windows/win32/api/winuser/ns-winuser-input
const (
	inputKeyboard   = 1
	keyeventfKeyUp  = 0x0002
	keyeventfUnicode = 0x0004
)

// keyboardInput mirrors the KEYBDINPUT structure.
type keyboardInput struct {
	wVk         uint16
	wScan       uint16
	dwFlags     uint32
	time        uint32
	dwExtraInfo uintptr
}

// inputUnion is INPUT { DWORD type; union { MOUSEINPUT; KEYBDINPUT; HARDWAREINPUT } }
// We only use the keyboard variant; pad to 28 bytes (type + largest union member).
type inputUnion struct {
	dwType  uint32
	_       uint32 // padding to align to 8 bytes on 64-bit
	ki      keyboardInput
}

func sendKeyEvent(vk uint16, keyUp bool) error {
	flags := uint32(0)
	if keyUp {
		flags = keyeventfKeyUp
	}
	inp := inputUnion{
		dwType: inputKeyboard,
		ki: keyboardInput{
			wVk:     vk,
			dwFlags: flags,
		},
	}
	ret, _, err := procSendInputKeyboard.Call(
		1,
		uintptr(unsafe.Pointer(&inp)),
		uintptr(unsafe.Sizeof(inp)),
	)
	if ret == 0 {
		return fmt.Errorf("SendInput failed: %w", err)
	}
	return nil
}

func sendUnicodeChar(ch rune) error {
	// Key down
	inp := inputUnion{
		dwType: inputKeyboard,
		ki: keyboardInput{
			wScan:   uint16(ch),
			dwFlags: keyeventfUnicode,
		},
	}
	ret, _, err := procSendInputKeyboard.Call(
		1,
		uintptr(unsafe.Pointer(&inp)),
		uintptr(unsafe.Sizeof(inp)),
	)
	if ret == 0 {
		return fmt.Errorf("SendInput (down) failed: %w", err)
	}
	// Key up
	inp.ki.dwFlags = keyeventfUnicode | keyeventfKeyUp
	ret, _, err = procSendInputKeyboard.Call(
		1,
		uintptr(unsafe.Pointer(&inp)),
		uintptr(unsafe.Sizeof(inp)),
	)
	if ret == 0 {
		return fmt.Errorf("SendInput (up) failed: %w", err)
	}
	return nil
}

// resolveKey converts a name like "a", "enter", "f5" to a virtual key code.
// Returns 0 if not found.
func resolveKey(name string) uint16 {
	name = strings.ToLower(strings.TrimSpace(name))
	if vk, ok := namedKeys[name]; ok {
		return vk
	}
	// Single letter A-Z
	if len(name) == 1 {
		c := name[0]
		if c >= 'a' && c <= 'z' {
			return uint16(vkA + int(c-'a'))
		}
		if c >= '0' && c <= '9' {
			return uint16('0' + int(c-'0'))
		}
	}
	return 0
}

// KeyboardTool simulates keyboard input.
type KeyboardTool struct{}

type keyboardParams struct {
	Operation string   `json:"operation"`
	Text      string   `json:"text,omitempty"`
	Key       string   `json:"key,omitempty"`
	Modifiers []string `json:"modifiers,omitempty"`
}

func (KeyboardTool) Name() string { return "keyboard" }

func (KeyboardTool) Description() string {
	return "Simulate keyboard input: type text, press a key, or send a hotkey combination. " +
		"Operations: type (sends text character by character), " +
		"press (presses and releases a single key), " +
		"hotkey (presses modifier+key combo, e.g. ctrl+c)."
}

func (KeyboardTool) Parameters() json.RawMessage {
	return json.RawMessage(`{
  "type": "object",
  "required": ["operation"],
  "properties": {
    "operation": {
      "type": "string",
      "enum": ["type", "press", "hotkey"],
      "description": "type: send text; press: single key; hotkey: modifier combo"
    },
    "text": {
      "type": "string",
      "description": "Text to type (used with operation=type)"
    },
    "key": {
      "type": "string",
      "description": "Key name to press (enter, tab, escape, a-z, f1-f12, etc.)"
    },
    "modifiers": {
      "type": "array",
      "items": {"type": "string"},
      "description": "Modifier keys to hold: ctrl, alt, shift, win"
    }
  }
}`)
}

func (t KeyboardTool) Execute(_ context.Context, params json.RawMessage) (*Result, error) {
	var p keyboardParams
	if err := json.Unmarshal(params, &p); err != nil {
		return &Result{Error: fmt.Sprintf("invalid parameters: %v", err)}, nil
	}

	switch p.Operation {
	case "type":
		if p.Text == "" {
			return &Result{Error: "text must not be empty for operation=type"}, nil
		}
		for _, ch := range p.Text {
			if err := sendUnicodeChar(ch); err != nil {
				return &Result{Error: err.Error()}, nil
			}
		}
		return &Result{Output: fmt.Sprintf("typed %d character(s)", len([]rune(p.Text)))}, nil

	case "press":
		if p.Key == "" {
			return &Result{Error: "key must not be empty for operation=press"}, nil
		}
		vk := resolveKey(p.Key)
		if vk == 0 {
			return &Result{Error: fmt.Sprintf("unknown key: %q", p.Key)}, nil
		}
		if err := sendKeyEvent(vk, false); err != nil {
			return &Result{Error: err.Error()}, nil
		}
		if err := sendKeyEvent(vk, true); err != nil {
			return &Result{Error: err.Error()}, nil
		}
		return &Result{Output: fmt.Sprintf("pressed key: %s", p.Key)}, nil

	case "hotkey":
		if p.Key == "" {
			return &Result{Error: "key must not be empty for operation=hotkey"}, nil
		}
		// Press modifiers down
		for _, mod := range p.Modifiers {
			vk, ok := modifierKeys[strings.ToLower(mod)]
			if !ok {
				return &Result{Error: fmt.Sprintf("unknown modifier: %q", mod)}, nil
			}
			if err := sendKeyEvent(vk, false); err != nil {
				return &Result{Error: err.Error()}, nil
			}
		}
		// Press and release the main key
		vk := resolveKey(p.Key)
		if vk == 0 {
			// Release modifiers before returning error
			for _, mod := range p.Modifiers {
				if modVk, ok := modifierKeys[strings.ToLower(mod)]; ok {
					_ = sendKeyEvent(modVk, true)
				}
			}
			return &Result{Error: fmt.Sprintf("unknown key: %q", p.Key)}, nil
		}
		if err := sendKeyEvent(vk, false); err != nil {
			return &Result{Error: err.Error()}, nil
		}
		if err := sendKeyEvent(vk, true); err != nil {
			return &Result{Error: err.Error()}, nil
		}
		// Release modifiers in reverse order
		for i := len(p.Modifiers) - 1; i >= 0; i-- {
			if modVk, ok := modifierKeys[strings.ToLower(p.Modifiers[i])]; ok {
				_ = sendKeyEvent(modVk, true)
			}
		}
		return &Result{Output: fmt.Sprintf("hotkey: %s+%s", strings.Join(p.Modifiers, "+"), p.Key)}, nil

	default:
		return &Result{Error: fmt.Sprintf("unknown operation: %q (want type|press|hotkey)", p.Operation)}, nil
	}
}

var _ Tool = KeyboardTool{}

func init() {
	Register(KeyboardTool{})
}
