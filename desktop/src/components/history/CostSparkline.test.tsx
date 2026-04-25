import { describe, expect, it } from "vitest";
import { scalePoints, type SparklinePoint } from "./CostSparkline";

// Unit tests for the pure point-scaling helper that drives CostSparkline.
// The component itself emits an SVG that's mostly mechanical formatting —
// the math worth covering is here so the chart logic can evolve without
// pulling in jsdom + RTK to assert on rendered <polyline points="…"/>.

describe("scalePoints", () => {
  it("returns empty for empty input", () => {
    expect(scalePoints([], 100, 20)).toEqual([]);
  });

  it("centers a single point at half-width / half-height", () => {
    expect(scalePoints([{ ts: 5, usd: 0.5 }], 80, 20)).toEqual([
      { x: 40, y: 10 },
    ]);
  });

  it("scales two points across the full width", () => {
    const out = scalePoints(
      [
        { ts: 0, usd: 0 },
        { ts: 10, usd: 1 },
      ],
      100,
      20,
    );
    expect(out).toHaveLength(2);
    expect(out[0].x).toBe(0);
    expect(out[1].x).toBe(100);
  });

  it("anchors the cost axis at zero so cheap series stay near baseline", () => {
    // Two-point series: usd=0 at t0, usd=10 at t1.
    // The 0-cost point must land at the bottom of the viewBox (y near
    // height-1), not at the top — anchoring to 0 (not to series min)
    // keeps a $0.01 task visually distinct from a $5 task.
    const out = scalePoints(
      [
        { ts: 0, usd: 0 },
        { ts: 10, usd: 10 },
      ],
      100,
      20,
    );
    // y0 should be near the bottom (height - padding = 19), y1 near top (1)
    expect(out[0].y).toBeGreaterThan(15);
    expect(out[1].y).toBeLessThan(5);
  });

  it("flat zero-cost series collapses to the baseline", () => {
    // Both points have usd=0. With the cost axis anchored to 0 they
    // should sit at the bottom (padding + innerH = height - 1) rather
    // than mid-height — a $0 task should look like a flat baseline.
    const out = scalePoints(
      [
        { ts: 0, usd: 0 },
        { ts: 10, usd: 0 },
      ],
      100,
      20,
    );
    expect(out).toHaveLength(2);
    expect(out[0].y).toBe(19);
    expect(out[1].y).toBe(19);
  });

  it("flat positive-cost series sits at the top (both points are the max)", () => {
    // usdMin is anchored to 0, so a series at constant usd=1 has both
    // points at the top of the viewBox — they ARE the max value.
    const out = scalePoints(
      [
        { ts: 0, usd: 1 },
        { ts: 10, usd: 1 },
      ],
      100,
      20,
    );
    expect(out[0].y).toBe(1);
    expect(out[1].y).toBe(1);
  });

  it("handles flat ts values (instantaneous sequence)", () => {
    const out = scalePoints(
      [
        { ts: 1000, usd: 1 },
        { ts: 1000, usd: 2 },
      ],
      100,
      20,
    );
    // tsSpan=0 → all x at width/2
    expect(out[0].x).toBe(50);
    expect(out[1].x).toBe(50);
  });

  it("preserves point order (chart reads left-to-right)", () => {
    const points: SparklinePoint[] = [
      { ts: 1, usd: 0.1 },
      { ts: 5, usd: 0.5 },
      { ts: 9, usd: 0.9 },
    ];
    const out = scalePoints(points, 100, 20);
    expect(out[0].x).toBeLessThan(out[1].x);
    expect(out[1].x).toBeLessThan(out[2].x);
  });

  it("pads top/bottom by 1px so strokes don't clip", () => {
    // Max-usd point should be at y=padding (1), not y=0.
    const out = scalePoints(
      [
        { ts: 0, usd: 0 },
        { ts: 10, usd: 1 },
      ],
      100,
      20,
    );
    expect(out[1].y).toBe(1);
  });
});
