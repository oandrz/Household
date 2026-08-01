// The manual half of "Roll unspent into savings" (spec decision 1): a
// toggle that only acts when someone clicks it is not automatic, and the
// design's own present-tense "rolls into the Bali trip goal at month end"
// sentence describes something this codebase refuses to build (see
// budgetCopy.ts's own onPaceToSave comment, and BudgetPage.test.tsx's
// "rolls into" guard). This card is the manual substitute: on a closed
// month with real money left over, it offers one click, nothing until then.
//
// `closed` arrives as a prop rather than being re-derived here from `month`
// against the browser's own clock -- an earlier version tried exactly that
// (comparing `month` to month.ts's own currentMonth()) and it silently
// disagreed with the server's own `daysLeft === 0` in the one case that
// actually matters: a past month whose `month` value nonetheless equals
// "now" by the caller's own clock (a real risk under client/server clock
// skew, and the exact shape every `daysLeft: 0` test fixture in
// BudgetPage.test.tsx already uses). BudgetPage.tsx's own `daysLeft` is
// the server's authoritative answer to "is this month closed" -- the same
// figure every other card on this screen already trusts (spentSoFar,
// dailyPaceOk) -- so this component reads it the same way rather than
// opening a second, less trustworthy source of the same fact.
//
// Calls `useBudget(month, { enabled: false })` for the `rollOver` mutation
// only -- the GoalContributionsPanel.tsx/GoalModal.tsx convention: this
// card never fetches the month itself (BudgetPage's own mounted
// `useBudget(month)` already did, and a write here invalidates that same
// query, not a second one this component owns) -- and `useGoals()` (no
// `includeArchived`) for the picker: the household's live goal list, which
// already excludes archived goals for free. If a goal used for an earlier
// rollover is later archived, the destination sentence below just says "a
// goal" instead of naming it -- cheaper, and just as honest, as fetching
// the archived-inclusive list only to resolve one name.
import { useState } from "react";
import { apiErrorMessage } from "../auth/copy";
import { BUDGET_COPY } from "./budgetCopy";
import { formatMoney } from "./formatMoney";
import { useBudget } from "./useBudget";
import { useGoals } from "./useGoals";

// "2026-07" -> "July". Anchored on day 2, not day 1 -- the same UTC-offset
// reason BudgetPage.tsx's own monthNameOnly documents (a Date built on day 1
// reads back as the last day of the *previous* month at a negative UTC
// offset). Duplicated rather than imported: BudgetPage.tsx does not export
// it, and reaching into a sibling component's internals for four lines is
// the wrong seam (GoalModal.tsx's own targetMonthLabel comment makes the
// identical call).
function monthNameOnly(month: string): string {
  const [year, monthNum] = month.split("-").map(Number);
  return new Date(year, monthNum - 1, 2).toLocaleDateString("en-US", { month: "long" });
}

export function BudgetRolloverCard({
  month,
  closed,
  remainingMinor,
  currency,
  rolledOverTo,
  onRolledOver,
  symbol,
  excludedNoRate,
}: {
  month: string;
  // Whether this month is closed -- BudgetPage.tsx's own `data.daysLeft ===
  // 0` (see this file's own header comment on why that, not a client-side
  // recompute against the browser's clock, is the source of truth here). A
  // current or future month never shows anything in this component,
  // regardless of the other props -- the one rule this whole feature exists
  // to honour.
  closed: boolean;
  remainingMinor: number;
  currency: string;
  // The stamped destination goal's id (budgetMonthResponseSchema's own
  // `rolloverGoalId`), or null before any move has happened. Named after
  // the wire fact it mirrors, not "goalId" -- this describes the month,
  // not a selection this component is making.
  rolledOverTo: string | null;
  // Fired once a move succeeds, after the mutation's own onSuccess has
  // already invalidated both the month and /goals (useBudget.ts's own
  // comment) -- for a caller that wants to react to the moment itself. Not
  // what triggers the refetch; that has already happened by the time this
  // fires.
  onRolledOver: () => void;
  symbol?: string;
  // budgetMonthResponse's own count of expenses excluded from Spent for
  // want of an exchange rate -- the same figure the page's own footer note
  // names. remainingMinor is Budgeted minus that same (possibly
  // undercounted) Spent, so a positive count here means the "unspent"
  // figure the offer below names can read higher than what the household
  // truly had left. Owner ruling, 2026-08-01: name it, don't block on it.
  excludedNoRate: number;
}) {
  const { rollOver } = useBudget(month, { enabled: false });
  const goals = useGoals();

  const [pickerOpen, setPickerOpen] = useState(false);
  const [selectedGoalId, setSelectedGoalId] = useState("");
  const [error, setError] = useState<string | null>(null);
  const [submitting, setSubmitting] = useState(false);

  async function handleMove() {
    if (!selectedGoalId) return;
    setSubmitting(true);
    setError(null);
    try {
      await rollOver(selectedGoalId);
      setPickerOpen(false);
      setSelectedGoalId("");
      onRolledOver();
    } catch (err) {
      setError(apiErrorMessage(err, "Something went wrong. Please try again."));
      // Cleared here too, not only on success: a refusal (ROLLOVER_ALREADY_
      // DONE from another tab, most concretely) leaves `submitting` false
      // and the picker open, which re-enables Confirm -- without this, a
      // second click would fire a second, equally doomed POST against a
      // month the server has already resolved. The refetch this same error
      // triggers (useBudget.ts's own onError comment) usually swaps this
      // whole component to the destination branch before that click could
      // happen, but this is the guard, not a race this relies on.
      setSelectedGoalId("");
    } finally {
      setSubmitting(false);
    }
  }

  if (!closed) return null;
  if (remainingMinor <= 0 && rolledOverTo === null) return null;

  // `error` is rendered in both branches below (this one and the offer
  // further down), not only where the click that set it happened -- see the
  // offer branch's own comment on `error` for why: a 409 here can cause
  // `rolledOverTo` to flip non-null out from under an in-flight error
  // message, and the message needs to survive landing in this branch
  // instead, not just the one it started in.
  if (rolledOverTo !== null) {
    const destination = goals.data?.goals.find((g) => g.id === rolledOverTo);
    const amount = formatMoney(remainingMinor, currency, symbol);
    return (
      <div data-testid="budget-rollover" className="flex flex-col gap-1.5">
        <p data-testid="budget-rollover-done" className="text-[12px] leading-relaxed text-accent-dark">
          {destination
            ? BUDGET_COPY.rolledOverDone(amount, destination.name)
            : BUDGET_COPY.rolledOverDoneUnknown(amount)}
        </p>
        {error !== null && (
          <p role="alert" data-testid="budget-rollover-error" className="text-xs leading-snug text-danger">
            {error}
          </p>
        )}
      </div>
    );
  }

  // useGoals() already excludes archived goals (GET /goals, no
  // include_archived) -- nothing here re-filters for that reason. Split by
  // currency instead: only a primary-currency goal is offered as a real
  // choice (spec decision 11), and a goal in another currency is still
  // listed, disabled, with its own reason -- never silently dropped.
  const liveGoals = goals.data?.goals ?? [];
  const eligibleGoals = liveGoals.filter((g) => g.currency === currency);
  const ineligibleGoals = liveGoals.filter((g) => g.currency !== currency);

  const disabledReason = goals.loading
    ? null
    : goals.error
      ? BUDGET_COPY.rolloverLoadError
      : liveGoals.length === 0
        ? BUDGET_COPY.rolloverNoGoalsYet
        : eligibleGoals.length === 0
          ? BUDGET_COPY.rolloverNoPrimaryCurrencyGoal(currency)
          : null;
  const canOffer = !goals.loading && disabledReason === null;

  return (
    <div data-testid="budget-rollover-offer" className="flex flex-col gap-1.5">
      <p className="text-[12px] leading-relaxed text-accent-dark">
        {BUDGET_COPY.rolloverOffer(formatMoney(remainingMinor, currency, symbol), monthNameOnly(month))}
        {" · "}
        {!pickerOpen && (
          <button
            type="button"
            data-testid="budget-rollover-cta"
            disabled={!canOffer}
            onClick={() => setPickerOpen(true)}
            className="font-semibold text-accent underline disabled:cursor-not-allowed disabled:text-muted disabled:no-underline"
          >
            {BUDGET_COPY.moveIntoGoal}
          </button>
        )}
      </p>

      {/* Owner ruling, 2026-08-01: information, not a refusal -- rendered
          next to the offer and its button (where the decision is actually
          made), not folded into BudgetPage.tsx's own footer note at the
          bottom of the page, and visible whether or not the picker is
          open, since either is a moment the household could still commit
          to the move. The button above stays enabled regardless. */}
      {excludedNoRate > 0 && (
        <p data-testid="budget-rollover-excluded-note" className="text-[11.5px] text-muted">
          {BUDGET_COPY.rolloverExcludedNoRateNote(excludedNoRate)}
        </p>
      )}

      {!pickerOpen && disabledReason && (
        <p data-testid="budget-rollover-disabled-reason" className="text-[11.5px] text-muted">
          {disabledReason}
        </p>
      )}

      {pickerOpen && (
        <div className="flex flex-col gap-2">
          <select
            data-testid="budget-rollover-select"
            value={selectedGoalId}
            onChange={(event) => setSelectedGoalId(event.target.value)}
            className="rounded-lg border border-hairline bg-card px-3 py-2 text-[12.5px]"
          >
            <option value="">{BUDGET_COPY.chooseAGoal}</option>
            {eligibleGoals.map((g) => (
              <option key={g.id} value={g.id}>
                {g.name}
              </option>
            ))}
            {ineligibleGoals.map((g) => (
              <option key={g.id} value={g.id} disabled>
                {BUDGET_COPY.rolloverIneligibleOption(g.name, g.currency, currency)}
              </option>
            ))}
          </select>
          <div className="flex gap-2">
            <button
              type="button"
              data-testid="budget-rollover-cancel"
              onClick={() => {
                setPickerOpen(false);
                setSelectedGoalId("");
                setError(null);
              }}
              className="flex-1 rounded-lg border border-hairline py-1.5 text-center text-[12px] font-semibold text-label"
            >
              {BUDGET_COPY.cancel}
            </button>
            <button
              type="button"
              data-testid="budget-rollover-confirm"
              disabled={!selectedGoalId || submitting}
              onClick={handleMove}
              className="flex-1 rounded-lg bg-accent py-1.5 text-center text-[12px] font-semibold text-white disabled:cursor-not-allowed disabled:opacity-60"
            >
              {BUDGET_COPY.moveIntoGoal}
            </button>
          </div>
        </div>
      )}

      {/* Rendered at this level, not nested inside the `pickerOpen` block
          above, and duplicated in the `rolledOverTo !== null` branch's own
          return further up: useBudget.ts's own rollOver mutation already
          invalidates this month's query on this same error (its own
          comment -- ROLLOVER_ALREADY_DONE means the server's truth moved,
          not that nothing happened), and that refetch can swap this whole
          component from this offer into the destination-sentence branch
          before this catch even finishes running. Keeping `error` alive in
          both branches' own JSX is what lets this message survive that
          swap instead of unmounting with the button it was about. */}
      {error !== null && (
        <p role="alert" data-testid="budget-rollover-error" className="text-xs leading-snug text-danger">
          {error}
        </p>
      )}
    </div>
  );
}
