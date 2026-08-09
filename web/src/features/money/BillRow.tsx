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

function PaymentRow({ payment, symbolFor }: { payment: BillPayment; symbolFor: SymbolFor }) {
  return (
    <div
      data-testid="bill-row"
      className="flex items-center justify-between gap-3 border-b border-hairline py-3 opacity-65 last:border-b-0"
    >
      <div className="flex items-center gap-3">
        <DateBadge nextDue={payment.dueOn} settled={false} overdue={false} />
        <div>
          <div className="text-[13.5px] font-semibold text-ink">{payment.billName}</div>
          <div className="text-[11.5px] text-muted">{BILL_COPY.paymentSubtitle(payment.autopay)}</div>
        </div>
      </div>
      <div className="flex items-center gap-3">
        <span className="text-[11px] font-semibold text-accent">{BILL_COPY.paidLabel}</span>
        <span className="w-20 text-right text-[13.5px] font-semibold text-ink">
          {formatMoney(payment.amountMinor, payment.currency, symbolFor(payment.currency))}
        </span>
      </div>
    </div>
  );
}

export function BillRow(props: BillRowProps) {
  if (props.kind === "payment") {
    return <PaymentRow payment={props.payment} symbolFor={props.symbolFor} />;
  }

  const { bill, symbolFor, onEdit, onArchive, onRestore, pending } = props;
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
      className={`flex items-center justify-between gap-3 border-b border-hairline py-3 last:border-b-0 ${
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
      <div className="flex items-center gap-3">
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
