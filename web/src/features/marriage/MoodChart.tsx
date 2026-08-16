// The twelve-month mood chart, as inline SVG -- twelve points is less code
// than a charting dependency, and this codebase's own floating-dependency
// history (CLAUDE.md's global constraints) is the reason no new package is
// pulled in for it. `points` is whatever `useRetros()` already fetched
// (RetrosPage.tsx's own mount-point comment: the chart wires against data
// the page already holds, never a second fetch).
import { RETRO_COPY, monthShortLabel, monthYearLabel } from "./retroCopy";
import type { MoodPoint } from "./retroSchemas";

const WIDTH = 320;
const HEIGHT = 120;
const PAD_X = 12;
const PAD_TOP = 12;
const PAD_BOTTOM = 28; // room for the month-label row under the plot
const PLOT_WIDTH = WIDTH - PAD_X * 2;
const PLOT_HEIGHT = HEIGHT - PAD_TOP - PAD_BOTTOM;

function xFor(index: number, count: number): number {
  // count <= 1 has no span to spread across -- centre the single point
  // rather than dividing by zero.
  if (count <= 1) return PAD_X + PLOT_WIDTH / 2;
  return PAD_X + (index / (count - 1)) * PLOT_WIDTH;
}

// Mood is 1-5, higher reads as "better" -- placed nearer the top of the
// plot, the up-is-good convention every other chart on this dashboard uses.
function yFor(mood: number): number {
  return PAD_TOP + ((5 - mood) / 4) * PLOT_HEIGHT;
}

type RunPoint = { index: number; mood: number };

// One run per unbroken stretch of months that have a mood. A null mood ends
// the current run outright (never "carries through" as a value) -- this is
// the one piece of logic the brief's own mutation check exists to prove:
// treating a null as 0 here would merge two runs either side of a gap into
// one continuous line sagging to the floor, which is exactly the "zero is a
// claim" defect moodPointDTO's own comment warns against.
function groupIntoRuns(points: MoodPoint[]): RunPoint[][] {
  const runs: RunPoint[][] = [];
  let current: RunPoint[] | null = null;
  points.forEach((point, index) => {
    if (point.mood === null) {
      current = null;
      return;
    }
    if (!current) {
      current = [];
      runs.push(current);
    }
    current.push({ index, mood: point.mood });
  });
  return runs;
}

export function MoodChart({ points }: { points: MoodPoint[] }) {
  const trackedCount = points.filter((point) => point.mood !== null).length;

  // A chart with nothing plottable is not a chart -- render the same "not
  // enough data yet" copy Budget's own empty states use rather than an axis
  // with no line and no dots on it.
  if (trackedCount === 0) {
    return (
      <p data-testid="mood-chart-empty" className="text-[12.5px] text-muted">
        {RETRO_COPY.moodChartEmpty}
      </p>
    );
  }

  const runs = groupIntoRuns(points);
  const first = points[0];
  const last = points[points.length - 1];
  const ariaLabel = RETRO_COPY.moodChartLabel(
    monthYearLabel(first.month),
    monthYearLabel(last.month),
    trackedCount,
    points.length,
  );

  return (
    <svg
      viewBox={`0 0 ${WIDTH} ${HEIGHT}`}
      // role="img" + aria-label: a line drawing has nothing else for a
      // screen reader to announce -- the label names the range and how many
      // months actually carry a mood, not merely that a chart exists.
      role="img"
      aria-label={ariaLabel}
      className="w-full text-accent"
    >
      {runs.map((run) => (
        <polyline
          key={run[0].index}
          points={run.map((point) => `${xFor(point.index, points.length)},${yFor(point.mood)}`).join(" ")}
          fill="none"
          stroke="currentColor"
          strokeWidth={2}
        />
      ))}
      {points.map((point, index) =>
        point.mood === null ? null : (
          <circle
            key={`dot-${point.month}`}
            cx={xFor(index, points.length)}
            cy={yFor(point.mood)}
            r={3}
            fill="currentColor"
          />
        ),
      )}
      {points.map((point, index) =>
        // Every third month only -- twelve full labels would overlap at
        // 320px (the brief's own constraint).
        index % 3 === 0 ? (
          <text
            key={`label-${point.month}`}
            x={xFor(index, points.length)}
            y={HEIGHT - 8}
            textAnchor="middle"
            fontSize={9}
            fill="currentColor"
            className="text-muted"
          >
            {monthShortLabel(point.month)}
          </text>
        ) : null,
      )}
    </svg>
  );
}
