// The Add/Edit bill modal (design/Household Dashboard.dc.html's "ADD BILL"
// panel) -- one modal for both, the TransactionModal.tsx pattern the task
// brief names directly: an `isEditing` branch off a single optional `bill`
// prop, not two components.
//
// Owns its own useAccounts/useCategories/useHouseholdMembers calls, mounted
// only while the modal is open (BillsPage.tsx's own conditional render), so
// none of the three fires on every Bills page load -- the same reason
// TransactionModal.tsx's own header comment gives for its categories query.
// This is *unlike* TransactionModal, which takes `accounts`/`members` as
// props already resolved by TransactionsPage: BillsPage.tsx fetches none of
// the three today, and handing it three new queries just to pass down here
// would be exactly the "a page grows a modal's worth of orchestration" shape
// useBills.ts's own header comment already warns TransactionsPage.tsx fell
// into.
//
// Split into an outer BillModal (the three queries, plus the one gate that
// actually matters) and an inner BillModalForm (every useState hook) --
// BudgetModal.tsx's own shape, for the identical reason: a useState
// initialiser that reads `accounts` before the query has resolved would seed
// a default that never updates once the real list arrives, since an
// initialiser only runs on a component's first mount. Only `accounts.data`
// gates the split -- it is the one query a stateful default (the create
// path's own Pay from account, and every Amount currency lookup) is derived
// from. Categories and members are NOT gated the same way: neither seeds a
// stateful default, only an <option> list, so each is handed down as
// `data ?? []` and simply renders empty until its own query resolves --
// TransactionModal.tsx's own `(categories.data ?? []).filter(...)` already
// tolerates the identical gap for the identical hook.
import { type FormEvent, useState } from "react";
import { Modal } from "../../components/Modal";
import { ToggleSwitch } from "../../components/ToggleSwitch";
import { ApiError } from "../../api/client";
import { apiErrorMessage } from "../auth/copy";
import type { MemberView } from "../settings/schemas";
import { useHouseholdMembers } from "../settings/useHouseholdMembers";
import { BILL_COPY, CADENCE_OPTIONS } from "./billCopy";
import { describeAmountError, minorUnitsToInputValue, toMinorUnits } from "./formatMoney";
import type { Account } from "./schemas";
import type { Category } from "./transactionSchemas";
import { useCategories } from "./useTransactions";
import { useAccounts } from "./useAccounts";
import { useCreateBill, useRestoreBill, useUpdateBill, type CreateBillBody, type UpdateBillBody } from "./useBills";
import type { Bill } from "./billSchemas";

// today() reads the *local* calendar date via getFullYear/getMonth/getDate,
// never toISOString() (which converts to UTC first) -- the same function and
// the same reason AccountModal.tsx's and TransactionModal.tsx's own today()
// give. Duplicated rather than imported: a small, already-tested four-line
// function is a smaller risk to share than the coupling importing it across
// features would add (TransactionModal.tsx's own comment on the identical
// choice).
function today(): string {
  const now = new Date();
  const year = now.getFullYear();
  const month = String(now.getMonth() + 1).padStart(2, "0");
  const day = String(now.getDate()).padStart(2, "0");
  return `${year}-${month}-${day}`;
}

export function BillModal({
  mode,
  bill,
  onClose,
  onSaved,
}: {
  mode: "create" | "edit";
  // Present only when mode === "edit" -- BillsPage.tsx's own modalBill state
  // is "new" | Bill | null, never "edit" paired with no bill, the identical
  // contract GoalModal.tsx's own `goal` prop documents.
  bill?: Bill;
  onClose: () => void;
  onSaved: () => void;
}) {
  const isEditing = mode === "edit";
  const accounts = useAccounts(false);
  const categories = useCategories();
  const members = useHouseholdMembers();

  if (!accounts.data) {
    return (
      <Modal open onClose={onClose} title={isEditing ? BILL_COPY.editBillModalTitle : BILL_COPY.addBillModalTitle}>
        <p className="text-xs text-muted" data-testid="bill-modal-loading">
          {BILL_COPY.loading}
        </p>
      </Modal>
    );
  }

  return (
    <BillModalForm
      mode={mode}
      bill={bill}
      accounts={accounts.data.accounts}
      categories={categories.data ?? []}
      members={members.data ?? []}
      onClose={onClose}
      onSaved={onSaved}
    />
  );
}

// Split from BillModal so every field's `useState(() => ...)` initialiser --
// which reads `accounts`/`bill` -- runs exactly once, the moment
// `accounts.data` first exists. See this file's own header comment for why.
function BillModalForm({
  mode,
  bill,
  accounts,
  categories,
  members,
  onClose,
  onSaved,
}: {
  mode: "create" | "edit";
  bill?: Bill;
  accounts: Account[];
  categories: Category[];
  members: MemberView[];
  onClose: () => void;
  onSaved: () => void;
}) {
  const isEditing = mode === "edit";
  const createBill = useCreateBill();
  const updateBill = useUpdateBill();
  const restoreBill = useRestoreBill();

  const [name, setName] = useState(bill?.name ?? "");
  const [amountInput, setAmountInput] = useState(() =>
    bill ? minorUnitsToInputValue(bill.amountMinor, bill.currency) : "",
  );
  const [cadence, setCadence] = useState<CreateBillBody["cadence"]>(bill?.cadence ?? "monthly");
  // bill?.nextDue ?? today() would be wrong here: for a settled one-off,
  // nextDue is null on purpose (billDTO's own comment), and `null ?? today()`
  // would silently prefill today's date -- turning an edit that never
  // touched the date into a save that un-settles the bill with a due date
  // the household never chose. Editing must start from what the bill
  // actually has (blank, for a settled one), not from a fabricated default;
  // only a brand-new bill (bill === undefined) gets today() as a sensible
  // starting point.
  const [nextDueInput, setNextDueInput] = useState(bill ? (bill.nextDue ?? "") : today());
  const [categoryId, setCategoryId] = useState(bill?.categoryId ?? "");
  const [payFromAccountId, setPayFromAccountId] = useState(() => bill?.payFromAccountId ?? accounts[0]?.id ?? "");
  const [paidByMembershipId, setPaidByMembershipId] = useState(bill?.paidByMembershipId ?? "");
  const [autopay, setAutopay] = useState(bill?.autopay ?? false);
  const [isSubscription, setIsSubscription] = useState(bill?.isSubscription ?? false);

  const [amountError, setAmountError] = useState<string | null>(null);
  const [nextDueError, setNextDueError] = useState<string | null>(null);
  const [saveError, setSaveError] = useState<string | null>(null);
  // Set only for a 409 BILL_NAME_TAKEN whose body names an archived bill --
  // writeBillWriteError's own lookup, which finds the archived bill itself so
  // this modal never has to guess which one collided (BudgetModal.tsx's
  // categoryNameTaken / GoalModal.tsx's restoreGoalId, the same pattern for a
  // third resource).
  const [restoreBillId, setRestoreBillId] = useState<string | null>(null);
  const [isSaving, setIsSaving] = useState(false);
  const [isRestoring, setIsRestoring] = useState(false);

  // A bill has no currency of its own to choose (spec decision 7): it is
  // denominated in whichever account pays it. Create mode reads the
  // currently-selected Pay from account's own currency, since nothing else
  // exists yet to derive one from; edit mode reads the bill's own stored
  // currency, which stays fixed regardless of which account is selected --
  // BILL_CURRENCY_IMMUTABLE is the server refusing to let this component's
  // own choice of a different-currency account silently reinterpret an
  // already-typed amount under a currency the household never saw.
  const currentAccount = accounts.find((a) => a.id === payFromAccountId);
  const currency = isEditing ? bill!.currency : (currentAccount?.balance?.currency ?? "");

  // Required unless editing a bill that is already settled (a paid one-off
  // with no next occurrence -- nextDue === null). createBillRequest has no
  // way to omit this field at all (billDTO's own comment: the only way to
  // reach nextDue === null is through MarkPaid, never through Create), but a
  // patch that never touches a settled bill's date must stay free to leave it
  // settled -- forcing a date here would mean the only way to rename a
  // settled bill is to also give it a new due date.
  const nextDueRequired = !isEditing || bill!.nextDue !== null;

  // Bills only ever produce expense transactions (decision 1: "a bill is an
  // actual payment to an actual company"), so this filters the same way
  // TransactionModal.tsx's own relevantCategories does for its Expense tab.
  const relevantCategories = categories.filter((c) => c.kind === "expense");

  async function handleSave(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    setAmountError(null);
    setNextDueError(null);
    setSaveError(null);
    setRestoreBillId(null);

    const amountMinor = toMinorUnits(amountInput, currency);
    if (amountMinor === null) {
      setAmountError(describeAmountError(amountInput, currency, "89.90"));
      return;
    }
    if (amountMinor <= 0) {
      setAmountError(BILL_COPY.amountMustBePositive);
      return;
    }

    if (nextDueRequired && nextDueInput.trim() === "") {
      setNextDueError(BILL_COPY.chooseADueDate);
      return;
    }

    const trimmedName = name.trim();
    setIsSaving(true);
    try {
      if (isEditing) {
        // Every field but category/payer/date is safe to resend
        // unconditionally: none of name/amountMinor/cadence/payFromAccountId/
        // autopay/isSubscription is a derived figure this form could restate
        // wrongly the way AccountModal's Balance once was
        // (docs/LEARNING.md pattern 1) -- each is exactly what is showing in
        // the field, prefilled from the same bill this PATCH targets.
        // Archiving is never one of these fields -- UpdateBillBody has no
        // archive key at all (its own comment: archiving is not a patchable
        // field, so an ordinary rename can never archive a bill as a side
        // effect of saving).
        const body: UpdateBillBody = {
          name: trimmedName,
          amountMinor,
          cadence,
          payFromAccountId,
          autopay,
          isSubscription,
        };
        if (nextDueInput.trim() !== "") {
          body.nextDue = nextDueInput;
        }
        // clearCategory/clearPayer mirror UpdateBillBody's own explicit-clear
        // fields (useBills.ts's own comment): an absent categoryId already
        // means "leave alone", so a blank selection cannot also mean that --
        // it has to say "clear" instead.
        if (categoryId === "") {
          body.clearCategory = true;
        } else {
          body.categoryId = categoryId;
        }
        if (paidByMembershipId === "") {
          body.clearPayer = true;
        } else {
          body.paidByMembershipId = paidByMembershipId;
        }
        // mode === "edit" guarantees the caller passed `bill` (this
        // component's own contract, documented on the prop above).
        await updateBill.mutateAsync({ id: bill!.id, body });
      } else {
        const body: CreateBillBody = {
          name: trimmedName,
          amountMinor,
          cadence,
          nextDue: nextDueInput,
          categoryId,
          payFromAccountId,
          paidByMembershipId,
          autopay,
          isSubscription,
        };
        await createBill.mutateAsync(body);
      }
      onSaved();
      onClose();
    } catch (err) {
      if (err instanceof ApiError && err.code === "BILL_NAME_TAKEN") {
        const archivedBillId = typeof err.details.archivedBillId === "string" ? err.details.archivedBillId : null;
        if (archivedBillId) {
          // writeBillWriteError's own archived-bill message already names
          // the bill and explains Restore -- nothing to build client-side.
          setSaveError(err.message);
          setRestoreBillId(archivedBillId);
        } else {
          setSaveError(BILL_COPY.billNameTaken(trimmedName));
        }
      } else {
        // Also covers BILL_CURRENCY_IMMUTABLE: writeBillCurrencyMismatch's
        // own message (bill_handlers.go) already names both currencies, so
        // there is nothing to extract from err.details here -- the server's
        // own sentence is what this modal shows, and keeping the modal open
        // (no onSaved/onClose on this path) is what "422 keeps the modal
        // open" actually means.
        setSaveError(apiErrorMessage(err, BILL_COPY.genericSaveError));
      }
    } finally {
      setIsSaving(false);
    }
  }

  async function handleRestore() {
    if (!restoreBillId) return;
    setIsRestoring(true);
    try {
      // Restoring brings the archived bill itself back -- it already holds
      // this name, so there is nothing left to create. A retried save would
      // just 409 again against the bill this call just un-archived
      // (GoalModal.tsx's own handleRestore carries the identical reasoning).
      await restoreBill.mutateAsync(restoreBillId);
      onSaved();
      onClose();
    } catch (err) {
      setSaveError(apiErrorMessage(err, BILL_COPY.genericSaveError));
    } finally {
      setIsRestoring(false);
    }
  }

  return (
    <Modal open onClose={onClose} title={isEditing ? BILL_COPY.editBillModalTitle : BILL_COPY.addBillModalTitle}>
      <form className="flex flex-col gap-4" onSubmit={handleSave}>
        <div className="grid grid-cols-2 gap-4">
          <div className="flex flex-col gap-1.5">
            <label htmlFor="bill-modal-name" className="text-xs font-semibold text-label">
              {BILL_COPY.billNameLabel}
            </label>
            <input
              id="bill-modal-name"
              type="text"
              required
              value={name}
              onChange={(event) => setName(event.target.value)}
              placeholder={BILL_COPY.billNamePlaceholder}
              className="rounded-lg border border-hairline bg-card px-3.5 py-2.5 text-[13.5px]"
            />
          </div>

          <div className="flex flex-col gap-1.5">
            <label htmlFor="bill-modal-amount" className="text-xs font-semibold text-label">
              {BILL_COPY.amountLabel}
            </label>
            <input
              id="bill-modal-amount"
              type="text"
              inputMode="decimal"
              required
              value={amountInput}
              onChange={(event) => setAmountInput(event.target.value)}
              className="rounded-lg border border-hairline bg-card px-3.5 py-2.5 text-[13.5px]"
            />
          </div>
        </div>

        {amountError && (
          <p role="alert" className="text-xs leading-snug text-danger">
            {amountError}
          </p>
        )}

        <div className="grid grid-cols-2 gap-4">
          <div className="flex flex-col gap-1.5">
            <label htmlFor="bill-modal-cadence" className="text-xs font-semibold text-label">
              {BILL_COPY.repeatsLabel}
            </label>
            <select
              id="bill-modal-cadence"
              value={cadence}
              onChange={(event) => setCadence(event.target.value as CreateBillBody["cadence"])}
              className="rounded-lg border border-hairline bg-card px-3.5 py-2.5 text-[13.5px]"
            >
              {CADENCE_OPTIONS.map((option) => (
                <option key={option.value} value={option.value}>
                  {option.label}
                </option>
              ))}
            </select>
          </div>

          <div className="flex flex-col gap-1.5">
            <label htmlFor="bill-modal-next-due" className="text-xs font-semibold text-label">
              {BILL_COPY.nextDueLabel}
            </label>
            <input
              id="bill-modal-next-due"
              type="date"
              required={nextDueRequired}
              value={nextDueInput}
              onChange={(event) => setNextDueInput(event.target.value)}
              className="rounded-lg border border-hairline bg-card px-3.5 py-2.5 text-[13.5px]"
            />
            {nextDueError && (
              <p role="alert" className="text-xs leading-snug text-danger">
                {nextDueError}
              </p>
            )}
          </div>
        </div>

        <div className="grid grid-cols-2 gap-4">
          <div className="flex flex-col gap-1.5">
            <label htmlFor="bill-modal-category" className="text-xs font-semibold text-label">
              {BILL_COPY.categoryLabel}
            </label>
            <select
              id="bill-modal-category"
              value={categoryId}
              onChange={(event) => setCategoryId(event.target.value)}
              className="rounded-lg border border-hairline bg-card px-3.5 py-2.5 text-[13.5px]"
            >
              <option value="">{BILL_COPY.noCategoryOption}</option>
              {/* Only reachable if the bill's own category has since been
                  archived -- useCategories() fetches live categories only, so
                  a still-selected archived category would otherwise vanish
                  from the list and this select would show blank instead of
                  what the bill actually has. A display fix only: categoryId
                  already holds the real id in state regardless of whether an
                  <option> exists to show it. */}
              {categoryId !== "" && !relevantCategories.some((c) => c.id === categoryId) && (
                <option value={categoryId}>{bill?.categoryName ?? categoryId}</option>
              )}
              {relevantCategories.map((c) => (
                <option key={c.id} value={c.id}>
                  {c.name}
                </option>
              ))}
            </select>
          </div>

          <div className="flex flex-col gap-1.5">
            <label htmlFor="bill-modal-pay-from" className="text-xs font-semibold text-label">
              {BILL_COPY.payFromLabel}
            </label>
            <select
              id="bill-modal-pay-from"
              required
              value={payFromAccountId}
              onChange={(event) => setPayFromAccountId(event.target.value)}
              className="rounded-lg border border-hairline bg-card px-3.5 py-2.5 text-[13.5px]"
            >
              {/* Same reasoning as Category above, for the identical case:
                  the bill's own pay-from account has since been archived.
                  A display fix only, not what makes the submitted id
                  correct -- payFromAccountId already holds the real id in
                  state either way. */}
              {payFromAccountId !== "" && !accounts.some((a) => a.id === payFromAccountId) && (
                <option value={payFromAccountId}>{bill?.accountName ?? payFromAccountId}</option>
              )}
              {accounts.map((a) => (
                <option key={a.id} value={a.id}>
                  {a.nickname}
                </option>
              ))}
            </select>
          </div>
        </div>

        <div className="flex flex-col gap-1.5">
          <label htmlFor="bill-modal-paid-by" className="text-xs font-semibold text-label">
            {BILL_COPY.paidByLabel}
          </label>
          <select
            id="bill-modal-paid-by"
            value={paidByMembershipId}
            onChange={(event) => setPaidByMembershipId(event.target.value)}
            className="rounded-lg border border-hairline bg-card px-3.5 py-2.5 text-[13.5px]"
          >
            <option value="">{BILL_COPY.unassignedPayer}</option>
            {members.map((m) => (
              <option key={m.id} value={m.id}>
                {m.user.displayName}
              </option>
            ))}
          </select>
        </div>

        <div className="flex items-center justify-between rounded-[10px] border border-hairline px-3.5 py-2.5">
          <div>
            <div className="text-[13px] font-semibold text-ink">{BILL_COPY.onAutopayLabel}</div>
            <div className="mt-0.5 text-[11.5px] text-muted">{BILL_COPY.onAutopayHelp}</div>
          </div>
          <ToggleSwitch checked={autopay} onChange={() => setAutopay((v) => !v)} label={BILL_COPY.onAutopayLabel} />
        </div>

        <div className="flex items-center justify-between rounded-[10px] border border-hairline px-3.5 py-2.5">
          <div>
            <div className="text-[13px] font-semibold text-ink">{BILL_COPY.isSubscriptionLabel}</div>
            <div className="mt-0.5 text-[11.5px] text-muted">{BILL_COPY.isSubscriptionHelp}</div>
          </div>
          <ToggleSwitch
            checked={isSubscription}
            onChange={() => setIsSubscription((v) => !v)}
            label={BILL_COPY.isSubscriptionLabel}
          />
        </div>

        {saveError !== null && (
          <div className="flex flex-col gap-2">
            <p role="alert" className="text-xs leading-snug text-danger">
              {saveError}
            </p>
            {restoreBillId && (
              <button
                type="button"
                disabled={isRestoring}
                onClick={handleRestore}
                className="self-start rounded-lg border border-hairline px-3 py-1.5 text-[12.5px] font-semibold text-accent disabled:cursor-not-allowed disabled:opacity-60"
              >
                {BILL_COPY.restore}
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
            {BILL_COPY.cancelAction}
          </button>
          <button
            type="submit"
            disabled={isSaving}
            className="flex-[2] rounded-lg bg-accent py-2.5 text-center text-[13px] font-semibold text-white disabled:cursor-not-allowed disabled:opacity-60"
          >
            {isEditing ? BILL_COPY.saveBillSubmit : BILL_COPY.addBillSubmit}
          </button>
        </div>
      </form>
    </Modal>
  );
}
