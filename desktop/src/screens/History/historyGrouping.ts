/**
 * Pure helpers for the WS#2 task-grouped History layout.
 *
 * The History screen takes a flat list of `ReceiptDTO` and renders one
 * `<TaskGroup>` per distinct `taskId`. Each group has a header (name,
 * status, total cost, duration, timestamp, sparkline) and two lanes
 * (REVERSIBLE / AUDIT-ONLY) that hold the receipts.
 *
 * The grouping math + summary derivation lives here so it can be tested
 * without spinning up the React tree (see historyGrouping.test.ts).
 */
import type { ReceiptDTO, Task, TaskEvent } from "../../api";
import type { SparklinePoint } from "../../components/history/CostSparkline";

export type ReceiptLane = "reversible" | "audit";

/**
 * Map a receipt's `reversalStatus` onto one of two desktop lanes.
 *
 *   reversible            → REVERSIBLE (Undo button enabled)
 *   audit_only            ┐
 *   reversed_externally   ┤→ AUDIT-ONLY (read-only)
 *   irrecoverable         ┘
 */
export function laneFor(receipt: ReceiptDTO): ReceiptLane {
  return receipt.reversalStatus === "reversible" ? "reversible" : "audit";
}

/**
 * Summary numbers derived from a task's `task_events`. Only `kind="cost"`
 * events contribute to `totalCostUsd`; the sparkline uses every cost
 * event in chronological order.
 *
 * The cost payload encoded in `payload_json` is shaped
 *   { "delta_usd": 0.0034 }
 * but malformed / legacy events are tolerated by simply ignoring them.
 */
export interface TaskCostSummary {
  totalCostUsd: number;
  sparkline: SparklinePoint[];
}

interface CostPayload {
  delta_usd?: number;
  /** Absolute reading some events emit instead of a delta. */
  cost_used_usd?: number;
}

export function summarizeTaskCost(events: TaskEvent[]): TaskCostSummary {
  const sparkline: SparklinePoint[] = [];
  let total = 0;
  // Sort events chronologically — the events endpoint returns
  // newest-first by default; sparkline reads left-to-right (old→new).
  const sorted = [...events].sort((a, b) => a.created_at - b.created_at);
  for (const ev of sorted) {
    if (ev.kind !== "cost") continue;
    let delta = 0;
    if (ev.payload_json) {
      try {
        const p = JSON.parse(ev.payload_json) as CostPayload;
        if (typeof p.delta_usd === "number" && Number.isFinite(p.delta_usd)) {
          delta = p.delta_usd;
        } else if (
          typeof p.cost_used_usd === "number" &&
          Number.isFinite(p.cost_used_usd)
        ) {
          // Absolute reading: convert to delta against the running total.
          delta = Math.max(0, p.cost_used_usd - total);
        }
      } catch {
        // Ignore malformed payloads.
      }
    }
    total += delta;
    sparkline.push({ ts: ev.created_at, usd: total });
  }
  return { totalCostUsd: total, sparkline };
}

/**
 * Duration in milliseconds between a task's creation and its last
 * heartbeat (or now, if it's never beaten — which only happens when a
 * task fails before its first beat, per the spec). Returns null if
 * the task hasn't been seen at all.
 */
export function taskDurationMs(task: Task, nowMs: number = Date.now()): number {
  const start = task.created_at;
  const end = task.last_heartbeat_at ?? nowMs;
  return Math.max(0, end - start);
}

export interface TaskGroupData {
  task: Task | null;
  taskId: string;
  receipts: ReceiptDTO[];
  reversible: ReceiptDTO[];
  audit: ReceiptDTO[];
}

/**
 * Group an unsorted, unscoped flat receipt list into per-task buckets.
 * Receipts within each group are kept newest-first; groups themselves
 * sort newest-first by their freshest receipt's `createdAt`.
 *
 * `tasks` may be partial — missing tasks render with a "task-<short-id>"
 * placeholder header. Receipts with no `taskId` (defensive) fall under
 * a synthetic empty-string group.
 */
export function groupReceiptsByTask(
  receipts: ReceiptDTO[],
  tasks: Map<string, Task>,
): TaskGroupData[] {
  const buckets = new Map<string, ReceiptDTO[]>();
  for (const r of receipts) {
    const key = r.taskId ?? "";
    const bucket = buckets.get(key);
    if (bucket) bucket.push(r);
    else buckets.set(key, [r]);
  }

  const groups: TaskGroupData[] = [];
  for (const [taskId, list] of buckets.entries()) {
    list.sort((a, b) => b.createdAt - a.createdAt);
    groups.push({
      taskId,
      task: tasks.get(taskId) ?? null,
      receipts: list,
      reversible: list.filter((r) => laneFor(r) === "reversible"),
      audit: list.filter((r) => laneFor(r) === "audit"),
    });
  }

  // Sort groups newest-first by their freshest receipt.
  groups.sort((a, b) => {
    const aTs = a.receipts[0]?.createdAt ?? 0;
    const bTs = b.receipts[0]?.createdAt ?? 0;
    return bTs - aTs;
  });

  return groups;
}

/**
 * Derive a human-readable name for a task. Falls back to a short ID
 * stub when the task record is missing or has no plan.
 */
export function taskHeading(taskId: string, task: Task | null): string {
  if (task?.plan_json) {
    const firstLine = task.plan_json.split("\n")[0]?.trim();
    if (firstLine) return firstLine.slice(0, 80);
  }
  if (taskId === "") return "(uncategorized actions)";
  return `task-${taskId.slice(0, 8)}`;
}

/** Compact ms → "12s" / "1m 4s" / "2h 14m" formatter for the header. */
export function formatDurationShort(ms: number): string {
  if (ms < 1000) return "<1s";
  const s = Math.floor(ms / 1000);
  if (s < 60) return `${s}s`;
  const m = Math.floor(s / 60);
  const sr = s % 60;
  if (m < 60) return sr ? `${m}m ${sr}s` : `${m}m`;
  const h = Math.floor(m / 60);
  const mr = m % 60;
  return mr ? `${h}h ${mr}m` : `${h}h`;
}
