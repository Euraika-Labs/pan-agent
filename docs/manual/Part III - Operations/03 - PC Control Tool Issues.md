# PC Control Tool Issues

This runbook covers problems with the screenshot, keyboard, mouse, window_manager, and OCR tools.

## Symptom: Tool returns "not supported on <os>"

The agent tries to use keyboard/mouse/window_manager and the response is `not supported on freebsd` (or another OS).

### Cause

The tool's stub implementation is being used because no platform-specific implementation matches your OS.

### Fix

Pan-Agent supports Windows, macOS, and Linux. On other Unix-likes, the stubs return errors. There's no fix beyond porting the implementation — see [[03 - Cross-Platform Tool Architecture]] for the pattern.

## Symptom: Linux PC control tool returns connection error

Error like `xgb.NewConn: Could not get DISPLAY` or `XTest extension not available`.

### Causes

**Cause 1: No X11 display.**

```bash
echo $DISPLAY
```

If empty, you're on a headless machine or Wayland without XWayland. The X11 tools cannot work.

**Cause 2: Wayland without XWayland.**

```bash
echo $XDG_SESSION_TYPE
```

If `wayland`, check whether XWayland is running. On GNOME and KDE, XWayland is on by default. On Sway and other wlroots-based compositors, you may need to install/start it explicitly.

XWayland-on-Wayland makes Pan-Agent's X11 tools work for X11 apps. Native Wayland apps cannot be controlled by these tools — Wayland deliberately blocks input injection and window enumeration for security.

**Cause 3: XTest extension disabled.**

Most distros enable XTest by default. If your X server is configured to disable extensions:

```bash
xdpyinfo | grep XTEST
```

Should show `XTEST` if available. If not, re-enable it in your X11 configuration.

## Symptom: macOS keyboard/mouse tool runs but nothing happens

CGo calls succeed but the target app doesn't see the events.

### Cause

macOS requires Pan Desktop to have Accessibility permission to inject keyboard/mouse events into other apps.

### Fix

1. System Preferences → Privacy & Security → Accessibility.
2. Click + and add "Pan Desktop" from /Applications.
3. Enable the toggle.
4. Restart Pan Desktop.

The first time you run the keyboard/mouse tool, macOS may prompt you to grant the permission directly. If you deny, no further prompt appears — you have to add it manually.

## Symptom: macOS window manager can't move windows

Listing windows works but `wmFocusWindow` / `wmMoveWindow` / `wmCloseWindow` errors.

### Cause

Listing uses `CGWindowListCopyWindowInfo` (no permission needed). Manipulation uses AppleScript via `osascript`, which requires Accessibility permission AND Automation permission.

### Fix

System Preferences → Privacy & Security → Accessibility AND Automation. Add Pan Desktop in both.

## Symptom: Screenshot returns empty / black image

The tool succeeds but the resulting image is blank.

### Causes

**Cause 1: macOS Screen Recording permission.**

System Preferences → Privacy & Security → Screen Recording → enable for Pan Desktop. Restart the app.

This is a separate permission from Accessibility. Without it, `kbinani/screenshot` returns valid-looking images that are completely black.

**Cause 2: Linux dual-monitor edge case.**

`kbinani/screenshot` enumerates displays via Xinerama. On rare multi-monitor setups (especially mixed DPI), the bounds calculation can return zero-width regions.

Try capturing a specific display:

```go
n := screenshot.NumActiveDisplays()
for i := 0; i < n; i++ {
    bounds := screenshot.GetDisplayBounds(i)
    img, err := screenshot.CaptureRect(bounds)
    // ...
}
```

The screenshot tool currently captures display 0 only. For multi-monitor support, file an issue.

## Symptom: Window manager can't find a target window

`wmFindWindow("MyApp")` returns "no window found matching MyApp" but the window is clearly on screen.

### Causes

**Cause 1: Substring match is case-insensitive but title is dynamic.**

Some apps include the document name in the title (e.g., "Untitled Document - LibreOffice"). Search for a stable substring like "LibreOffice".

**Cause 2: Linux: window has no `_NET_WM_NAME`.**

The Linux implementation tries `_NET_WM_NAME` (UTF-8) first, then falls back to `WM_NAME` (Latin-1). Some legacy X11 apps set neither — those windows are invisible to the tool.

**Cause 3: macOS: window belongs to a hidden app.**

`CGWindowListCopyWindowInfo` with `kCGWindowListOptionOnScreenOnly` excludes minimized/hidden windows. There is currently no way to find them via this tool.

## Symptom: Keyboard tool types wrong characters on AZERTY/Dvorak

You request typing "abc" and get "azé" (or similar).

### Cause

This was a bug in early Linux implementations using static keysym→keycode tables. The current implementation uses runtime `xproto.GetKeyboardMapping` which respects the active layout.

If you still see this on the latest version, your Linux distro may not be exposing the layout correctly to xgb. Try setting:

```bash
setxkbmap us
# or
setxkbmap fr  # if you actually want AZERTY
```

then restart Pan-Agent.

## Symptom: OCR returns garbage or empty text

The tool succeeds but the text is wrong or missing.

### Cause

OCR uses a vision LLM (not a local OCR engine like Tesseract). The quality depends on:
1. The screenshot resolution and clarity.
2. The vision model in use.
3. The prompt the OCR tool sends.

### Fix

Make sure your active LLM provider supports vision. Models known to handle OCR well: `gpt-4o`, `claude-3.5-sonnet`, `gemini-1.5-pro`, multimodal models on OpenRouter.

If you're using a text-only model (e.g., `gpt-4o-mini` or many local LLMs), vision capabilities won't work.

## Read next
- [[03 - Cross-Platform Tool Architecture]]
- [[02 - Tools Catalog]]
