// The "Goals on track" card (design/Household Dashboard.dc.html's
// go_goals-linked summary tile): a heading, the "X of Y" figure, and the
// next dated goal beneath it. Pure presentation over `summary`, the same
// contract GoalCard.tsx and GoalsPage.tsx's own subtitle already hold --
// every figure here arrives already computed from GET /goals, so this
// component's only job is formatting, never arithmetic.
import { Link } from "@tanstack/react-router";
import { OVERVIEW_COPY } from "./copy";
import type { GoalsResponse } from "../money/goalSchemas";

// "2026-12" -> "Dec 2026". Duplicated from GoalCard.tsx's own private
// targetMonthLabel rather than imported -- the identical trade-off that
// component's own sibling, GoalModal.tsx, already makes for the same
// four-line function: reaching into another feature's internals for this
// is the wrong seam, and Overview does not otherwise depend on Money's
// component internals at all. Anchored on day 2, not day 1, for the same
// UTC-offset reason both of those comments give: a Date built on day 1
// reads back as the last day of the *previous* month at a negative offset.
function targetMonthLabel(month: string): string {
  const [year, monthNum] = month.split("-").map(Number);
  return new Date(year, monthNum - 1, 2).toLocaleDateString("en-US", {
    month: "short",
    year: "numeric",
  });
}

export function GoalsCard({ goals }: { goals: GoalsResponse }) {
  const { summary } = goals;
  // The formulas table's own rule (spec's "X of Y on track"): datedCount
  // counts unarchived, dated, unachieved goals -- a household whose goals
  // are all dateless has nothing to be on track for, so `=== 0` hides this
  // clause entirely rather than rendering "0 of 0", which would read as
  // failure rather than as "nothing to measure."
  const trackClause = summary.datedCount > 0 ? OVERVIEW_COPY.goalsOnTrack(summary.onTrackCount, summary.datedCount) : null;
  // Mirrors GoalsPage.tsx's own noDateClause: named, not folded silently
  // into a denominator that would otherwise quietly shrink.
  const noDateClause = summary.noDateCount > 0 ? OVERVIEW_COPY.goalsWithNoDate(summary.noDateCount) : null;
  // Distinct from datedCount === 0: that state still has goals, just none
  // dated. This is the household having no live goals at all yet -- the
  // state that would otherwise render this card as a heading over nothing,
  // the same blank-card shape the interim Overview's own defect took.
  const hasAnyGoals = summary.datedCount + summary.noDateCount > 0;

  return (
    <section
      aria-labelledby="overview-goals-heading"
      className="flex flex-col rounded-xl border border-hairline bg-card p-[22px]"
    >
      <h2 id="overview-goals-heading" className="text-xs text-muted">
        {OVERVIEW_COPY.goalsHeading}
      </h2>

      {!hasAnyGoals ? (
        <>
          <p className="mt-1.5 text-[15px] text-ink">{OVERVIEW_COPY.goalsNone}</p>
          <Link to="/money/goals" className="mt-3 text-[13px] font-semibold text-accent">
            {OVERVIEW_COPY.goalsCreate}
          </Link>
        </>
      ) : (
        <>
          {trackClause && (
            <p className="mt-1.5 text-[30px] font-semibold tracking-[-0.03em] text-ink">{trackClause}</p>
          )}
          {/* Omitted outright when there is no next dated goal (spec's own
              "None -> the line is omitted"), not rendered blank -- the same
              rule the date clause on GoalCard.tsx follows for a dateless
              goal. */}
          {summary.nextGoal && (
            <p className="mt-1 text-[11.5px] text-muted">
              {OVERVIEW_COPY.goalsNext(
                summary.nextGoal.name,
                summary.nextGoal.targetMonth ? targetMonthLabel(summary.nextGoal.targetMonth) : null,
              )}
            </p>
          )}
          {noDateClause && <p className="mt-1 text-[11.5px] text-muted">{noDateClause}</p>}
        </>
      )}
    </section>
  );
}
