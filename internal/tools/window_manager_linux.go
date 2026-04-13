//go:build linux

package tools

import (
	"encoding/binary"
	"fmt"
	"strings"

	"github.com/jezek/xgb"
	"github.com/jezek/xgb/xproto"
)

// ewmhGetWindowList reads _NET_CLIENT_LIST from the root window.
func ewmhGetWindowList(c *xgb.Conn) ([]xproto.Window, error) {
	atom, err := x11InternAtom(c, "_NET_CLIENT_LIST")
	if err != nil {
		return nil, err
	}
	reply, err := xproto.GetProperty(c, false, x11Root(), atom, xproto.AtomWindow, 0, 1024).Reply()
	if err != nil {
		return nil, fmt.Errorf("GetProperty(_NET_CLIENT_LIST): %w", err)
	}
	if reply.Format != 32 {
		return nil, fmt.Errorf("_NET_CLIENT_LIST: unexpected format %d", reply.Format)
	}
	count := int(reply.ValueLen)
	windows := make([]xproto.Window, count)
	for i := 0; i < count; i++ {
		windows[i] = xproto.Window(binary.LittleEndian.Uint32(reply.Value[i*4:]))
	}
	return windows, nil
}

// ewmhGetWindowTitle reads _NET_WM_NAME (UTF-8) or falls back to WM_NAME.
func ewmhGetWindowTitle(c *xgb.Conn, w xproto.Window) string {
	// Try _NET_WM_NAME (UTF-8).
	utf8Atom, _ := x11InternAtom(c, "UTF8_STRING")
	netName, _ := x11InternAtom(c, "_NET_WM_NAME")
	reply, err := xproto.GetProperty(c, false, w, netName, utf8Atom, 0, 256).Reply()
	if err == nil && reply.ValueLen > 0 {
		return string(reply.Value[:reply.ValueLen])
	}
	// Fall back to WM_NAME (Latin-1).
	reply, err = xproto.GetProperty(c, false, w, xproto.AtomWmName, xproto.AtomString, 0, 256).Reply()
	if err == nil && reply.ValueLen > 0 {
		return string(reply.Value[:reply.ValueLen])
	}
	return ""
}

type linuxWindow struct {
	wid   xproto.Window
	title string
}

func listLinuxWindows(c *xgb.Conn) ([]linuxWindow, error) {
	wids, err := ewmhGetWindowList(c)
	if err != nil {
		return nil, err
	}
	var windows []linuxWindow
	for _, wid := range wids {
		title := ewmhGetWindowTitle(c, wid)
		if title != "" {
			windows = append(windows, linuxWindow{wid: wid, title: title})
		}
	}
	return windows, nil
}

func findLinuxWindow(c *xgb.Conn, substr string) (*linuxWindow, error) {
	windows, err := listLinuxWindows(c)
	if err != nil {
		return nil, err
	}
	lower := strings.ToLower(substr)
	for i := range windows {
		if strings.Contains(strings.ToLower(windows[i].title), lower) {
			return &windows[i], nil
		}
	}
	return nil, fmt.Errorf("no window found matching %q", substr)
}

func wmListWindows() (string, error) {
	c, err := x11Conn()
	if err != nil {
		return "", err
	}
	windows, err := listLinuxWindows(c)
	if err != nil {
		return "", err
	}
	var lines []string
	for _, w := range windows {
		lines = append(lines, fmt.Sprintf("wid=0x%X  %s", uint32(w.wid), w.title))
	}
	return strings.Join(lines, "\n"), nil
}

func wmFindWindow(title string) (string, error) {
	c, err := x11Conn()
	if err != nil {
		return "", err
	}
	w, err := findLinuxWindow(c, title)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("wid=0x%X  %s", uint32(w.wid), w.title), nil
}

func wmFocusWindow(title string) (string, error) {
	c, err := x11Conn()
	if err != nil {
		return "", err
	}
	w, err := findLinuxWindow(c, title)
	if err != nil {
		return "", err
	}
	atom, err := x11InternAtom(c, "_NET_ACTIVE_WINDOW")
	if err != nil {
		return "", err
	}
	// _NET_ACTIVE_WINDOW client message: source=2 (pager), timestamp=0, requestor=0.
	if err := x11SendClientMessage(c, w.wid, atom, 2, 0, 0); err != nil {
		return "", fmt.Errorf("_NET_ACTIVE_WINDOW: %w", err)
	}
	return fmt.Sprintf("focused: %s", w.title), nil
}

func wmMoveWindow(title string, x, y int32) (string, error) {
	c, err := x11Conn()
	if err != nil {
		return "", err
	}
	w, err := findLinuxWindow(c, title)
	if err != nil {
		return "", err
	}
	mask := uint16(xproto.ConfigWindowX | xproto.ConfigWindowY)
	if err := xproto.ConfigureWindowChecked(c, w.wid, mask, []uint32{uint32(x), uint32(y)}).Check(); err != nil {
		return "", fmt.Errorf("ConfigureWindow: %w", err)
	}
	return fmt.Sprintf("moved %q to (%d, %d)", w.title, x, y), nil
}

func wmResizeWindow(title string, width, height int32) (string, error) {
	c, err := x11Conn()
	if err != nil {
		return "", err
	}
	w, err := findLinuxWindow(c, title)
	if err != nil {
		return "", err
	}
	mask := uint16(xproto.ConfigWindowWidth | xproto.ConfigWindowHeight)
	if err := xproto.ConfigureWindowChecked(c, w.wid, mask, []uint32{uint32(width), uint32(height)}).Check(); err != nil {
		return "", fmt.Errorf("ConfigureWindow: %w", err)
	}
	return fmt.Sprintf("resized %q to %dx%d", w.title, width, height), nil
}

func wmCloseWindow(title string) (string, error) {
	c, err := x11Conn()
	if err != nil {
		return "", err
	}
	w, err := findLinuxWindow(c, title)
	if err != nil {
		return "", err
	}
	atom, err := x11InternAtom(c, "_NET_CLOSE_WINDOW")
	if err != nil {
		return "", err
	}
	if err := x11SendClientMessage(c, w.wid, atom, 0, 0); err != nil {
		return "", fmt.Errorf("_NET_CLOSE_WINDOW: %w", err)
	}
	return fmt.Sprintf("closed %q", w.title), nil
}

func init() { Register(WindowManagerTool{}) }
