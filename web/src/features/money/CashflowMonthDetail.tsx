// What one month carried that the ledger does not show as a plain expense:
// the yearly/quarterly bill shares spread into it and its loan repayments.
// These are the only two allowed differences from the Transactions page, so
// this panel is where a person reconciles the two.
import { Link } from "@tanstack/react-router";
import { cashflowCopy, dateLabel } from "./cashflowCopy";
import { monthLabel } from "./month";
import type { CashflowMonth } from "./cashflowSchemas";

type Props = {
  month: CashflowMonth;
  formatAmount: (minor: number) => string;
  onClose: () => void;
};

export function CashflowMonthDetail({ month, formatAmount, onClose }: Props) {
  return (
    <div data-testid="cashflow-month-detail" className="flex flex-col gap-3 py-2">
      <div className="flex items-center justify-between gap-3">
        <h3 className="text-[13px] font-semibold text-ink">{monthLabel(month.month)}</h3>
        <button
          type="button"
          onClick={onClose}
          className="min-h-11 text-xs font-semibold text-accent lg:min-h-0"
        >
          {cashflowCopy.detailsClose}
        </button>
      </div>
      {month.contributions.length === 0 ? (
        <p className="text-[13px] text-muted">{cashflowCopy.detailsNone}</p>
      ) : (
        <ul className="flex flex-col gap-2">
          {month.contributions.map((contribution, index) => (
            <li key={`${contribution.name}-${contribution.paidOn}-${index}`} className="flex items-baseline justify-between gap-3 text-[13px]">
              <span className="min-w-0">
                <span className="text-ink">{contribution.name}</span>
                <span className="block text-[11.5px] text-muted">
                  {contribution.kind === "loan" ? cashflowCopy.detailsLoan : cashflowCopy.detailsSpread},{" "}
                  {cashflowCopy.detailsPaidOn(dateLabel(contribution.paidOn))}
                </span>
              </span>
              <span className="shrink-0 text-ink">{formatAmount(contribution.amountMinor)}</span>
            </li>
          ))}
        </ul>
      )}
      <p className="text-[12px] text-muted">{cashflowCopy.transactionCount(month.transactionCount)}</p>
      <Link
        to="/money/transactions"
        search={{ month: month.month }}
        className="inline-flex min-h-11 items-center text-xs font-semibold text-accent lg:min-h-0"
      >
        {cashflowCopy.detailsLedger}
      </Link>
    </div>
  );
}
