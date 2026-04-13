//go:build darwin

package tools

/*
#cgo LDFLAGS: -framework CoreGraphics -framework ApplicationServices
#include <CoreGraphics/CoreGraphics.h>
#include <ApplicationServices/ApplicationServices.h>
#include <stdlib.h>

// getWindowList returns all on-screen windows as a CFArray of CFDictionaries.
CFArrayRef getWindowList() {
    return CGWindowListCopyWindowInfo(
        kCGWindowListOptionOnScreenOnly | kCGWindowListExcludeDesktopElements,
        kCGNullWindowID
    );
}
*/
import "C"

import (
	"fmt"
	"os/exec"
	"strings"
	"unsafe"
)

type darwinWindow struct {
	pid   int
	wid   int
	title string
	app   string
}

func listDarwinWindows() ([]darwinWindow, error) {
	list := C.getWindowList()
	if list == 0 {
		return nil, fmt.Errorf("CGWindowListCopyWindowInfo returned nil")
	}
	defer C.CFRelease(C.CFTypeRef(list))

	count := C.CFArrayGetCount(list)
	var windows []darwinWindow

	for i := C.CFIndex(0); i < count; i++ {
		dict := C.CFDictionaryRef(C.CFArrayGetValueAtIndex(list, i))

		// Get window name.
		var namePtr unsafe.Pointer
		nameKey := C.CFStringCreateWithCString(0, C.CString("kCGWindowName"), C.kCFStringEncodingUTF8)
		if C.CFDictionaryGetValueIfPresent(dict, unsafe.Pointer(nameKey), &namePtr) == 0 {
			C.CFRelease(C.CFTypeRef(nameKey))
			continue
		}
		C.CFRelease(C.CFTypeRef(nameKey))

		title := cfStringToGo(C.CFStringRef(namePtr))
		if title == "" {
			continue
		}

		// Get owner name (app name).
		var ownerPtr unsafe.Pointer
		ownerKey := C.CFStringCreateWithCString(0, C.CString("kCGWindowOwnerName"), C.kCFStringEncodingUTF8)
		app := ""
		if C.CFDictionaryGetValueIfPresent(dict, unsafe.Pointer(ownerKey), &ownerPtr) != 0 {
			app = cfStringToGo(C.CFStringRef(ownerPtr))
		}
		C.CFRelease(C.CFTypeRef(ownerKey))

		// Get PID.
		var pidPtr unsafe.Pointer
		pidKey := C.CFStringCreateWithCString(0, C.CString("kCGWindowOwnerPID"), C.kCFStringEncodingUTF8)
		pid := 0
		if C.CFDictionaryGetValueIfPresent(dict, unsafe.Pointer(pidKey), &pidPtr) != 0 {
			var val C.int
			C.CFNumberGetValue(C.CFNumberRef(pidPtr), C.kCFNumberIntType, unsafe.Pointer(&val))
			pid = int(val)
		}
		C.CFRelease(C.CFTypeRef(pidKey))

		// Get window ID.
		var widPtr unsafe.Pointer
		widKey := C.CFStringCreateWithCString(0, C.CString("kCGWindowNumber"), C.kCFStringEncodingUTF8)
		wid := 0
		if C.CFDictionaryGetValueIfPresent(dict, unsafe.Pointer(widKey), &widPtr) != 0 {
			var val C.int
			C.CFNumberGetValue(C.CFNumberRef(widPtr), C.kCFNumberIntType, unsafe.Pointer(&val))
			wid = int(val)
		}
		C.CFRelease(C.CFTypeRef(widKey))

		windows = append(windows, darwinWindow{pid: pid, wid: wid, title: title, app: app})
	}

	return windows, nil
}

func cfStringToGo(ref C.CFStringRef) string {
	length := C.CFStringGetLength(ref)
	if length == 0 {
		return ""
	}
	buf := make([]byte, length*4)
	var usedLen C.CFIndex
	C.CFStringGetBytes(ref, C.CFRangeMake(0, length), C.kCFStringEncodingUTF8, 0, 0,
		(*C.UInt8)(unsafe.Pointer(&buf[0])), C.CFIndex(len(buf)), &usedLen)
	return string(buf[:usedLen])
}

func findDarwinWindow(substr string) (*darwinWindow, error) {
	windows, err := listDarwinWindows()
	if err != nil {
		return nil, err
	}
	lower := strings.ToLower(substr)
	for i := range windows {
		if strings.Contains(strings.ToLower(windows[i].title), lower) ||
			strings.Contains(strings.ToLower(windows[i].app), lower) {
			return &windows[i], nil
		}
	}
	return nil, fmt.Errorf("no window found matching %q", substr)
}

// osascript runs an AppleScript command. Used for window operations that
// require the Accessibility API (which needs CGo with the full AX framework)
// or can be done more simply via AppleScript.
func osascript(script string) error {
	cmd := exec.Command("osascript", "-e", script)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("osascript: %s: %w", strings.TrimSpace(string(out)), err)
	}
	return nil
}

func wmListWindows() (string, error) {
	windows, err := listDarwinWindows()
	if err != nil {
		return "", err
	}
	var lines []string
	for _, w := range windows {
		lines = append(lines, fmt.Sprintf("wid=%d  pid=%d  [%s] %s", w.wid, w.pid, w.app, w.title))
	}
	return strings.Join(lines, "\n"), nil
}

func wmFindWindow(title string) (string, error) {
	w, err := findDarwinWindow(title)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("wid=%d  pid=%d  [%s] %s", w.wid, w.pid, w.app, w.title), nil
}

func wmFocusWindow(title string) (string, error) {
	w, err := findDarwinWindow(title)
	if err != nil {
		return "", err
	}
	script := fmt.Sprintf(`tell application "%s" to activate`, w.app)
	if err := osascript(script); err != nil {
		return "", fmt.Errorf("focus %q: %w", w.app, err)
	}
	return fmt.Sprintf("focused: [%s] %s", w.app, w.title), nil
}

func wmMoveWindow(title string, x, y int32) (string, error) {
	w, err := findDarwinWindow(title)
	if err != nil {
		return "", err
	}
	script := fmt.Sprintf(`tell application "System Events" to tell process "%s"
		set position of front window to {%d, %d}
	end tell`, w.app, x, y)
	if err := osascript(script); err != nil {
		return "", fmt.Errorf("move %q: %w", w.title, err)
	}
	return fmt.Sprintf("moved [%s] %q to (%d, %d)", w.app, w.title, x, y), nil
}

func wmResizeWindow(title string, width, height int32) (string, error) {
	w, err := findDarwinWindow(title)
	if err != nil {
		return "", err
	}
	script := fmt.Sprintf(`tell application "System Events" to tell process "%s"
		set size of front window to {%d, %d}
	end tell`, w.app, width, height)
	if err := osascript(script); err != nil {
		return "", fmt.Errorf("resize %q: %w", w.title, err)
	}
	return fmt.Sprintf("resized [%s] %q to %dx%d", w.app, w.title, width, height), nil
}

func wmCloseWindow(title string) (string, error) {
	w, err := findDarwinWindow(title)
	if err != nil {
		return "", err
	}
	script := fmt.Sprintf(`tell application "System Events" to tell process "%s"
		click button 1 of front window
	end tell`, w.app)
	// Try the close button first; fall back to Cmd+W.
	if err := osascript(script); err != nil {
		script = fmt.Sprintf(`tell application "%s" to close front window`, w.app)
		if err2 := osascript(script); err2 != nil {
			return "", fmt.Errorf("close %q: %w", w.title, err2)
		}
	}
	return fmt.Sprintf("closed [%s] %q", w.app, w.title), nil
}

func init() { Register(WindowManagerTool{}) }
