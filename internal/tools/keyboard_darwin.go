//go:build darwin

package tools

/*
#cgo LDFLAGS: -framework CoreGraphics -framework Carbon
#include <CoreGraphics/CoreGraphics.h>
#include <Carbon/Carbon.h>

void postKeyEvent(CGKeyCode keyCode, bool keyDown) {
    CGEventRef event = CGEventCreateKeyboardEvent(NULL, keyCode, keyDown);
    CGEventPost(kCGHIDEventTap, event);
    CFRelease(event);
}

void postKeyEventWithFlags(CGKeyCode keyCode, bool keyDown, CGEventFlags flags) {
    CGEventRef event = CGEventCreateKeyboardEvent(NULL, keyCode, keyDown);
    CGEventSetFlags(event, flags);
    CGEventPost(kCGHIDEventTap, event);
    CFRelease(event);
}

void typeUnicodeString(const UniChar *chars, int length) {
    CGEventRef event = CGEventCreateKeyboardEvent(NULL, 0, true);
    CGEventKeyboardSetUnicodeString(event, length, chars);
    CGEventPost(kCGHIDEventTap, event);
    CFRelease(event);
    // Key up
    event = CGEventCreateKeyboardEvent(NULL, 0, false);
    CGEventKeyboardSetUnicodeString(event, length, chars);
    CGEventPost(kCGHIDEventTap, event);
    CFRelease(event);
}
*/
import "C"

import (
	"fmt"
	"strings"
	"unsafe"
)

// macOS virtual key codes (Carbon kVK_* constants).
var macNamedKeys = map[string]C.CGKeyCode{
	"enter": C.kVK_Return, "return": C.kVK_Return, "tab": C.kVK_Tab,
	"escape": C.kVK_Escape, "esc": C.kVK_Escape, "backspace": C.kVK_Delete,
	"delete": C.kVK_ForwardDelete, "del": C.kVK_ForwardDelete,
	"left": C.kVK_LeftArrow, "up": C.kVK_UpArrow,
	"right": C.kVK_RightArrow, "down": C.kVK_DownArrow,
	"home": C.kVK_Home, "end": C.kVK_End,
	"pageup": C.kVK_PageUp, "pagedown": C.kVK_PageDown,
	"space": C.kVK_Space,
	"f1":    C.kVK_F1, "f2": C.kVK_F2, "f3": C.kVK_F3, "f4": C.kVK_F4,
	"f5": C.kVK_F5, "f6": C.kVK_F6, "f7": C.kVK_F7, "f8": C.kVK_F8,
	"f9": C.kVK_F9, "f10": C.kVK_F10, "f11": C.kVK_F11, "f12": C.kVK_F12,
}

// macOS letter key codes (ANSI layout).
var macLetterKeys = map[byte]C.CGKeyCode{
	'a': C.kVK_ANSI_A, 'b': C.kVK_ANSI_B, 'c': C.kVK_ANSI_C,
	'd': C.kVK_ANSI_D, 'e': C.kVK_ANSI_E, 'f': C.kVK_ANSI_F,
	'g': C.kVK_ANSI_G, 'h': C.kVK_ANSI_H, 'i': C.kVK_ANSI_I,
	'j': C.kVK_ANSI_J, 'k': C.kVK_ANSI_K, 'l': C.kVK_ANSI_L,
	'm': C.kVK_ANSI_M, 'n': C.kVK_ANSI_N, 'o': C.kVK_ANSI_O,
	'p': C.kVK_ANSI_P, 'q': C.kVK_ANSI_Q, 'r': C.kVK_ANSI_R,
	's': C.kVK_ANSI_S, 't': C.kVK_ANSI_T, 'u': C.kVK_ANSI_U,
	'v': C.kVK_ANSI_V, 'w': C.kVK_ANSI_W, 'x': C.kVK_ANSI_X,
	'y': C.kVK_ANSI_Y, 'z': C.kVK_ANSI_Z,
}

var macNumberKeys = map[byte]C.CGKeyCode{
	'0': C.kVK_ANSI_0, '1': C.kVK_ANSI_1, '2': C.kVK_ANSI_2,
	'3': C.kVK_ANSI_3, '4': C.kVK_ANSI_4, '5': C.kVK_ANSI_5,
	'6': C.kVK_ANSI_6, '7': C.kVK_ANSI_7, '8': C.kVK_ANSI_8,
	'9': C.kVK_ANSI_9,
}

var macModifierFlags = map[string]C.CGEventFlags{
	"ctrl":  C.kCGEventFlagMaskControl,
	"alt":   C.kCGEventFlagMaskAlternate,
	"shift": C.kCGEventFlagMaskShift,
	"cmd":   C.kCGEventFlagMaskCommand,
	"win":   C.kCGEventFlagMaskCommand,
	"super": C.kCGEventFlagMaskCommand,
}

var macModifierKeys = map[string]C.CGKeyCode{
	"ctrl":  C.CGKeyCode(0x3B), // kVK_Control
	"alt":   C.CGKeyCode(0x3A), // kVK_Option
	"shift": C.CGKeyCode(0x38), // kVK_Shift
	"cmd":   C.CGKeyCode(0x37), // kVK_Command
	"win":   C.CGKeyCode(0x37),
	"super": C.CGKeyCode(0x37),
}

func macResolveKey(name string) (C.CGKeyCode, bool) {
	name = strings.ToLower(strings.TrimSpace(name))
	if kc, ok := macNamedKeys[name]; ok {
		return kc, true
	}
	if len(name) == 1 {
		if kc, ok := macLetterKeys[name[0]]; ok {
			return kc, true
		}
		if kc, ok := macNumberKeys[name[0]]; ok {
			return kc, true
		}
	}
	return 0, false
}

func keyboardTypeText(text string) error {
	runes := []rune(text)
	// Send in chunks of up to 20 characters for efficiency.
	for i := 0; i < len(runes); i += 20 {
		end := i + 20
		if end > len(runes) {
			end = len(runes)
		}
		chunk := runes[i:end]
		chars := make([]C.UniChar, len(chunk))
		for j, r := range chunk {
			chars[j] = C.UniChar(r)
		}
		C.typeUnicodeString((*C.UniChar)(unsafe.Pointer(&chars[0])), C.int(len(chars)))
	}
	return nil
}

func keyboardPressKey(key string) error {
	kc, ok := macResolveKey(key)
	if !ok {
		return fmt.Errorf("unknown key: %q", key)
	}
	C.postKeyEvent(kc, C.bool(true))
	C.postKeyEvent(kc, C.bool(false))
	return nil
}

func keyboardHotkey(modifiers []string, key string) error {
	kc, ok := macResolveKey(key)
	if !ok {
		return fmt.Errorf("unknown key: %q", key)
	}

	// Build combined modifier flags.
	var flags C.CGEventFlags
	for _, mod := range modifiers {
		f, ok := macModifierFlags[strings.ToLower(mod)]
		if !ok {
			return fmt.Errorf("unknown modifier: %q", mod)
		}
		flags |= f
	}

	// Press modifier keys down.
	for _, mod := range modifiers {
		if mkc, ok := macModifierKeys[strings.ToLower(mod)]; ok {
			C.postKeyEvent(mkc, C.bool(true))
		}
	}

	// Press and release the main key with modifier flags.
	C.postKeyEventWithFlags(kc, C.bool(true), flags)
	C.postKeyEventWithFlags(kc, C.bool(false), flags)

	// Release modifier keys in reverse order.
	for i := len(modifiers) - 1; i >= 0; i-- {
		if mkc, ok := macModifierKeys[strings.ToLower(modifiers[i])]; ok {
			C.postKeyEvent(mkc, C.bool(false))
		}
	}
	return nil
}

func init() { Register(KeyboardTool{}) }
