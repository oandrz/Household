// What each holding earned, period by period, as inline SVG.
//
// No charting dependency, for the reason NetWorthChart.tsx gives and
// MoodChart.tsx gave before it: this project's own floating-dependency history
// is why no new package arrives to draw forty rectangles. This is the third
// chart built that way, which confirms the decision rather than straining it.
//
// It wires against the report the page already holds, never a second request.
//
// Grouped by period, one bar per holding inside each group. That shape is the
// question the PRD asks -- "which instrument is earning its place" -- because
// the eye compares instruments within a period and follows one instrument
// across periods. The unrealised / realised / income split lives in the table
// below, where three numbers can be read as three numbers.
import { HOLDING_REPORT_COPY } from "./holdingReportCopy";
import type { ReportHolding, ReportPeriod } from "./holdingSchemas";

const WIDTH = 320;
const HEIGHT = 150;
const PAD_X = 6;
const PAD_TOP = 10;
const PAD_BOTTOM = 26; // room for the period-label row under the plot
const PLOT_WIDTH = WIDTH - PAD_X * 2;
const PLOT_HEIGHT = HEIGHT - PAD_TOP - PAD_BOTTOM;
const GROUP_GAP = 6;
const BAR_GAP = 1;

// The bar budget. Twelve periods against six holdings is seventy-two bars
// across 320 pixels -- under three pixels each, which is a smear rather than a
// chart. Past this the OLDEST periods are dropped, because the newest is the
// one the eye goes to first and the one the owner is deciding from.
const MAX_BARS = 40;

// One hue per holding, cycled. Deliberately a small fixed list rather than a
// generated palette: six distinguishable colours is the honest limit of a
// grouped bar chart this size, and the seventh holding reusing the first is
// less misleading than two colours nobody can tell apart.
const HUES = [
  "text-accent",
  "text-emerald-600",
  "text-amber-600",
  "text-sky-600",
  "text-rose-500",
  "text-violet-600",
];

type Props = {
  periods: ReportPeriod[];
  holdings: ReportHolding[];
  primaryCurrency: string;
};

// visiblePeriods drops from the OLD end until the bars fit. It returns the
// count as well, so the caption can say what is on screen rather than letting
// the chart quietly show less than it was given.
function visiblePeriods(periods: ReportPeriod[], holdingCount: number) {
  if (holdingCount === 0) return periods;
  const affordable = Math.max(1, Math.floor(MAX_BARS / holdingCount));
  if (periods.length <= affordable) return periods;
  return periods.slice(periods.length - affordable);
}

export function PeriodReturnChart({ periods, holdings, primaryCurrency }: Props) {
  const shown = visiblePeriods(periods, holdings.length);
  const offset = periods.length - shown.length;

  // Every figure the chart can actually draw, in the household's own currency:
  // that is the one that answers "did this make us richer", which is the
  // question a comparison between instruments is really asking.
  const values: number[] = [];
  for (const holding of holdings) {
    for (let i = 0; i < shown.length; i += 1) {
      const total = holding.returns[offset + i]?.total;
      if (total) values.push(total.primaryMinor);
    }
  }

  if (values.length === 0) {
    return (
      <p data-testid="period-return-chart-empty" className="mt-4 text-[12.5px] text-muted">
        {HOLDING_REPORT_COPY.chartEmpty}
      </p>
    );
  }

  // The baseline is zero, not the smallest figure. A losing quarter has to read
  // as below the line; measuring from an arbitrary floor would draw a loss as a
  // short win. NetWorthChart.tsx makes the same choice for the same reason.
  const max = Math.max(0, ...values);
  const min = Math.min(0, ...values);
  const span = max - min || 1; // every figure is zero: draw them on the line
  const baselineY = PAD_TOP + (max / span) * PLOT_HEIGHT;

  const groupWidth = (PLOT_WIDTH - GROUP_GAP * (shown.length - 1)) / shown.length;
  const barWidth = Math.max(
    1,
    (groupWidth - BAR_GAP * (holdings.length - 1)) / Math.max(1, holdings.length),
  );

  return (
    <div className="mt-4">
      <svg
        data-testid="period-return-chart"
        // The baseline is published so a test can prove a loss is drawn below
        // it rather than merely drawn, and the plot floor beside it so a test
        // can prove nothing is drawn off the end of the chart -- which is what
        // a baseline measured from anything but zero would do.
        data-baseline={baselineY}
        data-plot-bottom={PAD_TOP + PLOT_HEIGHT}
        viewBox={`0 0 ${WIDTH} ${HEIGHT}`}
        // role + label: rectangles announce nothing on their own, and naming
        // the range beats a screen reader saying "chart".
        role="img"
        aria-label={`What each holding earned, ${shown[0].label} to ${shown[shown.length - 1].label}, in ${primaryCurrency}`}
        className="w-full"
      >
        {shown.map((period, groupIndex) =>
          holdings.map((holding, holdingIndex) => {
            const total = holding.returns[offset + groupIndex]?.total;
            // NO BAR for a figure that cannot be known. A zero-height bar on
            // the axis claims "this earned nothing", which is a different
            // statement from "nobody priced it" -- the rule TrendPoint's
            // nullable figure carries on the backend.
            if (!total) return null;

            const y = PAD_TOP + ((max - total.primaryMinor) / span) * PLOT_HEIGHT;
            return (
              <rect
                key={`${period.label}-${holding.id}`}
                data-testid="return-bar"
                data-period={period.label}
                data-holding={holding.name}
                x={
                  PAD_X +
                  groupIndex * (groupWidth + GROUP_GAP) +
                  holdingIndex * (barWidth + BAR_GAP)
                }
                y={Math.min(y, baselineY)}
                width={barWidth}
                // A period that earned exactly zero still gets a sliver, so
                // "we knew, and it was nothing" does not look like a gap.
                height={Math.max(1, Math.abs(baselineY - y))}
                rx={1}
                fill="currentColor"
                className={HUES[holdingIndex % HUES.length]}
                // The period still running is drawn faintly: it is a figure in
                // progress, not a result, and the table beside it says "to
                // date" for the same reason.
                opacity={period.current ? 0.45 : 1}
              >
                <title>{`${holding.name}, ${period.label}`}</title>
              </rect>
            );
          }),
        )}
        {/* The zero line itself, drawn only when something sits below it --
            otherwise the axis and the baseline are the same line and a second
            stroke is noise. */}
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
        {shown.map((period, index) => (
          <text
            key={period.label}
            x={PAD_X + index * (groupWidth + GROUP_GAP) + groupWidth / 2}
            y={HEIGHT - 8}
            textAnchor="middle"
            fontSize={8}
            fill="currentColor"
            className="text-muted"
          >
            {period.label}
          </text>
        ))}
      </svg>

      <ul
        data-testid="period-return-chart-legend"
        className="mt-2 flex flex-wrap gap-x-3 gap-y-1 text-[11.5px] text-muted"
      >
        {holdings.map((holding, index) => (
          <li key={holding.id} className="flex items-center gap-1">
            <span
              aria-hidden="true"
              className={`inline-block h-2 w-2 rounded-full bg-current ${HUES[index % HUES.length]}`}
            />
            {holding.name}
          </li>
        ))}
      </ul>

      {shown.length < periods.length && (
        <p data-testid="period-return-chart-window" className="mt-1 text-[11.5px] text-muted">
          Showing the last {shown.length} of {periods.length} periods, so the bars stay readable.
        </p>
      )}
    </div>
  );
}
