// Overview's "Next bill" tile (design/Household Dashboard.dc.html's own
// go_bills-linked card, third of the row): "S$142.30" over
// "SP utilities · Jul 8".
//
// Unlike BudgetCard.tsx/GoalsCard.tsx -- both pure presentation over a prop
// OverviewPage.tsx fetches for them -- this component calls useBills itself.
// That is deliberate: task-16-brief.md pins "NextBillCard renders nothing
// while its query is disabled" as this component's own testable behaviour,
// which only means something if the disabled state is reachable inside it.
// Under the BudgetCard/GoalsCard shape, OverviewPage would decide whether to
// mount this card at all, and the state a test would need to construct
// (mounted, but with no data) would never happen on the real page. So
// OverviewPage.tsx mounts this unconditionally inside its `hasMoney` branch
// and hands down `enabled={isOwner}` -- the same boolean that gates
// useBudget/useGoals there -- and this component is what decides, from
// `enabled`, whether GET /bills is ever asked for at all (Task 11's own
// `enabled` option: the request is never sent, not sent and 403ed).
import { Link } from "@tanstack/react-router";
import { useCurrencies } from "../auth/useAuth";
import { monthDayLabel } from "../money/billCopy";
import { formatMoney } from "../money/formatMoney";
import { useBills } from "../money/useBills";
import { OVERVIEW_COPY } from "./copy";

export function NextBillCard({ enabled }: { enabled: boolean }) {
  const bills = useBills(false, { enabled });
  // nextDue.amountMinor/currency are the BILL's own, never converted to the
  // household's primary (billsSummarySchema's own comment on
  // nextDueBillDTO) -- so the symbol lookup has to key off that bill's own
  // currency, not off useMe's household.primaryCurrency the way most other
  // Overview cards do. Public and cached with staleTime: Infinity
  // (useCurrencies' own comment), so this costs no request a limited member
  // wasn't already going to trigger elsewhere on this page.
  const currencies = useCurrencies();

  // Covers three states identically: the query disabled (a limited member,
  // or an owner whose accounts/me have not resolved yet), still in flight,
  // or errored -- none of the three has a figure to show, and this card's
  // only job is a glance-figure or nothing, never a loading spinner or an
  // error region competing for space with the cards beside it.
  if (!bills.data) return null;

  const { summary } = bills.data;
  const { nextDue } = summary;
  const symbol = nextDue
    ? currencies.data?.currencies.find((c) => c.code === nextDue.currency)?.symbol
    : undefined;

  return (
    <section
      aria-labelledby="overview-next-bill-heading"
      className="flex flex-col rounded-xl border border-hairline bg-card p-[22px]"
    >
      <h2 id="overview-next-bill-heading" className="text-xs text-muted">
        {OVERVIEW_COPY.nextBillHeading}
      </h2>

      {nextDue ? (
        <>
          <p className="mt-1.5 text-[30px] font-semibold tracking-[-0.03em] text-ink">
            {formatMoney(nextDue.amountMinor, nextDue.currency, symbol)}
          </p>
          <p className="mt-1 text-[11.5px] text-muted">
            {nextDue.overdue
              ? OVERVIEW_COPY.nextBillOverdueClause(nextDue.billName)
              : OVERVIEW_COPY.nextBillClause(nextDue.billName, monthDayLabel(nextDue.dueOn))}
          </p>
        </>
      ) : summary.billCount === 0 ? (
        <>
          <p className="mt-1.5 text-[15px] text-ink">{OVERVIEW_COPY.nextBillNone}</p>
          {/* inline-flex items-center min-h-11 sm:min-h-0: BudgetCard.tsx's
              own comment on this identical pattern has the reason. */}
          <Link
            to="/money/bills"
            className="mt-3 inline-flex min-h-11 items-center text-[13px] font-semibold text-accent sm:min-h-0"
          >
            {OVERVIEW_COPY.nextBillAdd}
          </Link>
        </>
      ) : (
        // billCount > 0 but nextDue === null: every live bill is a settled
        // one-off. Not the empty branch above -- GoalsCard.tsx's own
        // hasAnyGoals comment is the precedent for keeping this a third,
        // separate state rather than folding it into "no bills yet", which
        // would be false for a household that plainly has bills.
        <p className="mt-1.5 text-[15px] text-ink">{OVERVIEW_COPY.nextBillCaughtUp}</p>
      )}
    </section>
  );
}
