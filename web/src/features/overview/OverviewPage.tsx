// The app's front door. Four of the design's eight cards -- the four Money
// can already supply (net worth, this month's budget, next bill, goals on
// track) -- rather than the placeholder that stood here, which every
// household saw on every visit, new and established alike.
//
// This page is the only one every member reaches: Money's pages sit behind
// RequireCapability, so a member without money never sees them. Which means
// the no-access shapes below are not an edge case, they are one of the three
// normal renders. See the guards in api/internal/adapter/http/router.go:
// /accounts needs the money capability; /budgets/{month} and /goals both
// need money AND owner.
import { Link } from "@tanstack/react-router";
import { PageContainer } from "../../components/PageContainer";
import { useMe } from "../auth/useAuth";
import { NetWorthCard } from "../money/NetWorthCard";
import { currentMonth } from "../money/month";
import { useAccounts } from "../money/useAccounts";
import { useBudget } from "../money/useBudget";
import { useGoals } from "../money/useGoals";
import { BudgetCard } from "./BudgetCard";
import { OVERVIEW_COPY } from "./copy";
import { GoalsCard } from "./GoalsCard";
import { NextBillCard } from "./NextBillCard";
import { QuickAddMenu } from "./QuickAddMenu";
import { SetupChecklist } from "./SetupChecklist";

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
  // GET /goals carries the identical guard as GET /budgets/{month}
  // (requireCapability(money) AND requireOwner), so this is gated the same
  // way and for the same reason.
  const goals = useGoals({ enabled: isOwner });

  return (
    <PageContainer>
      <div className="flex items-center justify-between">
        <h1 className="font-serif text-2xl">{OVERVIEW_COPY.title}</h1>
        {isOwner && hasMoney && <QuickAddMenu accounts={accounts.data?.accounts ?? []} />}
      </div>

      {!hasMoney ? (
        <section className="rounded-xl border border-hairline bg-card p-[22px]">
          <p className="text-[13px] text-muted">{OVERVIEW_COPY.noMoneyAccess}</p>
        </section>
      ) : (
        <>
          <div className="grid grid-cols-1 gap-4 sm:grid-cols-2">
            {/* `summary` is omitted, not zeroed, for a member who may not see
                amounts (features/money/schemas.ts). Its absence is the only
                signal there is -- never synthesise one to fill the gap. */}
            {accounts.data?.summary && <NetWorthCard summary={accounts.data.summary} />}

            {/* Gated on isSuccess, not on `!accounts.data?.summary` alone:
                summary is equally undefined while the request is still in
                flight, so the looser condition would flash this panel at an
                owner on every visit before their figures arrived. */}
            {accounts.isSuccess && !accounts.data.summary && (
              <section
                aria-labelledby="overview-limited-heading"
                className="flex flex-col rounded-xl border border-hairline bg-card p-[22px]"
              >
                <h2 id="overview-limited-heading" className="text-xs text-muted">
                  {OVERVIEW_COPY.limitedHeading}
                </h2>
                <p className="mt-1.5 text-[13px] text-ink">{OVERVIEW_COPY.limitedNoAmounts}</p>
                <Link to="/money" className="mt-3 text-[13px] font-semibold text-accent">
                  {OVERVIEW_COPY.limitedGo}
                </Link>
              </section>
            )}

            {isOwner && budget.data && <BudgetCard month={budget.data} />}

            {/* Unlike BudgetCard/GoalsCard above, mounted unconditionally
                here rather than gated on `isOwner &&` -- NextBillCard.tsx
                owns its own useBills call and decides for itself, from
                `enabled`, whether GET /bills is ever asked for. See that
                file's own header comment for why: a limited member's
                browser still mounts this card, it just never fires the
                request, which is what makes "renders nothing while its
                query is disabled" a state the component itself has to
                handle rather than one OverviewPage prevents by never
                mounting it. */}
            <NextBillCard enabled={isOwner} />

            {isOwner && goals.data && <GoalsCard goals={goals.data} />}
          </div>

          {/* Full width beneath the cards rather than a third cell in the
              grid: the cards are the figures someone opens this page to
              glance at, and a chore list sitting beside them at the same
              size competes with that. */}
          {/* Both queries must have *answered* before this renders, not just
              the member be an owner. `hasAccount` and `hasBudget` are false
              while their requests are in flight, which is indistinguishable
              from "this household has neither" -- so without these two gates
              an established household is told to go and create the account
              and budget it already has, on every cold load, until the figures
              land. Same root cause as the limited-member panel above:
              a claim derived from data that has not arrived. */}
          {isOwner && accounts.isSuccess && budget.data && (
            <SetupChecklist
              hasAccount={accounts.data.accounts.length > 0}
              hasBudget={budget.data.budget != null}
            />
          )}
        </>
      )}
    </PageContainer>
  );
}
