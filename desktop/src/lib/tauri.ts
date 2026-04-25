/**
 * Typed bridge for Pan-Agent's Tauri `#[command]` invocations.
 *
 * Tauri's `invoke()` throws when it can't find the IPC channel — that
 * happens whenever the desktop bundle is opened in plain Vite
 * (`npm run dev:vite`) or a regular browser. The wrappers below detect
 * the absence of the IPC and return a sensible "stub" response so the
 * UI doesn't fall over during Vite-only development of the wizard.
 *
 * On the real Tauri shell, calls are forwarded straight through.
 */
import { invoke } from "@tauri-apps/api/core";

export type PermStatus = "granted" | "denied" | "not_determined" | "unknown";

export interface PermissionsReport {
  accessibility: PermStatus;
  screen_recording: PermStatus;
  automation: PermStatus;
  full_disk: PermStatus;
  /** False on platforms without TCC (Linux, Windows). */
  platform_supported: boolean;
}

export type SettingsPane =
  | "Accessibility"
  | "ScreenCapture"
  | "Automation"
  | "AllFiles"
  | "FullDiskAccess";

/**
 * Detects whether the page is running inside the Tauri shell. Tauri
 * injects `__TAURI_INTERNALS__` on the window before the React tree
 * mounts, so the check is sync-safe at component-render time.
 */
export function isTauri(): boolean {
  return (
    typeof window !== "undefined" &&
    "__TAURI_INTERNALS__" in window
  );
}

/**
 * Probe the four TCC classes. Returns a stub "all granted, platform
 * not supported" report when called outside the Tauri shell so the
 * wizard step transparently skips itself in plain Vite dev.
 */
export async function probePermissions(): Promise<PermissionsReport> {
  if (!isTauri()) {
    return {
      accessibility: "granted",
      screen_recording: "granted",
      automation: "granted",
      full_disk: "granted",
      platform_supported: false,
    };
  }
  return invoke<PermissionsReport>("permissions_probe");
}

/**
 * Trigger the system Screen Recording prompt. No-op in non-Tauri envs.
 */
export async function requestScreenRecording(): Promise<void> {
  if (!isTauri()) return;
  await invoke("permissions_request_screen_recording");
}

/**
 * Open the matching pane in System Settings.
 */
export async function openSettingsPane(pane: SettingsPane): Promise<void> {
  if (!isTauri()) return;
  await invoke("permissions_open_settings", { pane });
}
