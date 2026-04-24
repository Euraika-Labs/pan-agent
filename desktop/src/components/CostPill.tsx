interface CostPillProps {
  costUsed: number;
  costCap: number; // 0 means no cap
}

function formatCost(value: number): string {
  return `$${value.toFixed(2)}`;
}

export function CostPill({ costUsed, costCap }: CostPillProps): React.JSX.Element {
  const hasCap = costCap > 0;
  const ratio = hasCap ? costUsed / costCap : 0;

  let pillClass = "cost-pill";
  if (hasCap && ratio >= 1) {
    pillClass += " cost-pill--exceeded";
  } else if (hasCap && ratio >= 0.8) {
    pillClass += " cost-pill--warning";
  }

  const label = hasCap
    ? `${formatCost(costUsed)} / ${formatCost(costCap)}`
    : formatCost(costUsed);

  return (
    <span
      className={pillClass}
      title={
        hasCap
          ? `Cost: ${formatCost(costUsed)} of ${formatCost(costCap)} cap`
          : `Cost: ${formatCost(costUsed)}`
      }
    >
      {label}
    </span>
  );
}
