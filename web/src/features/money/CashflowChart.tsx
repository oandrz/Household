// Left over per month as inline SVG, bars above and below a zero line the way
// PeriodReturnChart.tsx draws them. No charting dependency, for the reason
// NetWorthChart.tsx gives.
//
// Each kind of month looks different on purpose, so a glance tells "a result"
// from "not a result":
//   finished   solid, green above zero and red below
//   grey       tinted with an outline: the month is there but too thin to trust
//   current    pale and uncoloured: still running
//   projected  dashed outline: a forecast, not money that happened
import { barTitle, cashflowCopy, cutOffNote, shortMonthLabel } from "./cashflowCopy";
import { monthLabel } from "./month";
import type { CashflowMonth } from "./cashflowSchemas";

const WIDTH = 320;
const HEIGHT = 150;
const PAD_X = 6;
const PAD_TOP = 10;
const PAD_BOTTOM = 24;
const PLOT_WIDTH = WIDTH - PAD_X * 2;
const PLOT_HEIGHT = HEIGHT - PAD_TOP - PAD_BOTTOM;
const BAR_GAP = 4;
const CUT_RISE = 6;
const CUT_RUN = 2;

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

// The colour a bar style is drawn in. Every style is listed so a new one
// cannot silently fall into another's colour.
function colourOf(style: BarStyle, negative = false): string {
  switch (style) {
    case "solid-positive":
      return "text-accent";
    case "solid-negative":
      return "text-danger";
    case "projected":
      return negative ? "text-danger" : "text-accent";
    case "grey":
    case "current":
      return "text-muted";
    default:
      throw new Error(`unknown bar style: ${style satisfies never}`);
  }
}

// Grey is a faint fill plus an outline rather than diagonal stripes: the chart
// is stretched to a fixed height, and a stripe pattern would stretch with it.
function fillOf(style: BarStyle): string {
  return style === "projected" ? "none" : "currentColor";
}

function opacityOf(style: BarStyle): number {
  switch (style) {
    case "current":
      return 0.35;
    case "grey":
      return 0.25;
    default:
      return 1;
  }
}

function isOutlined(style: BarStyle): boolean {
  return style === "grey" || style === "projected";
}

// One legend entry: a small swatch drawn the way its bar is drawn, then the
// label. The swatch is decoration; the label carries the meaning.
function LegendItem({ style, label }: { style: BarStyle; label: string }) {
  return (
    <li className="flex items-center gap-1.5">
      <svg
        data-testid="cashflow-legend-swatch"
        data-style={style}
        aria-hidden="true"
        width="12"
        height="12"
        viewBox="0 0 12 12"
        className={colourOf(style)}
      >
        <rect
          x="0.5"
          y="0.5"
          width="11"
          height="11"
          rx="1"
          fill={fillOf(style)}
          fillOpacity={opacityOf(style)}
          stroke={isOutlined(style) ? "currentColor" : "none"}
          strokeDasharray={style === "projected" ? "3 2" : undefined}
        />
      </svg>
      {label}
    </li>
  );
}

// Two white diagonal strokes across the clipped end of a bar, the usual "this
// goes on" mark. Drawn in the card colour so it reads as a cut in the bar.
function CutMark({ x, width, y, atTop }: { x: number; width: number; y: number; atTop: boolean }) {
  // The chart is stretched sideways to fill its card, so a slash as wide as the
  // bar would flatten into a stripe on a wide screen. A short fixed run keeps
  // it a slash at any width; the pointer passes through so the bar's title
  // still shows on hover.
  const run = Math.min(CUT_RUN, width);
  const centre = x + width / 2;
  return (
    <g data-testid="cashflow-bar-cut" className="text-card" pointerEvents="none">
      {[CUT_RISE, CUT_RISE * 2.5].map((offset) => {
        const lineY = y + (atTop ? offset : -offset);
        return (
          <line
            key={offset}
            x1={centre - run / 2}
            x2={centre + run / 2}
            y1={lineY + CUT_RISE / 2}
            y2={lineY - CUT_RISE / 2}
            stroke="currentColor"
            strokeWidth={1.5}
            vectorEffect="non-scaling-stroke"
          />
        );
      })}
    </g>
  );
}

type Props = {
  months: CashflowMonth[];
  currency: string;
  formatAmount: (minor: number) => string;
};

export function CashflowChart({ months, formatAmount }: Props) {
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
  const settledMin = Math.min(0, ...scaled);
  // A running month below zero (rent paid, salary not in yet) may lower the
  // floor so it is drawn under the line, but only as far as the finished
  // months reach on the other side: a deeper dip would squash them instead.
  // Beyond that it is cut off at the bottom, as a big payday is at the top.
  const reach = Math.max(max, -settledMin);
  const runningLow = Math.min(
    0,
    ...values.filter((value, index): value is number => value !== null && months[index].status === "current"),
  );
  const min =
    settled.length > 0 && reach > 0 ? Math.min(settledMin, Math.max(runningLow, -reach)) : Math.min(settledMin, runningLow);
  const span = max - min || 1;
  const baselineY = PAD_TOP + (max / span) * PLOT_HEIGHT;
  const slot = PLOT_WIDTH / months.length;
  const barWidth = Math.max(1, slot - BAR_GAP);
  // The bars' titles show only on mouse hover, so the same words are also
  // listed as text: a screen reader cannot reach a title inside role="img",
  // and a phone has no hover.
  const spoken = months.flatMap((month, index) => {
    const value = values[index];
    if (value === null) return [];
    const clipped = Math.min(max, Math.max(min, value)) !== value;
    return [
      {
        key: month.month,
        text: barTitle(month, value, clipped, formatAmount),
        cutNote: clipped ? cutOffNote(month, value, formatAmount) : null,
      },
    ];
  });

  return (
    <div>
      <div className="relative">
        <svg
          data-testid="cashflow-chart"
          data-baseline={baselineY}
          viewBox={`0 0 ${WIDTH} ${HEIGHT}`}
          role="img"
          aria-label={cashflowCopy.chartName(monthLabel(months[0].month), monthLabel(months[months.length - 1].month))}
          preserveAspectRatio="none"
          className="h-[200px] w-full"
        >
          {months.map((month, index) => {
            const value = values[index];
            if (value === null) return null;
            const style = styleOf(month, value);
            const clamped = Math.min(max, Math.max(min, value));
            const clipped = clamped !== value;
            const y = PAD_TOP + ((max - clamped) / span) * PLOT_HEIGHT;
            const x = PAD_X + index * slot + BAR_GAP / 2;
            const outlined = isOutlined(style);
            return (
              <g key={month.month}>
                <rect
                  data-testid="cashflow-bar"
                  data-month={month.month}
                  data-style={style}
                  data-clipped={clipped ? "true" : undefined}
                  x={x}
                  y={Math.min(y, baselineY)}
                  width={barWidth}
                  height={Math.max(1, Math.abs(baselineY - y))}
                  rx={1}
                  className={colourOf(style, value <= 0)}
                  fill={fillOf(style)}
                  fillOpacity={opacityOf(style)}
                  stroke={outlined ? "currentColor" : "none"}
                  strokeWidth={outlined ? 1 : 0}
                  strokeDasharray={style === "projected" ? "3 2" : undefined}
                  vectorEffect={outlined ? "non-scaling-stroke" : undefined}
                >
                  <title>{barTitle(month, value, clipped, formatAmount)}</title>
                </rect>
                {clipped && <CutMark x={x} width={barWidth} y={y} atTop={value > clamped} />}
              </g>
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
              vectorEffect="non-scaling-stroke"
              className="text-muted"
            />
          )}
        </svg>
        <ul data-testid="cashflow-chart-values" className="sr-only">
          {spoken.map((bar) => (
            <li key={bar.key}>{bar.text}</li>
          ))}
        </ul>
        {months.map((month, index) => (
          <span
            key={month.month}
            aria-hidden="true"
            className="absolute bottom-0 -translate-x-1/2 whitespace-nowrap text-[11px] text-muted"
            style={{ left: `${((PAD_X + index * slot + slot / 2) / WIDTH) * 100}%` }}
          >
            {shortMonthLabel(month.month)}
          </span>
        ))}
      </div>
      <ul data-testid="cashflow-legend" className="mt-2 flex flex-wrap gap-x-3 gap-y-1 text-[11.5px] text-muted">
        <LegendItem style="solid-positive" label={cashflowCopy.chartLegend.left} />
        <LegendItem style="solid-negative" label={cashflowCopy.chartLegend.nothing} />
        <LegendItem style="grey" label={cashflowCopy.chartLegend.grey} />
        <LegendItem style="current" label={cashflowCopy.chartLegend.current} />
        <LegendItem style="projected" label={cashflowCopy.chartLegend.projected} />
      </ul>
      {/* The cut marks alone are too faint to carry the real figure, and a
          title needs a mouse; this line says it in plain text. */}
      {spoken.map(
        (bar) =>
          bar.cutNote && (
            <p key={bar.key} data-testid="cashflow-cut-note" aria-hidden="true" className="mt-1 text-[11.5px] text-muted">
              {bar.cutNote}
            </p>
          ),
      )}
    </div>
  );
}
