//! Phase 12 WS#5 — macOS permission onboarding wizard backend.
//!
//! Probes the four TCC classes that Pan-Agent's tools need on macOS:
//!
//! | Permission        | Probe                                              | Block-until-granted? |
//! |-------------------|----------------------------------------------------|----------------------|
//! | Accessibility     | `AXIsProcessTrustedWithOptions(NULL)`              | yes                  |
//! | Screen Recording  | `CGPreflightScreenCaptureAccess()`                 | yes                  |
//! | Automation        | `osascript` no-op + `-1743` error matching         | optional             |
//! | Full Disk Access  | `fs::metadata("~/Library/Safari/Bookmarks.plist")` | lazy (heuristic)     |
//!
//! Public TCC APIs only — no `TCC.db` queries. Per Phase 12 design
//! decision I5, the SIP-protected database has been unreliable since
//! macOS 14 even with FDA, so this module avoids it entirely.
//!
//! All FFI is gated behind `#[cfg(target_os = "macos")]`; on every
//! other platform the commands return `Granted` so the wizard step
//! becomes a no-op (the React side hides itself anyway).

use serde::Serialize;

// `Denied`, `NotDetermined`, and `Unknown` are only constructed by the
// macOS-gated `imp` module (and serialized over IPC into the React
// PermissionsReport DTO). On Linux/Windows clippy's dead-code lint can't
// see those construction sites — gate the warning rather than splitting
// the enum, since that would force every consumer to also cfg-branch.
#[allow(dead_code)]
#[derive(Debug, Clone, Copy, Serialize, PartialEq, Eq)]
#[serde(rename_all = "snake_case")]
pub enum PermStatus {
    Granted,
    Denied,
    NotDetermined,
    Unknown,
}

#[derive(Debug, Clone, Serialize)]
pub struct PermissionsReport {
    pub accessibility: PermStatus,
    pub screen_recording: PermStatus,
    pub automation: PermStatus,
    pub full_disk: PermStatus,
    /// True on platforms where TCC doesn't apply (Linux, Windows). The
    /// React side reads this and skips the wizard step.
    pub platform_supported: bool,
    /// True when the host appears to be MDM-managed (per `profiles -P`
    /// output containing at least one configuration profile). Phase 12
    /// design decision D10 declares MDM-managed macOS officially
    /// unsupported — the wizard surfaces a banner and gates Finish on
    /// an explicit "proceed at your own risk" checkbox.
    pub mdm_managed: bool,
}

// ---------------------------------------------------------------------------
// macOS implementation
// ---------------------------------------------------------------------------

#[cfg(target_os = "macos")]
mod imp {
    use super::*;
    use core_foundation::base::TCFType;
    use core_foundation::dictionary::CFDictionary;
    use std::process::Command;

    extern "C" {
        // Accessibility
        // The non-prompting variant: pass NULL for options. The prompting
        // variant exists too (kAXTrustedCheckOptionPrompt -> true) but
        // we run it from the React Grant button, not from the silent probe.
        fn AXIsProcessTrusted() -> bool;

        // Screen Recording (macOS 10.15+).
        fn CGPreflightScreenCaptureAccess() -> bool;
        fn CGRequestScreenCaptureAccess() -> bool;
    }

    pub fn probe() -> PermissionsReport {
        let accessibility = probe_accessibility();
        let screen_recording = probe_screen_recording();
        let automation = probe_automation();
        let full_disk = probe_full_disk();
        let mdm_managed = probe_mdm();
        PermissionsReport {
            accessibility,
            screen_recording,
            automation,
            full_disk,
            platform_supported: true,
            mdm_managed,
        }
    }

    /// Best-effort MDM detection. `profiles -P` lists installed
    /// configuration profiles; an MDM-managed host has at least one.
    /// On unmanaged hosts the command prints "There are no configuration
    /// profiles installed". Failures (binary missing, permission denied)
    /// are treated as unmanaged — the banner is conservative-by-default.
    fn probe_mdm() -> bool {
        let out = Command::new("/usr/bin/profiles").arg("-P").output();
        match out {
            Ok(o) => {
                let combined = format!(
                    "{}{}",
                    String::from_utf8_lossy(&o.stdout),
                    String::from_utf8_lossy(&o.stderr),
                );
                !combined.contains("no configuration profiles") && !combined.is_empty()
            }
            Err(_) => false,
        }
    }

    fn probe_accessibility() -> PermStatus {
        if unsafe { AXIsProcessTrusted() } {
            PermStatus::Granted
        } else {
            // We can't reliably distinguish NotDetermined from Denied
            // through the AX API alone — both leave the process untrusted.
            // The React wizard treats both the same (CTA is always
            // "Open Settings → Accessibility").
            PermStatus::NotDetermined
        }
    }

    fn probe_screen_recording() -> PermStatus {
        if unsafe { CGPreflightScreenCaptureAccess() } {
            PermStatus::Granted
        } else {
            PermStatus::NotDetermined
        }
    }

    /// Probe Automation by running an `osascript` no-op against Finder.
    /// Per WS#5 decision Q2, Finder is the canonical probe target —
    /// scripting it is intuitive consent ("pan-agent wants to control
    /// Finder") and the runtime cookbook of WS#3 will mostly target
    /// per-app scripting permissions granted on first-use anyway.
    fn probe_automation() -> PermStatus {
        let out = Command::new("/usr/bin/osascript")
            .arg("-e")
            .arg(r#"tell application "Finder" to return name of (path to home folder)"#)
            .output();
        match out {
            Ok(o) if o.status.success() => PermStatus::Granted,
            Ok(o) => {
                let stderr = String::from_utf8_lossy(&o.stderr);
                // -1743 = "Not authorized to send Apple events to Finder"
                if stderr.contains("-1743") {
                    PermStatus::Denied
                } else {
                    PermStatus::NotDetermined
                }
            }
            Err(_) => PermStatus::Unknown,
        }
    }

    /// Best-effort FDA probe. Reading Safari's bookmarks plist requires
    /// Full Disk Access on macOS 10.14+. EPERM / "Operation not permitted"
    /// → Denied; success → Granted; ENOENT (rare — user has never opened
    /// Safari) → NotDetermined. Per WS#5 decision Q1, we only surface
    /// FDA in the wizard lazily — the action journal triggers it when
    /// it actually needs it.
    fn probe_full_disk() -> PermStatus {
        let home = match std::env::var_os("HOME") {
            Some(h) => h,
            None => return PermStatus::Unknown,
        };
        let path = std::path::Path::new(&home).join("Library/Safari/Bookmarks.plist");
        match std::fs::metadata(&path) {
            Ok(_) => PermStatus::Granted,
            Err(e) => match e.kind() {
                std::io::ErrorKind::NotFound => PermStatus::NotDetermined,
                std::io::ErrorKind::PermissionDenied => PermStatus::Denied,
                _ => PermStatus::Unknown,
            },
        }
    }

    pub fn request_screen_recording() {
        // CGRequestScreenCaptureAccess triggers the system prompt the
        // first time it's called per process. On subsequent calls (or
        // when the user already responded) it's a cheap no-op returning
        // the current grant.
        let _ = unsafe { CGRequestScreenCaptureAccess() };
    }

    pub fn open_settings_pane(pane: &str) -> Result<(), String> {
        // Map our short names to the macOS Settings deep-link scheme.
        let url = match pane {
            "Accessibility" => {
                "x-apple.systempreferences:com.apple.preference.security?Privacy_Accessibility"
            }
            "ScreenCapture" => {
                "x-apple.systempreferences:com.apple.preference.security?Privacy_ScreenCapture"
            }
            "Automation" => {
                "x-apple.systempreferences:com.apple.preference.security?Privacy_Automation"
            }
            "AllFiles" | "FullDiskAccess" => {
                "x-apple.systempreferences:com.apple.preference.security?Privacy_AllFiles"
            }
            _ => return Err(format!("unknown settings pane: {pane}")),
        };
        Command::new("/usr/bin/open")
            .arg(url)
            .status()
            .map_err(|e| e.to_string())?;
        Ok(())
    }

    /// Suppress unused-import warnings for transitive CFDictionary/TCFType
    /// imports that may be useful when we revisit the prompting AX
    /// variant. Cheap to leave in; saves a follow-up Cargo.toml churn.
    #[allow(dead_code)]
    fn _suppress_unused() {
        let _ = std::mem::size_of::<CFDictionary<i32, i32>>();
        let _ = <core_foundation::string::CFString as TCFType>::type_id();
    }
}

// ---------------------------------------------------------------------------
// Non-macOS stub
// ---------------------------------------------------------------------------

#[cfg(not(target_os = "macos"))]
mod imp {
    use super::*;

    pub fn probe() -> PermissionsReport {
        PermissionsReport {
            accessibility: PermStatus::Granted,
            screen_recording: PermStatus::Granted,
            automation: PermStatus::Granted,
            full_disk: PermStatus::Granted,
            platform_supported: false,
            mdm_managed: false,
        }
    }

    pub fn request_screen_recording() {}

    pub fn open_settings_pane(_pane: &str) -> Result<(), String> {
        Err("settings panes are macOS-only".into())
    }
}

// ---------------------------------------------------------------------------
// Tauri command surface
// ---------------------------------------------------------------------------

/// Returns the current TCC grant state for every permission Pan-Agent
/// cares about. The React wizard polls this every second while the
/// Setup step is open; cheap to call repeatedly (each probe is an FFI
/// or a bounded `osascript` exec).
#[tauri::command]
pub fn permissions_probe() -> PermissionsReport {
    imp::probe()
}

/// Triggers the system Screen Recording prompt on macOS. Idempotent:
/// once the user has granted (or permanently denied) the request, this
/// becomes a cheap no-op. Returns immediately — the React side polls
/// `permissions_probe` to detect the state change.
#[tauri::command]
pub fn permissions_request_screen_recording() {
    imp::request_screen_recording();
}

/// Opens the matching pane in System Settings. `pane` is one of:
/// `Accessibility | ScreenCapture | Automation | AllFiles`.
#[tauri::command]
pub fn permissions_open_settings(pane: String) -> Result<(), String> {
    imp::open_settings_pane(&pane)
}
