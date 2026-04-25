import { useEffect, useRef, useState } from "react";
import {
  ShieldCheck,
  AlertCircle,
  ExternalLink,
  Check,
  Eye,
  Mouse,
  HardDrive,
  Apple,
} from "lucide-react";
import {
  isTauri,
  openSettingsPane,
  probePermissions,
  requestScreenRecording,
  type PermissionsReport,
  type PermStatus,
  type SettingsPane,
} from "../../lib/tauri";
import {
  canFinishWizard,
  ctaLabel,
  HAS_PROGRAMMATIC_PROMPT,
} from "../../lib/permissionGate";

interface PermissionsProps {
  onComplete: () => void;
}

interface RowConfig {
  key: keyof PermissionsReport;
  title: string;
  description: string;
  icon: React.ComponentType<{ size?: number; className?: string }>;
  /** macOS Settings deep-link pane. Null for `platform_supported`. */
  pane: SettingsPane | null;
  /** Whether the row is required for Finish-button enablement. */
  required: boolean;
  /** Helper copy shown when the row is the only blocker. */
  hint: string;
}

const ROWS: RowConfig[] = [
  {
    key: "accessibility",
    title: "Accessibility",
    description:
      "Lets Pan-Agent read window titles and click UI elements when you ask it to drive other applications.",
    icon: Mouse,
    pane: "Accessibility",
    required: true,
    hint: "Required — open Settings → Privacy & Security → Accessibility and add Pan Desktop.",
  },
  {
    key: "screen_recording",
    title: "Screen Recording",
    description:
      "Lets the visual tools see what you see. Captures stay on your machine and are HMAC-redacted before being journaled.",
    icon: Eye,
    pane: "ScreenCapture",
    required: true,
    hint: "Required — click Grant Access to trigger the system prompt, or open Settings → Privacy & Security → Screen & System Audio Recording.",
  },
  {
    key: "automation",
    title: "Automation (optional)",
    description:
      "Lets Pan-Agent script other apps via Apple Events. Per-app prompts appear on first use — granting here is not required.",
    icon: Apple,
    pane: "Automation",
    required: false,
    hint: "macOS will request this per-app the first time the agent talks to a specific app.",
  },
  {
    key: "full_disk",
    title: "Full Disk Access (optional)",
    description:
      "Only needed if you ask the agent to write into TCC-protected folders (~/Library, ~/Desktop, ...). Pan-Agent will prompt lazily.",
    icon: HardDrive,
    pane: "FullDiskAccess",
    required: false,
    hint: "Optional — leave for later, the agent will surface a clear EPERM if it ever needs this.",
  },
];

const STATUS_LABEL: Record<PermStatus, string> = {
  granted: "Granted",
  denied: "Denied",
  not_determined: "Not requested",
  unknown: "Unknown",
};

const STATUS_CLASS: Record<PermStatus, string> = {
  granted: "perm-row-status--granted",
  denied: "perm-row-status--denied",
  not_determined: "perm-row-status--pending",
  unknown: "perm-row-status--pending",
};

const POLL_INTERVAL_MS = 1000;

export function Permissions({ onComplete }: PermissionsProps): React.JSX.Element {
  const [report, setReport] = useState<PermissionsReport | null>(null);
  const [busy, setBusy] = useState<string | null>(null);
  const finishRef = useRef<HTMLButtonElement>(null);

  // 1 s poll while the wizard step is mounted. Cheap — every probe is
  // a couple of FFI calls and a bounded osascript exec.
  useEffect(() => {
    let cancelled = false;
    const tick = async () => {
      try {
        const r = await probePermissions();
        if (cancelled) return;
        setReport(r);
        // If we're not on macOS the gate is open and the wizard step
        // has nothing to render — auto-skip immediately.
        if (!r.platform_supported) {
          onComplete();
        }
      } catch (err) {
        if (cancelled) return;
        console.error("[Permissions] probe failed:", err);
        // Treat probe failures as if the platform isn't supported so
        // we don't strand the user behind a broken gate.
        setReport({
          accessibility: "unknown",
          screen_recording: "unknown",
          automation: "unknown",
          full_disk: "unknown",
          platform_supported: false,
        });
        onComplete();
      }
    };
    void tick();
    const id = setInterval(() => void tick(), POLL_INTERVAL_MS);
    return () => {
      cancelled = true;
      clearInterval(id);
    };
  }, [onComplete]);

  // Pre-focus the Finish button as soon as it becomes enabled.
  useEffect(() => {
    if (report && canFinishWizard(report)) {
      finishRef.current?.focus();
    }
  }, [report]);

  if (!report) {
    return (
      <div className="setup-step setup-step--permissions">
        <div className="perm-loading">Probing permissions…</div>
      </div>
    );
  }

  // Non-macOS callers see a brief "skipped" frame before onComplete fires.
  if (!report.platform_supported) {
    return (
      <div className="setup-step setup-step--permissions">
        <div className="perm-skipped">
          <ShieldCheck size={20} />
          <span>No macOS permissions to grant on this platform — continuing.</span>
        </div>
      </div>
    );
  }

  async function handleCta(row: RowConfig) {
    if (!report || report[row.key] === "granted") return;
    setBusy(row.key);
    try {
      const status = report[row.key] as PermStatus;
      if (
        status === "not_determined" &&
        HAS_PROGRAMMATIC_PROMPT[row.key] &&
        row.key === "screen_recording"
      ) {
        await requestScreenRecording();
      } else if (row.pane) {
        await openSettingsPane(row.pane);
      }
    } catch (err) {
      console.error(`[Permissions] CTA failed (${row.key}):`, err);
    } finally {
      setBusy(null);
    }
  }

  const ready = canFinishWizard(report);
  const blockers = ROWS.filter(
    (r) => r.required && report[r.key] !== "granted",
  );

  return (
    <div className="setup-step setup-step--permissions">
      <div className="perm-header">
        <h2>System permissions</h2>
        <p>
          Pan-Agent needs a few macOS permissions to drive other apps and see
          your screen. Setup polls every second — flip a toggle in System
          Settings and this screen will update.
        </p>
      </div>

      <div className="perm-rows" role="list">
        {ROWS.map((row) => {
          const status = report[row.key] as PermStatus;
          const Icon = row.icon;
          return (
            <div
              key={row.key}
              className={`perm-row${row.required ? " perm-row--required" : ""}`}
              role="listitem"
            >
              <div className="perm-row-icon">
                <Icon size={18} />
              </div>
              <div className="perm-row-body">
                <div className="perm-row-title">
                  {row.title}
                  <span
                    className={`perm-row-status ${STATUS_CLASS[status]}`}
                    aria-label={`Status: ${STATUS_LABEL[status]}`}
                  >
                    {status === "granted" ? (
                      <Check size={11} aria-hidden="true" />
                    ) : status === "denied" ? (
                      <AlertCircle size={11} aria-hidden="true" />
                    ) : null}
                    {STATUS_LABEL[status]}
                  </span>
                </div>
                <p className="perm-row-desc">{row.description}</p>
              </div>
              <div className="perm-row-actions">
                {status !== "granted" && (
                  <button
                    type="button"
                    className="perm-row-cta"
                    onClick={() => handleCta(row)}
                    disabled={busy === row.key}
                  >
                    {busy === row.key ? "…" : ctaLabel(status, HAS_PROGRAMMATIC_PROMPT[row.key])}
                    <ExternalLink size={11} aria-hidden="true" />
                  </button>
                )}
              </div>
            </div>
          );
        })}
      </div>

      {!ready && blockers.length > 0 && (
        <div className="perm-blockers" role="status" aria-live="polite">
          <AlertCircle size={14} />
          <div>
            <strong>Waiting on:</strong>{" "}
            {blockers.map((b, i) => (
              <span key={b.key}>
                {b.title}
                {i < blockers.length - 1 ? ", " : ""}
              </span>
            ))}
          </div>
        </div>
      )}

      <div className="perm-footer">
        <button
          ref={finishRef}
          type="button"
          className="setup-btn setup-btn--primary"
          disabled={!ready}
          onClick={onComplete}
        >
          {ready ? "Finish" : "Waiting for permissions…"}
        </button>
        {!isTauri() && (
          <span className="perm-dev-stub">
            (Vite-only dev — Tauri shell not detected, wizard will auto-skip)
          </span>
        )}
      </div>
    </div>
  );
}
