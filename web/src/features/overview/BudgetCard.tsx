// This month's budget, reduced to the one number a household glances at. The
// full screen is /money/budget; this card links there rather than repeating
// its category grid.
import { Link } from "@tanstack/react-router";
import { useCurrencies } from "../auth/useAuth";
import { formatMoney } from "../money/formatMoney";
import type { BudgetMonthResponse } from "../money/budgetSchemas";
import { OVERVIEW_COPY } from "./copy";

export function BudgetCard({ month }: { month: BudgetMonthResponse }) {
  const currencies = useCurrencies();
  const symbol = currencies.data?.currencies.find((c) => c.code === month.currency)?.symbol;

  return (
    <section
      aria-labelledby="overview-budget-heading"
      className="flex flex-col rounded-xl border border-hairline bg-card p-[22px]"
    >
      <h2 id="overview-budget-heading" className="text-xs text-muted">
        {OVERVIEW_COPY.budgetHeading}
      </h2>

      {month.budget === null ? (
        <>
          <p className="mt-1.5 text-[15px] text-ink">{OVERVIEW_COPY.budgetNone}</p>
          {/* inline-flex items-center min-h-11 sm:min-h-0: an <a> never
              centers its own content the way a <button> does --
              TransactionFilters.tsx's own SELECT_CLASS comment has the
              measured reason a control this size falls short of the 44px
              floor on a phone. */}
          <Link
            to="/money/budget"
            className="mt-3 inline-flex min-h-11 items-center text-[13px] font-semibold text-accent sm:min-h-0"
          >
            {OVERVIEW_COPY.budgetSetUp}
          </Link>
        </>
      ) : (
        <>
          <p className="mt-1.5 text-[30px] font-semibold tracking-[-0.03em] text-ink">
            {OVERVIEW_COPY.budgetUsed(month.percentUsed)}
          </p>
          <p className="tabular mt-1 text-[11.5px] text-muted">
            {OVERVIEW_COPY.budgetOf(
              formatMoney(month.spentMinor, month.currency, symbol),
              formatMoney(month.budgetedMinor, month.currency, symbol),
            )}
          </p>
        </>
      )}
    </section>
  );
}
