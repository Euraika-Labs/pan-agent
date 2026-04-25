/**
 * Pure helpers shared by CostPill / BudgetBanner / BudgetExceededDialog.
 *
 * Extracted so the threshold logic and custom-cap parsing can be unit-tested
 * without spinning up a DOM (the desktop test suite is logic-only — see
 * `desktop/src/screens/Layout/layout.shortcuts.test.ts` for the pattern).
 */

/**
 * Class-name suffix for the cost pill. The cap=0 path is treated as
 * "no cap" rather than 100% — a freshly-started session must not flash
 * red just because it has spent a few cents but no cap was set yet.
 */
export type PillVariant = "base" | "warning" | "exceeded";

export function pillVariant(costUsed: number, costCap: number): PillVariant {
  if (costCap <= 0) return "base";
  const ratio = costUsed / costCap;
  if (ratio >= 1) return "exceeded";
  if (ratio >= 0.8) return "warning";
  return "base";
}

export function pillClass(costUsed: number, costCap: number): string {
  const v = pillVariant(costUsed, costCap);
  if (v === "exceeded") return "cost-pill cost-pill--exceeded";
  if (v === "warning") return "cost-pill cost-pill--warning";
  return "cost-pill";
}

/**
 * Parse a raw user-typed cap from the BudgetExceededDialog's custom input.
 * Returns the parsed number on success or null when the input is empty,
 * non-numeric, NaN, infinite, or non-positive. Callers must NOT submit
 * non-positive caps to the backend (the API rejects them with 400).
 */
export function parseCustomCap(raw: string): number | null {
  const trimmed = raw.trim();
  if (trimmed === "") return null;
  const parsed = parseFloat(trimmed);
  if (!Number.isFinite(parsed)) return null;
  if (parsed <= 0) return null;
  return parsed;
}
