// The Money space's landing page. It replaces the placeholder at /money; the
// four sibling pages (Transactions, Budget, Goals, Bills) keep theirs, and the
// sidebar is untouched -- it renders from the server's space list.
//
// The recent-transactions strip the design draws is deliberately absent: it
// has no data until Transactions ships, and an empty card promising future
// usefulness is a placeholder that looks considered. The historical net-worth
// chart is absent for the same reason -- the API answers with one point in
// time, not a series.
import { useState } from "react";
import { useMe } from "../auth/useAuth";
import { AccountsPanel } from "./AccountsPanel";
import { BreakdownCard } from "./BreakdownCard";
import { FINANCES_COPY } from "./copy";
import { NetWorthCard } from "./NetWorthCard";
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
function FirstRunPanel({ canAdd }: { canAdd: boolean }) {
  return (
    <div className="flex flex-col items-center gap-3 rounded-xl border border-hairline bg-card px-10 py-16 text-center">
      <p className="text-sm font-semibold text-ink">{FINANCES_COPY.emptyTitle}</p>
      <p className="max-w-sm text-[13px] text-muted">{FINANCES_COPY.emptyBody}</p>
      {canAdd && (
        // Inert for now -- see AccountsPanel's identical button for why: the
        // form it should open is Task 40's.
        <button
          type="button"
          className="mt-2 rounded-lg bg-accent px-3 py-1.5 text-xs font-semibold text-white"
        >
          {FINANCES_COPY.addAccount}
        </button>
      )}
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

  if (rows.length === 0 && !includeArchived) {
    return (
      <div className="flex flex-col gap-5 px-9 py-8">
        <PageHeader />
        <FirstRunPanel canAdd={isOwner} />
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
      <AccountsPanel
        accounts={rows}
        includeArchived={includeArchived}
        onIncludeArchivedChange={setIncludeArchived}
      />
    </div>
  );
}
