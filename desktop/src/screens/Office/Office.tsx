import { useState, useEffect, useRef, useCallback } from "react";
import {
  RefreshCw as Refresh,
  ExternalLink,
  Settings,
} from "lucide-react";
import { fetchJSON } from "../../api";

// How often to poll status while the Office tab is visible.
const STATUS_POLL_INTERVAL_MS = 5000;

type OfficeState =
  | "checking"
  | "not-installed"
  | "installing"
  | "ready"
  | "error";

interface Claw3dStatus {
  installed: boolean;
  running: boolean;
  port: number;
  portInUse: boolean;
  wsUrl: string | null;
  error: string | null;
}

interface OperationResult {
  success: boolean;
  error?: string;
}

interface SetupProgress {
  step: number;
  totalSteps: number;
  title: string;
  detail: string;
  log: string;
}

function Office({ visible }: { visible?: boolean }): React.JSX.Element {
  const [state, setState] = useState<OfficeState>("checking");
  const [running, setRunning] = useState(false);
  const [starting, setStarting] = useState(false);
  const [port, setPort] = useState(3000);
  const [portInput, setPortInput] = useState("3000");
  const [portInUse, setPortInUse] = useState(false);
  const [wsUrlInput, setWsUrlInput] = useState("ws://localhost:18789");
  const [error, setError] = useState("");
  const [showLogs, setShowLogs] = useState(false);
  const [logs, setLogs] = useState("");
  const [showSettings, setShowSettings] = useState(false);
  const [progress, setProgress] = useState<SetupProgress>({
    step: 0,
    totalSteps: 2,
    title: "Preparing...",
    detail: "",
    log: "",
  });
  const [iframeKey, setIframeKey] = useState(0);
  const logRef = useRef<HTMLDivElement>(null);

  const startingRef = useRef(starting);
  const runningRef = useRef(running);
  const errorRef = useRef(error);
  startingRef.current = starting;
  runningRef.current = running;
  errorRef.current = error;

  const checkStatus = useCallback(async (): Promise<void> => {
    setState("checking");
    try {
      const status = await fetchJSON<Claw3dStatus>("/v1/office/status");
      setRunning(status.running);
      setPort(status.port);
      setPortInput(String(status.port));
      setPortInUse(status.portInUse);
      setWsUrlInput(status.wsUrl || "ws://localhost:18789");
      if (status.error) setError(status.error);
      setState(status.installed ? "ready" : "not-installed");
    } catch (err) {
      console.error("[Office] checkStatus error:", err);
      setState("not-installed");
    }
  }, []);

  useEffect(() => {
    checkStatus();
  }, [checkStatus]);

  // Poll status only when tab is visible and in ready state
  useEffect(() => {
    if (state !== "ready" || !visible) return;
    const interval = setInterval(async () => {
      try {
        const status = await fetchJSON<Claw3dStatus>("/v1/office/status");
        setRunning(status.running);
        setPort(status.port);
        setPortInUse(status.portInUse);
        if (status.error && !errorRef.current) {
          setError(status.error);
        }
        if (startingRef.current && status.running) {
          setStarting(false);
        }
        if (!startingRef.current && !status.running && runningRef.current) {
          setRunning(false);
          if (status.error) setError(status.error);
        }
      } catch {
        /* ignore poll errors */
      }
    }, STATUS_POLL_INTERVAL_MS);
    return () => clearInterval(interval);
  }, [state, visible]);

  // Auto-scroll log
  useEffect(() => {
    if (logRef.current) {
      logRef.current.scrollTop = logRef.current.scrollHeight;
    }
  }, [progress.log, logs]);

  async function handleInstall(): Promise<void> {
    setState("installing");
    setError("");

    // Poll setup progress via SSE or polling endpoint
    const progressInterval = setInterval(async () => {
      try {
        const p = await fetchJSON<SetupProgress>("/v1/office/setup/progress");
        setProgress(p);
      } catch {
        /* ignore */
      }
    }, 500);

    try {
      const result = await fetchJSON<OperationResult>("/v1/office/setup", {
        method: "POST",
      });
      clearInterval(progressInterval);
      if (result.success) {
        setState("ready");
      } else {
        setError(result.error || "Setup failed");
        setState("error");
      }
    } catch (err) {
      clearInterval(progressInterval);
      setError((err as Error).message || "Setup failed");
      setState("error");
    }
  }

  async function handleStartStop(): Promise<void> {
    if (running) {
      try {
        await fetchJSON("/v1/office/stop", { method: "POST" });
        setRunning(false);
        setError("");
      } catch (err) {
        console.error("[Office] stop error:", err);
      }
    } else {
      setError("");
      setStarting(true);
      try {
        const result = await fetchJSON<OperationResult>("/v1/office/start", {
          method: "POST",
        });
        if (!result.success) {
          setError(result.error || "Failed to start Claw3D");
          setStarting(false);
        } else {
          setTimeout(() => {
            setRunning(true);
          }, 2000);
        }
      } catch (err) {
        setError((err as Error).message || "Failed to start Claw3D");
        setStarting(false);
      }
    }
  }

  async function handlePortSave(): Promise<void> {
    const newPort = parseInt(portInput, 10);
    if (isNaN(newPort) || newPort < 1024 || newPort > 65535) return;
    try {
      await fetchJSON("/v1/office/config", {
        method: "PUT",
        body: JSON.stringify({ port: newPort }),
      });
      setPort(newPort);
      const status = await fetchJSON<Claw3dStatus>("/v1/office/status");
      setPortInUse(status.portInUse);
    } catch (err) {
      console.error("[Office] handlePortSave error:", err);
    }
  }

  async function handleWsUrlSave(): Promise<void> {
    const trimmed = wsUrlInput.trim();
    if (!trimmed) return;
    try {
      await fetchJSON("/v1/office/config", {
        method: "PUT",
        body: JSON.stringify({ wsUrl: trimmed }),
      });
    } catch (err) {
      console.error("[Office] handleWsUrlSave error:", err);
    }
  }

  async function loadLogs(): Promise<void> {
    try {
      const data = await fetchJSON<{ logs: string }>("/v1/office/logs");
      setLogs(data.logs);
      setShowLogs(true);
    } catch (err) {
      console.error("[Office] loadLogs error:", err);
    }
  }

  function refreshIframe(): void {
    setIframeKey((k) => k + 1);
  }

  const percent =
    progress.totalSteps > 0
      ? Math.round((progress.step / progress.totalSteps) * 100)
      : 0;

  const claw3dUrl = `http://localhost:${port}`;

  // --- Checking ---
  if (state === "checking") {
    return (
      <div className="settings-container">
        <h1 className="settings-header">Office</h1>
        <div className="office-center">
          <div className="office-spinner" />
          <p className="office-muted">Checking Claw3D status...</p>
        </div>
      </div>
    );
  }

  // --- Not installed / error ---
  if (state === "not-installed" || state === "error") {
    return (
      <div className="settings-container">
        <h1 className="settings-header">Office</h1>
        <div className="office-center">
          <div className="office-setup-card">
            <h2 className="office-setup-title">Set Up Claw3D</h2>
            <p className="office-setup-desc">
              Claw3D is a 3D visualization environment for your AI agents.
              It lets you see your agents working in an interactive office
              space.
            </p>
            <p className="office-setup-desc">
              Click below to automatically download and set up Claw3D. This will
              clone the repository and install all dependencies.
            </p>
            {error && <div className="office-error">{error}</div>}
            <div className="office-setup-actions">
              <button className="btn btn-primary" onClick={handleInstall}>
                Install Claw3D
              </button>
              <a
                className="btn btn-secondary"
                href="https://github.com/Euraika-Labs/pan-office"
                target="_blank"
                rel="noreferrer"
              >
                <ExternalLink size={14} />
                View on GitHub
              </a>
            </div>
          </div>
        </div>
      </div>
    );
  }

  // --- Installing ---
  if (state === "installing") {
    return (
      <div className="settings-container">
        <h1 className="settings-header">Office</h1>
        <div className="office-installing">
          <h2 className="office-install-title">Setting Up Claw3D</h2>
          <div className="install-progress-container">
            <div className="install-progress-bar">
              <div
                className="install-progress-fill"
                style={{ width: `${percent}%` }}
              />
            </div>
            <div className="install-percent">{percent}%</div>
          </div>
          <div className="install-step-info">
            <div className="install-step-title">
              Step {progress.step}/{progress.totalSteps}: {progress.title}
            </div>
            <div className="install-step-detail">{progress.detail}</div>
          </div>
          <div className="install-log" ref={logRef}>
            {progress.log || "Waiting to start..."}
          </div>
        </div>
      </div>
    );
  }

  // --- Ready state ---
  return (
    <div className="office-ready">
      <div className="office-toolbar">
        <div className="office-toolbar-left">
          <h1 className="office-toolbar-title">Office</h1>
          <span
            className={`office-status-dot ${running ? "running" : "stopped"}`}
          />
          <span className="office-status-label">
            {starting ? "Starting..." : running ? "Running" : "Stopped"}
          </span>
        </div>
        <div className="office-toolbar-right">
          <button
            className={`btn btn-sm ${running ? "btn-secondary" : "btn-primary"}`}
            onClick={handleStartStop}
            disabled={starting || (portInUse && !running)}
          >
            {starting ? "Starting..." : running ? "Stop" : "Start"}
          </button>
          {running && (
            <>
              <button
                className="btn-ghost office-toolbar-btn"
                onClick={refreshIframe}
                title="Refresh"
              >
                <Refresh size={16} />
              </button>
              <a
                className="btn-ghost office-toolbar-btn"
                href={claw3dUrl}
                target="_blank"
                rel="noreferrer"
                title="Open in browser"
              >
                <ExternalLink size={16} />
              </a>
            </>
          )}
          <button
            className="btn-ghost office-toolbar-btn"
            onClick={() => setShowSettings(!showSettings)}
            title="Settings"
          >
            <Settings size={16} />
          </button>
        </div>
      </div>

      {showSettings && (
        <div className="office-settings-bar">
          <div className="office-setting">
            <label className="office-setting-label">Port</label>
            <input
              className="office-port-input"
              type="number"
              min={1024}
              max={65535}
              value={portInput}
              onChange={(e) => setPortInput(e.target.value)}
              onBlur={handlePortSave}
              onKeyDown={(e) => {
                if (e.key === "Enter") handlePortSave();
              }}
            />
          </div>
          <div className="office-setting">
            <label className="office-setting-label">WebSocket URL</label>
            <input
              className="office-ws-input"
              type="text"
              value={wsUrlInput}
              onChange={(e) => setWsUrlInput(e.target.value)}
              onBlur={handleWsUrlSave}
              onKeyDown={(e) => {
                if (e.key === "Enter") handleWsUrlSave();
              }}
              placeholder="ws://localhost:18789"
            />
          </div>
          <button className="btn btn-secondary btn-sm" onClick={loadLogs}>
            View Logs
          </button>
        </div>
      )}

      {portInUse && !running && (
        <div className="office-warning-bar">
          Port {port} is already in use. Change the port in settings or stop the
          other process.
        </div>
      )}

      {error && (
        <div className="office-error-bar">
          <div className="office-error-text">{error}</div>
          <div className="office-error-actions">
            <button className="btn btn-secondary btn-sm" onClick={loadLogs}>
              View Logs
            </button>
            <button
              className="btn btn-secondary btn-sm"
              onClick={() => setError("")}
            >
              Dismiss
            </button>
          </div>
        </div>
      )}

      {showLogs && (
        <div className="office-logs-panel">
          <div className="office-logs-header">
            <span>Process Logs</span>
            <button className="btn-ghost" onClick={() => setShowLogs(false)}>
              Close
            </button>
          </div>
          <div className="office-logs-content" ref={logRef}>
            {logs || "No logs yet. Start the services to see output."}
          </div>
        </div>
      )}

      <div className="office-content">
        {running && !showLogs ? (
          <iframe
            key={iframeKey}
            src={claw3dUrl}
            style={{ width: "100%", height: "100%", border: "none" }}
            title="Claw3D"
          />
        ) : !showLogs ? (
          <div className="office-center">
            <p className="office-muted">
              {portInUse && !running
                ? `Port ${port} is in use. Change it in settings to start.`
                : "Click Start to launch Claw3D"}
            </p>
          </div>
        ) : null}
      </div>
    </div>
  );
}

export default Office;
