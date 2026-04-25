import { describe, expect, it } from "vitest";
import { canFinishWizard, ctaLabel, REQUIRED_PERMS } from "./permissionGate";
import type { PermissionsReport } from "./tauri";

// Pure-helper tests for the WS#5 wizard gate logic. No jsdom; matches
// the existing desktop test convention (see budget-helpers.test.ts).

function report(overrides: Partial<PermissionsReport> = {}): PermissionsReport {
  return {
    accessibility: "granted",
    screen_recording: "granted",
    automation: "granted",
    full_disk: "granted",
    platform_supported: true,
    ...overrides,
  };
}

describe("canFinishWizard", () => {
  it("opens the gate when every required perm is granted", () => {
    expect(canFinishWizard(report())).toBe(true);
  });

  it("blocks when accessibility is not granted", () => {
    expect(canFinishWizard(report({ accessibility: "denied" }))).toBe(false);
    expect(canFinishWizard(report({ accessibility: "not_determined" }))).toBe(
      false,
    );
    expect(canFinishWizard(report({ accessibility: "unknown" }))).toBe(false);
  });

  it("blocks when screen recording is not granted", () => {
    expect(canFinishWizard(report({ screen_recording: "denied" }))).toBe(false);
  });

  it("ignores automation status (optional / contextual)", () => {
    expect(canFinishWizard(report({ automation: "denied" }))).toBe(true);
  });

  it("ignores full_disk status (lazy — requested per-action by the journal)", () => {
    expect(canFinishWizard(report({ full_disk: "denied" }))).toBe(true);
  });

  it("always opens the gate on platforms without TCC", () => {
    // Even with denied core perms, non-supported platforms (Win, Linux)
    // see the wizard skipped entirely.
    expect(
      canFinishWizard(
        report({
          accessibility: "denied",
          screen_recording: "denied",
          platform_supported: false,
        }),
      ),
    ).toBe(true);
  });

  it("REQUIRED_PERMS only includes the two block-until-granted rows", () => {
    expect([...REQUIRED_PERMS].sort()).toEqual([
      "accessibility",
      "screen_recording",
    ]);
  });
});

describe("ctaLabel", () => {
  it("granted → static 'Granted'", () => {
    expect(ctaLabel("granted", true)).toBe("Granted");
    expect(ctaLabel("granted", false)).toBe("Granted");
  });

  it("denied flips to the Raycast 'previously denied' label", () => {
    expect(ctaLabel("denied", true)).toBe("Open Settings (previously denied)");
    expect(ctaLabel("denied", false)).toBe(
      "Open Settings (previously denied)",
    );
  });

  it("not_determined uses Grant Access when a programmatic prompt exists", () => {
    expect(ctaLabel("not_determined", true)).toBe("Grant Access");
  });

  it("not_determined falls back to Open Settings when no programmatic prompt exists", () => {
    expect(ctaLabel("not_determined", false)).toBe("Open Settings");
  });

  it("unknown is treated like not_determined", () => {
    expect(ctaLabel("unknown", true)).toBe("Grant Access");
    expect(ctaLabel("unknown", false)).toBe("Open Settings");
  });
});
