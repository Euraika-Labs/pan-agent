import { useEffect, useState } from "react";
import { AlertCircle, X } from "lucide-react";
import {
  getMigrationStatus,
  patchConfig,
  postMigrationRun,
  type MigrationReport,
} from "../api";

/**
 * State machine for the first-launch migration banner:
 *
 *   loading   → status fetch in flight
 *   hidden    → nothing to show (not needed, already acked, or post-close)
 *   visible   → user has three actions: Import / Keep as backup / Dismiss
 *   importing → user clicked Import, migration is running
 *   success   → import returned; summary displayed until explicit close
 *
 * Per Gate-1 refinement #5 + WCAG 2.2.1 there is NO auto-dismiss on
 * success — the migration's ack flag is one-shot, so losing the summary
 * means losing the only evidence of what was imported. Close is always
 * explicit.
 */
type State = "loading" | "hidden" | "visible" | "importing" | "success";

export default function MigrationBanner() {
  const [state, setState] = useState<State>("loading");
  const [report, setReport] = useState<MigrationReport | null>(null);
  const [runError, setRunError] = useState<string | null>(null);
  const [ackError, setAckError] = useState<string | null>(null);

  // Initial status probe. If the backend is unreachable the banner stays
  // hidden — a broken banner is worse than no banner.
  useEffect(() => {
    let cancelled = false;
    getMigrationStatus()
      .then((s) => {
        if (cancelled) return;
        setState(s.needed && !s.acked ? "visible" : "hidden");
      })
      .catch((err: unknown) => {
        console.error("[MigrationBanner] status:", err);
        if (!cancelled) setState("hidden");
      });
    return () => {
      cancelled = true;
    };
  }, []);

  // Escape-key dismiss. Scoped to states where the banner is interactive
  // so we don't leak listeners while it's hidden.
  useEffect(() => {
    if (state === "loading" || state === "hidden") return;
    function onKey(e: KeyboardEvent) {
      if (e.key === "Escape") {
        e.preventDefault();
        void handleClose();
      }
    }
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [state]);

  async function handleImport() {
    setState("importing");
    setRunError(null);
    try {
      const r = await postMigrationRun({});
      setReport(r);
      setState("success"); // persists until explicit close
    } catch (err: unknown) {
      setRunError((err as Error).message);
      setState("visible"); // back to the action row so user can retry
    }
  }

  /**
   * Write the ack flag. On success the banner moves to `hidden`.
   * On failure (Gate-2 refinement #5) the banner stays visible with an
   * inline error explaining the banner will re-appear next launch. The
   * user can retry or intentionally accept that state.
   */
  async function ackAndHide() {
    setAckError(null);
    try {
      await patchConfig({ office: { migration_ack: true } });
      setState("hidden");
    } catch (err: unknown) {
      const msg = (err as Error).message;
      console.error("[MigrationBanner] ack failed:", err);
      setAckError(`Couldn't save dismissal (${msg}). Banner will reappear next launch.`);
      // Stay on the current state so the user still has the action buttons.
    }
  }

  async function handleKeepBackup() {
    await ackAndHide();
  }

  async function handleDismiss() {
    await ackAndHide();
  }

  async function handleClose() {
    await ackAndHide();
  }

  if (state === "loading" || state === "hidden") return null;

  return (
    <div role="region" aria-labelledby="mb-title" className="migration-banner">
      <div className="migration-banner-icon">
        <AlertCircle size={18} />
      </div>
      <div className="migration-banner-body">
        <div id="mb-title" className="migration-banner-title">
          Claw3D is now built-in
        </div>
        <div className="migration-banner-subtitle">
          Your previous Node-based install is no longer used.
        </div>
        {/*
          Dedicated aria-live child region (Gate-1 refinement #2). A
          container-level aria-live="polite" would queue announcements
          and can silently drop intermediate transitions on NVDA/JAWS;
          a dedicated aria-atomic span guarantees each state change is
          announced exactly once.
        */}
        <span
          aria-live="polite"
          aria-atomic="true"
          className="migration-banner-summary"
        >
          {state === "importing" && "Importing history…"}
          {state === "success" && report && (
            <>
              Imported {report.imported.agents} agents,{" "}
              {report.imported.sessions} sessions,{" "}
              {report.imported.messages} messages.
            </>
          )}
        </span>
        {runError && (
          <div className="migration-banner-error" role="alert">
            Import failed: {runError}
          </div>
        )}
        {ackError && (
          <div className="migration-banner-error" role="alert">
            {ackError}
          </div>
        )}
      </div>
      <div className="migration-banner-actions">
        {state === "visible" && (
          <>
            <button
              type="button"
              className="btn btn-primary btn-sm"
              onClick={handleImport}
            >
              Import history
            </button>
            <button
              type="button"
              className="btn btn-secondary btn-sm"
              onClick={handleKeepBackup}
            >
              Keep as backup
            </button>
            <button
              type="button"
              className="btn-ghost btn-sm"
              onClick={handleDismiss}
            >
              Dismiss
            </button>
          </>
        )}
        {state === "importing" && (
          <button type="button" className="btn btn-primary btn-sm" disabled>
            Importing…
          </button>
        )}
        {state === "success" && (
          <button
            type="button"
            className="btn btn-primary btn-sm"
            onClick={handleClose}
            aria-label="Close migration banner"
          >
            <X size={14} />
            &nbsp;Close
          </button>
        )}
      </div>
    </div>
  );
}
