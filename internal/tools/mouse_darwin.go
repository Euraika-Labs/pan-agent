//go:build darwin

package tools

/*
#cgo LDFLAGS: -framework CoreGraphics
#include <CoreGraphics/CoreGraphics.h>

void mouseMoveTo(int x, int y) {
    CGEventRef event = CGEventCreateMouseEvent(NULL, kCGEventMouseMoved, CGPointMake(x, y), kCGMouseButtonLeft);
    CGEventPost(kCGHIDEventTap, event);
    CFRelease(event);
}

void mouseButtonEvent(CGEventType type, int x, int y, CGMouseButton button) {
    CGEventRef event = CGEventCreateMouseEvent(NULL, type, CGPointMake(x, y), button);
    CGEventPost(kCGHIDEventTap, event);
    CFRelease(event);
}

void mouseScrollEvent(int deltaY) {
    CGEventRef event = CGEventCreateScrollWheelEvent(NULL, kCGScrollEventUnitLine, 1, deltaY);
    CGEventPost(kCGHIDEventTap, event);
    CFRelease(event);
}
*/
import "C"

func mouseMove(x, y int32) error {
	C.mouseMoveTo(C.int(x), C.int(y))
	return nil
}

func mouseClick(x, y int32) error {
	C.mouseMoveTo(C.int(x), C.int(y))
	C.mouseButtonEvent(C.kCGEventLeftMouseDown, C.int(x), C.int(y), C.kCGMouseButtonLeft)
	C.mouseButtonEvent(C.kCGEventLeftMouseUp, C.int(x), C.int(y), C.kCGMouseButtonLeft)
	return nil
}

func mouseDoubleClick(x, y int32) error {
	C.mouseMoveTo(C.int(x), C.int(y))
	// First click.
	C.mouseButtonEvent(C.kCGEventLeftMouseDown, C.int(x), C.int(y), C.kCGMouseButtonLeft)
	C.mouseButtonEvent(C.kCGEventLeftMouseUp, C.int(x), C.int(y), C.kCGMouseButtonLeft)
	// Second click.
	C.mouseButtonEvent(C.kCGEventLeftMouseDown, C.int(x), C.int(y), C.kCGMouseButtonLeft)
	C.mouseButtonEvent(C.kCGEventLeftMouseUp, C.int(x), C.int(y), C.kCGMouseButtonLeft)
	return nil
}

func mouseRightClick(x, y int32) error {
	C.mouseMoveTo(C.int(x), C.int(y))
	C.mouseButtonEvent(C.kCGEventRightMouseDown, C.int(x), C.int(y), C.kCGMouseButtonRight)
	C.mouseButtonEvent(C.kCGEventRightMouseUp, C.int(x), C.int(y), C.kCGMouseButtonRight)
	return nil
}

func mouseScroll(x, y, delta int32) error {
	C.mouseMoveTo(C.int(x), C.int(y))
	// macOS scroll: positive = scroll up, negative = scroll down.
	// Windows WHEEL_DELTA is 120 per notch; macOS uses line units.
	lines := delta / 120
	if lines == 0 {
		if delta > 0 {
			lines = 1
		} else {
			lines = -1
		}
	}
	C.mouseScrollEvent(C.int(lines))
	return nil
}

func init() { Register(MouseTool{}) }
