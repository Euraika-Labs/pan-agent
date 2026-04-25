import { useEffect, useRef, useState } from "react";
import { parseCustomCap } from "./budget-helpers";

interface BudgetExceededDialogProps {
  open: boolean;
  costUsed: number;
  costCap: number;
  onIncreaseDouble: () => void;
  onIncreaseCustom: (newCap: number) => void;
  onEndSession: () => void;
}

function formatCost(value: number): string {
  return `$${value.toFixed(2)}`;
}

export function BudgetExceededDialog({
  open,
  costUsed,
  costCap,
  onIncreaseDouble,
  onIncreaseCustom,
  onEndSession,
}: BudgetExceededDialogProps): React.JSX.Element | null {
  const [mode, setMode] = useState<"choices" | "custom">("choices");
  const [customValue, setCustomValue] = useState<string>("");
  const primaryButtonRef = useRef<HTMLButtonElement>(null);
  const customInputRef = useRef<HTMLInputElement>(null);

  useEffect(() => {
    if (!open) {
      setMode("choices");
      setCustomValue("");
      return;
    }
    primaryButtonRef.current?.focus();
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape") {
        e.preventDefault();
        onEndSession();
      }
    };
    document.addEventListener("keydown", onKey);
    return () => document.removeEventListener("keydown", onKey);
  }, [open, onEndSession]);

  useEffect(() => {
    if (mode === "custom") {
      const seed = costCap > 0 ? (costCap * 2).toFixed(2) : "5.00";
      setCustomValue(seed);
      // Defer focus until after render swaps the input into the DOM.
      requestAnimationFrame(() => customInputRef.current?.focus());
    }
  }, [mode, costCap]);

  if (!open) return null;

  const submitCustom = () => {
    const parsed = parseCustomCap(customValue);
    if (parsed !== null) {
      onIncreaseCustom(parsed);
    }
  };

  return (
    <div
      className="budget-dialog-backdrop"
      onClick={onEndSession}
      role="presentation"
    >
      <div
        className="budget-dialog"
        role="dialog"
        aria-modal="true"
        aria-labelledby="budget-dialog-title"
        aria-describedby="budget-dialog-desc"
        onClick={(e) => e.stopPropagation()}
      >
        <h2 id="budget-dialog-title" className="budget-dialog-title">
          Budget exceeded
        </h2>
        <p id="budget-dialog-desc" className="budget-dialog-desc">
          This session has used {formatCost(costUsed)} of its{" "}
          {formatCost(costCap)} cap. The agent is paused — choose how to
          continue.
        </p>

        {mode === "choices" && (
          <div className="budget-dialog-actions">
            <button
              ref={primaryButtonRef}
              type="button"
              className="budget-dialog-btn budget-dialog-btn--primary"
              onClick={onIncreaseDouble}
            >
              Increase 2x ({formatCost(costCap * 2)})
            </button>
            <button
              type="button"
              className="budget-dialog-btn"
              onClick={() => setMode("custom")}
            >
              Increase custom…
            </button>
            <button
              type="button"
              className="budget-dialog-btn budget-dialog-btn--end"
              onClick={onEndSession}
            >
              End session
            </button>
          </div>
        )}

        {mode === "custom" && (
          <form
            className="budget-dialog-custom"
            onSubmit={(e) => {
              e.preventDefault();
              submitCustom();
            }}
          >
            <label
              className="budget-dialog-label"
              htmlFor="budget-dialog-custom-input"
            >
              New cap (USD)
            </label>
            <input
              ref={customInputRef}
              id="budget-dialog-custom-input"
              type="number"
              min="0"
              step="0.01"
              inputMode="decimal"
              className="budget-dialog-input"
              value={customValue}
              onChange={(e) => setCustomValue(e.target.value)}
            />
            <div className="budget-dialog-actions">
              <button
                type="submit"
                className="budget-dialog-btn budget-dialog-btn--primary"
              >
                Set cap
              </button>
              <button
                type="button"
                className="budget-dialog-btn"
                onClick={() => setMode("choices")}
              >
                Back
              </button>
            </div>
          </form>
        )}
      </div>
    </div>
  );
}
