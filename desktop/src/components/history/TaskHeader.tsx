import { Clock, ChevronDown, ChevronUp } from "lucide-react";
import { CostSparkline, type SparklinePoint } from "./CostSparkline";
import {
  formatDurationShort,
  taskDurationMs,
  taskHeading,
} from "../../screens/History/historyGrouping";
import type { Task } from "../../api";

interface TaskHeaderProps {
  taskId: string;
  task: Task | null;
  totalCostUsd: number;
  sparkline: SparklinePoint[];
  receiptCount: number;
  expanded: boolean;
  freshestReceiptAt: number;
  onToggle: () => void;
}

function formatCost(usd: number): string {
  if (usd === 0) return "$0";
  if (usd < 0.01) return "<$0.01";
  return `$${usd.toFixed(2)}`;
}

function formatTimestamp(ms: number): string {
  if (!ms) return "";
  const d = new Date(ms);
  return (
    d.toLocaleDateString([], { month: "short", day: "numeric" }) +
    ", " +
    d.toLocaleTimeString([], { hour: "2-digit", minute: "2-digit" })
  );
}

const STATUS_LABEL: Record<string, string> = {
  queued: "queued",
  running: "running",
  paused: "paused",
  zombie: "zombie",
  succeeded: "done",
  failed: "failed",
  cancelled: "cancelled",
};

/**
 * Header row for a task group. Shows the plan-derived name, status badge,
 * total cost, duration, freshest-receipt timestamp, and an inline cost
 * sparkline. Acts as the disclosure toggle for the lanes below.
 */
export function TaskHeader({
  taskId,
  task,
  totalCostUsd,
  sparkline,
  receiptCount,
  expanded,
  freshestReceiptAt,
  onToggle,
}: TaskHeaderProps): React.JSX.Element {
  const heading = taskHeading(taskId, task);
  const statusKey = task?.status ?? "—";
  const statusLabel = STATUS_LABEL[statusKey] ?? statusKey;
  const duration = task ? taskDurationMs(task) : 0;

  return (
    <button
      type="button"
      className="task-group-header"
      onClick={onToggle}
      aria-expanded={expanded}
    >
      <div className="task-group-header-main">
        <span className="task-group-heading">{heading}</span>
        <span className={`task-group-status task-group-status--${statusKey}`}>
          {statusLabel}
        </span>
      </div>
      <div className="task-group-header-meta">
        <span
          className="task-group-cost"
          title={`Total cost across ${receiptCount} action${receiptCount === 1 ? "" : "s"}`}
        >
          {formatCost(totalCostUsd)}
        </span>
        <span
          className="task-group-sparkline"
          aria-hidden={sparkline.length === 0}
        >
          <CostSparkline
            points={sparkline}
            width={70}
            height={18}
            ariaLabel={`Cost over time, total ${formatCost(totalCostUsd)}`}
          />
        </span>
        {task && (
          <span className="task-group-duration">
            <Clock size={11} />
            {formatDurationShort(duration)}
          </span>
        )}
        <span className="task-group-time">
          {formatTimestamp(freshestReceiptAt)}
        </span>
        {expanded ? <ChevronUp size={13} /> : <ChevronDown size={13} />}
      </div>
    </button>
  );
}
