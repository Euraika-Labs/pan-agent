/**
 * CostSparkline — inline SVG cost-over-time mini-chart.
 *
 * Pure render with zero deps. Uses `currentColor` so the line + area
 * pick up `--status-warn` / `--status-err` / `--accent` from the parent
 * (typical task header: stroke = currentColor, fill = currentColor at
 * 12% opacity).
 *
 * Datadog-style: single <polyline> for the line + a closed <polygon>
 * for the area fill. Empty / single-point inputs render a flat baseline
 * rather than a degenerate path.
 *
 * Per WS#2 design-doc (`docs/design/phase12.md` line 213): "cost-over-time
 * spark-line (SVG, no deps, Datadog pattern)".
 */
export interface SparklinePoint {
  ts: number; // epoch ms or seconds — caller is consistent
  usd: number;
}

interface CostSparklineProps {
  points: SparklinePoint[];
  width?: number;
  height?: number;
  /** ARIA label describing the chart for screen readers. */
  ariaLabel?: string;
  /** Stroke width in user units. Default 1.5. */
  strokeWidth?: number;
}

interface ScaledPoint {
  x: number;
  y: number;
}

/**
 * Scale raw points into SVG viewBox coordinates with a 1px top/bottom
 * pad so the stroke isn't clipped at the extremes. Exposed for tests.
 *
 * Edge cases:
 *   - empty input → []
 *   - single point → centered, mid-height
 *   - all-equal usd → flat at mid-height (avoid divide-by-zero)
 *   - all-equal ts  → vertical line, all points at x=width/2
 */
export function scalePoints(
  points: SparklinePoint[],
  width: number,
  height: number,
): ScaledPoint[] {
  if (points.length === 0) return [];
  if (points.length === 1) {
    return [{ x: width / 2, y: height / 2 }];
  }

  const tsValues = points.map((p) => p.ts);
  const usdValues = points.map((p) => p.usd);
  const tsMin = Math.min(...tsValues);
  const tsMax = Math.max(...tsValues);
  const usdMin = 0; // anchor cost axis to zero so cheap groups stay flat
  const usdMax = Math.max(...usdValues);

  const tsSpan = tsMax - tsMin;
  const usdSpan = usdMax - usdMin;
  const padding = 1;
  const innerH = height - padding * 2;
  const baselineY = padding + innerH; // y at usd=0

  return points.map((p) => {
    const x = tsSpan === 0 ? width / 2 : ((p.ts - tsMin) / tsSpan) * width;
    // Three cases for y:
    //   - usdSpan=0 and usdMax=0  → flat at the baseline ($0 series)
    //   - usdSpan=0 and usdMax>0  → flat at the top (constant non-zero)
    //   - usdSpan>0               → linear scale anchored at usd=0
    let y: number;
    if (usdSpan === 0) {
      y = usdMax === 0 ? baselineY : padding;
    } else {
      y = baselineY - ((p.usd - usdMin) / usdSpan) * innerH;
    }
    return { x, y };
  });
}

function formatPath(scaled: ScaledPoint[]): string {
  return scaled.map((p) => `${p.x.toFixed(2)},${p.y.toFixed(2)}`).join(" ");
}

export function CostSparkline({
  points,
  width = 80,
  height = 20,
  ariaLabel,
  strokeWidth = 1.5,
}: CostSparklineProps): React.JSX.Element {
  const scaled = scalePoints(points, width, height);

  // Empty: show a flat baseline so the layout doesn't jump
  if (scaled.length === 0) {
    return (
      <svg
        className="cost-sparkline cost-sparkline--empty"
        width={width}
        height={height}
        viewBox={`0 0 ${width} ${height}`}
        role="img"
        aria-label={ariaLabel ?? "No cost data"}
      >
        <line
          x1={0}
          y1={height - 1}
          x2={width}
          y2={height - 1}
          stroke="currentColor"
          strokeOpacity={0.25}
          strokeWidth={strokeWidth}
        />
      </svg>
    );
  }

  const linePath = formatPath(scaled);
  // Polygon path closes the line down to the baseline for the fill.
  const areaPath =
    scaled.length > 1
      ? `${linePath} ${width.toFixed(2)},${(height - 1).toFixed(2)} ${(0).toFixed(2)},${(height - 1).toFixed(2)}`
      : linePath;

  return (
    <svg
      className="cost-sparkline"
      width={width}
      height={height}
      viewBox={`0 0 ${width} ${height}`}
      role="img"
      aria-label={ariaLabel ?? "Cost over time"}
    >
      {scaled.length > 1 && (
        <polygon
          points={areaPath}
          fill="currentColor"
          fillOpacity={0.12}
          stroke="none"
        />
      )}
      <polyline
        points={linePath}
        fill="none"
        stroke="currentColor"
        strokeWidth={strokeWidth}
        strokeLinejoin="round"
        strokeLinecap="round"
      />
    </svg>
  );
}
