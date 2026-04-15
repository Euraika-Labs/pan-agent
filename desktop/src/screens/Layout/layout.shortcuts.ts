// M4 W2 Commit D — keyboard shortcut router for the Office screen.
//
// Extracted as a pure function so the dispatch logic is unit-testable
// without mounting a DOM or a Tauri webview (Gate-1 refinement #6). The
// Playwright suite tests the toolbar button path; this module covers the
// keyboard path via vitest in layout.shortcuts.test.ts.
//
// Design note: the function takes a minimal KeyboardEvent shape, NOT
// the full DOM event, so tests don't need jsdom. Callers in Office.tsx
// adapt their DOM event by pulling out ctrlKey/metaKey/shiftKey/key and
// the current `document.activeElement?.tagName`.

export interface OfficeShortcutActions {
  reloadIframe(): void;
  toggleDebugPanel(): void;
}

export interface MinimalKeyEvent {
  ctrlKey: boolean;
  metaKey: boolean;
  shiftKey: boolean;
  /** The logical key, e.g. "R", "r", "d". Comparison is case-insensitive. */
  key: string;
  /**
   * The tag name of the currently focused element at the time of the
   * keypress, uppercased by the DOM spec (e.g. "IFRAME", "INPUT").
   * When undefined (no focused element), the handler treats the event
   * as "not in an iframe" and proceeds with shortcut dispatch. This is
   * intentional — if the user has no focus, the shortcut should fire.
   */
  targetTag?: string;
}

/**
 * handleOfficeKey routes Office-scoped keyboard shortcuts.
 *
 * Returns true if the event matched a shortcut and the action was
 * dispatched (the caller should preventDefault). Returns false if the
 * event should propagate normally.
 *
 * Rules:
 *   - Ctrl+Shift+R (or Cmd+Shift+R) → reloadIframe
 *   - Ctrl+Shift+D (or Cmd+Shift+D) → toggleDebugPanel
 *   - targetTag === "IFRAME" → ignore (iframe owns the keyboard)
 *   - any other key combination → ignore
 *
 * The Ctrl+R (browser reload) bare shortcut is intentionally NOT handled
 * here — we use Ctrl+SHIFT+R to avoid clobbering the user's muscle memory
 * for full-page reload of the pan-agent webview itself.
 */
export function handleOfficeKey(
  e: MinimalKeyEvent,
  actions: OfficeShortcutActions,
): boolean {
  if (e.targetTag === "IFRAME") return false;
  if (!(e.ctrlKey || e.metaKey) || !e.shiftKey) return false;

  const key = e.key.toLowerCase();
  if (key === "r") {
    actions.reloadIframe();
    return true;
  }
  if (key === "d") {
    actions.toggleDebugPanel();
    return true;
  }
  return false;
}
