import { useEffect, useState } from "react";
import { AlertTriangle, ExternalLink, X } from "lucide-react";
import { fetchJSON } from "../api";

// M5-C3 — persistent banner shown when the WebView2 fallback is active.
//
// Flow: main.tsx's WebGL2 probe detects failure, POSTs to
// /v1/office/fallback-detected which writes office.browser_fallback_until
// 7 days into the future. This banner mounts on every launch while that
// window is active, giving users a one-click way to re-open the system
// browser OR to clear the window and re-probe (e.g., after a driver
// update).
//
// Design notes:
//   - Self-gates on /v1/config. If office.browser_fallback_until is
//     absent or past, renders null.
//   - "Open in browser" always works (calls /v1/office/fallback-detected
//     again, which returns the same URL and extends the window).
//   - "Try again" clears the flag via /v1/config PUT and hard-reloads
//     the window so the probe runs fresh.
//   - "Dismiss" hides the banner for this session only — does NOT
//     clear the persistence. The next launch will still show it.
//
// The component is tiny on purpose: persistence lives in config.yaml,
// not in React state. Survives restart by design.

interface ConfigResponse {
  office?: {
    browser_fallback_until?: string;
  };
}

interface FallbackDetectedResponse {
  fallbackUrl: string;
  untilDate: string;
}

export default function FallbackBanner(): React.JSX.Element | null {
  const [active, setActive] = useState<boolean>(false);
  const [dismissed, setDismissed] = useState<boolean>(false);
  const [untilDate, setUntilDate] = useState<string>("");

  useEffect(() => {
    let cancelled = false;
    void (async () => {
      try {
        const cfg = await fetchJSON<ConfigResponse>("/v1/config");
        if (cancelled) return;
        const until = cfg.office?.browser_fallback_until;
        if (!until) return;
        const untilDateObj = new Date(until);
        if (isNaN(untilDateObj.getTime())) return;
        if (untilDateObj <= new Date()) return;
        setUntilDate(untilDateObj.toLocaleDateString());
        setActive(true);
      } catch (err) {
        // Config fetch failed — leave the banner off. The probe in
        // main.tsx would have caught a real WebView2 problem before
        // React mounted anyway.
        console.error("[FallbackBanner] config fetch:", err);
      }
    })();
    return () => {
      cancelled = true;
    };
  }, []);

  if (!active || dismissed) return null;

  async function handleOpenBrowser(): Promise<void> {
    try {
      const res = await fetchJSON<FallbackDetectedResponse>(
        "/v1/office/fallback-detected",
        { method: "POST" },
      );
      const { open } = await import("@tauri-apps/plugin-shell");
      await open(res.fallbackUrl);
    } catch (err) {
      console.error("[FallbackBanner] open in browser failed:", err);
    }
  }

  async function handleTryAgain(): Promise<void> {
    try {
      // Clear the persisted flag so the next launch re-runs the probe.
      // We reload immediately rather than wait for user-triggered
      // restart because the whole point is "retry now, not later".
      await fetchJSON<void>("/v1/config", {
        method: "PUT",
        body: JSON.stringify({ office: { browser_fallback_until: "" } }),
      });
      window.location.reload();
    } catch (err) {
      console.error("[FallbackBanner] try again failed:", err);
    }
  }

  return (
    <div role="alert" className="fallback-banner">
      <div className="fallback-banner-icon">
        <AlertTriangle size={18} />
      </div>
      <div className="fallback-banner-body">
        <strong>GPU acceleration not available.</strong>{" "}
        Pan Desktop's 3D rendering requires WebGL2. You are viewing the
        full feature set in your system browser. This banner will stay
        until {untilDate} or until you click "Try again".
      </div>
      <div className="fallback-banner-actions">
        <button
          type="button"
          className="btn btn-secondary btn-sm"
          onClick={handleOpenBrowser}
          aria-label="Re-open Claw3D in system browser"
        >
          <ExternalLink size={14} />
          &nbsp;Open in browser
        </button>
        <button
          type="button"
          className="btn btn-secondary btn-sm"
          onClick={handleTryAgain}
          aria-label="Try WebGL2 probe again"
        >
          Try again
        </button>
        <button
          type="button"
          className="btn-ghost"
          onClick={() => setDismissed(true)}
          aria-label="Dismiss fallback banner for this session"
        >
          <X size={14} />
        </button>
      </div>
    </div>
  );
}
