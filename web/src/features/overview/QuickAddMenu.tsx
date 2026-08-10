// The design's "+ Add" offers Transaction, Account, Bill, Savings goal,
// Calendar event and Marriage retro. Two of those features do not exist,
// and a permanently greyed row reads as broken rather than as a roadmap --
// the same rule Sidebar.tsx's SPACE_PAGES states. Each entry joins this list
// in the change that builds the thing it creates -- Bill is this change's
// own (Task 16), joining Account/Transaction/Savings goal before it.
//
// Rendered only for an owner: POST /transactions, POST /accounts,
// POST /bills and POST /goals are all requireOwner.
import { useState } from "react";
import { useCurrencies, useMe } from "../auth/useAuth";
import { AccountModal } from "../money/AccountModal";
import { BillModal } from "../money/BillModal";
import { GoalModal } from "../money/GoalModal";
import { TransactionModal, type TransactionFormValues } from "../money/TransactionModal";
import { useCreateTransaction } from "../money/useTransactions";
import { useHouseholdMembers } from "../settings/useHouseholdMembers";
import type { Account } from "../money/schemas";
import { OVERVIEW_COPY } from "./copy";

export function QuickAddMenu({ accounts }: { accounts: Account[] }) {
  const [open, setOpen] = useState(false);
  const [accountOpen, setAccountOpen] = useState(false);
  const [transactionOpen, setTransactionOpen] = useState(false);
  const [billOpen, setBillOpen] = useState(false);
  const [goalOpen, setGoalOpen] = useState(false);
  const members = useHouseholdMembers();
  const createTransaction = useCreateTransaction();
  // GoalModal.tsx takes `currencies`/`primaryCurrency` as explicit props
  // rather than fetching them itself (unlike AccountModal, whose own
  // internal useCurrencies/useMe calls are deferred to when it mounts) --
  // its own header comment on why: the currency select is disabled in edit
  // mode and needs the household's own currency to prefill create mode.
  // Called unconditionally here rather than gated behind `goalOpen`, the
  // same choice GoalsPage.tsx's own top-level useCurrencies() call already
  // makes for this exact modal: both keys are already warm on this page
  // regardless (['me'] from OverviewPage's own useMe() above this
  // component, ['currencies'] from BudgetCard's, and useCurrencies' own
  // staleTime: Infinity means neither ever re-fetches once cached), so this
  // costs no request TransactionModal's useCategories/AccountModal's own
  // fetches would have to avoid by staying deferred.
  const me = useMe();
  const currencies = useCurrencies();

  const canAddTransaction = accounts.length > 0;
  // Same condition as canAddTransaction, named separately for its own entry
  // below -- a bill needs a pay-from account exactly as a transaction needs
  // one to post against, the same precondition the brief pins directly to
  // this existing pattern.
  const canAddBill = accounts.length > 0;

  return (
    <div className="relative">
      <button
        type="button"
        onClick={() => setOpen((v) => !v)}
        className="rounded-lg bg-accent px-3.5 py-2 text-[13px] font-semibold text-white"
      >
        {OVERVIEW_COPY.quickAdd}
      </button>

      {open && (
        <div className="absolute right-0 z-10 mt-1.5 flex w-[220px] flex-col gap-0.5 rounded-xl border border-hairline bg-card p-1.5 shadow-[var(--shadow-auth-card)]">
          <button
            type="button"
            disabled={!canAddTransaction}
            onClick={() => {
              setOpen(false);
              setTransactionOpen(true);
            }}
            className="rounded-lg px-2.5 py-2 text-left text-[13px] text-ink disabled:cursor-not-allowed disabled:opacity-60"
          >
            {OVERVIEW_COPY.quickAddTransaction}
          </button>
          {/* Disabled with its reason beside it, not a modal whose account
              dropdown is empty -- the dead end TransactionsPage refuses. */}
          {!canAddTransaction && (
            <p className="px-2.5 pb-1 text-[11px] text-muted">
              {OVERVIEW_COPY.quickAddNeedsAccount}
            </p>
          )}

          <button
            type="button"
            onClick={() => {
              setOpen(false);
              setAccountOpen(true);
            }}
            className="rounded-lg px-2.5 py-2 text-left text-[13px] text-ink"
          >
            {OVERVIEW_COPY.quickAddAccount}
          </button>

          <button
            type="button"
            disabled={!canAddBill}
            onClick={() => {
              setOpen(false);
              setBillOpen(true);
            }}
            className="rounded-lg px-2.5 py-2 text-left text-[13px] text-ink disabled:cursor-not-allowed disabled:opacity-60"
          >
            {OVERVIEW_COPY.quickAddBill}
          </button>
          {/* Same reason as Transaction's own disabled state above -- a bill
              needs a pay-from account, and BillModal's own Pay from select
              would otherwise open with nothing to choose. */}
          {!canAddBill && (
            <p className="px-2.5 pb-1 text-[11px] text-muted">
              {OVERVIEW_COPY.quickAddNeedsAccount}
            </p>
          )}

          {/* No `disabled` guard, unlike Transaction/Bill above -- decision 6
              (goal contributions move no real money) means this has no
              account precondition to wait on. */}
          <button
            type="button"
            onClick={() => {
              setOpen(false);
              setGoalOpen(true);
            }}
            className="rounded-lg px-2.5 py-2 text-left text-[13px] text-ink"
          >
            {OVERVIEW_COPY.quickAddGoal}
          </button>
        </div>
      )}

      {/* All three mounted only while actually open, matching
          TransactionsPage's own AccountModal/TransactionModal. Each modal
          carries queries of its own -- TransactionModal's useCategories,
          AccountModal's currencies and members -- and mounting them
          alongside the menu would fire those on every visit to the app's
          front door, whether or not anyone ever opens one. GoalModal takes
          no query of its own to defer this way (its currencies/primaryCurrency
          are the props sourced above, already warm by construction), but
          stays gated on `goalOpen` regardless, for the same reason
          AccountModal/TransactionModal do: no reason to keep an unused form
          mounted in the tree. */}
      {accountOpen && <AccountModal open onClose={() => setAccountOpen(false)} />}
      {transactionOpen && (
        <TransactionModal
          open
          onClose={() => setTransactionOpen(false)}
          onSubmit={(values: TransactionFormValues) => createTransaction.mutateAsync(values)}
          accounts={accounts}
          // The same mapping TransactionsPage uses, not a second one -- the
          // two screens must not disagree about what a member is called.
          members={(members.data ?? []).map((m) => ({ id: m.id, name: m.user.displayName }))}
        />
      )}
      {/* BillModal fetches its own accounts/categories/members (BillModal.tsx's
          own header comment on why), so it needs no props sourced here the
          way TransactionModal above does -- gating on `billOpen` alone is
          enough, the same shape AccountModal uses. createBill (useBills.ts)
          invalidates both billsQueryKey variants on success, and
          NextBillCard's own `useBills(false, { enabled: isOwner })` --
          mounted and active the whole time this menu exists -- shares that
          exact cache entry, so it refetches on its own; nothing here needs
          to ask again. */}
      {billOpen && <BillModal mode="create" onClose={() => setBillOpen(false)} onSaved={() => setBillOpen(false)} />}
      {/* Gated on both me.data and currencies.data, the same guard
          GoalsPage.tsx's own modal mount uses: GoalModal's props are
          required, not optional, and this only matters for an implausibly
          fast click between opening the menu and choosing Savings goal
          before either has resolved. createGoal (useGoals.ts) invalidates
          the goals query on success, so OverviewPage's own
          `useGoals({ enabled: isOwner })` -- mounted and active the whole
          time this menu exists -- refetches on its own; nothing here needs
          to ask again. */}
      {goalOpen && me.data && currencies.data && (
        <GoalModal
          mode="create"
          currencies={currencies.data.currencies}
          primaryCurrency={me.data.household.primaryCurrency}
          onClose={() => setGoalOpen(false)}
          onSaved={() => setGoalOpen(false)}
        />
      )}
    </div>
  );
}
