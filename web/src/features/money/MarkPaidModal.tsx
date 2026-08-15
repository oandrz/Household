// The Mark-paid modal (design spec's own words: "amount prefilled from the
// bill and editable, date prefilled with today, pay-from account shown
// read-only, and the sentence naming what it will do -- write an expense --
// so the double-entry cost of decision 1 is stated where somebody could
// incur it"). Built on the shared Modal primitive, BillModal.tsx's/
// GoalContributionsPanel.tsx's own choice for the same reason: a small
// write-and-close surface, not a page of its own.
//
// Takes the bill it is paying as a prop rather than an id -- BillsPage.tsx
// already holds the full Bill from the list it rendered the row from, and
// re-fetching it here would be a second GET for data the caller already
// has (BillModal.tsx's own edit-mode shape makes the identical choice for
// the same reason).
//
// No restore-style special case for a 409/422 the way BillModal.tsx's own
// BILL_NAME_TAKEN handling has: every refusal MarkPaid can return --
// BILL_ARCHIVED, BILL_SETTLED, ACCOUNT_ARCHIVED, the already-paid 409 --
// already carries its own complete sentence from the server (writeMarkPaidError's
// own comment, bill_handlers.go), so apiErrorMessage's verbatim pass-through
// is the whole client-side job. Building a second, client-composed message
// for any one of them would risk it drifting from the server's own wording.
import { type FormEvent, useState } from "react";
import { FieldPair } from "../../components/FieldPair";
import { Modal } from "../../components/Modal";
import { apiErrorMessage } from "../auth/copy";
import { BILL_COPY } from "./billCopy";
import { describeAmountError, minorUnitsToInputValue, toMinorUnits } from "./formatMoney";
import { useMarkPaid, type PayBillBody } from "./useBills";
import type { Bill } from "./billSchemas";

// today() reads the *local* calendar date via getFullYear/getMonth/getDate,
// never toISOString() (which converts to UTC first) -- the same function and
// the same reason BillModal.tsx's/AccountModal.tsx's/TransactionModal.tsx's
// own today() give, duplicated rather than imported for the identical reason
// each of those states: a small, already-tested four-line function is a
// smaller risk to share than the coupling importing it across components
// would add.
function today(): string {
  const now = new Date();
  const year = now.getFullYear();
  const month = String(now.getMonth() + 1).padStart(2, "0");
  const day = String(now.getDate()).padStart(2, "0");
  return `${year}-${month}-${day}`;
}

export function MarkPaidModal({
  bill,
  onClose,
  onPaid,
}: {
  bill: Bill;
  onClose: () => void;
  onPaid: () => void;
}) {
  const markPaid = useMarkPaid();

  // Prefilled from the bill's own stored figure, editable -- utilities vary
  // month to month, which is the whole reason this is a modal rather than a
  // one-click "Mark paid" button (the task brief's own words).
  const [amountInput, setAmountInput] = useState(() => minorUnitsToInputValue(bill.amountMinor, bill.currency));
  const [paidOn, setPaidOn] = useState(today());
  const [amountError, setAmountError] = useState<string | null>(null);
  const [saveError, setSaveError] = useState<string | null>(null);
  const [isSaving, setIsSaving] = useState(false);

  async function handleSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    setAmountError(null);
    setSaveError(null);

    const amountMinor = toMinorUnits(amountInput, bill.currency);
    if (amountMinor === null) {
      setAmountError(describeAmountError(amountInput, bill.currency, "89.90"));
      return;
    }
    if (amountMinor <= 0) {
      setAmountError(BILL_COPY.amountMustBePositive);
      return;
    }

    const body: PayBillBody = { amountMinor, paidOn };
    setIsSaving(true);
    try {
      await markPaid.mutateAsync({ id: bill.id, body });
      onPaid();
      onClose();
    } catch (err) {
      // Keeps the modal open on every refusal -- an archived bill, a settled
      // one-off, an archived pay-from account, an occurrence already paid,
      // an unparseable date -- the same "422/409 keeps the modal open"
      // convention BillModal.tsx's own handleSave comment states, so a
      // household can read the server's own sentence and, where it applies
      // (BILL_ARCHIVED), go act on it (Restore) without losing what it typed.
      setSaveError(apiErrorMessage(err, BILL_COPY.genericSaveError));
    } finally {
      setIsSaving(false);
    }
  }

  return (
    <Modal open onClose={onClose} title={BILL_COPY.markPaidModalTitle}>
      <form className="flex flex-col gap-4" onSubmit={handleSubmit}>
        <FieldPair>
          <div className="flex flex-col gap-1.5">
            <label htmlFor="mark-paid-amount" className="text-xs font-semibold text-label">
              {BILL_COPY.amountLabel}
            </label>
            <input
              id="mark-paid-amount"
              type="text"
              inputMode="decimal"
              required
              value={amountInput}
              onChange={(event) => setAmountInput(event.target.value)}
              // min-h-11/sm:min-h-0 on every field in this modal:
              // TransactionFilters.tsx's own SELECT_CLASS comment has the
              // measured reason py-2.5 alone falls short of the 44px floor
              // on a phone.
              className="min-h-11 rounded-lg border border-hairline bg-card px-3.5 py-2.5 text-[13.5px] sm:min-h-0"
            />
          </div>

          <div className="flex flex-col gap-1.5">
            <label htmlFor="mark-paid-date" className="text-xs font-semibold text-label">
              {BILL_COPY.paidOnLabel}
            </label>
            <input
              id="mark-paid-date"
              type="date"
              required
              value={paidOn}
              onChange={(event) => setPaidOn(event.target.value)}
              className="min-h-11 rounded-lg border border-hairline bg-card px-3.5 py-2.5 text-[13.5px] sm:min-h-0"
            />
          </div>
        </FieldPair>

        {amountError && (
          <p role="alert" className="text-xs leading-snug text-danger">
            {amountError}
          </p>
        )}

        {/* Read-only, not a select -- decision 7's own consequence: a bill's
            pay-from account is chosen on the bill itself (BillModal's own
            "Pay from" field), never re-chosen at the moment of paying it. */}
        <div className="flex flex-col gap-1.5">
          <span className="text-xs font-semibold text-label">{BILL_COPY.payFromLabel}</span>
          <div className="rounded-lg border border-hairline bg-canvas px-3.5 py-2.5 text-[13.5px] text-muted">
            {bill.accountName}
          </div>
        </div>

        <p className="text-[12.5px] leading-relaxed text-muted">{BILL_COPY.markPaidWritesExpense(bill.name)}</p>

        {saveError !== null && (
          <p role="alert" className="text-xs leading-snug text-danger">
            {saveError}
          </p>
        )}

        <div className="mt-1 flex gap-2.5">
          <button
            type="button"
            onClick={onClose}
            className="min-h-11 flex-1 rounded-lg border border-hairline py-2.5 text-center text-[13px] font-semibold text-label sm:min-h-0"
          >
            {BILL_COPY.cancelAction}
          </button>
          <button
            type="submit"
            disabled={isSaving}
            className="min-h-11 flex-[2] rounded-lg bg-accent py-2.5 text-center text-[13px] font-semibold text-white disabled:cursor-not-allowed disabled:opacity-60 sm:min-h-0"
          >
            {BILL_COPY.markPaidSubmit}
          </button>
        </div>
      </form>
    </Modal>
  );
}
