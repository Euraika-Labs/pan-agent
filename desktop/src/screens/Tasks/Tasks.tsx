import { useEffect, useState, useRef, useCallback, useMemo, memo } from "react";
import {
  RefreshCw,
  ListChecks,
  Pause,
  Play,
  XCircle,
  ChevronDown,
  ChevronUp,
  Wrench,
  CheckSquare,
  BookOpen,
  Package,
  DollarSign,
  AlertTriangle,
  Heart,
  Layers,
} from "lucide-react";
import {
  listTasks,
  getTaskEvents,
  pauseTask,
  resumeTask,
  cancelTask,
} from "../../api";
import type { Task, TaskEvent, TaskEventKind } from "../../api";
import {
  costState,
  formatCostLabel,
  formatLastHeartbeat,
  formatTaskDuration,
  groupTasks,
  hasActiveTask,
  isTerminal,
  truncate,
} from "./tasksGrouping";
import { summarizeTaskCost } from "../History/historyGrouping";
import { CostSparkline } from "../../components/history/CostSparkline";
import { CancelConfirmDialog } from "../../components/tasks/CancelConfirmDialog";

interface TasksProps {
  profile: string;
}

// ─── Local helpers ──────────────────────────────────────────────────────────

function formatTime(ts: number): string {
  const date = new Date(ts * 1000);
  return date.toLocaleTimeString([], { hour: "2-digit", minute: "2-digit" });
}

function formatFullDate(ts: number): string {
  const date = new Date(ts * 1000);
  return (
    date.toLocaleDateString([], { month: "short", day: "numeric" }) +
    ", " +
    date.toLocaleTimeString([], { hour: "2-digit", minute: "2-digit" })
  );
}

// ─── Event kind icon ─────────────────────────────────────────────────────────

function EventKindIcon({
  kind,
}: {
  kind: TaskEventKind;
}): React.JSX.Element {
  const iconProps = { size: 13, className: "tasks-event-kind-icon" };
  switch (kind) {
    case "tool_call":
      return <Wrench {...iconProps} />;
    case "approval":
      return <CheckSquare {...iconProps} />;
    case "journal_receipt":
      return <BookOpen {...iconProps} />;
    case "artifact":
      return <Package {...iconProps} />;
    case "cost":
      return <DollarSign {...iconProps} />;
    case "error":
      return <AlertTriangle {...iconProps} />;
    case "heartbeat":
      return <Heart {...iconProps} />;
    case "step_completed":
      return <Layers {...iconProps} />;
  }
}

// ─── Task event row ──────────────────────────────────────────────────────────

const TaskEventRow = memo(function TaskEventRow({
  event,
}: {
  event: TaskEvent;
}): React.JSX.Element {
  let payloadExcerpt = "";
  if (event.payload_json) {
    try {
      const parsed = JSON.parse(event.payload_json) as Record<string, unknown>;
      // Prefer a human-readable field; fall back to raw JSON
      const text =
        (parsed.message as string) ||
        (parsed.summary as string) ||
        (parsed.tool as string) ||
        JSON.stringify(parsed);
      payloadExcerpt = truncate(text, 100);
    } catch {
      payloadExcerpt = truncate(event.payload_json, 100);
    }
  }

  return (
    <div className={`tasks-event tasks-event--${event.kind}`}>
      <div className="tasks-event-header">
        <EventKindIcon kind={event.kind} />
        <span className="tasks-event-kind">{event.kind.replace("_", " ")}</span>
        <span className="tasks-event-step">
          {truncate(event.step_id, 24)}
        </span>
        <span className="tasks-event-time">{formatTime(event.created_at)}</span>
      </div>
      {payloadExcerpt && (
        <p className="tasks-event-payload">{payloadExcerpt}</p>
      )}
    </div>
  );
});

// ─── Task card ───────────────────────────────────────────────────────────────

const TaskCard = memo(function TaskCard({
  task,
  expanded,
  events,
  eventsLoading,
  onToggle,
  onPause,
  onResume,
  onCancel,
}: {
  task: Task;
  expanded: boolean;
  events: TaskEvent[];
  eventsLoading: boolean;
  onToggle: () => void;
  onPause: () => void;
  onResume: () => void;
  onCancel: () => void;
}): React.JSX.Element {
  const terminal = isTerminal(task.status);
  const shortId = task.id.slice(-8);
  const shortSession = task.session_id.slice(-8);

  // Cost-used is derived from task_events (kind=cost) rather than a
  // dedicated column on the tasks table — events arrive lazily on
  // expand so the collapsed row shows cap-only, and the pill/sparkline
  // populate once the detail drawer opens.
  const costSummary = useMemo(
    () => summarizeTaskCost(events),
    [events],
  );
  const cap = task.cost_cap_usd;
  const used = costSummary.totalCostUsd;
  const pillState = costState(used, cap);
  const showHeartbeat = !terminal;
  const duration = formatTaskDuration(
    task.created_at,
    task.last_heartbeat_at,
  );

  return (
    <div className={`tasks-card tasks-card--${task.status}`}>
      {/* Summary row — clicking expands/collapses */}
      <button className="tasks-card-summary" onClick={onToggle}>
        <div className="tasks-card-main">
          <span
            className={`tasks-status-badge tasks-status-badge--${task.status}`}
          >
            {task.status}
          </span>
          <span className="tasks-card-id">task-{shortId}</span>
          <span className="tasks-card-session">
            session&nbsp;
            <code className="tasks-card-session-id">{shortSession}</code>
          </span>
          <span className="tasks-card-time">
            {formatFullDate(task.created_at)}
          </span>
          {showHeartbeat && (
            <span
              className="tasks-card-heartbeat"
              title="Last heartbeat from the runner — stale heartbeats trip the zombie reaper"
            >
              <Heart size={11} aria-hidden="true" />
              {formatLastHeartbeat(task.last_heartbeat_at)}
            </span>
          )}
          {terminal && (
            <span className="tasks-card-duration" title="Total task duration">
              {duration}
            </span>
          )}
        </div>

        <div className="tasks-card-meta">
          {cap > 0 && (
            <span className={`tasks-cost-pill tasks-cost-pill--${pillState}`}>
              {formatCostLabel(used, cap)}
            </span>
          )}
          {cap === 0 && used > 0 && (
            <span className="tasks-cost-pill tasks-cost-pill--ok">
              {formatCostLabel(used, 0)}
            </span>
          )}
          <span className="tasks-tag">step {task.next_plan_step_index}</span>
          {expanded ? <ChevronUp size={13} /> : <ChevronDown size={13} />}
        </div>
      </button>

      {/* Expanded detail panel */}
      {expanded && (
        <div className="tasks-detail">
          {/* Cost-over-time sparkline (Phase 13 WS#13.A polish) */}
          {costSummary.sparkline.length > 0 && (
            <div className="tasks-detail-cost">
              <span className="tasks-detail-cost-label">Cost over time</span>
              <span className="tasks-detail-cost-spark">
                <CostSparkline
                  points={costSummary.sparkline}
                  width={140}
                  height={26}
                  ariaLabel={`Cost over time, total ${formatCostLabel(used, cap)}`}
                />
              </span>
              <span className="tasks-detail-cost-total">
                {formatCostLabel(used, cap)}
              </span>
            </div>
          )}

          {/* Action buttons */}
          <div className="tasks-detail-actions">
            {task.status === "running" && (
              <button className="btn btn-sm" onClick={onPause}>
                <Pause size={12} />
                Pause
              </button>
            )}
            {task.status === "paused" && (
              <button className="btn btn-sm btn-primary" onClick={onResume}>
                <Play size={12} />
                Resume
              </button>
            )}
            {!terminal && (
              <button className="btn btn-sm btn-danger" onClick={onCancel}>
                <XCircle size={12} />
                Cancel
              </button>
            )}
          </div>

          {/* Events timeline */}
          <div className="tasks-events">
            {eventsLoading ? (
              <div className="tasks-events-loading">
                <div className="loading-spinner" />
              </div>
            ) : events.length === 0 ? (
              <p className="tasks-events-empty">No events recorded yet.</p>
            ) : (
              // Reverse-chronological
              [...events]
                .sort((a, b) => b.sequence - a.sequence)
                .map((evt) => <TaskEventRow key={evt.id} event={evt} />)
            )}
          </div>
        </div>
      )}
    </div>
  );
});

// ─── Main Tasks screen ───────────────────────────────────────────────────────

function Tasks({ profile: _profile }: TasksProps): React.JSX.Element {
  const [tasks, setTasks] = useState<Task[]>([]);
  const [loading, setLoading] = useState(true);
  const [expandedId, setExpandedId] = useState<string | null>(null);
  const [eventsMap, setEventsMap] = useState<
    Record<string, { loading: boolean; events: TaskEvent[] }>
  >({});
  // Pending cancel target — non-null while the CancelConfirmDialog is
  // open. Cancelling now requires explicit confirmation rather than a
  // single-click on the row card, mirroring the History undo flow.
  const [pendingCancelTask, setPendingCancelTask] = useState<Task | null>(
    null,
  );
  const autoRefreshTimer = useRef<ReturnType<typeof setInterval> | null>(null);

  const loadTasks = useCallback(async (): Promise<void> => {
    try {
      const list = await listTasks();
      // Newest first
      setTasks(list.sort((a, b) => b.created_at - a.created_at));
    } catch (err) {
      console.error("[Tasks] load error:", err);
    } finally {
      setLoading(false);
    }
  }, []);

  // Initial load
  useEffect(() => {
    setLoading(true);
    loadTasks();
  }, [loadTasks]);

  // Auto-refresh every 5 s when any task is active
  useEffect(() => {
    if (autoRefreshTimer.current) clearInterval(autoRefreshTimer.current);
    if (hasActiveTask(tasks)) {
      autoRefreshTimer.current = setInterval(() => {
        loadTasks();
        // Also refresh events for expanded task if it's active
        if (expandedId) {
          const t = tasks.find((x) => x.id === expandedId);
          if (t && !isTerminal(t.status)) {
            loadEvents(expandedId);
          }
        }
      }, 5000);
    }
    return () => {
      if (autoRefreshTimer.current) clearInterval(autoRefreshTimer.current);
    };
  // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [tasks, expandedId]);

  const loadEvents = useCallback(async (taskId: string): Promise<void> => {
    setEventsMap((prev) => ({
      ...prev,
      [taskId]: { loading: true, events: prev[taskId]?.events ?? [] },
    }));
    try {
      const events = await getTaskEvents(taskId);
      setEventsMap((prev) => ({
        ...prev,
        [taskId]: { loading: false, events },
      }));
    } catch (err) {
      console.error("[Tasks] events load error:", err);
      setEventsMap((prev) => ({
        ...prev,
        [taskId]: { loading: false, events: prev[taskId]?.events ?? [] },
      }));
    }
  }, []);

  const handleToggle = useCallback(
    (taskId: string): void => {
      setExpandedId((prev) => {
        const next = prev === taskId ? null : taskId;
        if (next && !eventsMap[next]) {
          loadEvents(next);
        }
        return next;
      });
    },
    [eventsMap, loadEvents],
  );

  const handlePause = useCallback(
    async (taskId: string): Promise<void> => {
      try {
        await pauseTask(taskId);
        await loadTasks();
      } catch (err) {
        console.error("[Tasks] pause error:", err);
      }
    },
    [loadTasks],
  );

  const handleResume = useCallback(
    async (taskId: string): Promise<void> => {
      try {
        await resumeTask(taskId);
        await loadTasks();
      } catch (err) {
        console.error("[Tasks] resume error:", err);
      }
    },
    [loadTasks],
  );

  const performCancel = useCallback(
    async (taskId: string): Promise<void> => {
      try {
        await cancelTask(taskId);
        await loadTasks();
      } catch (err) {
        console.error("[Tasks] cancel error:", err);
      }
    },
    [loadTasks],
  );

  const handleCancel = useCallback(
    (task: Task): void => {
      setPendingCancelTask(task);
    },
    [],
  );

  const grouped = groupTasks(tasks);

  return (
    <div className="tasks-container">
      {/* Header */}
      <div className="tasks-header">
        <h2 className="tasks-title">Tasks</h2>
        <button
          className="btn"
          onClick={() => {
            setLoading(true);
            loadTasks();
          }}
        >
          <RefreshCw size={14} />
          Refresh
        </button>
      </div>

      {/* Content */}
      {loading ? (
        <div className="tasks-loading">
          <div className="loading-spinner" />
        </div>
      ) : tasks.length === 0 ? (
        <div className="tasks-empty">
          <ListChecks size={32} className="tasks-empty-icon" />
          <p className="tasks-empty-text">No tasks yet</p>
          <p className="tasks-empty-hint">
            Tasks are created automatically when the agent runs multi-step plans
          </p>
        </div>
      ) : (
        <div className="tasks-list">
          {grouped.map((group) => (
            <div key={group.label} className="tasks-group">
              <div className="tasks-group-label">{group.label}</div>
              {group.tasks.map((t) => (
                <TaskCard
                  key={t.id}
                  task={t}
                  expanded={expandedId === t.id}
                  events={eventsMap[t.id]?.events ?? []}
                  eventsLoading={eventsMap[t.id]?.loading ?? false}
                  onToggle={() => handleToggle(t.id)}
                  onPause={() => handlePause(t.id)}
                  onResume={() => handleResume(t.id)}
                  onCancel={() => handleCancel(t)}
                />
              ))}
            </div>
          ))}
        </div>
      )}

      <CancelConfirmDialog
        task={pendingCancelTask}
        onConfirm={() => {
          if (pendingCancelTask) {
            void performCancel(pendingCancelTask.id);
          }
          setPendingCancelTask(null);
        }}
        onCancel={() => setPendingCancelTask(null)}
      />
    </div>
  );
}

export default Tasks;
