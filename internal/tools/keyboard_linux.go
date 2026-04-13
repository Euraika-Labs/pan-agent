//go:build linux

package tools

import (
	"fmt"
	"strings"
	"time"

	"github.com/jezek/xgb/xproto"
	"github.com/jezek/xgb/xtest"
)

// X11 keysyms for named keys.
// See /usr/include/X11/keysymdef.h
var x11NamedKeysyms = map[string]uint32{
	"enter": 0xff0d, "return": 0xff0d, "tab": 0xff09,
	"escape": 0xff1b, "esc": 0xff1b, "backspace": 0xff08,
	"delete": 0xffff, "del": 0xffff,
	"left": 0xff51, "up": 0xff52, "right": 0xff53, "down": 0xff54,
	"home": 0xff50, "end": 0xff57,
	"pageup": 0xff55, "pagedown": 0xff56,
	"insert": 0xff63, "space": 0x0020,
	"f1": 0xffbe, "f2": 0xffbf, "f3": 0xffc0, "f4": 0xffc1,
	"f5": 0xffc2, "f6": 0xffc3, "f7": 0xffc4, "f8": 0xffc5,
	"f9": 0xffc6, "f10": 0xffc7, "f11": 0xffc8, "f12": 0xffc9,
}

var x11ModifierKeysyms = map[string]uint32{
	"ctrl":  0xffe3, // XK_Control_L
	"alt":   0xffe9, // XK_Alt_L
	"shift": 0xffe1, // XK_Shift_L
	"win":   0xffeb, // XK_Super_L
	"super": 0xffeb,
}

// x11ResolveKey maps a friendly key name to an X11 keysym.
func x11ResolveKey(name string) (uint32, bool) {
	name = strings.ToLower(strings.TrimSpace(name))
	if sym, ok := x11NamedKeysyms[name]; ok {
		return sym, true
	}
	// Single ASCII character: keysym = codepoint for Latin-1.
	if len(name) == 1 {
		c := name[0]
		if c >= 'a' && c <= 'z' {
			return uint32(c), true
		}
		if c >= '0' && c <= '9' {
			return uint32(c), true
		}
	}
	return 0, false
}

func keyboardTypeText(text string) error {
	c, err := x11Conn()
	if err != nil {
		return err
	}

	for _, ch := range text {
		// For Latin-1 (0x20-0xff), keysym == codepoint.
		// For Unicode above 0xff, use XK_Unicode: 0x01000000 | codepoint.
		var keysym uint32
		if ch <= 0xff {
			keysym = uint32(ch)
		} else {
			keysym = 0x01000000 | uint32(ch)
		}

		kc, ok := x11KeysymToKeycode(c, keysym)
		if !ok {
			// Try lowercase variant for uppercase letters.
			if ch >= 'A' && ch <= 'Z' {
				kc, ok = x11KeysymToKeycode(c, uint32(ch+32))
				if ok {
					// Need shift for uppercase.
					shiftKC, shiftOK := x11KeysymToKeycode(c, x11ModifierKeysyms["shift"])
					if shiftOK {
						_ = xtest.FakeInputChecked(c, xproto.KeyPress, byte(shiftKC), 0, x11Root(), 0, 0, 0).Check()
						_ = xtest.FakeInputChecked(c, xproto.KeyPress, byte(kc), 0, x11Root(), 0, 0, 0).Check()
						_ = xtest.FakeInputChecked(c, xproto.KeyRelease, byte(kc), 0, x11Root(), 0, 0, 0).Check()
						_ = xtest.FakeInputChecked(c, xproto.KeyRelease, byte(shiftKC), 0, x11Root(), 0, 0, 0).Check()
						time.Sleep(time.Millisecond)
						continue
					}
				}
			}
			continue // Skip characters we can't type.
		}

		// Check if shift is needed (uppercase letter, shifted symbol).
		needShift := ch >= 'A' && ch <= 'Z'
		if needShift {
			shiftKC, _ := x11KeysymToKeycode(c, x11ModifierKeysyms["shift"])
			_ = xtest.FakeInputChecked(c, xproto.KeyPress, byte(shiftKC), 0, x11Root(), 0, 0, 0).Check()
		}

		_ = xtest.FakeInputChecked(c, xproto.KeyPress, byte(kc), 0, x11Root(), 0, 0, 0).Check()
		_ = xtest.FakeInputChecked(c, xproto.KeyRelease, byte(kc), 0, x11Root(), 0, 0, 0).Check()

		if needShift {
			shiftKC, _ := x11KeysymToKeycode(c, x11ModifierKeysyms["shift"])
			_ = xtest.FakeInputChecked(c, xproto.KeyRelease, byte(shiftKC), 0, x11Root(), 0, 0, 0).Check()
		}

		time.Sleep(time.Millisecond)
	}
	return nil
}

func keyboardPressKey(key string) error {
	c, err := x11Conn()
	if err != nil {
		return err
	}

	sym, ok := x11ResolveKey(key)
	if !ok {
		return fmt.Errorf("unknown key: %q", key)
	}
	kc, ok := x11KeysymToKeycode(c, sym)
	if !ok {
		return fmt.Errorf("no keycode for key %q (keysym 0x%x)", key, sym)
	}

	if err := xtest.FakeInputChecked(c, xproto.KeyPress, byte(kc), 0, x11Root(), 0, 0, 0).Check(); err != nil {
		return fmt.Errorf("key press: %w", err)
	}
	if err := xtest.FakeInputChecked(c, xproto.KeyRelease, byte(kc), 0, x11Root(), 0, 0, 0).Check(); err != nil {
		return fmt.Errorf("key release: %w", err)
	}
	return nil
}

func keyboardHotkey(modifiers []string, key string) error {
	c, err := x11Conn()
	if err != nil {
		return err
	}

	// Resolve main key.
	sym, ok := x11ResolveKey(key)
	if !ok {
		return fmt.Errorf("unknown key: %q", key)
	}
	mainKC, ok := x11KeysymToKeycode(c, sym)
	if !ok {
		return fmt.Errorf("no keycode for key %q", key)
	}

	// Resolve and press modifiers.
	var modKeycodes []xproto.Keycode
	for _, mod := range modifiers {
		modSym, ok := x11ModifierKeysyms[strings.ToLower(mod)]
		if !ok {
			return fmt.Errorf("unknown modifier: %q", mod)
		}
		modKC, ok := x11KeysymToKeycode(c, modSym)
		if !ok {
			return fmt.Errorf("no keycode for modifier %q", mod)
		}
		modKeycodes = append(modKeycodes, modKC)
		_ = xtest.FakeInputChecked(c, xproto.KeyPress, byte(modKC), 0, x11Root(), 0, 0, 0).Check()
	}

	// Press and release main key.
	_ = xtest.FakeInputChecked(c, xproto.KeyPress, byte(mainKC), 0, x11Root(), 0, 0, 0).Check()
	_ = xtest.FakeInputChecked(c, xproto.KeyRelease, byte(mainKC), 0, x11Root(), 0, 0, 0).Check()

	// Release modifiers in reverse.
	for i := len(modKeycodes) - 1; i >= 0; i-- {
		_ = xtest.FakeInputChecked(c, xproto.KeyRelease, byte(modKeycodes[i]), 0, x11Root(), 0, 0, 0).Check()
	}
	return nil
}

func init() { Register(KeyboardTool{}) }
