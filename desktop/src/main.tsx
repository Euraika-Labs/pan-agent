import React from "react";
import ReactDOM from "react-dom/client";
import App from "./App";
import "./index.css";

// ─── M4 W2 Commit D — CSP violation collector ──────────────────────────────
//
// Tauri v2 has no cspReportOnly mode, so we ship enforcing CSP with this
// local observability collector. Each securitypolicyviolation POSTs to
// pan-agent's gateway endpoint POST /v1/office/csp-report (Commit C), which
// appends to %LOCALAPPDATA%/pan-agent/csp-violations.log with a 10 MB hard
// cap. Bert's 5-day dogfood reads this log before tagging 0.4.0.
//
// Design decisions (post Gate-1 + Gate-2):
//   - URL is HARDCODED to VITE_API_BASE, not derived from
//     window.location.origin. In a bundled Tauri app the origin is
//     tauri://localhost which would route nowhere (Gate-2 critical fix #3).
//     This means engine=node users on a non-default gateway port won't
//     deliver reports — acceptable trade-off for the default case.
//   - Single-flight drain with `flushing` bool (Gate-2 refinement #2)
//     prevents burst violations from spawning N concurrent failing POSTs.
//   - Dedupe via Map<key,timestamp> for 60s window per (directive, URI).
//   - MUST run BEFORE ReactDOM.createRoot so early-mount violations land.
{
  const API_BASE = import.meta.env.VITE_API_BASE ?? "http://localhost:8642";
  const CSP_REPORT_URL = `${API_BASE}/v1/office/csp-report`;
  const CSP_DEDUPE_MS = 60_000;
  const MAX_PENDING = 128;

  const seen = new Map<string, number>();
  const pending: string[] = [];
  let flushing = false;

  async function flushCSPQueue(): Promise<void> {
    if (flushing) return;
    flushing = true;
    try {
      while (pending.length > 0) {
        const body = pending[0];
        try {
          const res = await fetch(CSP_REPORT_URL, {
            method: "POST",
            headers: { "Content-Type": "application/json" },
            body,
            keepalive: true,
          });
          if (!res.ok) break;
          pending.shift();
        } catch {
          break;
        }
      }
    } finally {
      flushing = false;
    }
  }

  document.addEventListener("securitypolicyviolation", (ev: Event) => {
    const e = ev as SecurityPolicyViolationEvent;
    const key = `${e.violatedDirective}|${e.blockedURI}`;
    const now = Date.now();
    const last = seen.get(key);
    if (last !== undefined && now - last < CSP_DEDUPE_MS) return;
    seen.set(key, now);

    const payload = JSON.stringify({
      ts: new Date().toISOString(),
      violatedDirective: e.violatedDirective,
      effectiveDirective: e.effectiveDirective,
      blockedURI: e.blockedURI,
      sourceFile: e.sourceFile,
      lineNumber: e.lineNumber,
      columnNumber: e.columnNumber,
      documentURI: e.documentURI,
      sample: e.sample,
    });

    if (pending.length < MAX_PENDING) pending.push(payload);
    void flushCSPQueue();
  });
}

// Apply data-theme to <html> BEFORE React renders. Without this, the
// first paint happens on an unstyled document: every rule in main.css
// is scoped to [data-theme="dark"] / [data-theme="light"], so missing
// the attribute means no CSS variable resolves and the whole UI
// renders with browser defaults (the "no CSS loaded" symptom).
// useTheme in Settings.tsx only runs when that screen mounts, which
// is far too late — most users never open Settings first.
(function applyInitialTheme() {
  const stored = localStorage.getItem("pan-theme") as
    | "system"
    | "light"
    | "dark"
    | null;
  const mode = stored ?? "system";
  const isDark =
    mode === "dark" ||
    (mode === "system" &&
      typeof window !== "undefined" &&
      window.matchMedia("(prefers-color-scheme: dark)").matches);
  document.documentElement.setAttribute("data-theme", isDark ? "dark" : "light");
  document.documentElement.classList.toggle("dark", isDark);
})();

// ─── M5-C3 — WebView2 fallback probe ───────────────────────────────────────
//
// Tauri v2 uses the host WebView2 (Windows), WKWebView (macOS), or
// WebKitGTK (Linux). On Windows in particular, WebView2 without GPU
// acceleration has no usable WebGL2 context, and Claw3D's 3D scene falls
// flat. We detect this before React mounts, POST to the gateway to record
// a 7-day skip window, and open the system browser via Tauri's shell
// plugin — the user sees the full experience there instead of a broken
// iframe here.
//
// Design constraints (from M5 Phase 1):
//   - URL hardcoded to VITE_API_BASE, same reason as CSP listener above.
//   - Probe skipped when office.browser_fallback_until is in the future;
//     prevents daily re-prompt after a user accepts the fallback once.
//   - Failure paths ALL fall through to React mount. Never block boot
//     because of a probe error; a broken probe should never hide the UI.
//   - Detection = typeof guard + canvas.getContext check. Both needed:
//     the first catches WebView2 without the WebGL2 API, the second
//     catches API-present-but-driver-broken (VMs, headless, GPU blocked).
async function probeWebGL2AndFallback(): Promise<void> {
  const API_BASE = import.meta.env.VITE_API_BASE ?? "http://localhost:8642";

  // Skip if user is still inside an active 7-day fallback window.
  try {
    const cfgRes = await fetch(`${API_BASE}/v1/config`);
    if (cfgRes.ok) {
      const cfg = (await cfgRes.json()) as {
        office?: { browser_fallback_until?: string };
      };
      const until = cfg.office?.browser_fallback_until;
      if (until) {
        const untilDate = new Date(until);
        if (!isNaN(untilDate.getTime()) && untilDate > new Date()) {
          return; // still in skip window
        }
      }
    }
  } catch {
    // Config fetch failed — proceed with probe. Missing config is
    // strictly safer than blocking boot.
  }

  if (webgl2Works()) return;

  // Probe failed. Record the fallback and trigger the splash + open.
  try {
    const res = await fetch(`${API_BASE}/v1/office/fallback-detected`, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
    });
    if (!res.ok) throw new Error(`fallback-detected HTTP ${res.status}`);
    const data = (await res.json()) as { fallbackUrl: string; untilDate: string };

    showFallbackSplash(data.untilDate);
    // Dynamic import so the shell plugin isn't pulled into the hot path
    // unless we actually need it. Open in system browser; if the user
    // closes the system browser, pan-agent stays running with the
    // FallbackBanner visible in the Office tab for retry.
    const { open } = await import("@tauri-apps/plugin-shell");
    await open(data.fallbackUrl);
  } catch (err) {
    console.error("[M5-C3] fallback trigger failed:", err);
    // Fall through to React mount anyway — broken probe must not
    // hide the UI.
  }
}

function webgl2Works(): boolean {
  if (typeof WebGL2RenderingContext === "undefined") return false;
  try {
    const canvas = document.createElement("canvas");
    return canvas.getContext("webgl2") !== null;
  } catch {
    return false;
  }
}

function showFallbackSplash(untilDate: string): void {
  const date = new Date(untilDate).toLocaleDateString();
  // Inline styles so the splash renders even if CSS bundles are still
  // loading. Uses color values from main.css :root but without the
  // var() indirection since the splash pre-dates theme attribute apply.
  document.body.innerHTML = `
    <div style="
      display:flex; flex-direction:column; align-items:center;
      justify-content:center; height:100vh; gap:1rem;
      background:#171717; color:#ececec;
      font-family:ui-sans-serif,system-ui,sans-serif;
      padding:2rem; text-align:center;
    ">
      <h2 style="margin:0;font-size:1.25rem;">3D Rendering Unavailable</h2>
      <p style="margin:0;max-width:36rem;line-height:1.5;color:#b4b4b4;">
        Your system does not support WebGL2. Opening Claw3D in your default browser…
      </p>
      <p style="margin:0;font-size:0.8rem;color:#8e8e8e;">
        This message will not show again until ${date}.
      </p>
    </div>`;
}

// Fire and forget — the promise is intentionally un-awaited. React
// mount must not wait for the probe; a slow probe on a healthy machine
// should never add visible latency to startup. The probe short-circuits
// to return on the happy path.
void probeWebGL2AndFallback();

ReactDOM.createRoot(document.getElementById("root") as HTMLElement).render(
  <React.StrictMode>
    <App />
  </React.StrictMode>,
);
