// The Money space's landing page. It replaces the placeholder at /money; the
// three sibling pages (Budget, Goals, Bills) keep theirs -- Transactions has
// its own real page now (Task 17), reached from this page's own strip below.
//
// The recent-transactions strip the design draws was absent through the
// accounts work: it had no data until Transactions shipped, and an empty card
// promising future usefulness is a placeholder that looks considered. It has
// data now. The historical net-worth chart stays absent for the same original
// reason -- the API answers with one point in time, not a series.
import { useState } from "react";
import { useMe } from "../auth/useAuth";
import { ToggleSwitch } from "../../components/ToggleSwitch";
import { AccountModal } from "./AccountModal";
import { AccountsPanel } from "./AccountsPanel";
import { BreakdownCard } from "./BreakdownCard";
import { FINANCES_COPY } from "./copy";
import { NetWorthCard } from "./NetWorthCard";
import { RecentTransactionsCard } from "./RecentTransactionsCard";
import { useAccounts } from "./useAccounts";

function PageHeader() {
  return (
    <h1 className="text-[23px] font-semibold tracking-[-0.02em] text-ink">
      {FINANCES_COPY.title}
    </h1>
  );
}

// The state every new household starts in. A separate panel rather than the
// three cards rendering their own zeroes, because a fresh household's summary
// is computable and genuinely zero (usecase/networth.go: "a household with no
// accounts at all is computable and genuinely zero") -- showing that as
// "Net worth S$0.00" would be truthful but useless, next to a blank breakdown
// and an empty accounts list saying the same nothing three times over.
//
// Carries its own "Show archived" toggle, the one AccountsPanel would
// otherwise own, because AccountsPanel isn't mounted here at all. Without it,
// an owner who archives their household's only account has no way back: the
// list empties, this panel takes over, and there is nothing on it to ask for
// the archived view -- exactly the household decision 8's restore guarantee
// exists for, and the one the browser walk never caught because the seeded
// household always had several accounts.
function FirstRunPanel({
  canAdd,
  includeArchived,
  onIncludeArchivedChange,
}: {
  canAdd: boolean;
  includeArchived: boolean;
  onIncludeArchivedChange: (next: boolean) => void;
}) {
  const [addOpen, setAddOpen] = useState(false);
  return (
    <div className="flex flex-col items-center gap-3 rounded-xl border border-hairline bg-card px-10 py-16 text-center">
      <div className="flex w-full items-center justify-end gap-1.5 text-[11px] text-muted">
        <ToggleSwitch
          checked={includeArchived}
          onChange={() => onIncludeArchivedChange(!includeArchived)}
          label={FINANCES_COPY.archivedToggle}
        />
        {FINANCES_COPY.archivedToggle}
      </div>
      <p className="text-sm font-semibold text-ink">{FINANCES_COPY.emptyTitle}</p>
      <p className="max-w-sm text-[13px] text-muted">{FINANCES_COPY.emptyBody}</p>
      {canAdd && (
        <button
          type="button"
          onClick={() => setAddOpen(true)}
          className="mt-2 rounded-lg bg-accent px-3 py-1.5 text-xs font-semibold text-white"
        >
          {FINANCES_COPY.addAccount}
        </button>
      )}
      {/* This is the *only* "+ Add account" a brand-new household can reach --
          AccountsPanel isn't mounted at all in this state (see below), so
          wiring just its button would leave account creation unreachable for
          exactly the household that needs it first. Mounted only while open,
          same reasoning as AccountsPanel's own copy of this comment. */}
      {canAdd && addOpen && <AccountModal open onClose={() => setAddOpen(false)} />}
    </div>
  );
}

export function FinancesPage() {
  const [includeArchived, setIncludeArchived] = useState(false);
  const accounts = useAccounts(includeArchived);
  const me = useMe();
  const isOwner = me.data?.membership.role === "owner";

  if (accounts.isPending) {
    return <p className="p-9 text-xs text-muted">Loading…</p>;
  }
  if (accounts.isError) {
    return (
      <p role="alert" className="p-9 text-xs text-danger">
        Couldn't load your accounts.
      </p>
    );
  }

  const { accounts: rows, summary } = accounts.data;

  // No summary at all means the caller is a limited member -- the server
  // omits it rather than zeroing it (accountsResponseSchema's own comment),
  // and this page must not invent one. There is no net worth or breakdown
  // card in this state, only the accounts they've been given visibility into.
  if (!summary) {
    return (
      <div className="flex flex-col gap-5 px-9 py-8">
        <PageHeader />
        <AccountsPanel
          accounts={rows}
          includeArchived={includeArchived}
          onIncludeArchivedChange={setIncludeArchived}
          emptyMessage={FINANCES_COPY.limitedEmpty}
        />
      </div>
    );
  }

  // Not `rows.length === 0 && !includeArchived`. A household that has never
  // held an account still fetches zero rows once its own new toggle above
  // flips `includeArchived` to true (there is nothing archived to return
  // either) -- keeping the old `&& !includeArchived` clause would then read
  // that as "not the empty-household state" and fall through to the three
  // cards below with nothing behind them: "Net worth S$0.00" next to a blank
  // breakdown, exactly what this panel exists to prevent. `rows.length === 0`
  // alone means "nothing to show for the toggle's current position," which
  // is correct regardless of which position that is.
  if (rows.length === 0) {
    return (
      <div className="flex flex-col gap-5 px-9 py-8">
        <PageHeader />
        <FirstRunPanel
          canAdd={isOwner}
          includeArchived={includeArchived}
          onIncludeArchivedChange={setIncludeArchived}
        />
      </div>
    );
  }

  return (
    <div className="flex flex-col gap-5 px-9 py-8">
      <PageHeader />
      <div className="grid grid-cols-1 gap-4 md:grid-cols-2">
        <NetWorthCard summary={summary} />
        <BreakdownCard summary={summary} />
      </div>
      {/* Only reachable here, in the branch `summary` is present in -- which
          is exactly the owner branch (the comment above this function's
          `!summary` check: its absence is how a limited member's response is
          shaped). Transactions decision 5 requires owner as well as `money`
          for every transactions route ("a limited member has no Transactions
          page at all" -- the ledger's amounts would reconstruct exactly the
          balances accounts decision 5 hides), so this card must never mount
          for a limited member; this branch already guarantees that without a
          second, separate isOwner check. */}
      <RecentTransactionsCard />
      <AccountsPanel
        accounts={rows}
        includeArchived={includeArchived}
        onIncludeArchivedChange={setIncludeArchived}
      />
    </div>
  );
}
