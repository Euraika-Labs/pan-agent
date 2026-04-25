import { useEffect, useState, useCallback, useRef, useMemo, memo } from "react";
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
} from "lucide-react";
import {
  listRecoveries,
  getRecoveryDiff,
  undoRecovery,
  getTask,
  getTaskEvents,
} from "../../api";
import type {
  ReceiptDTO,
  DiffResponse,
  Task,
  TaskEvent,
} from "../../api";
import { TaskHeader } from "../../components/history/TaskHeader";
import { UndoConfirmDialog } from "../../components/history/UndoConfirmDialog";
import {
  groupReceiptsByTask,
  summarizeTaskCost,
} from "./historyGrouping";
import type { TaskGroupData } from "./historyGrouping";

interface HistoryProps {
  profile: string;
}

interface UndoState {
  pending: boolean;
  approvalId?: string;
  reversed?: boolean;
  revertedAt?: number;
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

function KindIcon({ kind }: { kind: ReceiptDTO["kind"] }): React.JSX.Element {
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
      <div className="history-modal" onClick={(e) => e.stopPropagation()}>
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

// ─── Receipt row (used inside each lane) ────────────────────────────────────

const ReceiptRow = memo(function ReceiptRow({
  receipt,
  expanded,
  undoState,
  onToggle,
  onViewDiff,
  onUndoClick,
}: {
  receipt: ReceiptDTO;
  expanded: boolean;
  undoState?: UndoState;
  onToggle: () => void;
  onViewDiff: () => void;
  onUndoClick: () => void;
}): React.JSX.Element {
  const reversible = receipt.reversalStatus === "reversible";
  const shortId = receipt.id.slice(-8);

  return (
    <div
      className={`history-card ${
        reversible ? "history-card--reversible" : "history-card--audit"
      }`}
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
            className={`history-status-badge history-status-badge--${receipt.reversalStatus.replace(
              "_",
              "-",
            )}`}
          >
            {undoState?.reversed
              ? "reverted"
              : undoState?.approvalId
                ? "pending approval"
                : receipt.reversalStatus.replace("_", " ")}
          </span>
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
                onClick={onUndoClick}
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
                  ? `Reverted at ${new Date(
                      undoState.revertedAt,
                    ).toLocaleTimeString([], {
                      hour: "2-digit",
                      minute: "2-digit",
                    })}`
                  : "Reverted"}
              </span>
            )}
          </div>
        </div>
      )}
    </div>
  );
});

// ─── Lane — shared body for the two lanes inside a task group ───────────────

function Lane({
  variant,
  receipts,
  expandedReceiptId,
  undoStates,
  onToggle,
  onViewDiff,
  onUndoClick,
}: {
  variant: "reversible" | "audit";
  receipts: ReceiptDTO[];
  expandedReceiptId: string | null;
  undoStates: Record<string, UndoState>;
  onToggle: (id: string) => void;
  onViewDiff: (id: string, kind: string) => void;
  onUndoClick: (receipt: ReceiptDTO) => void;
}): React.JSX.Element {
  const title = variant === "reversible" ? "Reversible" : "Audit-only";
  const Icon = variant === "reversible" ? Undo2 : Clock;
  if (receipts.length === 0) return <></>;
  return (
    <div className={`history-lane history-lane--${variant}`}>
      <div className="history-lane-subheader">
        <Icon size={11} />
        <span className="history-lane-subheader-title">{title}</span>
        <span className="history-lane-count">{receipts.length}</span>
      </div>
      <div className="history-lane-list">
        {receipts.map((r) => (
          <ReceiptRow
            key={r.id}
            receipt={r}
            expanded={expandedReceiptId === r.id}
            undoState={undoStates[r.id]}
            onToggle={() => onToggle(r.id)}
            onViewDiff={() => onViewDiff(r.id, r.kind)}
            onUndoClick={() => onUndoClick(r)}
          />
        ))}
      </div>
    </div>
  );
}

// ─── TaskGroup — header + lanes for a single task ───────────────────────────

function TaskGroup({
  group,
  expanded,
  expandedReceiptId,
  undoStates,
  events,
  onToggle,
  onReceiptToggle,
  onViewDiff,
  onUndoClick,
}: {
  group: TaskGroupData;
  expanded: boolean;
  expandedReceiptId: string | null;
  undoStates: Record<string, UndoState>;
  events: TaskEvent[] | undefined;
  onToggle: () => void;
  onReceiptToggle: (id: string) => void;
  onViewDiff: (id: string, kind: string) => void;
  onUndoClick: (receipt: ReceiptDTO) => void;
}): React.JSX.Element {
  const summary = useMemo(
    () => summarizeTaskCost(events ?? []),
    [events],
  );
  return (
    <div className="task-group">
      <TaskHeader
        taskId={group.taskId}
        task={group.task}
        totalCostUsd={summary.totalCostUsd}
        sparkline={summary.sparkline}
        receiptCount={group.receipts.length}
        expanded={expanded}
        freshestReceiptAt={group.receipts[0]?.createdAt ?? 0}
        onToggle={onToggle}
      />
      {expanded && (
        <div className="task-group-body">
          <Lane
            variant="reversible"
            receipts={group.reversible}
            expandedReceiptId={expandedReceiptId}
            undoStates={undoStates}
            onToggle={onReceiptToggle}
            onViewDiff={onViewDiff}
            onUndoClick={onUndoClick}
          />
          <Lane
            variant="audit"
            receipts={group.audit}
            expandedReceiptId={expandedReceiptId}
            undoStates={undoStates}
            onToggle={onReceiptToggle}
            onViewDiff={onViewDiff}
            onUndoClick={onUndoClick}
          />
        </div>
      )}
    </div>
  );
}

// ─── Main History screen ────────────────────────────────────────────────────

function History({ profile: _profile }: HistoryProps): React.JSX.Element {
  const [receipts, setReceipts] = useState<ReceiptDTO[]>([]);
  const [tasks, setTasks] = useState<Map<string, Task>>(new Map());
  const [taskEvents, setTaskEvents] = useState<Map<string, TaskEvent[]>>(
    new Map(),
  );
  const [loading, setLoading] = useState(true);
  const [collapsedGroups, setCollapsedGroups] = useState<Set<string>>(
    new Set(),
  );
  const [expandedReceiptId, setExpandedReceiptId] = useState<string | null>(
    null,
  );
  const [undoStates, setUndoStates] = useState<Record<string, UndoState>>({});
  const [pendingUndoReceipt, setPendingUndoReceipt] =
    useState<ReceiptDTO | null>(null);

  // Diff modal state
  const [diffTarget, setDiffTarget] = useState<{
    receiptId: string;
    kind: string;
  } | null>(null);
  const [diffData, setDiffData] = useState<DiffResponse | null>(null);
  const [diffLoading, setDiffLoading] = useState(false);

  const autoRefreshTimer = useRef<ReturnType<typeof setInterval> | null>(null);

  // Group + sort receipts memoized off the latest fetch.
  const groups = useMemo(
    () => groupReceiptsByTask(receipts, tasks),
    [receipts, tasks],
  );

  const loadReceipts = useCallback(async (): Promise<void> => {
    try {
      const list = await listRecoveries(undefined, 200);
      setReceipts(list);
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

  // Auto-refresh every 5 s — receipts only. Tasks change less often
  // and are re-fetched lazily as new task IDs appear.
  useEffect(() => {
    if (autoRefreshTimer.current) clearInterval(autoRefreshTimer.current);
    autoRefreshTimer.current = setInterval(() => loadReceipts(), 5000);
    return () => {
      if (autoRefreshTimer.current) clearInterval(autoRefreshTimer.current);
    };
  }, [loadReceipts]);

  // Fan out task + events fetches whenever a new taskId shows up. Cached
  // by ID so repeated refreshes don't re-hit /v1/tasks/* for tasks we
  // already have. Failures are swallowed — a missing task just renders
  // with the placeholder header (taskHeading falls back to short ID).
  useEffect(() => {
    if (receipts.length === 0) return;
    const seen = new Set(receipts.map((r) => r.taskId).filter(Boolean));
    const missingTasks = Array.from(seen).filter((id) => !tasks.has(id));
    const missingEvents = Array.from(seen).filter((id) => !taskEvents.has(id));
    if (missingTasks.length === 0 && missingEvents.length === 0) return;

    Promise.allSettled(
      missingTasks.map((id) =>
        getTask(id).then((t) => ({ id, t })).catch(() => null),
      ),
    ).then((results) => {
      setTasks((prev) => {
        const next = new Map(prev);
        for (const r of results) {
          if (r.status === "fulfilled" && r.value) next.set(r.value.id, r.value.t);
        }
        return next;
      });
    });

    Promise.allSettled(
      missingEvents.map((id) =>
        getTaskEvents(id).then((events) => ({ id, events })).catch(() => null),
      ),
    ).then((results) => {
      setTaskEvents((prev) => {
        const next = new Map(prev);
        for (const r of results) {
          if (r.status === "fulfilled" && r.value)
            next.set(r.value.id, r.value.events);
        }
        return next;
      });
    });
  }, [receipts, tasks, taskEvents]);

  const handleReceiptToggle = useCallback((receiptId: string): void => {
    setExpandedReceiptId((prev) => (prev === receiptId ? null : receiptId));
  }, []);

  const handleGroupToggle = useCallback((taskId: string): void => {
    setCollapsedGroups((prev) => {
      const next = new Set(prev);
      if (next.has(taskId)) next.delete(taskId);
      else next.add(taskId);
      return next;
    });
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

  const performUndo = useCallback(
    async (receipt: ReceiptDTO): Promise<void> => {
      const receiptId = receipt.id;
      setUndoStates((prev) => ({
        ...prev,
        [receiptId]: { pending: true },
      }));
      try {
        const res = await undoRecovery(receiptId);
        if (res.httpStatus === 202) {
          setUndoStates((prev) => ({
            ...prev,
            [receiptId]: { pending: false, approvalId: res.approvalId },
          }));
        } else {
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

  const totalCount = receipts.length;

  return (
    <div className="history-container">
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

      {loading ? (
        <div className="history-loading">
          <div className="loading-spinner" />
        </div>
      ) : totalCount === 0 ? (
        <div className="history-empty">
          <HistoryIcon size={32} className="history-empty-icon" />
          <p className="history-empty-text">No recovery history yet</p>
          <p className="history-empty-hint">
            Actions taken by the agent will appear here grouped by task,
            with undo capability for reversible actions.
          </p>
        </div>
      ) : (
        <div className="history-groups">
          {groups.map((g) => (
            <TaskGroup
              key={g.taskId}
              group={g}
              expanded={!collapsedGroups.has(g.taskId)}
              expandedReceiptId={expandedReceiptId}
              undoStates={undoStates}
              events={taskEvents.get(g.taskId)}
              onToggle={() => handleGroupToggle(g.taskId)}
              onReceiptToggle={handleReceiptToggle}
              onViewDiff={handleViewDiff}
              onUndoClick={(receipt) => setPendingUndoReceipt(receipt)}
            />
          ))}
        </div>
      )}

      <UndoConfirmDialog
        receipt={pendingUndoReceipt}
        onConfirm={() => {
          if (pendingUndoReceipt) {
            performUndo(pendingUndoReceipt);
          }
          setPendingUndoReceipt(null);
        }}
        onCancel={() => setPendingUndoReceipt(null)}
      />

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
