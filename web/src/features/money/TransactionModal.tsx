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
import { Field } from "../../components/Field";
import { FIELD_CONTROL_CLASS } from "../../components/fieldClasses";
import { FieldPair } from "../../components/FieldPair";
import { Modal } from "../../components/Modal";
import { ModalActions } from "../../components/ModalActions";
import { useConfirmAction } from "../../components/useConfirmAction";
import { apiErrorMessage } from "../../api/errorMessage";
import { describeAmountError, minorUnitsToInputValue, toMinorUnits } from "./formatMoney";
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
  // Date, description and account carry `required` and nothing else. Until
  // noValidate went on the form below, the browser refused an empty one before
  // the submit event ever fired, so handleSubmit had never needed a check for
  // them -- and removing the interception without adding these would send an
  // empty description straight to an API that answers 422.
  const [dateError, setDateError] = useState<string | null>(null);
  const [descriptionError, setDescriptionError] = useState<string | null>(null);
  const [accountError, setAccountError] = useState<string | null>(null);
  const [receivedAmountError, setReceivedAmountError] = useState<string | null>(null);
  const [submitError, setSubmitError] = useState<unknown>(null);
  const [isSubmitting, setIsSubmitting] = useState(false);

  // Delete's in-page confirmation. A failed delete is shown in this form's one
  // error line, the same one Save uses, so handleDelete below catches the
  // failure itself and never reads the hook's own per-item error.
  const deletion = useConfirmAction();

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

  // currencyPairKey identifies which currency pair Amount received's current
  // value was computed under. Tracked in its own state, separately from
  // receivedAmountTouched, because a review caught these as two independent
  // events that a single flag cannot both gate: typing a bank fee while
  // dbs -> ocbc (both SGD) marks the field touched, and touched used to also
  // suppress clearing it when the destination then changed to bca (IDR) --
  // "120.00" stayed in the field and would have been sent as
  // receivedAmountMinor: 12000, silently reinterpreted as 120 *rupiah*
  // instead of the 120 Singapore dollars it was typed as. A figure typed
  // under one currency assumption is never valid to keep once the assumption
  // changes, regardless of who put it there -- so this clears on a genuine
  // change to the pair, unconditionally, while the mirror behaviour below
  // stays the only thing receivedAmountTouched gates.
  const currencyPairKey = `${fromCurrency ?? ""}:${toCurrency ?? ""}`;
  const [lastCurrencyPairKey, setLastCurrencyPairKey] = useState(currencyPairKey);
  if (kind === "transfer" && currencyPairKey !== lastCurrencyPairKey) {
    setLastCurrencyPairKey(currencyPairKey);
    setReceivedAmountInput("");
    setReceivedAmountTouched(false);
  }

  // Keeps Amount received mirroring Amount sent until the person overrides it
  // -- computed during render (not an effect) so it settles before this
  // render commits, the same pattern AccountModal's own currency-default
  // uses. Independent of the clear above: this only ever runs while nothing
  // has invalidated the field's honesty first.
  if (kind === "transfer" && !receivedAmountTouched && !receivedAmountRequired && receivedAmountInput !== amountInput) {
    setReceivedAmountInput(amountInput);
  }

  const primaryAccount = kind === "transfer" ? fromAccount : accounts.find((a) => a.id === accountId);
  const primaryCurrency = primaryAccount?.balance?.currency ?? "";

  const relevantCategories = (categories.data ?? []).filter(
    (c) => c.kind === (kind === "income" ? "income" : "expense"),
  );

  // A review caught categoryId surviving a kind switch: relevantCategories
  // only filters what the select *displays*, and setKind on its own never
  // touched the id actually held in state, so picking a category on Expense
  // and then switching to Income left the old expense category id sitting in
  // state -- invisible in the select (it isn't one of the options shown
  // anymore) but still sent verbatim by both submit branches, which the
  // backend refused with a rejection pointing at a field that looked empty.
  // Also resets receivedAmountTouched whenever Transfer is (re)selected: it
  // starts true when editing on purpose (an already-stored transfer's figure
  // must not be silently recomputed just because the person changed the
  // description), but that reasoning does not apply the first time a kind
  // switch turns the form into a transfer at all, and leaving it true there
  // would mean Amount received never prefills as the brief says it always does.
  //
  // The `next === kind` guard is load-bearing, not a redundant early return: a
  // second review caught that every kind button calls this on every click,
  // including a re-click of the kind already active. Without the guard, that
  // re-click silently discarded whatever the person had already typed --
  // a chosen category reverted to blank, and worse, a manually typed
  // same-currency bank fee in Amount received was overwritten by the mirror
  // behaviour the instant `receivedAmountTouched` got reset back to false.
  // Both are real work disappearing from a click that changed nothing.
  function handleKindChange(next: TransactionKind) {
    if (next === kind) return;
    setKind(next);
    setCategoryId("");
    if (next === "transfer") {
      setReceivedAmountTouched(false);
    }
  }

  function handleSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    setAmountError(null);
    setReceivedAmountError(null);
    setDateError(null);
    setDescriptionError(null);
    setAccountError(null);

    const amountMinor = toMinorUnits(amountInput, primaryCurrency);
    if (amountMinor === null) {
      // describeAmountError distinguishes "not a number" from "this currency
      // doesn't use cents" -- the same distinction AccountModal's Balance
      // field makes, shared so the two forms can't answer it differently for
      // the same currency (e.g. logging an expense against an IDR account).
      setAmountError(describeAmountError(amountInput, primaryCurrency, "52.30"));
      return;
    }
    if (amountMinor <= 0) {
      setAmountError("Enter an amount greater than zero.");
      return;
    }

    // Checked in the order the fields are drawn, so the message that appears
    // is the first empty field reading down the form rather than whichever
    // check happens to be written first here.
    if (date === "") {
      setDateError("Pick a date for this transaction.");
      return;
    }
    if (description.trim() === "") {
      // .trim(), deliberately broader than `required`, for the same reason
      // Amount received's own check below is: constraint validation counts a
      // whitespace-only value as present, so a form relying on the attribute
      // alone would have accepted "   " as a description even before this.
      setDescriptionError("Add a short description, like Cold Storage.");
      return;
    }
    // Only reachable with no accounts at all, since every account select
    // defaults to a real id and the Add button is disabled until one exists.
    // Checked anyway: this refuses a value nobody here constructed, rather
    // than posting an empty account id and letting the API name the problem.
    if (kind === "transfer" ? fromAccountId === "" || toAccountId === "" : accountId === "") {
      setAccountError("Choose an account for this transaction.");
      return;
    }

    let receivedAmountMinor: number | null = null;
    if (kind === "transfer") {
      // .trim() here is deliberately broader than the field's own `required`
      // attribute below, which only blocks a literally empty value -- a
      // browser's constraint validation (and jsdom's) treats a whitespace-only
      // string as "present" and lets the submit event through regardless.
      // This check is what actually refuses that case: it is real, reachable
      // code on that path, even though a literally empty required field never
      // reaches this far at all (native validation stops the submit event
      // first, the same as any other required input on this form).
      if (receivedAmountInput.trim() === "") {
        if (receivedAmountRequired) {
          setReceivedAmountError("Enter what actually arrived.");
          return;
        }
      } else {
        const parsed = toMinorUnits(receivedAmountInput, toCurrency ?? "");
        if (parsed === null) {
          setReceivedAmountError(describeAmountError(receivedAmountInput, toCurrency ?? "", "52.30"));
          return;
        }
        if (parsed <= 0) {
          setReceivedAmountError("Enter an amount greater than zero.");
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

  // useConfirmAction collapses the confirm pair once the delete settles,
  // whatever the outcome -- a failed delete must not leave Cancel/Confirm
  // stuck open. The failure itself lands in submitError, below the fields.
  function handleDelete() {
    if (!onDelete) return;
    setSubmitError(null);
    void deletion.confirm(() =>
      onDelete()
        .then(() => onClose())
        .catch((err: unknown) => setSubmitError(err)),
    );
  }

  return (
    <Modal open={open} onClose={onClose} title={TRANSACTIONS_COPY.logTransaction}>
      {/* noValidate: the browser's own constraint validation fires before the
          submit event, so handleSubmit -- and every message this modal already
          writes -- never ran for an empty field. What the person saw instead
          was Chrome's bubble, in Chrome's words, with Chrome's blue ring, on a
          form whose own message for that case was already written and already
          rendered. Every `required` below keeps its attribute: it still
          carries the semantics for assistive technology. The checks in
          handleSubmit are what now refuse. */}
      <form noValidate className="flex flex-col gap-4" onSubmit={handleSubmit}>
        <div className="flex gap-1.5">
          {KINDS.map((k) => (
            <button
              key={k}
              type="button"
              aria-pressed={kind === k}
              onClick={() => handleKindChange(k)}
              // min-h-11/sm:min-h-0: TransactionFilters.tsx's own
              // SELECT_CLASS comment has the measured reason py-2 alone
              // falls short of the 44px floor on a phone.
              className={
                kind === k
                  ? "min-h-11 flex-1 rounded-lg bg-accent py-2 text-center text-[13px] font-semibold text-white sm:min-h-0"
                  : "min-h-11 flex-1 rounded-lg border border-hairline py-2 text-center text-[13px] font-semibold text-label sm:min-h-0"
              }
            >
              {KIND_LABELS[k]}
            </button>
          ))}
        </div>

        <FieldPair>
          <Field label="Amount" htmlFor="transaction-amount">
            <input
              id="transaction-amount"
              type="text"
              inputMode="decimal"
              required
              value={amountInput}
              onChange={(event) => setAmountInput(event.target.value)}
              className={FIELD_CONTROL_CLASS}
            />
          </Field>

          <Field label="Date" htmlFor="transaction-date">
            <input
              id="transaction-date"
              type="date"
              required
              value={date}
              onChange={(event) => setDate(event.target.value)}
              className={FIELD_CONTROL_CLASS}
            />
          </Field>
        </FieldPair>

        {amountError && (
          <p role="alert" className="text-xs leading-snug text-danger">
            {amountError}
          </p>
        )}

        {dateError && (
          <p role="alert" className="text-xs leading-snug text-danger">
            {dateError}
          </p>
        )}

        <Field label="Description" htmlFor="transaction-description" error={descriptionError}>
          <input
            id="transaction-description"
            type="text"
            required
            value={description}
            onChange={(event) => setDescription(event.target.value)}
            className={FIELD_CONTROL_CLASS}
          />
        </Field>

        {kind === "transfer" ? (
          <FieldPair>
            <Field label="From account" htmlFor="transaction-from-account">
              <select
                id="transaction-from-account"
                required
                value={fromAccountId}
                onChange={(event) => setFromAccountId(event.target.value)}
                className={FIELD_CONTROL_CLASS}
              >
                {accounts.map((a) => (
                  <option key={a.id} value={a.id}>
                    {a.nickname}
                  </option>
                ))}
              </select>
            </Field>
            <Field label="To account" htmlFor="transaction-to-account">
              <select
                id="transaction-to-account"
                required
                value={toAccountId}
                onChange={(event) => setToAccountId(event.target.value)}
                className={FIELD_CONTROL_CLASS}
              >
                {accounts.map((a) => (
                  <option key={a.id} value={a.id}>
                    {a.nickname}
                  </option>
                ))}
              </select>
            </Field>
          </FieldPair>
        ) : (
          <FieldPair>
            <Field label="Category" htmlFor="transaction-category">
              <select
                id="transaction-category"
                value={categoryId}
                onChange={(event) => setCategoryId(event.target.value)}
                className={FIELD_CONTROL_CLASS}
              >
                <option value="">{TRANSACTIONS_COPY.noCategory}</option>
                {relevantCategories.map((c) => (
                  <option key={c.id} value={c.id}>
                    {c.name}
                  </option>
                ))}
              </select>
            </Field>
            <Field label="Account" htmlFor="transaction-account">
              <select
                id="transaction-account"
                required
                value={accountId}
                onChange={(event) => setAccountId(event.target.value)}
                className={FIELD_CONTROL_CLASS}
              >
                {accounts.map((a) => (
                  <option key={a.id} value={a.id}>
                    {a.nickname}
                  </option>
                ))}
              </select>
            </Field>
          </FieldPair>
        )}

        {accountError && (
          <p role="alert" className="text-xs leading-snug text-danger">
            {accountError}
          </p>
        )}

        {kind === "transfer" && (
          <Field
            label={TRANSACTIONS_COPY.amountReceived}
            htmlFor="transaction-received-amount"
            error={receivedAmountError}
          >
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
              className={FIELD_CONTROL_CLASS}
            />
            <p className="text-[11.5px] text-muted">
              {TRANSACTIONS_COPY.amountReceivedHint(toCurrency ?? "")}
            </p>
          </Field>
        )}

        {kind === "expense" && (
          <Field label="Paid by" htmlFor="transaction-paid-by">
            <select
              id="transaction-paid-by"
              value={paidByMembershipId}
              onChange={(event) => setPaidByMembershipId(event.target.value)}
              className={FIELD_CONTROL_CLASS}
            >
              <option value="">Unassigned</option>
              {members.map((m) => (
                <option key={m.id} value={m.id}>
                  {m.name}
                </option>
              ))}
            </select>
          </Field>
        )}

        {submitError !== null && (
          <p role="alert" className="text-xs leading-snug text-danger">
            {apiErrorMessage(submitError, "Something went wrong. Please try again.")}
          </p>
        )}

        {onDelete && (
          <div className="rounded-[10px] border border-hairline p-3">
            {deletion.isConfirming() ? (
              <div className="flex flex-col gap-2.5">
                <p className="text-[12.5px] text-ink">{TRANSACTIONS_COPY.deleteConfirmBody}</p>
                <div className="flex gap-2.5">
                  <button
                    type="button"
                    onClick={deletion.cancel}
                    className="min-h-11 flex-1 rounded-lg border border-hairline py-2 text-center text-[13px] font-semibold text-label sm:min-h-0"
                  >
                    {TRANSACTIONS_COPY.deleteCancelAction}
                  </button>
                  <button
                    type="button"
                    disabled={deletion.isPending()}
                    onClick={handleDelete}
                    className="min-h-11 flex-1 rounded-lg bg-danger py-2 text-center text-[13px] font-semibold text-white disabled:cursor-not-allowed disabled:opacity-60 sm:min-h-0"
                  >
                    {TRANSACTIONS_COPY.deleteConfirmAction}
                  </button>
                </div>
              </div>
            ) : (
              <button
                type="button"
                onClick={() => deletion.ask()}
                className="min-h-11 text-[13px] font-semibold text-danger sm:min-h-0"
              >
                {TRANSACTIONS_COPY.deleteTransaction}
              </button>
            )}
          </div>
        )}

        <ModalActions
          secondaryLabel="Cancel"
          onSecondary={onClose}
          primaryLabel={TRANSACTIONS_COPY.saveTransaction}
          primaryType="submit"
          primaryDisabled={isSubmitting}
        />
      </form>
    </Modal>
  );
}
