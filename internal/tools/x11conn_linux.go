//go:build linux

// x11conn_linux provides a shared X11 connection and helpers for the
// keyboard, mouse, and window manager tools on Linux.
package tools

import (
	"fmt"
	"sync"

	"github.com/jezek/xgb"
	"github.com/jezek/xgb/xproto"
	"github.com/jezek/xgb/xtest"
)

var (
	x11Once sync.Once
	x11conn *xgb.Conn
	x11err  error
	x11root xproto.Window
	x11min  xproto.Keycode

	// keysymToKeycode is built once from GetKeyboardMapping.
	keymapOnce sync.Once
	keymapData map[uint32]xproto.Keycode // keysym → keycode

	// atomCache caches InternAtom results.
	atomMu    sync.Mutex
	atomCache = map[string]xproto.Atom{}
)

// x11Conn returns a lazily-opened X11 connection. Safe for concurrent use.
func x11Conn() (*xgb.Conn, error) {
	x11Once.Do(func() {
		x11conn, x11err = xgb.NewConn()
		if x11err != nil {
			return
		}
		setup := xproto.Setup(x11conn)
		x11root = setup.DefaultScreen(x11conn).Root
		x11min = setup.MinKeycode

		// Initialize XTest extension.
		if err := xtest.Init(x11conn); err != nil {
			x11err = fmt.Errorf("XTest extension not available: %w", err)
			x11conn.Close()
			x11conn = nil
		}
	})
	return x11conn, x11err
}

// x11Root returns the root window of the default screen.
func x11Root() xproto.Window { return x11root }

// buildKeymap reads the keyboard mapping from the X server once.
func buildKeymap(c *xgb.Conn) {
	keymapOnce.Do(func() {
		keymapData = make(map[uint32]xproto.Keycode)
		setup := xproto.Setup(c)
		minKC := setup.MinKeycode
		maxKC := setup.MaxKeycode
		count := byte(maxKC - minKC + 1)

		reply, err := xproto.GetKeyboardMapping(c, minKC, count).Reply()
		if err != nil || reply == nil {
			return
		}

		symsPerCode := int(reply.KeysymsPerKeycode)
		for i := 0; i < int(count); i++ {
			kc := xproto.Keycode(byte(minKC) + byte(i))
			for j := 0; j < symsPerCode; j++ {
				sym := reply.Keysyms[i*symsPerCode+j]
				if sym != 0 {
					// First mapping wins (unshifted).
					if _, exists := keymapData[uint32(sym)]; !exists {
						keymapData[uint32(sym)] = kc
					}
				}
			}
		}
	})
}

// x11KeysymToKeycode resolves an X11 keysym to a keycode using the server's
// current keyboard mapping.
func x11KeysymToKeycode(c *xgb.Conn, keysym uint32) (xproto.Keycode, bool) {
	buildKeymap(c)
	kc, ok := keymapData[keysym]
	return kc, ok
}

// x11InternAtom looks up (or caches) an X11 atom by name.
func x11InternAtom(c *xgb.Conn, name string) (xproto.Atom, error) {
	atomMu.Lock()
	if a, ok := atomCache[name]; ok {
		atomMu.Unlock()
		return a, nil
	}
	atomMu.Unlock()

	reply, err := xproto.InternAtom(c, false, uint16(len(name)), name).Reply()
	if err != nil {
		return 0, fmt.Errorf("InternAtom(%s): %w", name, err)
	}

	atomMu.Lock()
	atomCache[name] = reply.Atom
	atomMu.Unlock()
	return reply.Atom, nil
}

// x11SendClientMessage sends a ClientMessage event to the root window,
// targeting a specific window with a given message type and data.
func x11SendClientMessage(c *xgb.Conn, window xproto.Window, msgType xproto.Atom, data ...uint32) error {
	var d [5]uint32
	copy(d[:], data)

	ev := xproto.ClientMessageEvent{
		Format: 32,
		Window: window,
		Type:   msgType,
	}
	// Pack uint32 data into the Data union (20 bytes = 5 x uint32).
	for i, v := range d {
		ev.Data.Data32[i] = v
	}

	mask := uint32(xproto.EventMaskSubstructureRedirect | xproto.EventMaskSubstructureNotify)
	return xproto.SendEventChecked(c, false, x11Root(), mask, string(ev.Bytes())).Check()
}
