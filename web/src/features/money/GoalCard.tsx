// One card on the Goals screen: a progress ring, the name, the status pill,
// the "S$2,600 of S$4,000 · by Dec 2026" line and the planned-monthly line.
// Pure presentation over an already-parsed `Goal` -- every figure arrives
// already computed (percent, status, requiredMonthlyMinor) from GET /goals,
// and this component's only job is formatting, never arithmetic (the task
// brief's own rule: "the page never does money arithmetic").
//
// No "from OCBC Joint" line -- spec decision 6. The design draws a
// funding-source account on every card; contributions move no real money
// (spec decision 1), so naming an account would be decoration on a screen
// whose whole job is to be believed.
import { GOAL_COPY } from "./goalCopy";
import { formatMoney } from "./formatMoney";
import type { Goal } from "./goalSchemas";

// "2026-12" -> "Dec 2026". Anchored on day 2, not day 1 -- BudgetPage.tsx's
// own monthLabel comment: a Date built on day 1 reads back as the last day
// of the *previous* month at a negative UTC offset, which would show the
// wrong month here.
function targetMonthLabel(month: string): string {
  const [year, monthNum] = month.split("-").map(Number);
  return new Date(year, monthNum - 1, 2).toLocaleDateString("en-US", {
    month: "short",
    year: "numeric",
  });
}

// One entry per real status -- "none" is handled by the caller (no pill at
// all, decision 3), so it has no entry here rather than an empty one a
// switch's `default` would have to refuse. achieved and on_track share a
// style: both are "this goal is fine," and only behind needs to read as a
// problem.
const STATUS_PILL: Record<Exclude<Goal["status"], "none">, { label: string; className: string }> = {
  on_track: { label: GOAL_COPY.statusOnTrack, className: "bg-callout text-accent" },
  achieved: { label: GOAL_COPY.statusAchieved, className: "bg-callout text-accent" },
  behind: { label: GOAL_COPY.statusBehind, className: "bg-danger-soft text-danger" },
};

function ProgressRing({ percent }: { percent: number }) {
  // A plain conic-gradient div, the design's own approach (Household
  // Dashboard.dc.html's is_goals cards) -- `percent` already arrives capped
  // at 100 and floored at 0 from the server (domain.GoalProgressPercent's own
  // comment), so this never re-derives or re-clamps it.
  return (
    <div
      data-testid="goal-card-ring"
      className="flex h-[92px] w-[92px] flex-none items-center justify-center rounded-full"
      style={{
        background: `conic-gradient(var(--color-accent) 0 ${percent}%, var(--color-canvas) ${percent}% 100%)`,
      }}
    >
      <div className="flex h-[70px] w-[70px] items-center justify-center rounded-full bg-card text-[16px] font-semibold text-ink">
        {percent}%
      </div>
    </div>
  );
}

export function GoalCard({
  goal,
  symbol,
  onEdit,
  onAddContribution,
  onArchive,
  onRestore,
  pending,
}: {
  goal: Goal;
  symbol?: string;
  // Only a live card opens the edit modal on click -- an archived row's own
  // affordance is Restore below; editing a goal that isn't live is not a
  // flow this feature supports (restore it first).
  onEdit?: (goal: Goal) => void;
  // Task 13's own control. Gated on !archived the same way onEdit's own
  // `clickable` is: the API 422s a contribution against an archived goal
  // (GoalService's own AddContribution check), so this button never offers a
  // flow the server would refuse anyway.
  onAddContribution?: (goal: Goal) => void;
  onRestore?: (id: string) => void;
  // The counterpart to onRestore, and the only way a household can reach the
  // archived view at all. Gated on !archived below, exactly as onRestore is
  // gated on archived, so a card never offers both at once -- AccountRow's
  // own either/or in AccountsPanel.tsx. No window.confirm, for that row's
  // stated reason: archiving is reversible from the archived view, and a
  // native browser modal blocks the tooling every browser walk here uses.
  onArchive?: (id: string) => void;
  // True while an archive OR restore call for *this* card is in flight --
  // scoped per card, not one page-wide flag, the same reason
  // AccountsPanel.tsx's own pendingIds is per-row: a single mutation
  // instance is shared by every card, so its own isPending only ever
  // reflects the most recently dispatched call. Named `pending` rather than
  // `restorePending` now that it covers both directions, matching
  // AccountRow's own prop of the same name.
  pending?: boolean;
}) {
  const archived = goal.archivedAt !== null;
  const clickable = !archived && Boolean(onEdit);
  const pill = goal.status === "none" ? null : STATUS_PILL[goal.status];

  return (
    <div
      data-testid="goal-card"
      role={clickable ? "button" : undefined}
      tabIndex={clickable ? 0 : undefined}
      onClick={clickable ? () => onEdit?.(goal) : undefined}
      onKeyDown={
        clickable
          ? (event) => {
              if (event.key === "Enter" || event.key === " ") {
                event.preventDefault();
                onEdit?.(goal);
              }
            }
          : undefined
      }
      className={`flex items-center gap-[22px] rounded-xl border border-hairline bg-card p-6 ${
        clickable ? "cursor-pointer" : ""
      }`}
    >
      <ProgressRing percent={goal.percent} />
      <div className="flex-1">
        <div className="flex items-baseline justify-between gap-2">
          <div className="text-[15px] font-semibold text-ink">
            {goal.name}
            {archived && (
              <span className="ml-1.5 text-[11px] font-normal text-muted">{GOAL_COPY.archivedMarker}</span>
            )}
          </div>
          {pill && (
            <span
              data-testid="goal-card-status"
              className={`rounded-full px-2.5 py-1 text-[11px] font-semibold ${pill.className}`}
            >
              {pill.label}
            </span>
          )}
        </div>
        <div className="mt-1.5 text-[13px] text-muted">
          <span className="tabular">{formatMoney(goal.contributedMinor, goal.currency, symbol)}</span> of{" "}
          <span className="tabular">{formatMoney(goal.targetMinor, goal.currency, symbol)}</span>
          {/* The date clause is omitted entirely when targetMonth is null
              (a dateless goal, decision 3) -- not rendered empty. Kept
              outside both tabular spans above: it carries a year
              ("Dec 2026"), and the placement rule excludes dates by name. */}
          {goal.targetMonth && ` · ${GOAL_COPY.dateClause(targetMonthLabel(goal.targetMonth))}`}
        </div>
        <div className="tabular mt-2.5 text-[12px] text-muted">
          {GOAL_COPY.perMonth(formatMoney(goal.plannedMonthlyMinor, goal.currency, symbol))}
        </div>
        {/* "Behind" is never a verdict without its arithmetic (task brief).
            Scoped to behind specifically, not "whenever requiredMonthlyOk" --
            an on_track goal's own planned-monthly line above already says
            enough, and repeating a second, near-identical figure under it
            would read as the goal being in two states at once. */}
        {goal.status === "behind" && goal.requiredMonthlyOk && (
          <div data-testid="goal-card-required" className="tabular mt-1 text-[12px] font-semibold text-danger">
            {GOAL_COPY.needsPerMonth(formatMoney(goal.requiredMonthlyMinor, goal.currency, symbol))}
          </div>
        )}
        {archived && onRestore && (
          <button
            type="button"
            aria-label={`Restore ${goal.name}`}
            disabled={pending}
            onClick={() => onRestore(goal.id)}
            // min-h-11/sm:min-h-0: TransactionFilters.tsx's own
            // SELECT_CLASS comment has the measured reason a control this
            // size falls short of the 44px floor on a phone. Not dense --
            // this is the card's one action in that state, not a row it
            // shares with anything else.
            className="-mx-1.5 mt-2.5 inline-flex min-h-11 items-center rounded-md px-1.5 text-[11px] font-semibold text-accent transition-colors duration-[var(--transition-state)] hover:bg-canvas active:bg-toggle-off disabled:cursor-not-allowed disabled:opacity-60 sm:min-h-0"
          >
            {GOAL_COPY.restore}
          </button>
        )}
        {!archived && (onAddContribution || onArchive) && (
          <div className="mt-2.5 flex items-center gap-3.5 text-[11px] font-semibold">
            {onAddContribution && (
              <button
                type="button"
                // No aria-label suffixing the goal name (unlike Restore
                // above, which disambiguates for tests that query it
                // unscoped across several archived cards at once) -- every
                // caller of this control in practice reaches it via
                // `within(card)`, and the visible text alone is already this
                // button's accessible name. Both handlers below stop the
                // event rather than letting it bubble to the card's own
                // onClick/onKeyDown above -- click and keydown are two
                // independent events (a real browser's
                // Enter-activates-a-button default action fires a *separate*
                // click after the keydown already bubbled), so stopping only
                // one leaves the other free to reach the card and open the
                // edit modal behind this button instead of the contributions
                // panel it actually asked for.
                onClick={(event) => {
                  event.stopPropagation();
                  onAddContribution(goal);
                }}
                onKeyDown={(event) => event.stopPropagation()}
                className="-mx-1.5 min-h-11 rounded-md px-1.5 text-accent transition-colors duration-[var(--transition-state)] hover:bg-canvas active:bg-toggle-off sm:min-h-0"
              >
                {GOAL_COPY.addContribution}
              </button>
            )}
            {onArchive && (
              <button
                type="button"
                // Named, unlike Add contribution beside it: "Archive" alone
                // is ambiguous the moment two cards are on screen, which is
                // the same reason Restore above and AccountRow's own Archive
                // both carry the row's name.
                aria-label={`Archive ${goal.name}`}
                disabled={pending}
                // Stops BOTH events for the identical reason Add
                // contribution's own comment gives -- without the keydown
                // handler, pressing Enter on this button archives the goal
                // AND opens the edit modal for it behind the disappearing
                // card. fireEvent.click in a test never presses a key, so
                // only a real browser catches that (docs/HANDOVER.md §1's
                // TransactionFilters defect, in a second form).
                onClick={(event) => {
                  event.stopPropagation();
                  onArchive(goal.id);
                }}
                onKeyDown={(event) => event.stopPropagation()}
                className="-mx-1.5 min-h-11 rounded-md px-1.5 text-danger transition-colors duration-[var(--transition-state)] hover:bg-canvas active:bg-toggle-off disabled:cursor-not-allowed disabled:opacity-60 sm:min-h-0"
              >
                {GOAL_COPY.archive}
              </button>
            )}
          </div>
        )}
      </div>
    </div>
  );
}
