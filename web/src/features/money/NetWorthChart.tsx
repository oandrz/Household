// The twelve-month net worth chart, as inline SVG. Twelve bars is less code
// than a charting dependency, and this project's own floating-dependency
// history is why no new package arrives for it -- MoodChart.tsx made the same
// call for the same reason.
//
// `points` is whatever useAccounts() already fetched: the chart wires against
// data the page holds, never a second request.
import { FINANCES_COPY, monthTickLabel } from "./copy";
import type { TrendPoint } from "./schemas";

const WIDTH = 320;
const HEIGHT = 150;
const PAD_X = 6;
const PAD_TOP = 10;
const PAD_BOTTOM = 26; // room for the month-label row under the plot
const PLOT_WIDTH = WIDTH - PAD_X * 2;
const PLOT_HEIGHT = HEIGHT - PAD_TOP - PAD_BOTTOM;
const BAR_GAP = 4;

// The design draws the same green at three strengths. One colour at three
// opacities rather than three hard-coded tints, so a theme change carries.
const NEWEST = 1;
const COMPLETE = 0.35;
const INCOMPLETE = 0.15;

// Ticks at the first, fourth, seventh and last month -- the design's own axis
// (`Aug '25 · Nov '25 · Feb '26 · Jul '26`). Twelve labels overlap at this
// width, and an evenly-spaced rule would drop the newest month, which is the
// one the eye goes to first.
const TICKS = [0, 3, 6, 11];

export function NetWorthChart({ points }: { points: TrendPoint[] }) {
  const known = points.filter((point) => point.netWorthMinor !== null);

  // Fewer than two known months is not a trend. A brand-new household has
  // every account opened today, so it has exactly one -- and a single bar
  // pinned to the right-hand edge with eleven empty slots beside it says less
  // than the sentence does.
  if (known.length < 2) {
    return (
      <p data-testid="net-worth-chart-empty" className="mt-4 text-[12.5px] text-muted">
        {FINANCES_COPY.trendEmpty}
      </p>
    );
  }

  const values = known.map((point) => point.netWorthMinor as number);
  // The baseline is zero, not the smallest figure: a debt-heavy month has to
  // read as below the line, and bars measured from an arbitrary floor would
  // make a household worth 1000 look like one worth nothing.
  const max = Math.max(0, ...values);
  const min = Math.min(0, ...values);
  const span = max - min || 1; // every figure is zero: draw them on the line
  const baselineY = PAD_TOP + (max / span) * PLOT_HEIGHT;
  const barWidth = (PLOT_WIDTH - BAR_GAP * (points.length - 1)) / points.length;

  const firstComplete = points.find((point) => point.netWorthMinor !== null && point.complete);
  const hasIncomplete = known.some((point) => !point.complete);

  return (
    <div className="mt-4">
      <svg
        viewBox={`0 0 ${WIDTH} ${HEIGHT}`}
        // role="img" + aria-label: bars have nothing for a screen reader to
        // announce on their own, and the label names the range and how much of
        // it carries a figure rather than merely saying "chart".
        role="img"
        aria-label={FINANCES_COPY.trendChartLabel(
          monthTickLabel(points[0].month),
          monthTickLabel(points[points.length - 1].month),
          known.length,
        )}
        className="w-full text-accent"
      >
        {points.map((point, index) => {
          if (point.netWorthMinor === null) return null;
          const y = PAD_TOP + ((max - point.netWorthMinor) / span) * PLOT_HEIGHT;
          return (
            <rect
              key={point.month}
              data-testid="net-worth-bar"
              data-month={point.month}
              data-complete={point.complete}
              x={PAD_X + index * (barWidth + BAR_GAP)}
              y={Math.min(y, baselineY)}
              width={barWidth}
              // A month that is exactly zero still gets a visible sliver, so
              // "we knew, and it was nothing" does not look like a gap.
              height={Math.max(1, Math.abs(baselineY - y))}
              rx={2}
              fill="currentColor"
              // The newest point is always complete: networth_trend.go clamps
              // trackedFrom down to the current month and only ever computes a
              // trend when at least one account counts toward net worth, so
              // months[11] cannot be missing (backend invariant, proven by
              // TestAnAccountOpenedNextMonthByClockSkewIsInTheNewestBar). Full
              // opacity here can therefore never contradict the "lighter bars
              // are incomplete" note below.
              opacity={
                index === points.length - 1 ? NEWEST : point.complete ? COMPLETE : INCOMPLETE
              }
            />
          );
        })}
        {/* points.length is a runtime fact, not a compile-time one -- trendSchema
            (schemas.ts) is a plain array with no length check, so a truncated
            or malformed response that still parses must not crash on
            points[11]. Filtering keeps whichever of the four design
            positions actually exist and degrades to fewer ticks instead;
            MoodChart.tsx's `index % 3 === 0` is the same kind of
            length-safety for its own axis. */}
        {TICKS.filter((index) => index < points.length).map((index) => (
          <text
            key={points[index].month}
            x={PAD_X + index * (barWidth + BAR_GAP) + barWidth / 2}
            y={HEIGHT - 8}
            textAnchor="middle"
            fontSize={9}
            fill="currentColor"
            className="text-muted"
          >
            {monthTickLabel(points[index].month)}
          </text>
        ))}
      </svg>
      {hasIncomplete && (
        <p data-testid="net-worth-chart-note" className="mt-2 text-[11.5px] text-muted">
          {firstComplete
            ? FINANCES_COPY.trendIncomplete(monthTickLabel(firstComplete.month))
            : // Same backend invariant as the newest-bar opacity above means
              // the newest month is always complete, so firstComplete can
              // never come back null against real data -- this branch is
              // unreachable in production. Kept anyway: TypeScript cannot
              // encode a cross-service invariant, and deleting the fallback
              // would turn a future violation of that invariant into a crash
              // instead of a slightly less specific sentence.
              FINANCES_COPY.trendIncompleteUnknownStart}
        </p>
      )}
    </div>
  );
}
