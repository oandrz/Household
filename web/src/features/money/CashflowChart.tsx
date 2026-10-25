// Left over per month as inline SVG, bars above and below a zero line the way
// PeriodReturnChart.tsx draws them. No charting dependency, for the reason
// NetWorthChart.tsx gives.
//
// Each kind of month looks different on purpose, so a glance tells "a result"
// from "not a result":
//   finished   solid, green above zero and red below
//   grey       striped: the month is there but too thin to trust
//   current    pale and uncoloured: still running
//   projected  dashed outline: a forecast, not money that happened
import { cashflowCopy, shortMonthLabel } from "./cashflowCopy";
import type { CashflowMonth } from "./cashflowSchemas";

const WIDTH = 320;
const HEIGHT = 150;
const PAD_X = 6;
const PAD_TOP = 10;
const PAD_BOTTOM = 24;
const PLOT_WIDTH = WIDTH - PAD_X * 2;
const PLOT_HEIGHT = HEIGHT - PAD_TOP - PAD_BOTTOM;
const BAR_GAP = 4;

type BarStyle = "solid-positive" | "solid-negative" | "grey" | "current" | "projected";

// Zero left over is drawn as a bad month (red), as the table does. A grey
// month is never coloured by its sign: a month with spending missing would
// otherwise be the tallest green bar.
// Months with no data at all (status "none") have no value to draw.
function valueOf(month: CashflowMonth): number | null {
  switch (month.status) {
    case "none":
      return null;
    case "future":
      return month.projectedLeftMinor;
    default:
      return month.leftMinor;
  }
}

function styleOf(month: CashflowMonth, value: number): BarStyle {
  switch (month.status) {
    case "grey":
      return "grey";
    case "current":
      return "current";
    case "future":
      return "projected";
    default:
      return value > 0 ? "solid-positive" : "solid-negative";
  }
}

// One legend entry: a small swatch drawn the way its bar is drawn, then the
// label. The swatch is decoration; the label carries the meaning. Its stripe
// pattern has its own id so it does not depend on the chart's <defs>.
function LegendItem({ style, label }: { style: BarStyle; label: string }) {
  const stripes = "cashflow-legend-stripes";
  return (
    <li className="flex items-center gap-1.5">
      <svg
        data-testid="cashflow-legend-swatch"
        data-style={style}
        aria-hidden="true"
        width="12"
        height="12"
        viewBox="0 0 12 12"
        className={style === "solid-positive" ? "text-accent" : "text-muted"}
      >
        {style === "grey" && (
          <defs>
            <pattern id={stripes} width="4" height="4" patternUnits="userSpaceOnUse" patternTransform="rotate(45)">
              <line x1="0" y1="0" x2="0" y2="4" stroke="currentColor" strokeWidth="2" />
            </pattern>
          </defs>
        )}
        <rect
          x="0.5"
          y="0.5"
          width="11"
          height="11"
          rx="1"
          fill={style === "grey" ? `url(#${stripes})` : style === "projected" ? "none" : "currentColor"}
          stroke={style === "grey" || style === "projected" ? "currentColor" : "none"}
          strokeDasharray={style === "projected" ? "3 2" : undefined}
          opacity={style === "current" ? 0.35 : 1}
        />
      </svg>
      {label}
    </li>
  );
}

type Props = {
  months: CashflowMonth[];
  currency: string;
};

export function CashflowChart({ months, currency }: Props) {
  if (months.length === 0) return null;

  const values = months.map(valueOf);
  const drawn = values.filter((value): value is number => value !== null);
  // The month still running never sets the scale: one large payday in it
  // would flatten every finished month, the ones the light judges, into a
  // sliver. Its bar is cut off at the edge instead. It sets the scale only
  // when it is all there is.
  const settled = values.filter(
    (value, index): value is number => value !== null && months[index].status !== "current",
  );
  const scaled = settled.length > 0 ? settled : drawn;
  // Zero is always inside the range, so a loss reads as below the line
  // rather than as a short win.
  const max = Math.max(0, ...scaled);
  const min = Math.min(0, ...scaled);
  const span = max - min || 1;
  const baselineY = PAD_TOP + (max / span) * PLOT_HEIGHT;
  const slot = PLOT_WIDTH / months.length;
  const barWidth = Math.max(1, slot - BAR_GAP);

  return (
    <div>
      <div className="relative">
        <svg
          data-testid="cashflow-chart"
          data-baseline={baselineY}
          viewBox={`0 0 ${WIDTH} ${HEIGHT}`}
          role="img"
          aria-label={`${cashflowCopy.chartLabel}, ${months[0].month} to ${months[months.length - 1].month}, in ${currency}`}
          className="w-full"
        >
          <defs>
            <pattern id="cashflow-stripes" width="4" height="4" patternUnits="userSpaceOnUse" patternTransform="rotate(45)" className="text-muted">
              <line x1="0" y1="0" x2="0" y2="4" stroke="currentColor" strokeWidth="2" />
            </pattern>
          </defs>
          {months.map((month, index) => {
            const value = values[index];
            if (value === null) return null;
            const style = styleOf(month, value);
            const y = PAD_TOP + ((max - Math.min(max, Math.max(min, value))) / span) * PLOT_HEIGHT;
            const colourClass =
              style === "current" || style === "grey"
                ? "text-muted"
                : value > 0
                  ? "text-accent"
                  : "text-danger";
            return (
              <rect
                key={month.month}
                data-testid="cashflow-bar"
                data-month={month.month}
                data-style={style}
                x={PAD_X + index * slot + BAR_GAP / 2}
                y={Math.min(y, baselineY)}
                width={barWidth}
                height={Math.max(1, Math.abs(baselineY - y))}
                rx={1}
                className={colourClass}
                fill={
                  style === "grey"
                    ? "url(#cashflow-stripes)"
                    : style === "projected"
                      ? "none"
                      : "currentColor"
                }
                stroke={style === "grey" || style === "projected" ? "currentColor" : "none"}
                strokeWidth={style === "grey" || style === "projected" ? 1 : 0}
                strokeDasharray={style === "projected" ? "3 2" : undefined}
                opacity={style === "current" ? 0.35 : 1}
              >
                <title>{month.month}</title>
              </rect>
            );
          })}
          {min < 0 && (
            <line
              x1={PAD_X}
              x2={WIDTH - PAD_X}
              y1={baselineY}
              y2={baselineY}
              stroke="currentColor"
              strokeWidth={0.5}
              className="text-muted"
            />
          )}
        </svg>
        {months.map((month, index) => (
          <span
            key={month.month}
            className="absolute bottom-0 -translate-x-1/2 whitespace-nowrap text-[11px] text-muted"
            style={{ left: `${((PAD_X + index * slot + slot / 2) / WIDTH) * 100}%` }}
          >
            {shortMonthLabel(month.month)}
          </span>
        ))}
      </div>
      <ul data-testid="cashflow-legend" className="mt-2 flex flex-wrap gap-x-3 gap-y-1 text-[11.5px] text-muted">
        <LegendItem style="solid-positive" label={cashflowCopy.chartLegend.solid} />
        <LegendItem style="grey" label={cashflowCopy.chartLegend.grey} />
        <LegendItem style="current" label={cashflowCopy.chartLegend.current} />
        <LegendItem style="projected" label={cashflowCopy.chartLegend.projected} />
      </ul>
    </div>
  );
}
