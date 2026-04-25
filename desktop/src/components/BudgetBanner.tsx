interface BudgetBannerProps {
  /**
   * Only the "warning" (80%) state lives here. The 100% "exceeded" state is
   * a focus-trapped modal — see BudgetExceededDialog. Pass null to hide.
   */
  type: "warning" | null;
  costUsed: number;
  costCap: number;
  onDismiss?: () => void;
}

function formatCost(value: number): string {
  return `$${value.toFixed(2)}`;
}

export function BudgetBanner({
  type,
  costUsed,
  costCap,
  onDismiss,
}: BudgetBannerProps): React.JSX.Element | null {
  if (!type) return null;

  const costLabel = `${formatCost(costUsed)} / ${formatCost(costCap)}`;

  return (
    <div
      className="budget-banner budget-banner--warning"
      role="alert"
      aria-live="polite"
    >
      <span className="budget-banner-icon" aria-hidden="true">
        ⚠
      </span>
      <span className="budget-banner-text">
        Budget at 80% ({costLabel})
      </span>
      <div className="budget-banner-actions">
        <button
          className="budget-banner-btn budget-banner-btn--dismiss"
          onClick={onDismiss}
          type="button"
          aria-label="Dismiss budget warning"
        >
          Dismiss
        </button>
      </div>
    </div>
  );
}
