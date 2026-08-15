// The "Add contribution" panel (design spec's own line: "'Add contribution'
// on each card opens a small form ... and lists that goal's recent
// contributions with a delete control behind an in-page confirmation, never
// window.confirm"). Built on the shared Modal primitive -- the same choice
// GoalModal.tsx/TransactionModal.tsx/BudgetModal.tsx/AccountModal.tsx all
// already made for their own small write-and-list surfaces, and `onClose` in
// this component's own produced signature only makes sense against that
// primitive's own contract.
//
// Calls `useGoals({ enabled: false })` for the two mutations only, the same
// reason GoalModal.tsx's own header comment gives: this component is never
// handed a mutation as a prop, and it has no business fetching the goals
// list itself -- GoalsPage's own mounted `useGoals()` is what actually
// refetches once these mutations invalidate it, moving the card's progress
// while this panel is still open over it.
//
// useGoalContributions(goal.id, true) is its own separate query, gated by
// this component only ever being mounted while the panel is open --
// GoalsPage's own `contributingGoal && (...)` conditional render, the same
// mount-is-the-gate pattern `modalGoal && (<GoalModal ... />)` already uses
// one state slot over. `true` here is not "always fetch": it is "fetch for
// exactly as long as this component exists," which is what
// `useGoalContributions`'s own header comment means by "the same shape
// useBudgetHistory.ts uses" -- BudgetPage.tsx gates that hook with a boolean
// it flips; this file's mount/unmount is the equivalent gate for a component
// that (unlike BudgetHistoryModal) owns its own query instead of receiving
// already-fetched data as a prop.
import { type FormEvent, useState } from "react";
import { FieldPair } from "../../components/FieldPair";
import { Modal } from "../../components/Modal";
import { apiErrorMessage } from "../auth/copy";
import { describeAmountError, formatMoney, toMinorUnits } from "./formatMoney";
import { GOAL_COPY, contributionSourceLabel } from "./goalCopy";
import { useGoalContributions, useGoals } from "./useGoals";
import type { Goal, GoalContribution } from "./goalSchemas";

// today() reads the *local* calendar date via getFullYear/getMonth/getDate,
// never toISOString() (which converts to UTC first) -- the same function,
// duplicated for the same reason, as TransactionModal.tsx's/AccountModal.tsx's
// own today(): a small duplicated four-line function is a smaller risk than
// coupling three features' date handling through one shared import.
function today(): string {
  const now = new Date();
  const year = now.getFullYear();
  const month = String(now.getMonth() + 1).padStart(2, "0");
  const day = String(now.getDate()).padStart(2, "0");
  return `${year}-${month}-${day}`;
}

function ContributionRow({
  contribution,
  currency,
  symbol,
  confirming,
  deleting,
  error,
  onAskToDelete,
  onCancelDelete,
  onConfirmDelete,
}: {
  contribution: GoalContribution;
  currency: string;
  // The goal's own symbol (GoalsPage.tsx's `symbolFor(goal.currency)`), not
  // the household's primary-currency one -- a contribution is its goal's
  // currency by construction (00007_goals.sql's own comment), and every
  // other money figure on this screen (GoalCard's own progress amounts)
  // already renders with the symbol, not the bare ISO code. undefined falls
  // back to the code, formatMoney's own contract.
  symbol: string | undefined;
  confirming: boolean;
  deleting: boolean;
  // Scoped to this one row, not a single panel-level slot -- a 404 on row 1
  // of four must read next to row 1, not detached at the bottom of the
  // list under row 4 (a real defect a single-row test would never have
  // caught: it has nowhere else to sit).
  error: string | null;
  onAskToDelete: () => void;
  onCancelDelete: () => void;
  onConfirmDelete: () => void;
}) {
  const label = contributionSourceLabel(contribution.source, contribution.sourceBudgetMonth, contribution.note);

  return (
    <div
      data-testid="goal-contribution-row"
      className="flex flex-col gap-2 border-b border-hairline py-2.5 last:border-b-0"
    >
      <div className="flex items-center justify-between gap-3">
        <div>
          <div className="text-[13px] font-semibold text-ink">{label}</div>
          <div className="text-[11px] text-muted">{contribution.occurredOn}</div>
        </div>
        <div className="flex items-center gap-3">
          <span className="text-[13px] font-semibold text-ink">
            {formatMoney(contribution.amountMinor, currency, symbol)}
          </span>
          {!confirming && (
            <button
              type="button"
              onClick={onAskToDelete}
              className="min-h-11 text-[11.5px] font-semibold text-danger sm:min-h-0"
            >
              {GOAL_COPY.deleteContributionTrigger}
            </button>
          )}
        </div>
      </div>
      {confirming && (
        <div className="flex flex-col gap-2 rounded-[10px] border border-hairline p-2.5">
          <p className="text-[12.5px] text-ink">{GOAL_COPY.deleteContributionConfirmBody}</p>
          <div className="flex gap-2.5">
            <button
              type="button"
              onClick={onCancelDelete}
              // min-h-11/sm:min-h-0: py-2.5 alone measured short of the 44px
              // floor at this text size -- TransactionFilters.tsx's own
              // SELECT_CLASS comment has the measured numbers.
              className="min-h-11 flex-1 rounded-lg border border-hairline py-2.5 text-center text-[12.5px] font-semibold text-label sm:min-h-0 sm:py-1.5"
            >
              {GOAL_COPY.deleteContributionCancelAction}
            </button>
            <button
              type="button"
              disabled={deleting}
              onClick={onConfirmDelete}
              className="min-h-11 flex-1 rounded-lg bg-danger py-2.5 text-center text-[12.5px] font-semibold text-white disabled:cursor-not-allowed disabled:opacity-60 sm:min-h-0 sm:py-1.5"
            >
              {GOAL_COPY.deleteContributionConfirmAction}
            </button>
          </div>
        </div>
      )}
      {error !== null && (
        <p role="alert" className="text-xs leading-snug text-danger">
          {error}
        </p>
      )}
    </div>
  );
}

export function GoalContributionsPanel({
  goal,
  symbol,
  onClose,
}: {
  goal: Goal;
  // GoalsPage.tsx's own `symbolFor(goal.currency)` -- see ContributionRow's
  // own `symbol` prop comment for why this must be the goal's currency, not
  // the household's primary one.
  symbol: string | undefined;
  onClose: () => void;
}) {
  const { addContribution, deleteContribution } = useGoals({ enabled: false });
  const contributions = useGoalContributions(goal.id, true);

  const [amountInput, setAmountInput] = useState("");
  const [occurredOn, setOccurredOn] = useState(today());
  const [note, setNote] = useState("");
  const [amountError, setAmountError] = useState<string | null>(null);
  const [addError, setAddError] = useState<string | null>(null);
  const [isAdding, setIsAdding] = useState(false);

  const [confirmingId, setConfirmingId] = useState<string | null>(null);
  const [deletingId, setDeletingId] = useState<string | null>(null);
  // Keyed by contribution id, not one shared slot -- a 404 on one row must
  // read next to that row, not detached under whichever row happens to be
  // last in the list (see ContributionRow's own `error` prop comment).
  const [deleteErrors, setDeleteErrors] = useState<Record<string, string>>({});

  async function handleAdd(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    setAmountError(null);
    setAddError(null);

    const amountMinor = toMinorUnits(amountInput, goal.currency);
    if (amountMinor === null) {
      setAmountError(describeAmountError(amountInput, goal.currency, "50.00"));
      return;
    }
    // amount_minor <> 0, not > 0 (spec decision) -- a mistyped contribution
    // is corrected by a negative row, so a negative amount is valid and must
    // not be refused here alongside zero.
    if (amountMinor === 0) {
      setAmountError(GOAL_COPY.zeroAmountError);
      return;
    }

    setIsAdding(true);
    try {
      await addContribution(goal.id, { amountMinor, occurredOn, note });
      // Cleared, not left holding the just-submitted values -- the panel
      // stays open (no onClose here) so the refetched list and the card
      // behind it are what the household sees move, ready for another
      // entry immediately after.
      setAmountInput("");
      setNote("");
      setOccurredOn(today());
    } catch (err) {
      setAddError(apiErrorMessage(err, "Something went wrong. Please try again."));
    } finally {
      setIsAdding(false);
    }
  }

  async function handleDelete(contributionId: string) {
    setDeleteErrors((prev) =>
      Object.fromEntries(Object.entries(prev).filter(([id]) => id !== contributionId)),
    );
    setDeletingId(contributionId);
    try {
      await deleteContribution(goal.id, contributionId);
    } catch (err) {
      // A 404 -- the row already gone in another tab -- surfaces here rather
      // than silently appearing to succeed: nothing in this catch removes
      // the row from view, and the list only ever changes via the
      // mutation's own onSuccess-triggered refetch.
      const message = apiErrorMessage(err, "Something went wrong. Please try again.");
      setDeleteErrors((prev) => ({ ...prev, [contributionId]: message }));
    } finally {
      setDeletingId(null);
      // Collapses back to the plain trigger regardless of outcome --
      // TransactionModal.tsx's own handleDelete does the same in its
      // `finally`, and the error (when there is one) is what stays visible
      // on this same row, not the confirm pair.
      setConfirmingId(null);
    }
  }

  const rows = contributions.data?.contributions ?? [];

  return (
    <Modal open onClose={onClose} title={GOAL_COPY.contributionsTitle(goal.name)}>
      <form className="flex flex-col gap-4" onSubmit={handleAdd}>
        <FieldPair>
          <div className="flex flex-col gap-1.5">
            <label htmlFor="contribution-amount" className="text-xs font-semibold text-label">
              Amount
            </label>
            <input
              id="contribution-amount"
              type="text"
              inputMode="decimal"
              required
              value={amountInput}
              onChange={(event) => setAmountInput(event.target.value)}
              // min-h-11/sm:min-h-0: TransactionFilters.tsx's own
              // SELECT_CLASS comment has the measured reason py-2.5 alone
              // falls short of the 44px floor on a phone.
              className="min-h-11 rounded-lg border border-hairline bg-card px-3.5 py-2.5 text-[13.5px] sm:min-h-0"
            />
          </div>
          <div className="flex flex-col gap-1.5">
            <label htmlFor="contribution-date" className="text-xs font-semibold text-label">
              Date
            </label>
            <input
              id="contribution-date"
              type="date"
              required
              value={occurredOn}
              onChange={(event) => setOccurredOn(event.target.value)}
              className="min-h-11 rounded-lg border border-hairline bg-card px-3.5 py-2.5 text-[13.5px] sm:min-h-0"
            />
          </div>
        </FieldPair>

        {amountError && (
          <p role="alert" className="text-xs leading-snug text-danger">
            {amountError}
          </p>
        )}

        <div className="flex flex-col gap-1.5">
          <label htmlFor="contribution-note" className="text-xs font-semibold text-label">
            Note
          </label>
          <input
            id="contribution-note"
            type="text"
            value={note}
            onChange={(event) => setNote(event.target.value)}
            className="min-h-11 rounded-lg border border-hairline bg-card px-3.5 py-2.5 text-[13.5px] sm:min-h-0"
          />
        </div>

        {addError !== null && (
          <p role="alert" className="text-xs leading-snug text-danger">
            {addError}
          </p>
        )}

        <button
          type="submit"
          disabled={isAdding}
          className="min-h-11 rounded-lg bg-accent py-2.5 text-center text-[13px] font-semibold text-white disabled:cursor-not-allowed disabled:opacity-60 sm:min-h-0"
        >
          Add
        </button>
      </form>

      <div className="mt-5 flex flex-col">
        <h3 className="text-xs font-semibold text-label">Recent contributions</h3>
        {contributions.loading && <p className="mt-2 text-xs text-muted">Loading…</p>}
        {contributions.error && (
          <p role="alert" className="mt-2 text-xs text-danger">
            Couldn't load contributions.
          </p>
        )}
        {!contributions.loading && !contributions.error && rows.length === 0 && (
          <p data-testid="goal-contributions-empty" className="mt-2 text-xs text-muted">
            {GOAL_COPY.noContributionsYet}
          </p>
        )}
        {rows.map((contribution) => (
          <ContributionRow
            key={contribution.id}
            contribution={contribution}
            currency={goal.currency}
            symbol={symbol}
            confirming={confirmingId === contribution.id}
            deleting={deletingId === contribution.id}
            error={deleteErrors[contribution.id] ?? null}
            onAskToDelete={() => setConfirmingId(contribution.id)}
            onCancelDelete={() => setConfirmingId(null)}
            onConfirmDelete={() => handleDelete(contribution.id)}
          />
        ))}
      </div>
    </Modal>
  );
}
