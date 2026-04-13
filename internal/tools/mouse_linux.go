//go:build linux

package tools

import (
	"fmt"

	"github.com/jezek/xgb/xproto"
	"github.com/jezek/xgb/xtest"
)

// X11 button codes: 1=left, 2=middle, 3=right, 4=scroll-up, 5=scroll-down.
const (
	x11ButtonLeft     = 1
	x11ButtonRight    = 3
	x11ButtonScrollUp = 4
	x11ButtonScrollDn = 5
)

func mouseMove(x, y int32) error {
	c, err := x11Conn()
	if err != nil {
		return err
	}
	// WarpPointer moves the cursor without needing XTest.
	return xproto.WarpPointerChecked(c, 0, x11Root(), 0, 0, 0, 0, int16(x), int16(y)).Check()
}

func mouseClick(x, y int32) error {
	c, err := x11Conn()
	if err != nil {
		return err
	}
	if err := xproto.WarpPointerChecked(c, 0, x11Root(), 0, 0, 0, 0, int16(x), int16(y)).Check(); err != nil {
		return fmt.Errorf("warp: %w", err)
	}
	if err := xtest.FakeInputChecked(c, xproto.ButtonPress, x11ButtonLeft, 0, x11Root(), 0, 0, 0).Check(); err != nil {
		return fmt.Errorf("button down: %w", err)
	}
	return xtest.FakeInputChecked(c, xproto.ButtonRelease, x11ButtonLeft, 0, x11Root(), 0, 0, 0).Check()
}

func mouseDoubleClick(x, y int32) error {
	if err := mouseClick(x, y); err != nil {
		return err
	}
	return mouseClick(x, y)
}

func mouseRightClick(x, y int32) error {
	c, err := x11Conn()
	if err != nil {
		return err
	}
	if err := xproto.WarpPointerChecked(c, 0, x11Root(), 0, 0, 0, 0, int16(x), int16(y)).Check(); err != nil {
		return fmt.Errorf("warp: %w", err)
	}
	if err := xtest.FakeInputChecked(c, xproto.ButtonPress, x11ButtonRight, 0, x11Root(), 0, 0, 0).Check(); err != nil {
		return fmt.Errorf("button down: %w", err)
	}
	return xtest.FakeInputChecked(c, xproto.ButtonRelease, x11ButtonRight, 0, x11Root(), 0, 0, 0).Check()
}

func mouseScroll(x, y, delta int32) error {
	c, err := x11Conn()
	if err != nil {
		return err
	}
	if err := xproto.WarpPointerChecked(c, 0, x11Root(), 0, 0, 0, 0, int16(x), int16(y)).Check(); err != nil {
		return fmt.Errorf("warp: %w", err)
	}
	// Each button 4/5 press+release = one scroll notch.
	// Windows WHEEL_DELTA=120 per notch.
	notches := delta / 120
	if notches == 0 {
		if delta > 0 {
			notches = 1
		} else {
			notches = -1
		}
	}

	button := byte(x11ButtonScrollUp)
	if notches < 0 {
		button = x11ButtonScrollDn
		notches = -notches
	}
	for i := int32(0); i < notches; i++ {
		_ = xtest.FakeInputChecked(c, xproto.ButtonPress, button, 0, x11Root(), 0, 0, 0).Check()
		_ = xtest.FakeInputChecked(c, xproto.ButtonRelease, button, 0, x11Root(), 0, 0, 0).Check()
	}
	return nil
}

func init() { Register(MouseTool{}) }
