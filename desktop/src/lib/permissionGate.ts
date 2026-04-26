/**
 * Pure helpers for the WS#5 macOS permission wizard.
 *
 * The "block-until-granted" gate logic lives here so it can be unit-
 * tested without spinning up the React tree (matches the existing
 * desktop test style — pure helpers, no jsdom).
 */
import type { PermissionsReport, PermStatus } from "./tauri";

/**
 * Permissions Pan-Agent treats as required to ship the desktop bundle.
 * Per WS#5 design (`docs/design/phase12.md` line 220+):
 *   - Accessibility + Screen Recording are block-until-granted core.
 *   - Automation is contextual (per-app prompt on first use).
 *   - Full Disk Access is lazy (only requested when journal hits EPERM).
 */
export const REQUIRED_PERMS = ["accessibility", "screen_recording"] as const;
export type RequiredPerm = (typeof REQUIRED_PERMS)[number];

/**
 * True when the user can advance past the wizard step. On non-macOS
 * platforms (`platform_supported === false`) the gate is always open.
 *
 * On MDM-managed macOS hosts (officially unsupported per design D10)
 * the gate also requires `mdmAcknowledged` — the React wizard surfaces
 * a "proceed at your own risk" checkbox. This is checked AFTER the
 * normal required-perms gate, so an MDM user who hasn't granted the
 * core permissions still sees the missing permissions, not just the
 * MDM blocker.
 */
export function canFinishWizard(
  report: PermissionsReport,
  mdmAcknowledged: boolean = false,
): boolean {
  if (!report.platform_supported) return true;
  const permsGranted = REQUIRED_PERMS.every(
    (perm) => report[perm] === "granted",
  );
  if (!permsGranted) return false;
  if (report.mdm_managed && !mdmAcknowledged) return false;
  return true;
}

/**
 * Per-row CTA label. Per WS#5 plan, on Denied the label flips to
 * "Open Settings (previously denied)" — Raycast pattern. On
 * NotDetermined / Unknown the label is "Grant Access" (or "Open
 * Settings" for permissions Apple doesn't expose a programmatic prompt
 * for).
 */
export function ctaLabel(
  status: PermStatus,
  hasProgrammaticPrompt: boolean,
): string {
  if (status === "granted") return "Granted";
  if (status === "denied") return "Open Settings (previously denied)";
  if (hasProgrammaticPrompt) return "Grant Access";
  return "Open Settings";
}

/**
 * Lookup table mapping each permission to whether macOS exposes a
 * programmatic prompt API for it. AX + Automation + FDA all require
 * the user to flip a toggle in System Settings; only Screen Recording
 * has a CGRequestScreenCaptureAccess() that pops the system prompt.
 *
 * `platform_supported` and `mdm_managed` are flag fields on the DTO,
 * not actual permissions, so they map to false (callers don't
 * iterate on them as if they were permission rows).
 */
export const HAS_PROGRAMMATIC_PROMPT: Record<keyof PermissionsReport, boolean> =
  {
    accessibility: false,
    screen_recording: true,
    automation: false,
    full_disk: false,
    platform_supported: false,
    mdm_managed: false,
  };
