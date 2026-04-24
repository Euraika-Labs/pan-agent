interface BudgetBannerProps {
  type: "warning" | "exceeded" | null;
  costUsed: number;
  costCap: number;
  onIncrease?: () => void;
  onDismiss?: () => void;
}

function formatCost(value: number): string {
  return `$${value.toFixed(2)}`;
}

export function BudgetBanner({
  type,
  costUsed,
  costCap,
  onIncrease,
  onDismiss,
}: BudgetBannerProps): React.JSX.Element | null {
  if (!type) return null;

  const costLabel = `${formatCost(costUsed)} / ${formatCost(costCap)}`;
  const isExceeded = type === "exceeded";

  return (
    <div
      className={`budget-banner budget-banner--${type}`}
      role="alert"
      aria-live="assertive"
    >
      <span className="budget-banner-icon" aria-hidden="true">
        {isExceeded ? "!" : "⚠"}
      </span>
      <span className="budget-banner-text">
        {isExceeded
          ? `Budget exceeded (${costLabel})`
          : `Budget at 80% (${costLabel})`}
      </span>
      <div className="budget-banner-actions">
        {isExceeded && (
          <button
            className="budget-banner-btn budget-banner-btn--increase"
            onClick={onIncrease}
            type="button"
          >
            Increase Limit
          </button>
        )}
        {isExceeded && (
          <button
            className="budget-banner-btn budget-banner-btn--end"
            onClick={onDismiss}
            type="button"
          >
            End Session
          </button>
        )}
        {!isExceeded && (
          <button
            className="budget-banner-btn budget-banner-btn--dismiss"
            onClick={onDismiss}
            type="button"
            aria-label="Dismiss budget warning"
          >
            Dismiss
          </button>
        )}
      </div>
    </div>
  );
}
