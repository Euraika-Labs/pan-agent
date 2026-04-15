import { describe, expect, it, vi, type Mock } from "vitest";
import {
  handleOfficeKey,
  type MinimalKeyEvent,
  type OfficeShortcutActions,
} from "./layout.shortcuts";

// Typed spy helper: keep the interface shape `() => void` at the
// call-site so `handleOfficeKey` accepts them, and expose the `Mock`
// surface so tests can assert call counts. Splitting the types lets
// strict mode narrow correctly under vitest v4's mock signature.
interface SpiedActions extends OfficeShortcutActions {
  reloadIframe: (() => void) & Mock;
  toggleDebugPanel: (() => void) & Mock;
}

// Unit tests for the pure shortcut dispatcher. The Playwright suite
// (desktop/tests/e2e/office.spec.ts) covers the DOM toolbar button path;
// these tests exercise the keyboard router in isolation so regressions
// in the routing logic surface before Playwright's slower matrix runs.
//
// The 5 test cases match the Gate-1 refinement #6 contract: both
// shortcuts, no-Shift (ignore), iframe focus (ignore), unrelated key.

describe("handleOfficeKey", () => {
  function freshActions(): SpiedActions {
    // vi.fn<() => void>() narrows the mock's callable signature to
    // match OfficeShortcutActions. Without the explicit generic, vitest
    // v4 infers the widest-possible signature and refuses to unify with
    // the interface under strict mode.
    return {
      reloadIframe: vi.fn<() => void>() as SpiedActions["reloadIframe"],
      toggleDebugPanel: vi.fn<() => void>() as SpiedActions["toggleDebugPanel"],
    };
  }

  function ev(overrides: Partial<MinimalKeyEvent>): MinimalKeyEvent {
    return {
      ctrlKey: false,
      metaKey: false,
      shiftKey: false,
      key: "",
      ...overrides,
    };
  }

  it("Ctrl+Shift+R reloads the iframe", () => {
    const a = freshActions();
    const handled = handleOfficeKey(
      ev({ ctrlKey: true, shiftKey: true, key: "R" }),
      a,
    );
    expect(handled).toBe(true);
    expect(a.reloadIframe).toHaveBeenCalledOnce();
    expect(a.toggleDebugPanel).not.toHaveBeenCalled();
  });

  it("Cmd+Shift+D toggles the debug panel (lowercase)", () => {
    const a = freshActions();
    const handled = handleOfficeKey(
      ev({ metaKey: true, shiftKey: true, key: "d" }),
      a,
    );
    expect(handled).toBe(true);
    expect(a.toggleDebugPanel).toHaveBeenCalledOnce();
    expect(a.reloadIframe).not.toHaveBeenCalled();
  });

  it("Ctrl+R without Shift is ignored (we use Shift to avoid browser reload)", () => {
    const a = freshActions();
    const handled = handleOfficeKey(ev({ ctrlKey: true, key: "r" }), a);
    expect(handled).toBe(false);
    expect(a.reloadIframe).not.toHaveBeenCalled();
  });

  it("ignores the shortcut when an iframe has focus", () => {
    const a = freshActions();
    const handled = handleOfficeKey(
      ev({
        ctrlKey: true,
        shiftKey: true,
        key: "r",
        targetTag: "IFRAME",
      }),
      a,
    );
    expect(handled).toBe(false);
    expect(a.reloadIframe).not.toHaveBeenCalled();
  });

  it("ignores unrelated keys even with the modifier combo", () => {
    const a = freshActions();
    const handled = handleOfficeKey(
      ev({ ctrlKey: true, shiftKey: true, key: "t" }),
      a,
    );
    expect(handled).toBe(false);
    expect(a.reloadIframe).not.toHaveBeenCalled();
    expect(a.toggleDebugPanel).not.toHaveBeenCalled();
  });

  it("undefined targetTag (no focus) still dispatches — intended", () => {
    const a = freshActions();
    const handled = handleOfficeKey(
      ev({ ctrlKey: true, shiftKey: true, key: "r", targetTag: undefined }),
      a,
    );
    expect(handled).toBe(true);
    expect(a.reloadIframe).toHaveBeenCalledOnce();
  });
});
