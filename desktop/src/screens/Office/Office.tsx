import { useEffect, useState } from "react";
import { ExternalLink, MoreVertical, RefreshCw } from "lucide-react";
import OfficeDebugPanel from "./OfficeDebugPanel";
import {
  handleOfficeKey,
  type OfficeShortcutActions,
} from "../Layout/layout.shortcuts";

interface OfficeProps {
  /**
   * Layout.tsx sets `visible={view === "office"}`. The Office screen is
   * permanently mounted after first visit (Layout's `officeVisited` ref),
   * so we CANNOT rely on mount/unmount for lifecycle scoping — both the
   * keyboard listener AND the debug panel's poll check this prop before
   * doing anything. Without the gate, Ctrl+Shift+R would bump the iframe
   * nonce while the user is browsing Chat, Memory, or any other screen.
   */
  visible?: boolean;
}

// M4 W2 Commit D: full rewrite from ~460 LOC legacy Node-sidecar lifecycle
// UI to a ~65 LOC happy-path wrapper around the Go-served Claw3D bundle.
// The legacy state machine (checking/installing/running), the Setup card,
// the log viewer, and the port/wsUrl settings bar all move into the debug
// panel (port override) or go away entirely (Setup is now "always ready"
// because the bundle is embedded in pan-agent.exe).

const API_BASE = import.meta.env.VITE_API_BASE ?? "http://localhost:8642";

function Office({ visible = false }: OfficeProps): React.JSX.Element {
  const [nonce, setNonce] = useState(0);
  const [debugOpen, setDebugOpen] = useState(false);
  const iframeSrc = `${API_BASE}/office/?_n=${nonce}`;

  // Keyboard shortcuts — gated on `visible` so Office.tsx doesn't react
  // to Ctrl+Shift+R while another screen is foregrounded. Routing logic
  // lives in layout.shortcuts.ts as a pure function (testable without
  // DOM or React); here we just adapt the DOM KeyboardEvent shape and
  // pass the two actions.
  useEffect(() => {
    if (!visible) return;
    const actions: OfficeShortcutActions = {
      reloadIframe: () => setNonce((n) => n + 1),
      toggleDebugPanel: () => setDebugOpen((v) => !v),
    };
    function onKey(e: KeyboardEvent) {
      const handled = handleOfficeKey(
        {
          ctrlKey: e.ctrlKey,
          metaKey: e.metaKey,
          shiftKey: e.shiftKey,
          key: e.key,
          targetTag: (document.activeElement?.tagName ?? undefined) as
            | string
            | undefined,
        },
        actions,
      );
      if (handled) e.preventDefault();
    }
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [visible]);

  return (
    <div className="office-ready">
      <header className="office-toolbar">
        <h1 className="office-toolbar-title">Office</h1>
        <div className="office-toolbar-right">
          <button
            type="button"
            className="btn-ghost office-toolbar-btn"
            onClick={() => setNonce((n) => n + 1)}
            title="Refresh (Ctrl+Shift+R)"
            aria-label="Refresh Office iframe"
          >
            <RefreshCw size={16} />
          </button>
          <a
            className="btn-ghost office-toolbar-btn"
            href={iframeSrc}
            target="_blank"
            rel="noreferrer"
            title="Open in browser"
            aria-label="Open Claw3D in a new browser window"
          >
            <ExternalLink size={16} />
          </a>
          <button
            type="button"
            className="btn-ghost office-toolbar-btn"
            onClick={() => setDebugOpen((v) => !v)}
            title="Debug panel (Ctrl+Shift+D)"
            aria-label="Toggle debug panel"
            aria-expanded={debugOpen}
            aria-controls="office-debug-panel"
          >
            <MoreVertical size={16} />
          </button>
        </div>
      </header>
      <OfficeDebugPanel
        visible={visible}
        open={debugOpen}
        onClose={() => setDebugOpen(false)}
        onReload={() => setNonce((n) => n + 1)}
      />
      <iframe
        key={nonce}
        src={iframeSrc}
        title="Claw3D"
        style={{ flex: 1, border: "none", width: "100%" }}
      />
    </div>
  );
}

export default Office;
