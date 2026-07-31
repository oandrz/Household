// The design's "+ Add" offers Transaction, Account, Bill, Savings goal,
// Calendar event and Marriage retro. Four of those features do not exist, and
// a permanently greyed row reads as broken rather than as a roadmap -- the
// same rule Sidebar.tsx's SPACE_PAGES states. Each entry joins this list in
// the change that builds the thing it creates.
//
// Rendered only for an owner: POST /transactions and POST /accounts are both
// requireOwner.
import { useState } from "react";
import { AccountModal } from "../money/AccountModal";
import { TransactionModal, type TransactionFormValues } from "../money/TransactionModal";
import { useCreateTransaction } from "../money/useTransactions";
import { useHouseholdMembers } from "../settings/useHouseholdMembers";
import type { Account } from "../money/schemas";
import { OVERVIEW_COPY } from "./copy";

export function QuickAddMenu({ accounts }: { accounts: Account[] }) {
  const [open, setOpen] = useState(false);
  const [accountOpen, setAccountOpen] = useState(false);
  const [transactionOpen, setTransactionOpen] = useState(false);
  const members = useHouseholdMembers();
  const createTransaction = useCreateTransaction();

  const canAddTransaction = accounts.length > 0;

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
        </div>
      )}

      {/* Both mounted only while actually open, matching TransactionsPage's
          own AccountModal/TransactionModal. Each modal carries queries of its
          own -- TransactionModal's useCategories, AccountModal's currencies
          and members -- and mounting them alongside the menu would fire those
          on every visit to the app's front door, whether or not anyone ever
          opens one. */}
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
    </div>
  );
}
