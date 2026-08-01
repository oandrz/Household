// The Monthly contributions card, below the goal grid on /money/goals: the
// design's stacked bar of planned monthly amounts across every unarchived
// goal, its legend, and beside the planned total the actual figure for this
// month -- kept as two clearly labelled figures rather than folded into one
// (spec decision 7). The design's own bar sums to a single "S$2,050 total"
// and calls that the answer; a planned figure is a commitment, and with
// every contribution here entered by hand the two diverge constantly.
// Planned-only would read as an achievement on a month nothing was logged;
// actual-only would leave the card blank on the 1st of every month and
// strand the planned figures that drive decision 2 nowhere but the
// individual cards. The design's own "next transfer Aug 1" line does not
// ship -- nothing in this feature runs on a clock.
//
// Pure presentation over already-computed figures, GoalCard.tsx's own
// convention: every number here is either summary's own field or, for the
// bar and legend, a goal's own plannedMonthlyMinor -- the only arithmetic
// this component performs is the bar's own segment-width percentages (see
// localPlannedTotal below for why that sum can never be summary's own).
import { formatMoney } from "./formatMoney";
import { GOAL_COPY } from "./goalCopy";
import type { Goal, GoalsResponse } from "./goalSchemas";

// The design's own four-tone palette (Household Dashboard.dc.html's Monthly
// contributions bar). Cycled by index, not mapped from a goal id -- nothing
// on the wire assigns a goal a colour, and a household looking at its own
// handful of goals on one card never needs more than this to tell segments
// apart.
const SEGMENT_COLORS = ["#1a6b52", "#5b8f7c", "#8fb5a5", "#c4d8cf"];

export function MonthlyContributionsCard({
  goals,
  summary,
  currency,
  symbolFor,
}: {
  goals: Goal[];
  summary: GoalsResponse["summary"];
  // The household's primary currency -- what summary's two totals are
  // already converted into server-side. Not a field on `summary` itself
  // (goalsSummarySchema carries no currency of its own), so GoalsPage
  // passes its own `data.currency` through.
  currency: string;
  // Same shape as GoalsPage's own symbolFor: looked up per currency code,
  // not a single flat symbol -- the legend below needs each goal's own
  // currency symbol, which can differ from `currency` (decision 5's
  // no-rate goal still renders its own card in its own currency).
  symbolFor: (currency: string) => string | undefined;
}) {
  // Archived contributes nothing here -- summary's own two totals already
  // exclude it (goalsSummarySchema's own comment: every count there is live
  // goals only), and drawing it into the bar or legend would show a
  // segment for a figure neither total counts.
  const live = goals.filter((goal) => goal.archivedAt === null);

  // A local, unconverted sum -- BudgetByPerson.tsx's own totalMinor,
  // reused for the identical reason: it exists only to give each segment
  // something to divide by, and is never itself displayed. It cannot be
  // summary.plannedMonthlyTotalMinor: that figure is converted to primary
  // currency and excludes a no-rate goal (decision 5), while this bar shows
  // EVERY unarchived goal including one with no rate -- if the denominator
  // dropped a goal the numerator still includes, segment widths would sum
  // past 100% and the bar would overflow its own row.
  const localPlannedTotal = live.reduce((sum, goal) => sum + goal.plannedMonthlyMinor, 0);

  const primarySymbol = symbolFor(currency);
  const plannedTotalLabel = formatMoney(summary.plannedMonthlyTotalMinor, currency, primarySymbol);
  const actualLabel = formatMoney(summary.actualThisMonthMinor, currency, primarySymbol);

  // One sentence only when the two figures actually differ (spec decision
  // 7: "where they differ, the card says so"). Exact-zero is checked first
  // and worded distinctly from actualShort below, even though "nothing
  // logged" is arithmetically just the largest possible shortfall --
  // stating it as an observation ("nothing yet") reads differently from a
  // verdict against the household's own whole plan.
  let diffSentence: string | null = null;
  if (summary.actualThisMonthMinor === 0 && summary.plannedMonthlyTotalMinor > 0) {
    diffSentence = GOAL_COPY.actualNone;
  } else {
    const diffMinor = summary.actualThisMonthMinor - summary.plannedMonthlyTotalMinor;
    if (diffMinor < 0) {
      diffSentence = GOAL_COPY.actualShort(formatMoney(-diffMinor, currency, primarySymbol));
    } else if (diffMinor > 0) {
      diffSentence = GOAL_COPY.actualOver(formatMoney(diffMinor, currency, primarySymbol));
    }
  }

  return (
    <div data-testid="monthly-contributions-card" className="rounded-xl border border-hairline bg-card p-[22px]">
      <div className="text-[14px] font-semibold text-ink">{GOAL_COPY.contributionsCardTitle}</div>

      {/* The planned side: bar and legend, scoped under their own heading
          so neither reads as the actual figure that follows below it --
          the confusion this whole card exists to avoid is guarded right
          next to where it could happen. */}
      <div className="mt-4">
        <div className="text-[11px] font-semibold uppercase tracking-wide text-muted">
          {GOAL_COPY.plannedMonthlyHeading}
        </div>
        <div data-testid="monthly-contributions-bar" className="mt-2 flex h-3 overflow-hidden rounded-md bg-canvas">
          {live.map((goal, index) => {
            // Guards localPlannedTotal === 0 the same way BudgetByPerson's
            // own width does -- a household with only zero-planned goals,
            // or none live at all, must render a flat empty bar, not
            // `NaN%`.
            const width = localPlannedTotal > 0 ? (goal.plannedMonthlyMinor / localPlannedTotal) * 100 : 0;
            return (
              <div
                key={goal.id}
                data-testid="monthly-contributions-segment"
                style={{ width: `${width}%`, background: SEGMENT_COLORS[index % SEGMENT_COLORS.length] }}
              />
            );
          })}
        </div>
        <div className="mt-3 flex flex-wrap gap-x-6 gap-y-2 text-[12.5px]">
          {live.map((goal, index) => (
            <span
              key={goal.id}
              data-testid="monthly-contributions-legend-item"
              className="flex items-center gap-1.5 text-ink"
            >
              <span
                className="h-2 w-2 flex-none rounded-sm"
                style={{ background: SEGMENT_COLORS[index % SEGMENT_COLORS.length] }}
              />
              {goal.name} · {formatMoney(goal.plannedMonthlyMinor, goal.currency, symbolFor(goal.currency))}
            </span>
          ))}
        </div>
      </div>

      <div className="mt-4 flex flex-wrap items-baseline gap-x-6 gap-y-1 border-t border-hairline pt-3.5 text-[13px]">
        <div>
          <span className="text-muted">{GOAL_COPY.plannedMonthlyTotalLabel}</span>{" "}
          <span data-testid="monthly-contributions-planned" className="font-semibold text-ink">
            {plannedTotalLabel}
          </span>
        </div>
        <div>
          <span className="text-muted">{GOAL_COPY.actualThisMonthLabel}</span>{" "}
          <span data-testid="monthly-contributions-actual" className="font-semibold text-accent">
            {actualLabel}
          </span>
        </div>
      </div>

      {diffSentence && (
        <p data-testid="monthly-contributions-diff" className="mt-1.5 text-[12px] text-muted">
          {diffSentence}
        </p>
      )}

      {/* Moved from GoalsPage.tsx (Task 14) -- the count explains a
          discrepancy between the two totals THIS card states (a no-rate
          goal's own figure sits in the legend above but not in either
          total below), so it belongs beside them rather than elsewhere on
          the page. */}
      {summary.excludedNoRate > 0 && (
        <p data-testid="goals-excluded-no-rate" className="mt-1.5 text-[12px] text-muted">
          {GOAL_COPY.excludedNoRate(summary.excludedNoRate)}
        </p>
      )}
    </div>
  );
}
