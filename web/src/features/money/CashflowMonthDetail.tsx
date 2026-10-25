// How one month's Money out adds up: Spent from the Transactions page, minus
// each yearly or quarterly bill paid this month, plus its share for each month
// it covers, plus loan repayments. The server sends the lines in display
// order and has already checked that they add up to Money out, so this panel
// only displays them. When the server says the sum cannot be shown, or sends
// a line this build cannot describe, one sentence replaces the whole sum:
// never a partial one that would not add up.
import { Link } from "@tanstack/react-router";
import { cadenceWord, cashflowCopy, dateLabel, fullMonthName } from "./cashflowCopy";
import { monthLabel } from "./month";
import type { CashflowMonth, CashflowReconciliation, CashflowSumLine } from "./cashflowSchemas";

type Props = {
  month: CashflowMonth;
  formatAmount: (minor: number) => string;
  onClose: () => void;
};

type SumRow = { label: string; note: string; amount: string };

// Signs sit on the amount only. formatAmount already prints a minus; a plus
// is added here because it never prints one.
function signedAmount(minor: number, formatAmount: (minor: number) => string): string {
  return minor > 0 ? `+${formatAmount(minor)}` : formatAmount(minor);
}

// Null when the line cannot be described, which hides the whole sum.
function describeLine(
  line: CashflowSumLine,
  monthKey: string,
  monthName: string,
  formatAmount: (minor: number) => string,
): SumRow | null {
  const amount = signedAmount(line.amountMinor, formatAmount);
  switch (line.kind) {
    case "bill_in_full": {
      const cadence = cadenceWord(line.spread);
      if (cadence === null) return null;
      return {
        label: cashflowCopy.sum.wholeBill(line.name, cadence),
        note: cashflowCopy.sum.wholeBillNote(dateLabel(line.paidOn), line.spread),
        amount,
      };
    }
    case "bill_share": {
      const cadence = cadenceWord(line.spread);
      if (cadence === null) return null;
      const paidThisMonth = line.paidOn.slice(0, 7) === monthKey;
      return {
        label: cashflowCopy.sum.share(line.name, monthName),
        note: paidThisMonth
          ? cashflowCopy.sum.shareNote(line.shareIndex, line.spread)
          : cashflowCopy.sum.shareNoteEarlier(line.shareIndex, line.spread, cadence, dateLabel(line.paidOn)),
        amount,
      };
    }
    case "loan":
      return {
        label: cashflowCopy.sum.loan(line.name),
        note: cashflowCopy.sum.loanNote(dateLabel(line.paidOn)),
        amount,
      };
    default:
      return null;
  }
}

function sumRows(
  month: CashflowMonth,
  reconciliation: CashflowReconciliation,
  formatAmount: (minor: number) => string,
): SumRow[] | null {
  const monthName = fullMonthName(month.month);
  const rows: SumRow[] = [
    {
      label: cashflowCopy.sum.spent(monthName),
      note: cashflowCopy.sum.spentNote,
      amount: formatAmount(reconciliation.spentMinor),
    },
  ];
  for (const line of reconciliation.lines) {
    const row = describeLine(line, month.month, monthName, formatAmount);
    if (row === null) return null;
    rows.push(row);
  }
  return rows;
}

// The one sentence shown instead of a sum that cannot be trusted.
function hiddenSentence(month: CashflowMonth, reconciliation: CashflowReconciliation): string {
  const monthName = fullMonthName(month.month);
  const { unratedCount, unratedEarlierBill, unratedCurrencies } = reconciliation;
  if (reconciliation.status === "no_rate") {
    if (unratedCount > 0 && unratedEarlierBill) {
      return cashflowCopy.sum.noRateBoth(monthName, unratedCount, unratedCurrencies);
    }
    if (unratedCount > 0) return cashflowCopy.sum.noRateHere(monthName, unratedCount, unratedCurrencies);
    if (unratedEarlierBill) return cashflowCopy.sum.noRateEarlier(monthName, unratedCurrencies);
  }
  return cashflowCopy.sum.mismatch(monthName);
}

function SumList({ rows, month, formatAmount }: { rows: SumRow[]; month: CashflowMonth; formatAmount: Props["formatAmount"] }) {
  const total = month.status === "current" ? cashflowCopy.sum.totalSoFar : cashflowCopy.sum.total;
  return (
    <ul className="flex max-w-[480px] flex-col gap-2">
      {rows.map((row, index) => (
        <li key={index} className="flex items-baseline justify-between gap-3 text-[13px]">
          <span className="min-w-0">
            <span data-part="label" className="text-ink">{row.label}</span>
            <span className="block text-[11.5px] text-muted">{row.note}</span>
          </span>
          <span data-part="amount" className="tabular shrink-0 text-ink">{row.amount}</span>
        </li>
      ))}
      <li className="flex items-baseline justify-between gap-3 border-t border-hairline pt-2 text-[14px] font-semibold">
        <span data-part="label" className="text-ink">{total}</span>
        <span data-part="amount" className="tabular shrink-0 text-ink">{formatAmount(month.outMinor)}</span>
      </li>
    </ul>
  );
}

export function CashflowMonthDetail({ month, formatAmount, onClose }: Props) {
  const monthName = fullMonthName(month.month);
  const reconciliation = month.reconciliation;
  const rows =
    reconciliation !== null && reconciliation.status === "shown"
      ? sumRows(month, reconciliation, formatAmount)
      : null;

  return (
    <div data-testid="cashflow-month-detail" className="flex flex-col gap-3 py-2">
      <div className="flex items-center justify-between gap-3">
        <h3 className="text-[13px] font-semibold text-ink">
          {reconciliation === null ? monthLabel(month.month) : cashflowCopy.sum.heading(monthName)}
        </h3>
        <button
          type="button"
          onClick={onClose}
          className="min-h-11 text-xs font-semibold text-accent lg:min-h-0"
        >
          {cashflowCopy.detailsClose}
        </button>
      </div>
      {reconciliation !== null &&
        (rows === null ? (
          <p className="text-[13px] text-ink">{hiddenSentence(month, reconciliation)}</p>
        ) : (
          <>
            <SumList rows={rows} month={month} formatAmount={formatAmount} />
            {reconciliation.lines.length === 0 && (
              <p className="text-[13px] text-muted">{cashflowCopy.detailsNone}</p>
            )}
          </>
        ))}
      <Link
        to="/money/transactions"
        search={{ month: month.month }}
        className="inline-flex min-h-11 items-center text-xs font-semibold text-accent lg:min-h-0"
      >
        {cashflowCopy.sum.link(monthName, month.transactionCount)}
      </Link>
    </div>
  );
}
