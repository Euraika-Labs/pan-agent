import { useEffect, useRef, useState } from "react";
import { AlertCircle, X } from "lucide-react";
import {
  emitPersistenceAlert,
  getBundleInfo,
  getEngine,
  postEngine,
  type BundleInfo,
  type EngineGetResponse,
} from "../../api";

interface OfficeDebugPanelProps {
  /**
   * True when the Office screen is the active tab in the Layout shell.
   * The poll below only runs while BOTH `visible` and `open` are true —
   * this is the Gate-1 refinement that prevents the interval from firing
   * ~43k times per day if a user leaves the panel open overnight on a
   * backgrounded tab.
   */
  visible: boolean;
  /** Controlled open/closed state driven by the kebab in Office.tsx. */
  open: boolean;
  /** Called when the user clicks the panel's Close button. */
  onClose: () => void;
  /** Asks Office.tsx to bump the iframe nonce (full reload). */
  onReload: () => void;
}

/**
 * OfficeDebugPanel — M4 W2 slide-in panel behind the kebab-menu button in
 * Office.tsx. Exposes bundle SHA, the runtime engine toggle, and a logs
 * placeholder. Lifecycle is cooperative with its parent: polling only runs
 * when the panel is open AND the Office tab has focus, and all in-flight
 * fetches are aborted on unmount or when the gate closes.
 */
export default function OfficeDebugPanel(props: OfficeDebugPanelProps) {
  const { visible, open, onClose, onReload } = props;

  const [bundle, setBundle] = useState<BundleInfo | null>(null);
  const [engine, setEngine] = useState<EngineGetResponse | null>(null);
  const [swapping, setSwapping] = useState(false);
  const [swapError, setSwapError] = useState<string | null>(null);
  const abortRef = useRef<AbortController | null>(null);

  // Bundle SHA: one-shot fetch on panel open. No poll needed — the bundle
  // hash is stamped at build time and never changes while the process runs.
  useEffect(() => {
    if (!open) return;
    let cancelled = false;
    getBundleInfo()
      .then((b) => {
        if (!cancelled) setBundle(b);
      })
      .catch((err: unknown) => {
        console.error("[OfficeDebugPanel] bundle load:", err);
      });
    return () => {
      cancelled = true;
    };
  }, [open]);

  // Engine status poll — gated on `visible && open`. The AbortController is
  // captured LOCALLY in the effect so cleanup aborts the exact instance
  // created by this run, not whatever is currently in the ref (Gate-2
  // refinement — prevents late-resolution races after tab switch).
  useEffect(() => {
    if (!visible || !open) return;

    const abort = new AbortController();
    abortRef.current = abort;

    const poll = async () => {
      try {
        const next = await getEngine({ signal: abort.signal });
        if (abort.signal.aborted) return;
        setEngine(next);
      } catch (err: unknown) {
        if ((err as DOMException).name === "AbortError") return;
        console.error("[OfficeDebugPanel] engine poll:", err);
      }
    };

    poll();
    const interval = window.setInterval(poll, 2000);

    return () => {
      window.clearInterval(interval);
      abort.abort();
    };
  }, [visible, open]);

  async function handleEngineChange(target: string) {
    if (target !== "go" && target !== "node") return;
    if (swapping || !engine || target === engine.engine) return;

    setSwapping(true);
    setSwapError(null);
    try {
      const res = await postEngine({ engine: target });
      setEngine({ engine: res.engine, switchable: true });

      // Gate-1 refinement #6: persisted=false is a restart-time flip, not
      // cosmetic. Fire a page-level sticky alert so the user sees the
      // divergence even after they re-change engines or close the panel.
      if (!res.persisted) {
        emitPersistenceAlert({ engine: res.engine, from: res.from });
      }

      // Force the iframe to reload so the new engine actually serves content.
      onReload();
    } catch (err: unknown) {
      setSwapError((err as Error).message);
      console.error("[OfficeDebugPanel] engine swap:", err);
    } finally {
      setSwapping(false);
    }
  }

  if (!open) return null;

  return (
    <aside
      id="office-debug-panel"
      role="region"
      aria-label="Office debug panel"
      className="office-debug-panel"
    >
      <header className="office-debug-panel-header">
        <h2>Debug</h2>
        <button
          type="button"
          className="btn-ghost"
          aria-label="Close debug panel"
          onClick={onClose}
        >
          <X size={14} />
        </button>
      </header>

      <section className="office-debug-panel-section">
        <div className="office-debug-panel-label">Bundle SHA</div>
        <div className="office-debug-panel-value">
          {bundle ? bundle.sha.slice(0, 12) : "loading…"}
        </div>
      </section>

      <section className="office-debug-panel-section">
        <div className="office-debug-panel-label">Engine</div>
        <select
          value={engine?.engine ?? "go"}
          onChange={(e) => handleEngineChange(e.target.value)}
          disabled={swapping || !engine}
          aria-label="Claw3D engine"
        >
          <option value="go">go (embedded)</option>
          <option value="node">node (legacy sidecar)</option>
        </select>
        {swapError && (
          <div className="office-debug-panel-warn" role="alert">
            <AlertCircle size={12} />
            <span>Swap failed: {swapError}</span>
          </div>
        )}
      </section>

      <section className="office-debug-panel-section">
        <div className="office-debug-panel-label">Logs</div>
        <div className="office-debug-panel-logs">
          Logs not available in embedded mode (M5 endpoint pending).
        </div>
      </section>

      <section className="office-debug-panel-section">
        <button type="button" className="btn btn-secondary btn-sm" onClick={onReload}>
          Reload iframe
        </button>
      </section>
    </aside>
  );
}
