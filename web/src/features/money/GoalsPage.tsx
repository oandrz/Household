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
import { GOAL_COPY } from "./goalCopy";
import { useGoals } from "./useGoals";
import type { Goal } from "./goalSchemas";

export function GoalsPage() {
  const [includeArchived, setIncludeArchived] = useState(false);
  const goals = useGoals({ includeArchived });
  const currencies = useCurrencies();
  // Task 12's New/Edit modal and Task 13's contribution panel both open
  // through state owned here (the task brief's own "Produces" line). "new"
  // opens Create; a Goal opens Edit for that card. Neither modal exists yet,
  // so the branch at the bottom of this file is a minimal stub -- the same
  // seam Task 10's router placeholder was, named in a testid so Task 12 has
  // somewhere concrete to replace rather than adding its own state to this
  // file.
  const [modalGoal, setModalGoal] = useState<Goal | "new" | null>(null);
  // Restore is scoped per card, not one page-wide flag -- AccountsPanel.tsx's
  // own pendingIds reasoning: useGoals exposes one restoreGoal function
  // shared by every card, so tracking "is a call for this id in flight" has
  // to live here, not inside a single shared mutation's own isPending.
  const [restoringIds, setRestoringIds] = useState<Set<string>>(new Set());

  function handleRestore(id: string) {
    setRestoringIds((prev) => new Set(prev).add(id));
    void goals.restoreGoal(id).finally(() => {
      setRestoringIds((prev) => {
        const next = new Set(prev);
        next.delete(id);
        return next;
      });
    });
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
              onRestore={handleRestore}
              restorePending={restoringIds.has(goal.id)}
            />
          ))}
        </div>
      )}

      {summary.excludedNoRate > 0 && (
        <p data-testid="goals-excluded-no-rate" className="text-xs text-muted">
          {GOAL_COPY.excludedNoRate(summary.excludedNoRate)}
        </p>
      )}

      {/* Task 12's real modal replaces this branch outright, the same way
          Task 11 replaced router.tsx's own placeholder -- not something to
          edit around. modalGoal is "new" for Create, a Goal for Edit. */}
      {modalGoal && (
        <div data-testid="goal-modal-stub" role="dialog" aria-modal="true">
          {modalGoal === "new" ? "New goal" : `Edit ${modalGoal.name}`}
          <button type="button" onClick={() => setModalGoal(null)}>
            Close
          </button>
        </div>
      )}
    </div>
  );
}
