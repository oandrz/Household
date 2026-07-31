// The app's front door. Two of the design's eight cards -- the two Money can
// already supply -- rather than the placeholder that stood here, which every
// household saw on every visit, new and established alike.
//
// This page is the only one every member reaches: Money's pages sit behind
// RequireCapability, so a member without money never sees them. Which means
// the no-access shapes below are not an edge case, they are one of the three
// normal renders. See the guards in api/internal/adapter/http/router.go:
// /accounts needs the money capability; /budgets/{month} needs money AND
// owner.
import { useMe } from "../auth/useAuth";
import { NetWorthCard } from "../money/NetWorthCard";
import { currentMonth } from "../money/month";
import { useAccounts } from "../money/useAccounts";
import { useBudget } from "../money/useBudget";
import { BudgetCard } from "./BudgetCard";
import { OVERVIEW_COPY } from "./copy";

export function OverviewPage() {
  const me = useMe();
  const isOwner = me.data?.membership.role === "owner";
  const hasMoney = me.data?.capabilities.includes("money") ?? false;

  const accounts = useAccounts(false);
  // Only an owner may read a budget at all, so a limited member must not even
  // ask -- the request would be answered 403 and leave a failed query in the
  // cache for nobody to read. `enabled`, not a fake month and not a
  // conditional call.
  const budget = useBudget(currentMonth(), { enabled: isOwner });

  return (
    <div className="flex flex-col gap-5 p-10">
      <h1 className="font-serif text-2xl">{OVERVIEW_COPY.title}</h1>

      {!hasMoney ? (
        <section className="rounded-xl border border-hairline bg-card p-[22px]">
          <p className="text-[13px] text-muted">{OVERVIEW_COPY.noMoneyAccess}</p>
        </section>
      ) : (
        <div className="grid grid-cols-1 gap-4 sm:grid-cols-2">
          {/* `summary` is omitted, not zeroed, for a member who may not see
              amounts (features/money/schemas.ts). Its absence is the only
              signal there is -- never synthesise one to fill the gap. */}
          {accounts.data?.summary && <NetWorthCard summary={accounts.data.summary} />}
          {isOwner && budget.data && <BudgetCard month={budget.data} />}
        </div>
      )}
    </div>
  );
}
