import { describe, expect, it } from "vitest";
import {
  costState,
  formatCostLabel,
  formatLastHeartbeat,
  formatTaskDuration,
  getDateGroup,
  groupTasks,
  hasActiveTask,
  isTerminal,
  truncate,
} from "./tasksGrouping";
import type { Task, TaskStatus } from "../../api";

// Pure-helper tests for the WS#13.A Tasks screen polish. Logic-only style
// matches the existing layout.shortcuts / budget-helpers / historyGrouping
// suites — no React Testing Library, no jsdom.

function task(id: string, partial: Partial<Task> = {}): Task {
  return {
    id,
    status: "queued",
    session_id: "sess-1",
    created_at: 1_700_000_000,
    next_plan_step_index: 0,
    token_budget_cap: 0,
    cost_cap_usd: 0,
    ...partial,
  };
}

describe("isTerminal", () => {
  it("succeeded / failed / cancelled are terminal", () => {
    expect(isTerminal("succeeded")).toBe(true);
    expect(isTerminal("failed")).toBe(true);
    expect(isTerminal("cancelled")).toBe(true);
  });

  it("queued / running / paused / zombie are non-terminal", () => {
    const states: TaskStatus[] = ["queued", "running", "paused", "zombie"];
    for (const s of states) {
      expect(isTerminal(s)).toBe(false);
    }
  });
});

describe("hasActiveTask", () => {
  it("true when at least one task is queued or running", () => {
    expect(hasActiveTask([task("a", { status: "running" })])).toBe(true);
    expect(hasActiveTask([task("a", { status: "queued" })])).toBe(true);
  });

  it("false when only paused / zombie / terminal", () => {
    expect(hasActiveTask([task("a", { status: "paused" })])).toBe(false);
    expect(hasActiveTask([task("a", { status: "zombie" })])).toBe(false);
    expect(hasActiveTask([task("a", { status: "succeeded" })])).toBe(false);
    expect(hasActiveTask([])).toBe(false);
  });

  it("a single active task in a terminal-heavy list still triggers true", () => {
    expect(
      hasActiveTask([
        task("a", { status: "succeeded" }),
        task("b", { status: "running" }),
        task("c", { status: "failed" }),
      ]),
    ).toBe(true);
  });
});

describe("getDateGroup", () => {
  // getDateGroup compares local-time year/month/day (so users see
  // Today/Yesterday relative to their wall clock, not UTC). All
  // fixtures use the local-time `new Date(y, m, d, ...)` constructor
  // so the suite passes regardless of the runner's timezone.
  const NOW_MS = new Date(2026, 3, 26, 12, 0, 0).getTime(); // 2026-04-26 12:00 local

  it("returns Today for a same-day timestamp", () => {
    const morning = Math.floor(new Date(2026, 3, 26, 9, 0, 0).getTime() / 1000);
    expect(getDateGroup(morning, NOW_MS)).toBe("Today");
  });

  it("returns Yesterday for a yesterday timestamp", () => {
    const yesterdayNoon = Math.floor(
      new Date(2026, 3, 25, 12, 0, 0).getTime() / 1000,
    );
    expect(getDateGroup(yesterdayNoon, NOW_MS)).toBe("Yesterday");
  });

  it("returns Earlier for older timestamps", () => {
    const lastWeek = Math.floor(
      new Date(2026, 3, 19, 12, 0, 0).getTime() / 1000,
    );
    expect(getDateGroup(lastWeek, NOW_MS)).toBe("Earlier");
  });

  it("midnight crossing — 23:59 yesterday vs 00:01 today (local)", () => {
    const yesterday2359 = Math.floor(
      new Date(2026, 3, 25, 23, 59, 0).getTime() / 1000,
    );
    const today0001 = Math.floor(
      new Date(2026, 3, 26, 0, 1, 0).getTime() / 1000,
    );
    expect(getDateGroup(yesterday2359, NOW_MS)).toBe("Yesterday");
    expect(getDateGroup(today0001, NOW_MS)).toBe("Today");
  });
});

describe("groupTasks", () => {
  const NOW_MS = new Date(2026, 3, 26, 12, 0, 0).getTime();
  const todayTs = Math.floor(new Date(2026, 3, 26, 11, 0, 0).getTime() / 1000);
  const yesterdayTs = Math.floor(
    new Date(2026, 3, 25, 12, 0, 0).getTime() / 1000,
  );
  const earlierTs = Math.floor(
    new Date(2026, 3, 19, 12, 0, 0).getTime() / 1000,
  );

  it("buckets in canonical order Today → Yesterday → Earlier", () => {
    const groups = groupTasks(
      [
        task("e", { created_at: earlierTs }),
        task("y", { created_at: yesterdayTs }),
        task("t", { created_at: todayTs }),
      ],
      NOW_MS,
    );
    expect(groups.map((g) => g.label)).toEqual(["Today", "Yesterday", "Earlier"]);
  });

  it("preserves task order within each bucket (caller-newest-first)", () => {
    const groups = groupTasks(
      [
        task("t1", { created_at: todayTs }),
        task("t2", { created_at: todayTs - 60 }),
      ],
      NOW_MS,
    );
    expect(groups[0].tasks.map((t) => t.id)).toEqual(["t1", "t2"]);
  });

  it("drops empty buckets rather than emitting empty headers", () => {
    const groups = groupTasks(
      [task("t", { created_at: todayTs })],
      NOW_MS,
    );
    expect(groups).toHaveLength(1);
    expect(groups[0].label).toBe("Today");
  });

  it("returns empty array for empty input", () => {
    expect(groupTasks([], NOW_MS)).toEqual([]);
  });
});

describe("truncate", () => {
  it("returns input unchanged when short enough", () => {
    expect(truncate("hello", 10)).toBe("hello");
  });

  it("appends a Unicode ellipsis when over budget", () => {
    expect(truncate("hello world", 5)).toBe("hello…");
  });

  it("respects a tight budget", () => {
    expect(truncate("abcdef", 3)).toBe("abc…");
  });
});

describe("formatTaskDuration", () => {
  it("under a second renders as <1s", () => {
    expect(formatTaskDuration(100, 100, 100)).toBe("<1s");
  });

  it("seconds-only when below 60s", () => {
    expect(formatTaskDuration(100, 142, 142)).toBe("42s");
  });

  it("minutes only when seconds remainder is zero", () => {
    expect(formatTaskDuration(0, 180, 180)).toBe("3m");
  });

  it("minutes+seconds when both non-zero", () => {
    expect(formatTaskDuration(0, 64, 64)).toBe("1m 4s");
  });

  it("hours+minutes when above an hour", () => {
    expect(formatTaskDuration(0, 8040, 8040)).toBe("2h 14m");
  });

  it("falls back to now when no heartbeat", () => {
    expect(formatTaskDuration(100, null, 200)).toBe("1m 40s");
    expect(formatTaskDuration(100, undefined, 200)).toBe("1m 40s");
  });

  it("never returns negative durations on clock skew", () => {
    expect(formatTaskDuration(200, 100, 100)).toBe("<1s");
  });
});

describe("formatLastHeartbeat", () => {
  it("returns 'never' when heartbeat is null/undefined", () => {
    expect(formatLastHeartbeat(null, 100)).toBe("never");
    expect(formatLastHeartbeat(undefined, 100)).toBe("never");
  });

  it("renders 'just now' inside 5 seconds", () => {
    expect(formatLastHeartbeat(98, 100)).toBe("just now");
    expect(formatLastHeartbeat(100, 100)).toBe("just now");
  });

  it("seconds within a minute", () => {
    expect(formatLastHeartbeat(70, 100)).toBe("30s ago");
  });

  it("minutes within an hour", () => {
    expect(formatLastHeartbeat(0, 600)).toBe("10m ago");
  });

  it("hours within a day", () => {
    expect(formatLastHeartbeat(0, 7200)).toBe("2h ago");
  });

  it("yesterday for ~1 day", () => {
    expect(formatLastHeartbeat(0, 86400)).toBe("yesterday");
  });

  it("days for older heartbeats", () => {
    expect(formatLastHeartbeat(0, 86400 * 5)).toBe("5d ago");
  });

  it("future timestamps don't crash", () => {
    expect(formatLastHeartbeat(1000, 100)).toBe("in the future");
  });
});

describe("costState", () => {
  it("returns 'none' when no cap configured", () => {
    expect(costState(0, 0)).toBe("none");
    expect(costState(1, 0)).toBe("none");
  });

  it("returns 'ok' below 80% of cap", () => {
    expect(costState(0, 1)).toBe("ok");
    expect(costState(0.79, 1)).toBe("ok");
  });

  it("returns 'warn' from 80% up to 100%", () => {
    expect(costState(0.8, 1)).toBe("warn");
    expect(costState(0.99, 1)).toBe("warn");
  });

  it("returns 'exceeded' at or above 100%", () => {
    expect(costState(1, 1)).toBe("exceeded");
    expect(costState(2, 1)).toBe("exceeded");
  });
});

describe("formatCostLabel", () => {
  it("uncapped form when cap=0", () => {
    expect(formatCostLabel(0.04, 0)).toBe("$0.04");
  });

  it("capped form when cap>0", () => {
    expect(formatCostLabel(0.04, 1.5)).toBe("$0.04 / $1.50");
  });

  it("rounds to two decimals", () => {
    expect(formatCostLabel(0.123, 0)).toBe("$0.12");
    expect(formatCostLabel(0.005, 0)).toBe("$0.01");
  });
});
