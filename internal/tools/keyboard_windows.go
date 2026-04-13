//go:build windows

package tools

import (
	"fmt"
	"strings"
	"syscall"
	"unsafe"
)

var (
	user32DLLKeyboard     = syscall.NewLazyDLL("user32.dll")
	procSendInputKeyboard = user32DLLKeyboard.NewProc("SendInput")
)

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
	vkMenu     = 0x12
	vkShift    = 0x10
	vkWin      = 0x5B
	vkA        = 0x41
)

var namedKeys = map[string]uint16{
	"enter": vkReturn, "return": vkReturn, "tab": vkTab,
	"escape": vkEscape, "esc": vkEscape, "backspace": vkBack,
	"delete": vkDelete, "del": vkDelete,
	"left": vkLeft, "up": vkUp, "right": vkRight, "down": vkDown,
	"home": vkHome, "end": vkEnd, "pageup": vkPageUp, "pagedown": vkPageDown,
	"insert": vkInsert, "space": vkSpace,
	"f1": vkF1, "f2": vkF2, "f3": vkF3, "f4": vkF4,
	"f5": vkF5, "f6": vkF6, "f7": vkF7, "f8": vkF8,
	"f9": vkF9, "f10": vkF10, "f11": vkF11, "f12": vkF12,
}

var modifierKeys = map[string]uint16{
	"ctrl": vkControl, "alt": vkMenu, "shift": vkShift, "win": vkWin, "super": vkWin,
}

const (
	inputKeyboard    = 1
	keyeventfKeyUp   = 0x0002
	keyeventfUnicode = 0x0004
)

type keyboardInput struct {
	wVk         uint16
	wScan       uint16
	dwFlags     uint32
	time        uint32
	dwExtraInfo uintptr
}

type inputUnion struct {
	dwType uint32
	_      uint32
	ki     keyboardInput
}

func sendKeyEvent(vk uint16, keyUp bool) error {
	flags := uint32(0)
	if keyUp {
		flags = keyeventfKeyUp
	}
	inp := inputUnion{dwType: inputKeyboard, ki: keyboardInput{wVk: vk, dwFlags: flags}}
	ret, _, err := procSendInputKeyboard.Call(1, uintptr(unsafe.Pointer(&inp)), uintptr(unsafe.Sizeof(inp)))
	if ret == 0 {
		return fmt.Errorf("SendInput failed: %w", err)
	}
	return nil
}

func sendUnicodeChar(ch rune) error {
	inp := inputUnion{dwType: inputKeyboard, ki: keyboardInput{wScan: uint16(ch), dwFlags: keyeventfUnicode}}
	ret, _, err := procSendInputKeyboard.Call(1, uintptr(unsafe.Pointer(&inp)), uintptr(unsafe.Sizeof(inp)))
	if ret == 0 {
		return fmt.Errorf("SendInput (down) failed: %w", err)
	}
	inp.ki.dwFlags = keyeventfUnicode | keyeventfKeyUp
	ret, _, err = procSendInputKeyboard.Call(1, uintptr(unsafe.Pointer(&inp)), uintptr(unsafe.Sizeof(inp)))
	if ret == 0 {
		return fmt.Errorf("SendInput (up) failed: %w", err)
	}
	return nil
}

func resolveKey(name string) uint16 {
	name = strings.ToLower(strings.TrimSpace(name))
	if vk, ok := namedKeys[name]; ok {
		return vk
	}
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

func keyboardTypeText(text string) error {
	for _, ch := range text {
		if err := sendUnicodeChar(ch); err != nil {
			return err
		}
	}
	return nil
}

func keyboardPressKey(key string) error {
	vk := resolveKey(key)
	if vk == 0 {
		return fmt.Errorf("unknown key: %q", key)
	}
	if err := sendKeyEvent(vk, false); err != nil {
		return err
	}
	return sendKeyEvent(vk, true)
}

func keyboardHotkey(modifiers []string, key string) error {
	for _, mod := range modifiers {
		vk, ok := modifierKeys[strings.ToLower(mod)]
		if !ok {
			return fmt.Errorf("unknown modifier: %q", mod)
		}
		if err := sendKeyEvent(vk, false); err != nil {
			return err
		}
	}
	vk := resolveKey(key)
	if vk == 0 {
		for _, mod := range modifiers {
			if modVk, ok := modifierKeys[strings.ToLower(mod)]; ok {
				_ = sendKeyEvent(modVk, true)
			}
		}
		return fmt.Errorf("unknown key: %q", key)
	}
	_ = sendKeyEvent(vk, false)
	_ = sendKeyEvent(vk, true)
	for i := len(modifiers) - 1; i >= 0; i-- {
		if modVk, ok := modifierKeys[strings.ToLower(modifiers[i])]; ok {
			_ = sendKeyEvent(modVk, true)
		}
	}
	return nil
}

func init() { Register(KeyboardTool{}) }
