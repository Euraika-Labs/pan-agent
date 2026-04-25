import { describe, expect, it } from "vitest";
import {
  formatDurationShort,
  groupReceiptsByTask,
  laneFor,
  summarizeTaskCost,
  taskDurationMs,
  taskHeading,
} from "./historyGrouping";
import type { ReceiptDTO, Task, TaskEvent } from "../../api";

// Pure-helper tests for the WS#2 task-grouped History layout. These
// cover the math + classification logic that the rendered components
// depend on; the components themselves are mostly mechanical wiring.
// Logic-only style matches the existing budget-helpers test suite —
// no React Testing Library or jsdom infra added.

function rec(
  partial: Partial<ReceiptDTO> & { id: string; taskId: string; createdAt: number },
): ReceiptDTO {
  return {
    kind: "fs_write",
    snapshotTier: "cow",
    reversalStatus: "reversible",
    redactedPayload: "",
    ...partial,
  } as ReceiptDTO;
}

function task(id: string, partial: Partial<Task> = {}): Task {
  return {
    id,
    status: "succeeded",
    session_id: "sess-1",
    created_at: 1000,
    next_plan_step_index: 0,
    token_budget_cap: 0,
    cost_cap_usd: 0,
    ...partial,
  };
}

describe("laneFor", () => {
  it("maps reversible → reversible lane", () => {
    expect(laneFor(rec({ id: "r1", taskId: "t", createdAt: 0, reversalStatus: "reversible" }))).toBe("reversible");
  });

  it("maps every non-reversible status to audit", () => {
    for (const status of ["audit_only", "reversed_externally", "irrecoverable"] as const) {
      expect(
        laneFor(rec({ id: "x", taskId: "t", createdAt: 0, reversalStatus: status })),
      ).toBe("audit");
    }
  });
});

describe("groupReceiptsByTask", () => {
  it("groups by taskId and sorts within each group newest-first", () => {
    const receipts = [
      rec({ id: "r1", taskId: "ta", createdAt: 100 }),
      rec({ id: "r2", taskId: "tb", createdAt: 200 }),
      rec({ id: "r3", taskId: "ta", createdAt: 300 }),
      rec({ id: "r4", taskId: "tb", createdAt: 50 }),
    ];
    const groups = groupReceiptsByTask(receipts, new Map());
    expect(groups.map((g) => g.taskId)).toEqual(["ta", "tb"]);
    expect(groups[0].receipts.map((r) => r.id)).toEqual(["r3", "r1"]);
    expect(groups[1].receipts.map((r) => r.id)).toEqual(["r2", "r4"]);
  });

  it("sorts groups newest-first by freshest receipt", () => {
    // ta has receipts at ts=100,300; tb at ts=400. tb should sort first.
    const receipts = [
      rec({ id: "a", taskId: "ta", createdAt: 100 }),
      rec({ id: "b", taskId: "ta", createdAt: 300 }),
      rec({ id: "c", taskId: "tb", createdAt: 400 }),
    ];
    const groups = groupReceiptsByTask(receipts, new Map());
    expect(groups[0].taskId).toBe("tb");
    expect(groups[1].taskId).toBe("ta");
  });

  it("splits each group into reversible + audit lanes", () => {
    const receipts = [
      rec({ id: "r1", taskId: "t", createdAt: 100, reversalStatus: "reversible" }),
      rec({ id: "r2", taskId: "t", createdAt: 200, reversalStatus: "audit_only" }),
      rec({ id: "r3", taskId: "t", createdAt: 300, reversalStatus: "irrecoverable" }),
    ];
    const [g] = groupReceiptsByTask(receipts, new Map());
    expect(g.reversible.map((r) => r.id)).toEqual(["r1"]);
    expect(g.audit.map((r) => r.id)).toEqual(["r3", "r2"]);
  });

  it("attaches Task records when present in the lookup map", () => {
    const t = task("t1", { plan_json: "do the thing" });
    const groups = groupReceiptsByTask(
      [rec({ id: "r1", taskId: "t1", createdAt: 100 })],
      new Map([["t1", t]]),
    );
    expect(groups[0].task).toBe(t);
  });

  it("falls back to null when a task record is missing", () => {
    const groups = groupReceiptsByTask(
      [rec({ id: "r1", taskId: "missing", createdAt: 100 })],
      new Map(),
    );
    expect(groups[0].task).toBeNull();
  });

  it("buckets receipts with empty taskId under a synthetic group", () => {
    // Receipts that somehow lack a taskId still need to render somewhere.
    const groups = groupReceiptsByTask(
      [{ ...rec({ id: "r", taskId: "", createdAt: 100 }) }],
      new Map(),
    );
    expect(groups).toHaveLength(1);
    expect(groups[0].taskId).toBe("");
  });
});

describe("summarizeTaskCost", () => {
  function ev(seq: number, ts: number, payload: object | null, kind: TaskEvent["kind"] = "cost"): TaskEvent {
    return {
      id: seq,
      task_id: "t",
      step_id: "s",
      attempt: 1,
      sequence: seq,
      kind,
      payload_json: payload === null ? undefined : JSON.stringify(payload),
      created_at: ts,
    };
  }

  it("sums delta_usd payloads in chronological order", () => {
    const summary = summarizeTaskCost([
      ev(1, 200, { delta_usd: 0.02 }),
      ev(2, 100, { delta_usd: 0.01 }),
      ev(3, 300, { delta_usd: 0.04 }),
    ]);
    expect(summary.totalCostUsd).toBeCloseTo(0.07, 4);
    expect(summary.sparkline.map((p) => p.usd)).toEqual([0.01, 0.03, 0.07]);
    expect(summary.sparkline.map((p) => p.ts)).toEqual([100, 200, 300]);
  });

  it("ignores non-cost events", () => {
    const summary = summarizeTaskCost([
      ev(1, 100, { delta_usd: 0.01 }, "cost"),
      ev(2, 150, null, "heartbeat"),
      ev(3, 200, { something: "else" }, "step_completed"),
    ]);
    expect(summary.sparkline).toHaveLength(1);
  });

  it("converts an absolute cost_used_usd reading into a delta", () => {
    const summary = summarizeTaskCost([
      ev(1, 100, { delta_usd: 0.02 }),
      ev(2, 200, { cost_used_usd: 0.05 }),
    ]);
    expect(summary.totalCostUsd).toBeCloseTo(0.05, 4);
  });

  it("tolerates malformed payloads", () => {
    const summary = summarizeTaskCost([
      ev(1, 100, { delta_usd: 0.02 }),
      { ...ev(2, 200, null), payload_json: "{not json" },
      ev(3, 300, { delta_usd: 0.01 }),
    ]);
    expect(summary.totalCostUsd).toBeCloseTo(0.03, 4);
  });

  it("returns zero totals when no cost events exist", () => {
    const summary = summarizeTaskCost([]);
    expect(summary.totalCostUsd).toBe(0);
    expect(summary.sparkline).toEqual([]);
  });
});

describe("taskDurationMs", () => {
  it("uses last_heartbeat_at when present", () => {
    expect(taskDurationMs(task("t", { created_at: 1000, last_heartbeat_at: 4000 }))).toBe(3000);
  });

  it("falls back to now when no heartbeat has happened yet", () => {
    expect(
      taskDurationMs(task("t", { created_at: 1000, last_heartbeat_at: undefined }), 5000),
    ).toBe(4000);
  });

  it("never returns negative durations even on clock skew", () => {
    expect(
      taskDurationMs(task("t", { created_at: 4000, last_heartbeat_at: 1000 })),
    ).toBe(0);
  });
});

describe("taskHeading", () => {
  it("uses the first line of plan_json (truncated to 80 chars)", () => {
    const t = task("ta", { plan_json: "First line\nDetails here" });
    expect(taskHeading("ta", t)).toBe("First line");
  });

  it("truncates long single-line plans", () => {
    const long = "x".repeat(200);
    const t = task("ta", { plan_json: long });
    expect(taskHeading("ta", t)).toHaveLength(80);
  });

  it("falls back to task-<short-id> when no plan", () => {
    expect(taskHeading("abcdef1234567890", null)).toBe("task-abcdef12");
  });

  it("renders the synthetic empty bucket distinctly", () => {
    expect(taskHeading("", null)).toBe("(uncategorized actions)");
  });
});

describe("formatDurationShort", () => {
  it("under a second renders as <1s", () => {
    expect(formatDurationShort(0)).toBe("<1s");
    expect(formatDurationShort(999)).toBe("<1s");
  });

  it("seconds-only when below 60s", () => {
    expect(formatDurationShort(12_000)).toBe("12s");
  });

  it("minutes only when seconds remainder is zero", () => {
    expect(formatDurationShort(180_000)).toBe("3m");
  });

  it("minutes+seconds when both are non-zero", () => {
    expect(formatDurationShort(64_000)).toBe("1m 4s");
  });

  it("hours+minutes when above an hour", () => {
    expect(formatDurationShort(8_040_000)).toBe("2h 14m");
  });

  it("hours-only when minutes remainder is zero", () => {
    expect(formatDurationShort(7_200_000)).toBe("2h");
  });
});
