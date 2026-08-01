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
  onRestore,
  restorePending,
}: {
  goal: Goal;
  symbol?: string;
  // Only a live card opens the edit modal on click -- an archived row's own
  // affordance is Restore below; editing a goal that isn't live is not a
  // flow this feature supports (restore it first).
  onEdit?: (goal: Goal) => void;
  onRestore?: (id: string) => void;
  // True while a restore call for *this* card is in flight -- scoped per
  // card, not one page-wide flag, the same reason AccountsPanel.tsx's own
  // pendingIds is per-row: a single mutation instance is shared by every
  // card, so its own isPending only ever reflects the most recently
  // dispatched call.
  restorePending?: boolean;
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
          {formatMoney(goal.contributedMinor, goal.currency, symbol)} of{" "}
          {formatMoney(goal.targetMinor, goal.currency, symbol)}
          {/* The date clause is omitted entirely when targetMonth is null
              (a dateless goal, decision 3) -- not rendered empty. */}
          {goal.targetMonth && ` · ${GOAL_COPY.dateClause(targetMonthLabel(goal.targetMonth))}`}
        </div>
        <div className="mt-2.5 text-[12px] text-muted">
          {GOAL_COPY.perMonth(formatMoney(goal.plannedMonthlyMinor, goal.currency, symbol))}
        </div>
        {/* "Behind" is never a verdict without its arithmetic (task brief).
            Scoped to behind specifically, not "whenever requiredMonthlyOk" --
            an on_track goal's own planned-monthly line above already says
            enough, and repeating a second, near-identical figure under it
            would read as the goal being in two states at once. */}
        {goal.status === "behind" && goal.requiredMonthlyOk && (
          <div data-testid="goal-card-required" className="mt-1 text-[12px] font-semibold text-danger">
            {GOAL_COPY.needsPerMonth(formatMoney(goal.requiredMonthlyMinor, goal.currency, symbol))}
          </div>
        )}
        {archived && onRestore && (
          <button
            type="button"
            aria-label={`Restore ${goal.name}`}
            disabled={restorePending}
            onClick={() => onRestore(goal.id)}
            className="mt-2.5 text-[11px] font-semibold text-accent disabled:cursor-not-allowed disabled:opacity-60"
          >
            {GOAL_COPY.restore}
          </button>
        )}
      </div>
    </div>
  );
}
