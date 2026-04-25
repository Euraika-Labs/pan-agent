import { useEffect, useState, useCallback, useRef, memo } from "react";
import {
  History as HistoryIcon,
  RefreshCw,
  Undo2,
  Eye,
  ShieldCheck,
  FileText,
  Terminal,
  Globe,
  Cloud,
  Trash2,
  X,
  Clock,
  ChevronDown,
  ChevronUp,
} from "lucide-react";
import { listRecoveries, getRecoveryDiff, undoRecovery } from "../../api";
import type { ReceiptDTO, DiffResponse } from "../../api";

interface HistoryProps {
  profile: string;
}

// ─── Helpers ────────────────────────────────────────────────────────────────

function formatFullDate(ms: number): string {
  const date = new Date(ms);
  return (
    date.toLocaleDateString([], { month: "short", day: "numeric" }) +
    ", " +
    date.toLocaleTimeString([], { hour: "2-digit", minute: "2-digit" })
  );
}

function kindLabel(kind: ReceiptDTO["kind"]): string {
  switch (kind) {
    case "fs_write":
      return "File Write";
    case "fs_delete":
      return "File Delete";
    case "shell":
      return "Shell";
    case "browser_form":
      return "Browser Form";
    case "saas_api":
      return "SaaS API";
  }
}

// ─── Kind icon ──────────────────────────────────────────────────────────────

function KindIcon({
  kind,
}: {
  kind: ReceiptDTO["kind"];
}): React.JSX.Element {
  const iconProps = { size: 14, className: "history-kind-icon" };
  switch (kind) {
    case "fs_write":
      return <FileText {...iconProps} />;
    case "fs_delete":
      return <Trash2 {...iconProps} />;
    case "shell":
      return <Terminal {...iconProps} />;
    case "browser_form":
      return <Globe {...iconProps} />;
    case "saas_api":
      return <Cloud {...iconProps} />;
  }
}

// ─── Diff modal ─────────────────────────────────────────────────────────────

const DiffModal = memo(function DiffModal({
  diff,
  loading,
  receiptKind,
  onClose,
}: {
  diff: DiffResponse | null;
  loading: boolean;
  receiptKind: string;
  onClose: () => void;
}): React.JSX.Element {
  return (
    <div className="history-modal-backdrop" onClick={onClose}>
      <div
        className="history-modal"
        onClick={(e) => e.stopPropagation()}
      >
        <div className="history-modal-header">
          <h3 className="history-modal-title">
            Diff &mdash; {receiptKind.replace("_", " ")}
          </h3>
          <button className="history-modal-close" onClick={onClose}>
            <X size={16} />
          </button>
        </div>

        <div className="history-modal-body">
          {loading ? (
            <div className="history-modal-loading">
              <div className="loading-spinner" />
            </div>
          ) : diff ? (
            <div className="history-diff-panels">
              <div className="history-diff-panel">
                <div className="history-diff-label">Before</div>
                <pre className="history-diff-content">
                  {diff.before || "(empty)"}
                </pre>
              </div>
              <div className="history-diff-panel">
                <div className="history-diff-label">After</div>
                <pre className="history-diff-content">
                  {diff.after || "(empty)"}
                </pre>
              </div>
            </div>
          ) : (
            <p className="history-diff-error">
              Could not load diff for this receipt.
            </p>
          )}
        </div>
      </div>
    </div>
  );
});

// ─── Receipt card ───────────────────────────────────────────────────────────

const ReceiptCard = memo(function ReceiptCard({
  receipt,
  expanded,
  undoState,
  onToggle,
  onViewDiff,
  onUndo,
}: {
  receipt: ReceiptDTO;
  expanded: boolean;
  undoState?: {
    pending: boolean;
    approvalId?: string;
    reversed?: boolean;
    revertedAt?: number;
  };
  onToggle: () => void;
  onViewDiff: () => void;
  onUndo: () => void;
}): React.JSX.Element {
  const reversible = receipt.reversalStatus === "reversible";
  const shortId = receipt.id.slice(-8);

  return (
    <div
      className={`history-card ${reversible ? "history-card--reversible" : "history-card--audit"}`}
    >
      <button className="history-card-summary" onClick={onToggle}>
        <div className="history-card-main">
          <KindIcon kind={receipt.kind} />
          <span className="history-card-kind">{kindLabel(receipt.kind)}</span>
          <span className="history-card-id">#{shortId}</span>
          <span className="history-card-time">
            {formatFullDate(receipt.createdAt)}
          </span>
        </div>
        <div className="history-card-meta">
          <span
            className={`history-tier-badge history-tier-badge--${receipt.snapshotTier}`}
          >
            {receipt.snapshotTier === "audit_only"
              ? "audit"
              : receipt.snapshotTier}
          </span>
          <span
            className={`history-status-badge history-status-badge--${receipt.reversalStatus.replace("_", "-")}`}
          >
            {undoState?.reversed
              ? "reversed"
              : undoState?.approvalId
                ? "pending approval"
                : receipt.reversalStatus.replace("_", " ")}
          </span>
          {expanded ? <ChevronUp size={13} /> : <ChevronDown size={13} />}
        </div>
      </button>

      {expanded && (
        <div className="history-detail">
          <div className="history-detail-payload">
            <pre className="history-detail-payload-text">
              {receipt.redactedPayload || "(no payload)"}
            </pre>
          </div>

          {receipt.saasUrl && (
            <a
              className="history-detail-link"
              href={receipt.saasUrl}
              target="_blank"
              rel="noopener noreferrer"
            >
              <Globe size={12} />
              Open in service
            </a>
          )}

          <div className="history-detail-actions">
            <button className="btn btn-sm" onClick={onViewDiff}>
              <Eye size={12} />
              View Diff
            </button>

            {reversible && !undoState?.reversed && (
              <button
                className="btn btn-sm btn-danger"
                onClick={onUndo}
                disabled={undoState?.pending}
              >
                <Undo2 size={12} />
                {undoState?.pending
                  ? "Undoing…"
                  : undoState?.approvalId
                    ? "Pending Approval"
                    : "Undo"}
              </button>
            )}

            {undoState?.reversed && (
              <span className="history-reversed-label">
                <ShieldCheck size={12} />
                {undoState.revertedAt
                  ? `Reverted at ${new Date(undoState.revertedAt).toLocaleTimeString(
                      [],
                      { hour: "2-digit", minute: "2-digit" },
                    )}`
                  : "Reverted"}
              </span>
            )}
          </div>
        </div>
      )}
    </div>
  );
});

// ─── Main History screen ────────────────────────────────────────────────────

function History({ profile: _profile }: HistoryProps): React.JSX.Element {
  const [receipts, setReceipts] = useState<ReceiptDTO[]>([]);
  const [loading, setLoading] = useState(true);
  const [expandedId, setExpandedId] = useState<string | null>(null);
  const [undoStates, setUndoStates] = useState<
    Record<
      string,
      {
        pending: boolean;
        approvalId?: string;
        reversed?: boolean;
        /**
         * Local timestamp captured when an undo succeeds. Drives the
         * "Reverted at HH:MM" stamp; survives auto-refreshes since it's
         * keyed by receipt ID, so the row stays in place rather than
         * reordering when the journal status updates.
         */
        revertedAt?: number;
      }
    >
  >({});

  // Diff modal state
  const [diffTarget, setDiffTarget] = useState<{
    receiptId: string;
    kind: string;
  } | null>(null);
  const [diffData, setDiffData] = useState<DiffResponse | null>(null);
  const [diffLoading, setDiffLoading] = useState(false);

  const autoRefreshTimer = useRef<ReturnType<typeof setInterval> | null>(null);

  const loadReceipts = useCallback(async (): Promise<void> => {
    try {
      const list = await listRecoveries(undefined, 200);
      // Newest first
      setReceipts(list.sort((a, b) => b.createdAt - a.createdAt));
    } catch (err) {
      console.error("[History] load error:", err);
    } finally {
      setLoading(false);
    }
  }, []);

  // Initial load
  useEffect(() => {
    setLoading(true);
    loadReceipts();
  }, [loadReceipts]);

  // Auto-refresh every 5s
  useEffect(() => {
    if (autoRefreshTimer.current) clearInterval(autoRefreshTimer.current);
    autoRefreshTimer.current = setInterval(() => {
      loadReceipts();
    }, 5000);
    return () => {
      if (autoRefreshTimer.current) clearInterval(autoRefreshTimer.current);
    };
  }, [loadReceipts]);

  const handleToggle = useCallback((receiptId: string): void => {
    setExpandedId((prev) => (prev === receiptId ? null : receiptId));
  }, []);

  const handleViewDiff = useCallback(
    async (receiptId: string, kind: string): Promise<void> => {
      setDiffTarget({ receiptId, kind });
      setDiffLoading(true);
      setDiffData(null);
      try {
        const data = await getRecoveryDiff(receiptId);
        setDiffData(data);
      } catch (err) {
        console.error("[History] diff error:", err);
      } finally {
        setDiffLoading(false);
      }
    },
    [],
  );

  const handleUndo = useCallback(
    async (receiptId: string): Promise<void> => {
      setUndoStates((prev) => ({
        ...prev,
        [receiptId]: { pending: true },
      }));
      try {
        const res = await undoRecovery(receiptId);
        if (res.httpStatus === 202) {
          // Needs approval — caller should poll /v1/approvals/{id}.
          setUndoStates((prev) => ({
            ...prev,
            [receiptId]: { pending: false, approvalId: res.approvalId },
          }));
        } else {
          // 200 — reversed synchronously. Stamp the local revertedAt so
          // the row keeps its position rather than reordering (per the
          // WS#2 design-doc "Never reorder post-revert").
          setUndoStates((prev) => ({
            ...prev,
            [receiptId]: {
              pending: false,
              reversed: true,
              revertedAt: Date.now(),
            },
          }));
          await loadReceipts();
        }
      } catch (err) {
        console.error("[History] undo error:", err);
        setUndoStates((prev) => ({
          ...prev,
          [receiptId]: { pending: false },
        }));
      }
    },
    [loadReceipts],
  );

  const closeDiffModal = useCallback(() => {
    setDiffTarget(null);
    setDiffData(null);
  }, []);

  // Split receipts into two swim lanes
  const reversible = receipts.filter(
    (r) => r.reversalStatus === "reversible",
  );
  const auditTrail = receipts.filter(
    (r) => r.reversalStatus !== "reversible",
  );

  return (
    <div className="history-container">
      {/* Header */}
      <div className="history-header">
        <h2 className="history-title">History</h2>
        <button
          className="btn"
          onClick={() => {
            setLoading(true);
            loadReceipts();
          }}
        >
          <RefreshCw size={14} />
          Refresh
        </button>
      </div>

      {/* Content */}
      {loading ? (
        <div className="history-loading">
          <div className="loading-spinner" />
        </div>
      ) : receipts.length === 0 ? (
        <div className="history-empty">
          <HistoryIcon size={32} className="history-empty-icon" />
          <p className="history-empty-text">No recovery history yet</p>
          <p className="history-empty-hint">
            Actions taken by the agent will appear here with undo capability
          </p>
        </div>
      ) : (
        <div className="history-lanes">
          {/* Lane 1: Reversible */}
          <div className="history-lane">
            <div className="history-lane-header">
              <Undo2 size={14} />
              <span className="history-lane-title">Reversible</span>
              <span className="history-lane-count">{reversible.length}</span>
            </div>
            {reversible.length === 0 ? (
              <p className="history-lane-empty">
                No reversible actions at this time.
              </p>
            ) : (
              <div className="history-lane-list">
                {reversible.map((r) => (
                  <ReceiptCard
                    key={r.id}
                    receipt={r}
                    expanded={expandedId === r.id}
                    undoState={undoStates[r.id]}
                    onToggle={() => handleToggle(r.id)}
                    onViewDiff={() => handleViewDiff(r.id, r.kind)}
                    onUndo={() => handleUndo(r.id)}
                  />
                ))}
              </div>
            )}
          </div>

          {/* Lane 2: Audit Trail */}
          <div className="history-lane">
            <div className="history-lane-header">
              <Clock size={14} />
              <span className="history-lane-title">Audit Trail</span>
              <span className="history-lane-count">{auditTrail.length}</span>
            </div>
            {auditTrail.length === 0 ? (
              <p className="history-lane-empty">
                No audit-only entries recorded.
              </p>
            ) : (
              <div className="history-lane-list">
                {auditTrail.map((r) => (
                  <ReceiptCard
                    key={r.id}
                    receipt={r}
                    expanded={expandedId === r.id}
                    undoState={undoStates[r.id]}
                    onToggle={() => handleToggle(r.id)}
                    onViewDiff={() => handleViewDiff(r.id, r.kind)}
                    onUndo={() => handleUndo(r.id)}
                  />
                ))}
              </div>
            )}
          </div>
        </div>
      )}

      {/* Diff modal */}
      {diffTarget && (
        <DiffModal
          diff={diffData}
          loading={diffLoading}
          receiptKind={diffTarget.kind}
          onClose={closeDiffModal}
        />
      )}
    </div>
  );
}

export default History;
