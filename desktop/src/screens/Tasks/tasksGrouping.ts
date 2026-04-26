/**
 * Pure helpers for the Tasks screen — extracted from Tasks.tsx so the
 * grouping/status/duration math is unit-testable without spinning up
 * the React tree (matches the existing pattern in
 * `desktop/src/screens/History/historyGrouping.ts` introduced in v0.6.0).
 *
 * Phase 13 WS#13.A polish: surfaces a cost-vs-cap pill, last-heartbeat
 * info, and a duration formatter on each task row. The cost-summary
 * helper (`summarizeTaskCost`) lives in `historyGrouping` and is
 * imported there — same task_events shape, same payload semantics.
 */
import type { Task, TaskStatus } from "../../api";

/** Date bucket the row falls under in the list header. */
export type DateGroup = "Today" | "Yesterday" | "Earlier";

const TERMINAL_STATUSES: ReadonlyArray<TaskStatus> = [
  "succeeded",
  "failed",
  "cancelled",
];

/**
 * True when the task is in a terminal state. Pause/Resume/Cancel
 * buttons are gated on this.
 */
export function isTerminal(status: TaskStatus): boolean {
  return TERMINAL_STATUSES.includes(status);
}

/**
 * True when at least one task in the list is actively making progress.
 * Drives the 5 s auto-refresh: idle lists don't poll the backend.
 *
 * `running` and `queued` are the only states the runner advances on
 * its own — `paused` waits for human input, `zombie` was already
 * detected by the reaper, and the three terminal states are immutable.
 */
export function hasActiveTask(tasks: ReadonlyArray<Task>): boolean {
  return tasks.some((t) => t.status === "running" || t.status === "queued");
}

/**
 * Compute the "Today / Yesterday / Earlier" bucket for an epoch-second
 * timestamp. `nowMs` is injectable so tests can pin a clock; default is
 * `Date.now()` so callers don't need to thread one through.
 */
export function getDateGroup(tsSeconds: number, nowMs: number = Date.now()): DateGroup {
  const date = new Date(tsSeconds * 1000);
  const now = new Date(nowMs);

  const sameDay = (a: Date, b: Date): boolean =>
    a.getDate() === b.getDate() &&
    a.getMonth() === b.getMonth() &&
    a.getFullYear() === b.getFullYear();

  if (sameDay(date, now)) return "Today";

  const yesterday = new Date(now);
  yesterday.setDate(yesterday.getDate() - 1);
  if (sameDay(date, yesterday)) return "Yesterday";

  return "Earlier";
}

/**
 * Bucket a (newest-first) task list into ordered date groups.
 * Empty buckets are dropped from the output rather than rendered as
 * empty headers.
 */
export function groupTasks(
  tasks: ReadonlyArray<Task>,
  nowMs: number = Date.now(),
): Array<{ label: DateGroup; tasks: Task[] }> {
  const groups = new Map<DateGroup, Task[]>();
  for (const t of tasks) {
    const group = getDateGroup(t.created_at, nowMs);
    const bucket = groups.get(group);
    if (bucket) bucket.push(t);
    else groups.set(group, [t]);
  }
  const order: DateGroup[] = ["Today", "Yesterday", "Earlier"];
  return order
    .filter((label) => groups.has(label))
    .map((label) => ({ label, tasks: groups.get(label)! }));
}

/**
 * Truncate a string with a Unicode-safe ellipsis. Mirrors the inline
 * helper that Tasks.tsx used; extracted for reuse + testability.
 */
export function truncate(s: string, n: number): string {
  if (s.length <= n) return s;
  return s.slice(0, n) + "…";
}

/**
 * Format the task duration as a compact human string.
 * Inputs are epoch-second timestamps (matches the Task struct fields
 * `created_at` and `last_heartbeat_at`); pass `null`/`undefined` for
 * a never-beaten task and the duration runs from `created_at` to now.
 *
 *   "3s" | "12s" | "1m 4s" | "2h 14m"
 *
 * Mirrors `formatDurationShort` in historyGrouping; we keep a separate
 * implementation here to avoid cross-screen imports for a 10-line fn.
 */
export function formatTaskDuration(
  createdAtSeconds: number,
  lastHeartbeatSeconds: number | null | undefined,
  nowSeconds: number = Math.floor(Date.now() / 1000),
): string {
  const end = lastHeartbeatSeconds ?? nowSeconds;
  const elapsed = Math.max(0, end - createdAtSeconds);
  if (elapsed < 1) return "<1s";
  if (elapsed < 60) return `${elapsed}s`;
  const m = Math.floor(elapsed / 60);
  const s = elapsed % 60;
  if (m < 60) return s ? `${m}m ${s}s` : `${m}m`;
  const h = Math.floor(m / 60);
  const mr = m % 60;
  return mr ? `${h}h ${mr}m` : `${h}h`;
}

/**
 * Format the "last heartbeat" relative time for the row meta.
 * Matches Slack/Linear-style relative timestamps so the row shows
 * "2m ago" rather than a full ISO timestamp.
 *
 *   "just now" | "12s ago" | "3m ago" | "2h ago" | "yesterday" | "<formatted date>"
 *
 * Only meaningful for non-terminal tasks; terminal tasks should show
 * the absolute ended-at timestamp instead.
 */
export function formatLastHeartbeat(
  heartbeatSeconds: number | null | undefined,
  nowSeconds: number = Math.floor(Date.now() / 1000),
): string {
  if (heartbeatSeconds == null) return "never";
  const elapsed = nowSeconds - heartbeatSeconds;
  if (elapsed < 0) return "in the future";
  if (elapsed < 5) return "just now";
  if (elapsed < 60) return `${elapsed}s ago`;
  const m = Math.floor(elapsed / 60);
  if (m < 60) return `${m}m ago`;
  const h = Math.floor(m / 60);
  if (h < 24) return `${h}h ago`;
  const d = Math.floor(h / 24);
  if (d === 1) return "yesterday";
  return `${d}d ago`;
}

/**
 * Cost-vs-cap state used by the cost pill.
 *   - "none" — no cap configured (cost_cap_usd === 0)
 *   - "ok" — under 80% of cap
 *   - "warn" — 80–99% of cap
 *   - "exceeded" — at or over 100% of cap
 *
 * Mirrors the `pillVariant` semantics from `desktop/src/components/budget-helpers.ts`
 * but operates on the Task struct's `cost_cap_usd` (which doesn't yet
 * have a sibling `cost_used_usd` on the Task DTO — we derive it from
 * the task_events stream via `summarizeTaskCost`).
 */
export type CostState = "none" | "ok" | "warn" | "exceeded";

export function costState(used: number, cap: number): CostState {
  if (cap <= 0) return "none";
  const ratio = used / cap;
  if (ratio >= 1) return "exceeded";
  if (ratio >= 0.8) return "warn";
  return "ok";
}

/**
 * Render the cost label shown in the pill. Cap=0 produces the
 * un-capped form ("$0.04") so the row doesn't read like a 0-cap
 * exhausted task.
 */
export function formatCostLabel(used: number, cap: number): string {
  const u = used.toFixed(2);
  if (cap <= 0) return `$${u}`;
  return `$${u} / $${cap.toFixed(2)}`;
}
