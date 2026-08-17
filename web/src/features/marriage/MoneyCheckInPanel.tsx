// The 10-minute money check-in (design's own dc.html panel: "10-min money
// check-in" over "Budget: 66% used, on pace to save S$1,780" / "Goals: 4 of 4
// on track"). Read live, stored nowhere -- design spec decision 3. This
// component computes NOTHING: every figure below comes straight off
// useBudget(month)'s and useGoals()'s own responses, both of which already
// carry every derived number (percentUsed, dailyPaceOk, onTrackCount...)
// computed once by BudgetService.Month/GoalService.List. Re-deriving any of
// them here is exactly how two screens come to disagree -- the same
// reasoning BudgetStatCards.tsx's own header comment gives for reading
// dailyPaceOk off the wire rather than recomputing it from remainingMinor.
//
// The two figures are scoped differently, and this panel says so rather than
// implying otherwise (decision 3): budget is the retro's own month
// (`useBudget(month)`, the same month useRetro(month) itself reads -- a June
// retro reopened in December still shows June's own budget), while goals are
// TODAY's live standing (`useGoals()`, no month argument at all -- a goal's
// progress is a contributions ledger against a target date, not a monthly
// bucket, so there is no month-scoped goals figure to ask for). The
// "Goals today" label carries that distinction in words, not just in code.
import { useBudget } from "../money/useBudget";
import { useGoals } from "../money/useGoals";
import { BUDGET_COPY } from "../money/budgetCopy";
import { GOAL_COPY } from "../money/goalCopy";
import { formatMoney } from "../money/formatMoney";
import { RETRO_COPY, monthNameOnly } from "./retroCopy";

export function MoneyCheckInPanel({ month }: { month: string }) {
  const budget = useBudget(month);
  const goals = useGoals();

  if (budget.loading || goals.loading) {
    return <p className="text-xs text-muted">Loading…</p>;
  }

  // useBudget(month) and useGoals() are both owner-gated on the server
  // (budget_handlers.go/goal_handlers.go's own requireOwner) -- a household
  // owner who holds marriage but not money capability 403s here the moment
  // this modal opens. Shown inline, never blocking Save/Finish: the check-in
  // is a prompt for a ten-minute conversation, not a gate on writing the
  // retro's own text.
  if (budget.error || goals.error || !budget.data || !goals.data) {
    return (
      <p role="alert" className="text-xs text-danger">
        {RETRO_COPY.checkInLoadError}
      </p>
    );
  }

  const b = budget.data;
  // domain.PercentUsed's own doc comment: "ok=false when nothing is
  // budgeted" -- true whether this month never got a budget row at all
  // (`budget: null`, BudgetPage's own empty state) or a budget exists with
  // every category cap at zero. Either way there is nothing to report a
  // percentage against, so this line reuses Budget's own "no budget set"
  // headline rather than rendering 0%, which is not what "nothing tracked"
  // means and is exactly the zero-render shape this feature has already
  // shipped four times (Tasks 10-12's own "0 done"/"0 actions"/empty meta/
  // empty notes defects).
  const budgetClause = b.percentOk
    ? [
        BUDGET_COPY.percentUsed(b.percentUsed),
        // dailyPaceOk gates the whole "on pace to save" figure the same way
        // BudgetStatCards.tsx's own fourth card is gated -- server-computed,
        // hidden when Remaining <= 0 or the viewed month isn't current, so
        // this reads the flag rather than re-deriving "is this worth saying"
        // from remainingMinor itself.
        b.dailyPaceOk ? BUDGET_COPY.onPaceToSave(formatMoney(b.remainingMinor, b.currency)) : null,
      ]
        .filter((part): part is string => part !== null)
        .join(" · ")
    : BUDGET_COPY.emptyHeadline(monthNameOnly(b.month));

  const summary = goals.data.summary;
  // The formulas table's own rule (GoalsPage.tsx's identical guard):
  // datedCount === 0 hides "N of M on track" rather than rendering "0 of 0",
  // and noDateCount === 0 hides "N with no date" the same way. If a
  // household has no live goals at all yet, both clauses are null --
  // checkInNoGoalsYet says so instead of leaving "Goals today:" with nothing
  // after the colon.
  const trackClause = summary.datedCount > 0 ? GOAL_COPY.onTrack(summary.onTrackCount, summary.datedCount) : null;
  const noDateClause = summary.noDateCount > 0 ? GOAL_COPY.withNoDate(summary.noDateCount) : null;
  const goalsClause =
    [trackClause, noDateClause].filter((part): part is string => part !== null).join(" · ") ||
    RETRO_COPY.checkInNoGoalsYet;

  return (
    <div className="flex flex-col gap-2">
      <h3 className="text-xs font-semibold text-label">{RETRO_COPY.moneyCheckInHeading}</h3>
      <div className="flex flex-col gap-1.5 rounded-[10px] border border-hairline bg-canvas px-3.5 py-3 text-[12.5px] leading-relaxed text-ink sm:flex-row sm:items-baseline sm:justify-between">
        <span data-testid="checkin-budget">
          {RETRO_COPY.budgetLabel}: {budgetClause}
        </span>
        <span data-testid="checkin-goals">
          {RETRO_COPY.goalsTodayLabel}: {goalsClause}
        </span>
      </div>
    </div>
  );
}
