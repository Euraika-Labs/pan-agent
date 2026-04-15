import { useEffect, useState } from "react";
import { AlertTriangle, X } from "lucide-react";
import {
  PERSISTENCE_ALERT_EVENT,
  type PersistenceAlertDetail,
} from "../api";

/**
 * PersistenceAlert — sticky page-level warning that an engine swap
 * succeeded in-memory but failed to persist to config.yaml. Per Gate-1
 * refinement #6, this is a restart-time behaviour-flip risk (not a
 * cosmetic badge), so it stays visible across engine re-changes until
 * the user explicitly dismisses it.
 *
 * Subscribes to the DOM-level `pan-agent:persistence-alert` CustomEvent
 * that OfficeDebugPanel emits via `emitPersistenceAlert`. Using the
 * browser's event bus keeps this component independent of the Office
 * screen — it can be mounted at the Layout level and listen passively.
 */
export default function PersistenceAlert() {
  const [detail, setDetail] = useState<PersistenceAlertDetail | null>(null);

  useEffect(() => {
    function onAlert(e: Event) {
      const ce = e as CustomEvent<PersistenceAlertDetail>;
      if (ce.detail) setDetail(ce.detail);
    }
    window.addEventListener(PERSISTENCE_ALERT_EVENT, onAlert);
    return () => window.removeEventListener(PERSISTENCE_ALERT_EVENT, onAlert);
  }, []);

  if (!detail) return null;

  return (
    <div role="alert" className="persistence-alert">
      <div className="persistence-alert-icon">
        <AlertTriangle size={18} />
      </div>
      <div className="persistence-alert-body">
        <strong>Engine swap not saved.</strong>{" "}
        The runtime is now using <code>{detail.engine}</code>
        {detail.from && (
          <>
            {" "}
            (was <code>{detail.from}</code>)
          </>
        )}
        , but writing <code>office.engine</code> to <code>config.yaml</code>{" "}
        failed. On next restart the engine will revert. Check file
        permissions, then swap again from the debug panel.
      </div>
      <button
        type="button"
        className="persistence-alert-close"
        aria-label="Dismiss persistence alert"
        onClick={() => setDetail(null)}
      >
        <X size={16} />
      </button>
    </div>
  );
}
