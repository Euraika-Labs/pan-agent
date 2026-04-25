import { describe, expect, it } from "vitest";
import { pillVariant, pillClass, parseCustomCap } from "./budget-helpers";

// Unit tests for the pure budget-pill helpers. These match the WS#1
// (Phase 12) cost-pill contract documented in docs/design/phase12.md:
// amber at 80% of cap, red at 100%, no decoration when no cap is set.
// Heavier component-level tests would need React Testing Library +
// jsdom; the existing desktop/src/screens/Layout suite is logic-only,
// so this file follows the same convention.

describe("pillVariant", () => {
  it("returns base when no cap is set", () => {
    expect(pillVariant(0, 0)).toBe("base");
    expect(pillVariant(99, 0)).toBe("base");
  });

  it("returns base for negative cap (paranoia)", () => {
    expect(pillVariant(0.5, -1)).toBe("base");
  });

  it("returns base below 80% of cap", () => {
    expect(pillVariant(0, 1)).toBe("base");
    expect(pillVariant(0.5, 1)).toBe("base");
    expect(pillVariant(0.79, 1)).toBe("base");
  });

  it("returns warning at exactly 80% of cap", () => {
    expect(pillVariant(0.8, 1)).toBe("warning");
  });

  it("returns warning between 80% and 100%", () => {
    expect(pillVariant(0.99, 1)).toBe("warning");
  });

  it("returns exceeded at exactly 100%", () => {
    expect(pillVariant(1, 1)).toBe("exceeded");
  });

  it("returns exceeded above 100%", () => {
    expect(pillVariant(2, 1)).toBe("exceeded");
  });
});

describe("pillClass", () => {
  it("emits only the base class when no cap", () => {
    expect(pillClass(0, 0)).toBe("cost-pill");
  });

  it("emits the warning modifier at 80%", () => {
    expect(pillClass(0.8, 1)).toBe("cost-pill cost-pill--warning");
  });

  it("emits the exceeded modifier at 100%", () => {
    expect(pillClass(1, 1)).toBe("cost-pill cost-pill--exceeded");
  });
});

describe("parseCustomCap", () => {
  it("parses a normal positive number", () => {
    expect(parseCustomCap("5")).toBe(5);
    expect(parseCustomCap("12.34")).toBe(12.34);
  });

  it("trims surrounding whitespace", () => {
    expect(parseCustomCap("  3  ")).toBe(3);
  });

  it("returns null on empty input", () => {
    expect(parseCustomCap("")).toBe(null);
    expect(parseCustomCap("   ")).toBe(null);
  });

  it("returns null on non-numeric input", () => {
    expect(parseCustomCap("abc")).toBe(null);
  });

  it("returns null on zero (API rejects 0)", () => {
    expect(parseCustomCap("0")).toBe(null);
    expect(parseCustomCap("0.0")).toBe(null);
  });

  it("returns null on negatives", () => {
    expect(parseCustomCap("-1")).toBe(null);
  });

  it("returns null on Infinity / NaN", () => {
    expect(parseCustomCap("Infinity")).toBe(null);
    expect(parseCustomCap("NaN")).toBe(null);
  });
});
