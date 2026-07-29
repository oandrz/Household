// The "Log a transaction" modal (design/Household Dashboard.dc.html's
// "ADD TRANSACTION" panel), shared by add and edit -- Task 16 is the only
// caller of PATCH /api/v1/transactions/{id}, and it opens this same component
// populated from a ledger row rather than a second form. `initial` is what
// lets one component serve both: present, it prefills every field and shows
// the delete control; absent, the form starts blank.
//
// Follows AccountModal.tsx's shape (the components/Modal primitive, the
// select-not-pills choice for Owner/Type there and Paid-by/Category/Account
// here, the field-error-then-mutation-error rendering order) but does not
// own its own mutations the way AccountModal does: `onSubmit`/`onDelete` are
// passed in, because Task 16 must translate the same field set into either a
// POST body or a real-patch PATCH body (updateTransactionRequest's pointers
// and its clearReceivedAmount flag), and that translation is the caller's
// concern, not this form's.
import { type FormEvent, useState } from "react";
import { Modal } from "../../components/Modal";
import { apiErrorMessage } from "../auth/copy";
import { minorUnitsToInputValue, toMinorUnits } from "./formatMoney";
import { TRANSACTIONS_COPY } from "./transactionCopy";
import { useCategories } from "./useTransactions";
import type { Account } from "./schemas";
import type { Transaction, TransactionKind } from "./transactionSchemas";

// TransactionFormValues mirrors createTransactionRequest
// (api/internal/adapter/http/transaction_handlers.go) field for field --
// including its absence of a currency field. The server derives currency from
// whichever account is named; a field here the server ignored would be the
// shape guarding-partial-writes exists for.
export type TransactionFormValues = {
  kind: TransactionKind;
  occurredOn: string;
  description: string;
  categoryId: string | null;
  paidByMembershipId: string | null;
  fromAccountId: string | null;
  toAccountId: string | null;
  amountMinor: number;
  receivedAmountMinor: number | null;
};

// today() reads the *local* calendar date via getFullYear/getMonth/getDate,
// never toISOString() (which converts to UTC first) -- the same function and
// the same reason as AccountModal.tsx's own today(). Not extracted to a
// shared module: it is small, single-purpose, and duplicating an exact,
// already-tested four-line function is a smaller risk than coupling two
// features' date handling through one import for a single line of logic.
function today(): string {
  const now = new Date();
  const year = now.getFullYear();
  const month = String(now.getMonth() + 1).padStart(2, "0");
  const day = String(now.getDate()).padStart(2, "0");
  return `${year}-${month}-${day}`;
}

const KINDS: TransactionKind[] = ["expense", "income", "transfer"];
const KIND_LABELS: Record<TransactionKind, string> = {
  expense: "Expense",
  income: "Income",
  transfer: "Transfer",
};

export function TransactionModal({
  open,
  onClose,
  onSubmit,
  onDelete,
  initial,
  accounts,
  members,
}: {
  open: boolean;
  onClose: () => void;
  onSubmit: (values: TransactionFormValues) => Promise<unknown>;
  // Present only when editing -- renders the delete control, behind its own
  // in-page confirmation (never window.confirm, which blocks every browser
  // event and would freeze Task 19's automated walk).
  onDelete?: () => Promise<unknown>;
  initial?: Transaction;
  accounts: Account[];
  members: { id: string; name: string }[];
}) {
  const isEditing = initial !== undefined;
  const categories = useCategories();

  const [kind, setKind] = useState<TransactionKind>(initial?.kind ?? "expense");
  const [date, setDate] = useState(initial?.occurredOn ?? today());
  const [description, setDescription] = useState(initial?.description ?? "");
  const [amountInput, setAmountInput] = useState(() =>
    initial ? minorUnitsToInputValue(initial.amount.amountMinor, initial.amount.currency) : "",
  );
  const [categoryId, setCategoryId] = useState(initial?.categoryId ?? "");
  const [paidByMembershipId, setPaidByMembershipId] = useState(initial?.paidByMembershipId ?? "");

  // The expense/income forms show one "Account" field; internally it is
  // whichever side of the ledger row that kind actually writes to
  // (FromAccountID for an expense, ToAccountID for an income --
  // TransactionService.validate's own switch on t.Kind). Defaulting to the
  // first account rather than "" mirrors AccountModal's Type select: every
  // account-picking field here is required, so there is no honest blank
  // state to default to once at least one account exists.
  const [accountId, setAccountId] = useState(
    () => initial?.fromAccountId ?? initial?.toAccountId ?? accounts[0]?.id ?? "",
  );
  const [fromAccountId, setFromAccountId] = useState(
    () => initial?.fromAccountId ?? accounts[0]?.id ?? "",
  );
  const [toAccountId, setToAccountId] = useState(
    () => initial?.toAccountId ?? accounts[1]?.id ?? accounts[0]?.id ?? "",
  );

  const [receivedAmountInput, setReceivedAmountInput] = useState(() =>
    initial?.receivedAmount
      ? minorUnitsToInputValue(initial.receivedAmount.amountMinor, initial.receivedAmount.currency)
      : "",
  );
  // Whether the person has typed into Amount received themselves. Once true,
  // the sync-from-Amount-sent effect below leaves it alone -- the same
  // "derive until touched" pattern AccountModal's currency default uses.
  // Starts true when editing: an already-stored transfer's received figure
  // (a real bank fee outcome) must never be silently recomputed just because
  // the person opened the form and changed the description.
  const [receivedAmountTouched, setReceivedAmountTouched] = useState(isEditing);

  const [amountError, setAmountError] = useState<string | null>(null);
  const [receivedAmountError, setReceivedAmountError] = useState<string | null>(null);
  const [submitError, setSubmitError] = useState<unknown>(null);
  const [isSubmitting, setIsSubmitting] = useState(false);

  const [confirmingDelete, setConfirmingDelete] = useState(false);
  const [isDeleting, setIsDeleting] = useState(false);

  const fromAccount = accounts.find((a) => a.id === fromAccountId);
  const toAccount = accounts.find((a) => a.id === toAccountId);
  const fromCurrency = fromAccount?.balance?.currency;
  const toCurrency = toAccount?.balance?.currency;
  // Decision 3: required exactly when a transfer crosses currencies, because
  // there is no honest figure to prefill with -- what arrives depends on the
  // bank's own rate, which this product does not hold. Optional within one
  // currency (a bank fee).
  const receivedAmountRequired =
    kind === "transfer" && fromCurrency !== undefined && toCurrency !== undefined && fromCurrency !== toCurrency;

  // Keeps Amount received mirroring Amount sent until the person overrides it,
  // and clears it the moment the two accounts stop sharing a currency --
  // computed during render (not an effect) so it settles before this render
  // commits, the same pattern AccountModal's own currency-default uses.
  if (kind === "transfer" && !receivedAmountTouched) {
    if (!receivedAmountRequired && receivedAmountInput !== amountInput) {
      setReceivedAmountInput(amountInput);
    } else if (receivedAmountRequired && receivedAmountInput !== "") {
      setReceivedAmountInput("");
    }
  }

  const primaryAccount = kind === "transfer" ? fromAccount : accounts.find((a) => a.id === accountId);
  const primaryCurrency = primaryAccount?.balance?.currency ?? "";

  const relevantCategories = (categories.data ?? []).filter(
    (c) => c.kind === (kind === "income" ? "income" : "expense"),
  );

  function handleSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    setAmountError(null);
    setReceivedAmountError(null);

    const amountMinor = toMinorUnits(amountInput, primaryCurrency);
    if (amountMinor === null || amountMinor <= 0) {
      setAmountError("Enter an amount, like 52.30.");
      return;
    }

    let receivedAmountMinor: number | null = null;
    if (kind === "transfer") {
      if (receivedAmountInput.trim() === "") {
        if (receivedAmountRequired) {
          setReceivedAmountError("Enter what actually arrived.");
          return;
        }
      } else {
        const parsed = toMinorUnits(receivedAmountInput, toCurrency ?? "");
        if (parsed === null || parsed <= 0) {
          setReceivedAmountError("Enter an amount, like 52.30.");
          return;
        }
        receivedAmountMinor = parsed;
      }
    }

    const common = { kind, occurredOn: date, description: description.trim(), amountMinor };
    let values: TransactionFormValues;
    if (kind === "transfer") {
      values = {
        ...common,
        categoryId: null,
        paidByMembershipId: null,
        fromAccountId: fromAccountId || null,
        toAccountId: toAccountId || null,
        receivedAmountMinor,
      };
    } else if (kind === "income") {
      values = {
        ...common,
        categoryId: categoryId || null,
        paidByMembershipId: null,
        fromAccountId: null,
        toAccountId: accountId || null,
        receivedAmountMinor: null,
      };
    } else {
      values = {
        ...common,
        categoryId: categoryId || null,
        paidByMembershipId: paidByMembershipId || null,
        fromAccountId: accountId || null,
        toAccountId: null,
        receivedAmountMinor: null,
      };
    }

    setSubmitError(null);
    setIsSubmitting(true);
    onSubmit(values)
      .then(() => onClose())
      .catch((err: unknown) => setSubmitError(err))
      .finally(() => setIsSubmitting(false));
  }

  function handleDelete() {
    if (!onDelete) return;
    setSubmitError(null);
    setIsDeleting(true);
    onDelete()
      .then(() => onClose())
      .catch((err: unknown) => setSubmitError(err))
      .finally(() => {
        setIsDeleting(false);
        setConfirmingDelete(false);
      });
  }

  return (
    <Modal open={open} onClose={onClose} title={TRANSACTIONS_COPY.logTransaction}>
      <form className="flex flex-col gap-4" onSubmit={handleSubmit}>
        <div className="flex gap-1.5">
          {KINDS.map((k) => (
            <button
              key={k}
              type="button"
              aria-pressed={kind === k}
              onClick={() => setKind(k)}
              className={
                kind === k
                  ? "flex-1 rounded-lg bg-accent py-2 text-center text-[13px] font-semibold text-white"
                  : "flex-1 rounded-lg border border-hairline py-2 text-center text-[13px] font-semibold text-label"
              }
            >
              {KIND_LABELS[k]}
            </button>
          ))}
        </div>

        <div className="grid grid-cols-2 gap-4">
          <div className="flex flex-col gap-1.5">
            <label htmlFor="transaction-amount" className="text-xs font-semibold text-label">
              Amount
            </label>
            <input
              id="transaction-amount"
              type="text"
              inputMode="decimal"
              required
              value={amountInput}
              onChange={(event) => setAmountInput(event.target.value)}
              className="rounded-lg border border-hairline bg-card px-3.5 py-2.5 text-[13.5px]"
            />
          </div>

          <div className="flex flex-col gap-1.5">
            <label htmlFor="transaction-date" className="text-xs font-semibold text-label">
              Date
            </label>
            <input
              id="transaction-date"
              type="date"
              required
              value={date}
              onChange={(event) => setDate(event.target.value)}
              className="rounded-lg border border-hairline bg-card px-3.5 py-2.5 text-[13.5px]"
            />
          </div>
        </div>

        {amountError && (
          <p role="alert" className="text-xs leading-snug text-danger">
            {amountError}
          </p>
        )}

        <div className="flex flex-col gap-1.5">
          <label htmlFor="transaction-description" className="text-xs font-semibold text-label">
            Description
          </label>
          <input
            id="transaction-description"
            type="text"
            required
            value={description}
            onChange={(event) => setDescription(event.target.value)}
            className="rounded-lg border border-hairline bg-card px-3.5 py-2.5 text-[13.5px]"
          />
        </div>

        {kind === "transfer" ? (
          <div className="grid grid-cols-2 gap-4">
            <div className="flex flex-col gap-1.5">
              <label htmlFor="transaction-from-account" className="text-xs font-semibold text-label">
                From account
              </label>
              <select
                id="transaction-from-account"
                required
                value={fromAccountId}
                onChange={(event) => setFromAccountId(event.target.value)}
                className="rounded-lg border border-hairline bg-card px-3.5 py-2.5 text-[13.5px]"
              >
                {accounts.map((a) => (
                  <option key={a.id} value={a.id}>
                    {a.nickname}
                  </option>
                ))}
              </select>
            </div>
            <div className="flex flex-col gap-1.5">
              <label htmlFor="transaction-to-account" className="text-xs font-semibold text-label">
                To account
              </label>
              <select
                id="transaction-to-account"
                required
                value={toAccountId}
                onChange={(event) => setToAccountId(event.target.value)}
                className="rounded-lg border border-hairline bg-card px-3.5 py-2.5 text-[13.5px]"
              >
                {accounts.map((a) => (
                  <option key={a.id} value={a.id}>
                    {a.nickname}
                  </option>
                ))}
              </select>
            </div>
          </div>
        ) : (
          <div className="grid grid-cols-2 gap-4">
            <div className="flex flex-col gap-1.5">
              <label htmlFor="transaction-category" className="text-xs font-semibold text-label">
                Category
              </label>
              <select
                id="transaction-category"
                value={categoryId}
                onChange={(event) => setCategoryId(event.target.value)}
                className="rounded-lg border border-hairline bg-card px-3.5 py-2.5 text-[13.5px]"
              >
                <option value="">{TRANSACTIONS_COPY.noCategory}</option>
                {relevantCategories.map((c) => (
                  <option key={c.id} value={c.id}>
                    {c.name}
                  </option>
                ))}
              </select>
            </div>
            <div className="flex flex-col gap-1.5">
              <label htmlFor="transaction-account" className="text-xs font-semibold text-label">
                Account
              </label>
              <select
                id="transaction-account"
                required
                value={accountId}
                onChange={(event) => setAccountId(event.target.value)}
                className="rounded-lg border border-hairline bg-card px-3.5 py-2.5 text-[13.5px]"
              >
                {accounts.map((a) => (
                  <option key={a.id} value={a.id}>
                    {a.nickname}
                  </option>
                ))}
              </select>
            </div>
          </div>
        )}

        {kind === "transfer" && (
          <div className="flex flex-col gap-1.5">
            <label htmlFor="transaction-received-amount" className="text-xs font-semibold text-label">
              {TRANSACTIONS_COPY.amountReceived}
            </label>
            <input
              id="transaction-received-amount"
              type="text"
              inputMode="decimal"
              required={receivedAmountRequired}
              value={receivedAmountInput}
              onChange={(event) => {
                setReceivedAmountTouched(true);
                setReceivedAmountInput(event.target.value);
              }}
              className="rounded-lg border border-hairline bg-card px-3.5 py-2.5 text-[13.5px]"
            />
            <p className="text-[11.5px] text-muted">
              {TRANSACTIONS_COPY.amountReceivedHint(toCurrency ?? "")}
            </p>
            {receivedAmountError && (
              <p role="alert" className="text-xs leading-snug text-danger">
                {receivedAmountError}
              </p>
            )}
          </div>
        )}

        {kind === "expense" && (
          <div className="flex flex-col gap-1.5">
            <label htmlFor="transaction-paid-by" className="text-xs font-semibold text-label">
              Paid by
            </label>
            <select
              id="transaction-paid-by"
              value={paidByMembershipId}
              onChange={(event) => setPaidByMembershipId(event.target.value)}
              className="rounded-lg border border-hairline bg-card px-3.5 py-2.5 text-[13.5px]"
            >
              <option value="">Unassigned</option>
              {members.map((m) => (
                <option key={m.id} value={m.id}>
                  {m.name}
                </option>
              ))}
            </select>
          </div>
        )}

        {submitError !== null && (
          <p role="alert" className="text-xs leading-snug text-danger">
            {apiErrorMessage(submitError, "Something went wrong. Please try again.")}
          </p>
        )}

        {onDelete && (
          <div className="rounded-[10px] border border-hairline p-3">
            {confirmingDelete ? (
              <div className="flex flex-col gap-2.5">
                <p className="text-[12.5px] text-ink">{TRANSACTIONS_COPY.deleteConfirmBody}</p>
                <div className="flex gap-2.5">
                  <button
                    type="button"
                    onClick={() => setConfirmingDelete(false)}
                    className="flex-1 rounded-lg border border-hairline py-2 text-center text-[13px] font-semibold text-label"
                  >
                    {TRANSACTIONS_COPY.deleteCancelAction}
                  </button>
                  <button
                    type="button"
                    disabled={isDeleting}
                    onClick={handleDelete}
                    className="flex-1 rounded-lg bg-danger py-2 text-center text-[13px] font-semibold text-white disabled:cursor-not-allowed disabled:opacity-60"
                  >
                    {TRANSACTIONS_COPY.deleteConfirmAction}
                  </button>
                </div>
              </div>
            ) : (
              <button
                type="button"
                onClick={() => setConfirmingDelete(true)}
                className="text-[13px] font-semibold text-danger"
              >
                {TRANSACTIONS_COPY.deleteTransaction}
              </button>
            )}
          </div>
        )}

        <div className="mt-1 flex gap-2.5">
          <button
            type="button"
            onClick={onClose}
            className="flex-1 rounded-lg border border-hairline py-2.5 text-center text-[13px] font-semibold text-label"
          >
            Cancel
          </button>
          <button
            type="submit"
            disabled={isSubmitting}
            className="flex-[2] rounded-lg bg-accent py-2.5 text-center text-[13px] font-semibold text-white disabled:cursor-not-allowed disabled:opacity-60"
          >
            {TRANSACTIONS_COPY.saveTransaction}
          </button>
        </div>
      </form>
    </Modal>
  );
}
