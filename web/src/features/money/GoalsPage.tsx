// The Goals screen: cards in a grid, the empty state, and "Show archived".
// Composition only -- fetch orchestration lives in useGoals.ts (the
// BudgetPage.tsx/useBudget.ts convention, spec's own "GoalsPage.tsx stays a
// rendering shell" line), and every figure on a card goes through
// GoalCard.tsx, which is the one place formatMoney is called.
//
// "Show archived" is a union, not a filter swap: useGoals({ includeArchived })
// re-keys the query (goalsQueryKey), and the server's own response already
// contains live-and-archived together once asked -- AccountsPanel.tsx's own
// shape, restated in goalSchemas.ts's header comment. This file never filters
// `data.goals` itself; it renders exactly the array the hook returned.
import { useState } from "react";
import { ApiError } from "../../api/client";
import { useCurrencies } from "../auth/useAuth";
import { ToggleSwitch } from "../../components/ToggleSwitch";
import { GoalCard } from "./GoalCard";
import { GoalContributionsPanel } from "./GoalContributionsPanel";
import { GoalModal } from "./GoalModal";
import { MonthlyContributionsCard } from "./MonthlyContributionsCard";
import { GOAL_COPY } from "./goalCopy";
import { useGoals } from "./useGoals";
import type { Goal } from "./goalSchemas";

export function GoalsPage() {
  const [includeArchived, setIncludeArchived] = useState(false);
  const goals = useGoals({ includeArchived });
  const currencies = useCurrencies();
  // Task 12's New/Edit modal and Task 13's contribution panel both open
  // through state owned here (the task brief's own "Produces" line). "new"
  // opens Create; a Goal opens Edit for that card. Task 13's panel does not
  // exist yet, so only the New/Edit modal renders below today.
  const [modalGoal, setModalGoal] = useState<Goal | "new" | null>(null);
  // Task 13's own panel. A separate state slot from modalGoal above, not a
  // union sharing it -- the two surfaces can never be open for the same
  // click (GoalCard.tsx's own onAddContribution stops its event before it
  // ever reaches the card's onClick that would set modalGoal), but keeping
  // them as two independent pieces of state means a future change to one
  // never has to reason about the other's cases.
  const [contributingGoal, setContributingGoal] = useState<Goal | null>(null);
  // Archive and restore are scoped per card, not one page-wide flag --
  // AccountsPanel.tsx's own pendingIds reasoning: useGoals exposes one
  // archiveGoal/restoreGoal function shared by every card, so tracking "is a
  // call for this id in flight" has to live here, not inside a single shared
  // mutation's own isPending. One set covers both directions: a given card
  // offers exactly one of the two at a time (GoalCard gates them on
  // `archived`), so there is never a card with an archive and a restore in
  // flight at once for the set to have to tell apart.
  const [pendingIds, setPendingIds] = useState<Set<string>>(new Set());

  function trackPending(id: string, call: Promise<void>) {
    setPendingIds((prev) => new Set(prev).add(id));
    void call.finally(() => {
      setPendingIds((prev) => {
        const next = new Set(prev);
        next.delete(id);
        return next;
      });
    });
  }

  function handleRestore(id: string) {
    trackPending(id, goals.restoreGoal(id));
  }

  // The only way a household reaches the archived view at all. Task 11
  // shipped "Show archived" and Restore without it, and useGoals.archiveGoal
  // sat unwired behind its own passing test until the Task 18 walk found the
  // dead end at criterion 12 (docs/LEARNING.md).
  function handleArchive(id: string) {
    trackPending(id, goals.archiveGoal(id));
  }

  const symbolFor = (currency: string) =>
    currencies.data?.currencies.find((c) => c.code === currency)?.symbol;

  if (goals.loading) {
    return <p className="p-9 text-xs text-muted">Loading…</p>;
  }

  if (goals.error) {
    // GET /goals is money AND owner-gated (spec decision 10) -- a limited
    // member holding money reaches this route (RequireCapability only checks
    // the capability, not the role) and the request answers 403. Branching
    // on the real status, not a second useMe() role check: a role check here
    // would be a second source of truth that could disagree with what the
    // server actually decided, and a third stub in every test of this page.
    const status = goals.error instanceof ApiError ? goals.error.status : undefined;
    if (status === 403) {
      return (
        <section data-testid="goals-owner-only" className="m-9 rounded-xl border border-hairline bg-card p-[22px]">
          <h1 className="text-[23px] font-semibold tracking-[-0.02em] text-ink">{GOAL_COPY.title}</h1>
          <h2 className="mt-4 text-xs text-muted">{GOAL_COPY.ownerOnlyHeading}</h2>
          <p className="mt-1.5 text-[13px] text-ink">{GOAL_COPY.ownerOnlyBody}</p>
        </section>
      );
    }
    return (
      <p role="alert" data-testid="goals-load-error" className="p-9 text-xs text-danger">
        {GOAL_COPY.loadError}
      </p>
    );
  }

  // query.error is null and loading is false at this point, so data is
  // present -- TanStack Query's own contract. This guards the type only, the
  // same defensive shape BudgetPage.tsx's `!budget.data` guard uses.
  if (!goals.data) {
    return null;
  }

  const data = goals.data;
  const { summary } = data;
  // The formulas table's own rule: `datedCount === 0` hides the "X of Y on
  // track" clause rather than rendering "0 of 0". Composed from `summary`,
  // never by filtering `data.goals` -- with include_archived=true that array
  // grows while summary counts live goals only (goalsSummarySchema's own
  // comment), so filtering here would make the header drift from what the
  // server actually counted the moment archived rows enter the list.
  const trackClause = summary.datedCount > 0 ? GOAL_COPY.onTrack(summary.onTrackCount, summary.datedCount) : null;
  const noDateClause = summary.noDateCount > 0 ? GOAL_COPY.withNoDate(summary.noDateCount) : null;
  const subtitle = [trackClause, noDateClause].filter(Boolean).join(" · ");
  // Shown once the toggle is on and the union came back with nothing
  // archived in it -- AccountsPanel.tsx's own `noneArchived` shape, so
  // switching the toggle with nothing behind it explains itself instead of
  // silently rendering the same live list again.
  const noneArchived = includeArchived && data.goals.length > 0 && data.goals.every((g) => g.archivedAt === null);

  return (
    <div data-testid="goals-page" className="flex flex-col gap-5 px-9 py-8">
      <div className="flex items-start justify-between gap-4">
        <div>
          <h1 className="text-[23px] font-semibold tracking-[-0.02em] text-ink">{GOAL_COPY.title}</h1>
          {subtitle && (
            <p data-testid="goals-subtitle" className="mt-1 text-[13px] text-muted">
              {subtitle}
            </p>
          )}
        </div>
        {/* The toggle is NOT gated on `data.goals.length > 0` -- a household
            that archives its last live goal would otherwise lose every way
            back to it (FinancesPage.tsx's own FirstRunPanel carries the
            identical fix, and its own comment names the defect: "an owner
            who archives their household's only account has no way back").
            "+ New goal" stays gated: the empty state below offers its own
            "Create your first goal" action instead, so this would be a
            second, redundant way into the same modal on that screen. */}
        <div className="flex items-center gap-3.5">
          <div className="flex items-center gap-1.5 text-[11px] text-muted">
            <ToggleSwitch
              checked={includeArchived}
              onChange={() => setIncludeArchived((prev) => !prev)}
              label={GOAL_COPY.archivedToggle}
            />
            {GOAL_COPY.archivedToggle}
          </div>
          {data.goals.length > 0 && (
            <button
              type="button"
              data-testid="goals-new"
              onClick={() => setModalGoal("new")}
              className="rounded-lg bg-accent px-3.5 py-2 text-[13px] font-semibold text-white"
            >
              {GOAL_COPY.newGoal}
            </button>
          )}
        </div>
      </div>

      {noneArchived && (
        <p data-testid="goals-archived-empty" className="text-xs text-muted">
          {GOAL_COPY.archivedEmpty}
        </p>
      )}

      {data.goals.length === 0 ? (
        <div data-testid="goals-empty-state" className="rounded-xl border border-hairline bg-card p-16 text-center">
          <div className="text-[19px] font-semibold tracking-[-0.01em] text-ink">{GOAL_COPY.emptyHeadline}</div>
          <p className="mx-auto mt-2 max-w-[420px] text-[13.5px] leading-relaxed text-muted">
            {GOAL_COPY.emptyBody}
          </p>
          {/* One action, no templates -- a goal has no equivalent of a
              category set to prefill, and a fake starter goal is a number
              nobody chose (task brief). */}
          <div className="mt-6 flex justify-center">
            <button
              type="button"
              data-testid="goals-create-first"
              onClick={() => setModalGoal("new")}
              className="rounded-lg bg-accent px-5 py-2.5 text-[13px] font-semibold text-white"
            >
              {GOAL_COPY.createFirstGoal}
            </button>
          </div>
        </div>
      ) : (
        <div data-testid="goals-grid" className="grid grid-cols-1 gap-4 lg:grid-cols-2">
          {data.goals.map((goal) => (
            <GoalCard
              key={goal.id}
              goal={goal}
              symbol={symbolFor(goal.currency)}
              onEdit={setModalGoal}
              onAddContribution={setContributingGoal}
              onArchive={handleArchive}
              onRestore={handleRestore}
              pending={pendingIds.has(goal.id)}
            />
          ))}
        </div>
      )}

      {/* Task 14's own card, below the grid -- gated the same as "+ New
          goal" above: a household with zero goals altogether has nothing
          for this card to summarise, and the empty state above already
          covers that screen, so the two never render together. Once
          toggled to "Show archived" with only archived rows in view,
          data.goals.length is still > 0 (the union, not a filter swap) and
          the card renders showing summary's own live-only totals -- both
          legitimately zero there, not a bug. excludedNoRate now renders
          from inside this card, not here -- the count explains a
          discrepancy between the two totals the card itself states, so it
          belongs beside them rather than elsewhere on the page. */}
      {data.goals.length > 0 && (
        <MonthlyContributionsCard
          goals={data.goals}
          summary={summary}
          currency={data.currency}
          symbolFor={symbolFor}
        />
      )}

      {/* modalGoal is "new" for Create, a Goal for Edit -- GoalCard.tsx's
          own `clickable = !archived && Boolean(onEdit)` already refuses a
          click on an archived card, so onEdit={setModalGoal} above never
          opens this in edit mode for one. Gated on currencies.data because
          GoalModal's `currencies` prop is required, not optional -- this
          page already fetches it (symbolFor above needs it too), and it is
          small and independent of `goals.data`, so in practice it has
          settled by the time a person has clicked anything on this page;
          this guard only matters for an implausibly fast double-click.
          onSaved and onClose both just close the modal -- the same
          BudgetPage.tsx/BudgetModal convention, since createGoal/updateGoal
          (useGoals.ts) already invalidate the goals query on success, so
          this page's own `useGoals({ includeArchived })` call -- mounted
          and active the whole time the modal is open -- refetches on its
          own with no extra call needed here. */}
      {modalGoal && currencies.data && (
        <GoalModal
          mode={modalGoal === "new" ? "create" : "edit"}
          goal={modalGoal === "new" ? undefined : modalGoal}
          currencies={currencies.data.currencies}
          primaryCurrency={data.currency}
          onClose={() => setModalGoal(null)}
          onSaved={() => setModalGoal(null)}
        />
      )}

      {/* Task 13's own panel, opened by GoalCard's "Add contribution"
          control -- mounted only while a goal is selected, the same
          conditional-mount gate modalGoal above uses, and the gate
          GoalContributionsPanel.tsx's own header comment relies on for its
          `useGoalContributions(goal.id, true)` call to mean "while open,"
          not "always." symbol is `symbolFor(contributingGoal.currency)`, the
          same expression GoalCard above already uses -- still no
          currencies.data GUARD needed (unlike GoalModal above): symbolFor
          returns undefined before that query settles, and formatMoney falls
          back to the bare currency code for undefined, so there is nothing
          here that must block on a second query. Finding 2 of the review:
          this panel used to take no symbol at all, rendering "SGD 400.00"
          directly above a card reading "S$400.00". */}
      {contributingGoal && (
        <GoalContributionsPanel
          goal={contributingGoal}
          symbol={symbolFor(contributingGoal.currency)}
          onClose={() => setContributingGoal(null)}
        />
      )}
    </div>
  );
}
