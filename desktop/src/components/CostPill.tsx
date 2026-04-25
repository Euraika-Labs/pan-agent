import { pillClass } from "./budget-helpers";

interface CostPillProps {
  costUsed: number;
  costCap: number; // 0 means no cap
}

function formatCost(value: number): string {
  return `$${value.toFixed(2)}`;
}

export function CostPill({ costUsed, costCap }: CostPillProps): React.JSX.Element {
  const hasCap = costCap > 0;
  const label = hasCap
    ? `${formatCost(costUsed)} / ${formatCost(costCap)}`
    : formatCost(costUsed);

  return (
    <span
      className={pillClass(costUsed, costCap)}
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
