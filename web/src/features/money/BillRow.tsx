// One row on the Bills screen -- Due soon, Later and Paid this month all
// share this component, the task brief's own "Produces" line. A `Bill` and a
// `BillPayment` are genuinely different shapes (a payment carries no
// categoryName or accountName -- billPaymentSchema simply has neither field,
// which is why the design's own "Education · manual" paid row draws no
// account: not an inconsistency in the mockup, a fact about the DTO), so
// this takes a discriminated union on `kind` rather than pretending the two
// are the same row with optional fields.
//
// Archive/Restore is the either-or AccountRow.tsx/GoalCard.tsx already use:
// gated on `bill.archivedAt`, never both at once. onEdit follows GoalCard's
// own optional-prop shape -- undefined here (BillsPage.tsx does not pass it
// yet) means the row is simply not clickable, which is how this task leaves
// a hook for Task 13's BillModal without shipping a control that opens
// nothing (this task's own scope-boundary rule).
//
// Mark paid (Task 14) is the same either-or as Archive/Restore, on the same
// `bill.archivedAt` gate -- never shown on an archived row. writeMarkPaidError's
// own BILL_ARCHIVED message reads "Restore it before marking a payment," and
// this row's own archived branch already offers Restore as its one action;
// a second, dead-ending way to attempt payment on the same row would just
// repeat what that message already has to correct. Settled is deliberately
// NOT excluded the same way (billCopy.ts's own comment on markPaidTrigger) --
// only bill.archivedAt gates it.
import { BILL_COPY, dayMonthLabel, dayNumber, monthAbbrev } from "./billCopy";
import { formatMoney } from "./formatMoney";
import type { Bill, BillPayment } from "./billSchemas";

type SymbolFor = (currency: string) => string | undefined;

type BillRowProps =
  | {
      kind: "bill";
      bill: Bill;
      symbolFor: SymbolFor;
      // Only a live bill opens the edit modal on click -- an archived row's
      // own affordance is Restore below, the same reasoning GoalCard.tsx's
      // own onEdit comment gives for goals.
      onEdit?: (bill: Bill) => void;
      onArchive?: (id: string) => void;
      onRestore?: (id: string) => void;
      onMarkPaid?: (bill: Bill) => void;
      // True while an archive OR restore call for *this* row is in flight --
      // scoped per row because useArchiveBill/useRestoreBill are each one
      // shared mutation instance, so a single mutation's own isPending only
      // ever reflects the most recently dispatched call (AccountsPanel.tsx's
      // own pendingIds reasoning).
      pending?: boolean;
    }
  | {
      kind: "payment";
      payment: BillPayment;
      symbolFor: SymbolFor;
      // Undo (Task 14), the GoalContributionsPanel.tsx/ContributionRow.tsx
      // pattern restated for a payment row: onAskUndo opens THIS row's own
      // in-page confirmation (never window.confirm -- the house rule),
      // onConfirmUndo/onCancelUndo answer it. All four are optional so a
      // caller that never wires them (none exists yet outside BillsPage.tsx)
      // gets a row with no Undo control rather than one wired to nothing --
      // the identical reasoning onEdit's own comment gives above.
      onAskUndo?: (payment: BillPayment) => void;
      onCancelUndo?: () => void;
      onConfirmUndo?: () => void;
      confirming?: boolean;
      undoing?: boolean;
      // Scoped to this one row, not a page-wide slot -- ContributionRow.tsx's
      // own `error` prop comment gives the exact reason: a 409 on one payment
      // must read next to that payment, not detached under whichever row
      // happens to be last in "Paid this month."
      error?: string | null;
    };

// The two-line date badge (design: a small uppercase month over a large day
// number), or the word "Settled" in its place for a paid one-off with no
// next occurrence. `nextDue === null` is checked alongside `settled` --
// belt-and-braces against the two ever disagreeing, since the alternative is
// this component parsing `null` as a date and crashing mid-render.
function DateBadge({ nextDue, settled, overdue }: { nextDue: string | null; settled: boolean; overdue: boolean }) {
  if (settled || nextDue === null) {
    return (
      <div className="w-[38px] flex-none text-center text-[10px] font-semibold uppercase italic text-muted">
        {BILL_COPY.settled}
      </div>
    );
  }
  return (
    <div className="w-[38px] flex-none text-center">
      <div className={`text-[10px] font-semibold uppercase ${overdue ? "text-danger" : "text-muted"}`}>
        {monthAbbrev(nextDue)}
      </div>
      <div className="text-[16px] font-semibold text-ink">{dayNumber(nextDue)}</div>
    </div>
  );
}

function PaymentRow({
  payment,
  symbolFor,
  onAskUndo,
  onCancelUndo,
  onConfirmUndo,
  confirming,
  undoing,
  error,
}: {
  payment: BillPayment;
  symbolFor: SymbolFor;
  onAskUndo?: (payment: BillPayment) => void;
  onCancelUndo?: () => void;
  onConfirmUndo?: () => void;
  confirming?: boolean;
  undoing?: boolean;
  error?: string | null;
}) {
  return (
    <div
      data-testid="bill-row"
      className="flex flex-col gap-2 border-b border-hairline py-3 last:border-b-0"
    >
      {/* items-start below sm: the right cluster below stacks up to 3 items
          (label, amount, Undo) while the left block stays ~24px tall, so
          items-center would float the date badge and name down to the
          cluster's midpoint. Converges to sm:items-center once the cluster
          is back to one line and the two blocks are close enough in height
          for centring to look right again. */}
      <div className="flex items-start justify-between gap-3 opacity-65 sm:items-center">
        <div className="flex items-center gap-3">
          <DateBadge nextDue={payment.dueOn} settled={false} overdue={false} />
          <div>
            <div className="text-[13.5px] font-semibold text-ink">{payment.billName}</div>
            <div className="text-[11.5px] text-muted">{BILL_COPY.paymentSubtitle(payment.autopay)}</div>
          </div>
        </div>
        {/* Column below sm, row from sm up -- the amount and Undo were
            competing with the name block for a 343px row; stacked, each item
            gets the row's full width instead of squeezing into a slice of it. */}
        <div className="flex flex-col items-end gap-1 sm:flex-row sm:items-center sm:gap-3">
          <span className="text-[11px] font-semibold text-accent">{BILL_COPY.paidLabel}</span>
          <span className="w-20 text-right text-[13.5px] font-semibold text-ink">
            {formatMoney(payment.amountMinor, payment.currency, symbolFor(payment.currency))}
          </span>
          {!confirming && onAskUndo && (
            <button
              type="button"
              aria-label={BILL_COPY.undoAriaLabel(payment.billName)}
              onClick={() => onAskUndo(payment)}
              className="text-[11px] font-semibold text-danger"
            >
              {BILL_COPY.undoTrigger}
            </button>
          )}
        </div>
      </div>
      {/* In-page confirmation, never window.confirm -- the house rule
          GoalContributionsPanel.tsx's own header comment states, and its
          ContributionRow is the pattern this mirrors. */}
      {confirming && (
        <div className="flex flex-col gap-2 rounded-[10px] border border-hairline p-2.5">
          <p className="text-[12.5px] text-ink">{BILL_COPY.undoConfirmBody}</p>
          <div className="flex gap-2.5">
            <button
              type="button"
              onClick={onCancelUndo}
              // min-h-11/sm:min-h-0: py-2.5 alone measured short of the 44px
              // floor at this text size -- TransactionFilters.tsx's own
              // SELECT_CLASS comment has the measured numbers.
              className="min-h-11 flex-1 rounded-lg border border-hairline py-2.5 text-center text-[12.5px] font-semibold text-label sm:min-h-0 sm:py-1.5"
            >
              {BILL_COPY.cancelAction}
            </button>
            <button
              type="button"
              disabled={undoing}
              onClick={onConfirmUndo}
              className="min-h-11 flex-1 rounded-lg bg-danger py-2.5 text-center text-[12.5px] font-semibold text-white disabled:cursor-not-allowed disabled:opacity-60 sm:min-h-0 sm:py-1.5"
            >
              {BILL_COPY.undoConfirmAction}
            </button>
          </div>
        </div>
      )}
      {error != null && (
        <p role="alert" className="text-xs leading-snug text-danger">
          {error}
        </p>
      )}
    </div>
  );
}

export function BillRow(props: BillRowProps) {
  if (props.kind === "payment") {
    return (
      <PaymentRow
        payment={props.payment}
        symbolFor={props.symbolFor}
        onAskUndo={props.onAskUndo}
        onCancelUndo={props.onCancelUndo}
        onConfirmUndo={props.onConfirmUndo}
        confirming={props.confirming}
        undoing={props.undoing}
        error={props.error}
      />
    );
  }

  const { bill, symbolFor, onEdit, onArchive, onRestore, onMarkPaid, pending } = props;
  const archived = bill.archivedAt !== null;
  const clickable = !archived && Boolean(onEdit);
  // The overdue sentence REPLACES the ordinary category/autopay/account
  // subtitle rather than sitting beside it -- a household reading an overdue
  // row needs to know what to do about it, not what account it usually
  // leaves from. Guarded on `bill.nextDue !== null` (never true for a
  // settled bill in practice -- Settled and Overdue are mutually exclusive
  // states, BillView's own comment) so this never has to force a possibly-
  // null date with a non-null assertion.
  const subtitle =
    bill.overdue && bill.nextDue !== null
      ? bill.autopay
        ? BILL_COPY.overdueAutopay(dayMonthLabel(bill.nextDue))
        : BILL_COPY.overdueManual(dayMonthLabel(bill.nextDue))
      : BILL_COPY.rowSubtitle(bill.categoryName, bill.autopay, bill.accountName);

  return (
    <div
      data-testid="bill-row"
      role={clickable ? "button" : undefined}
      tabIndex={clickable ? 0 : undefined}
      onClick={clickable ? () => onEdit?.(bill) : undefined}
      onKeyDown={
        clickable
          ? (event) => {
              if (event.key === "Enter" || event.key === " ") {
                event.preventDefault();
                onEdit?.(bill);
              }
            }
          : undefined
      }
      className={`flex items-start justify-between gap-3 border-b border-hairline py-3 last:border-b-0 sm:items-center ${
        clickable ? "cursor-pointer" : ""
      }`}
    >
      <div className="flex items-center gap-3">
        <DateBadge nextDue={bill.nextDue} settled={bill.settled} overdue={bill.overdue} />
        <div>
          <div className="text-[13.5px] font-semibold text-ink">
            {bill.name}
            {archived && (
              <span className="ml-1.5 text-[11px] font-normal text-muted">{BILL_COPY.archivedMarker}</span>
            )}
          </div>
          <div className="text-[11.5px] text-muted">{subtitle}</div>
        </div>
      </div>
      {/* Column below sm, row from sm up -- the Overdue/Autopay pill, amount
          and archive/mark-paid actions were competing with the name block for
          a 343px row; stacked, each item gets the row's full width instead of
          squeezing into a slice of it. items-start on the outer row (above)
          matches: an autopay bill's cluster stacks up to 4 items (pill,
          amount, Mark paid, Archive) below sm, tall enough that centring
          against the ~24px name block would float it down; sm:items-center
          returns once the cluster is one line again. */}
      <div className="flex flex-col items-end gap-1 sm:flex-row sm:items-center sm:gap-3">
        {/* Overdue takes priority over the Autopay pill -- the subtitle
            sentence above already carries the autopay-vs-manual distinction
            for an overdue row, so the pill's own job here is just "this
            needs attention," never both facts competing for the same slot. */}
        {bill.overdue ? (
          <span className="rounded-full bg-danger-soft px-2.5 py-1 text-[11px] font-semibold text-danger">
            {BILL_COPY.overduePill}
          </span>
        ) : (
          bill.autopay && (
            <span className="rounded-full bg-callout px-2.5 py-1 text-[11px] font-semibold text-accent">
              {BILL_COPY.autopayPill}
            </span>
          )
        )}
        <span className="w-20 text-right text-[13.5px] font-semibold text-ink">
          {formatMoney(bill.amountMinor, bill.currency, symbolFor(bill.currency))}
        </span>
        {/* Never on an archived row -- see this file's own header comment on
            why Restore is that row's one action, not a second way to pay. */}
        {!archived && onMarkPaid && (
          <button
            type="button"
            aria-label={BILL_COPY.markPaidAriaLabel(bill.name)}
            onClick={(event) => {
              // Row-level onClick opens BillModal in edit mode; onKeyDown's
              // own stopPropagation below is just as load-bearing as this one
              // -- without it, Enter/Space on THIS button would still bubble
              // to the row's own onKeyDown and open the edit modal alongside
              // this one (the identical pair Archive/Restore already need,
              // restated here since this row can be clickable at the same
              // time this button is present).
              event.stopPropagation();
              onMarkPaid(bill);
            }}
            onKeyDown={(event) => event.stopPropagation()}
            className="text-[11px] font-semibold text-accent"
          >
            {BILL_COPY.markPaidTrigger}
          </button>
        )}
        {archived && onRestore && (
          <button
            type="button"
            aria-label={`Restore ${bill.name}`}
            disabled={pending}
            onClick={(event) => {
              event.stopPropagation();
              onRestore(bill.id);
            }}
            onKeyDown={(event) => event.stopPropagation()}
            className="text-[11px] font-semibold text-accent disabled:cursor-not-allowed disabled:opacity-60"
          >
            {BILL_COPY.restore}
          </button>
        )}
        {!archived && onArchive && (
          <button
            type="button"
            aria-label={`Archive ${bill.name}`}
            disabled={pending}
            onClick={(event) => {
              event.stopPropagation();
              onArchive(bill.id);
            }}
            onKeyDown={(event) => event.stopPropagation()}
            className="text-[11px] font-semibold text-danger disabled:cursor-not-allowed disabled:opacity-60"
          >
            {BILL_COPY.archive}
          </button>
        )}
      </div>
    </div>
  );
}
