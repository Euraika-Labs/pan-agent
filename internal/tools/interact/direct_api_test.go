package interact

import (
	"context"
	"testing"
)

func TestLinuxTypeValidation(t *testing.T) {
	d := &DirectAPI{platform: "linux"}

	// Control character rejection
	_, err := d.linuxType(context.Background(), "hello\x00world")
	if err == nil {
		t.Error("expected error for control character in text")
	}

	// Text too long
	longText := make([]byte, maxTextLen+1)
	for i := range longText {
		longText[i] = 'a'
	}
	_, err = d.linuxType(context.Background(), string(longText))
	if err == nil {
		t.Error("expected error for text exceeding max length")
	}

	// Newline and tab are allowed (won't error on validation, will fail on xdotool not being available)
	// We just test the validation passes — the command will fail because xdotool isn't installed in CI
}

func TestLinuxClickValidation(t *testing.T) {
	d := &DirectAPI{platform: "linux"}

	_, err := d.linuxClick(context.Background(), -1, 100)
	if err == nil {
		t.Error("expected error for negative x coordinate")
	}

	_, err = d.linuxClick(context.Background(), 100, 70000)
	if err == nil {
		t.Error("expected error for y coordinate > 65535")
	}
}

func TestLinuxRightClickValidation(t *testing.T) {
	d := &DirectAPI{platform: "linux"}

	_, err := d.linuxRightClick(context.Background(), -1, 100)
	if err == nil {
		t.Error("expected error for negative x coordinate")
	}
}

func TestLinuxKeyComboValidation(t *testing.T) {
	d := &DirectAPI{platform: "linux"}

	// Valid combos (will fail on exec but not on validation)
	// We just check the regex
	if !safeKeyCombo.MatchString("ctrl+c") {
		t.Error("ctrl+c should be valid")
	}
	if !safeKeyCombo.MatchString("super") {
		t.Error("super should be valid")
	}
	if !safeKeyCombo.MatchString("alt+F4") {
		t.Error("alt+F4 should be valid")
	}

	// Invalid combos
	if safeKeyCombo.MatchString("$(rm -rf /)") {
		t.Error("shell injection should be rejected")
	}
	if safeKeyCombo.MatchString("key;evil") {
		t.Error("semicolon should be rejected")
	}
	if safeKeyCombo.MatchString("") {
		t.Error("empty string should be rejected")
	}

	// Test via method
	_, err := d.linuxKey(context.Background(), "rm;evil")
	if err == nil {
		t.Error("expected error for invalid key combo")
	}
}

func TestLinuxWindowFocusValidation(t *testing.T) {
	d := &DirectAPI{platform: "linux"}

	_, err := d.linuxFocus(context.Background(), "'; rm -rf /")
	if err == nil {
		t.Error("expected error for invalid window name")
	}

	_, err = d.linuxFocus(context.Background(), "")
	if err == nil {
		t.Error("expected error for empty window name")
	}
}

func TestLinuxAvailability(t *testing.T) {
	d := &DirectAPI{platform: "linux"}
	// Just tests the method doesn't panic — result depends on CI environment
	_ = d.Available()
}
