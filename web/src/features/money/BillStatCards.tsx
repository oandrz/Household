// The design's three-card row at the top of the populated Bills screen.
// Pure presentation, BudgetStatCards.tsx's own shape: every figure arrives
// already computed (BillsSummary), and this file's only job is formatMoney
// plus which cards to show.
//
// Only two of the three figures take an amount at all. The Next-due card
// never shows one -- the design's own mockup ("Jul 24 · Tax GIRO") draws
// none either, and billsSummarySchema's own comment says why the figure
// that exists for it (nextDue.amountMinor/currency) is reserved for
// Overview's NextBillCard (Task 16): it is the BILL's own currency, not
// converted, so pairing it with this card's household-primary symbol would
// print a wrong-currency amount next to the right-currency symbol.
import { BILL_COPY, monthDayLabel } from "./billCopy";
import { formatMoney } from "./formatMoney";

function StatCard({
  testId,
  label,
  value,
  subtitle,
  valueClassName,
}: {
  testId: string;
  label: string;
  value: string;
  subtitle?: string;
  valueClassName?: string;
}) {
  return (
    <div data-testid={testId} className="rounded-xl border border-hairline bg-card px-[22px] py-[18px]">
      <div className="text-xs text-muted">{label}</div>
      <div className={`mt-1.5 text-2xl font-semibold tracking-[-0.02em] ${valueClassName ?? "text-ink"}`}>
        {value}
        {subtitle && <span className="ml-1.5 text-[13px] font-normal text-muted">{subtitle}</span>}
      </div>
    </div>
  );
}

export function BillStatCards({
  currency,
  symbol,
  dueThisMonthMinor,
  paidSoFarMinor,
  nextDue,
}: {
  currency: string;
  symbol?: string;
  dueThisMonthMinor: number;
  paidSoFarMinor: number;
  // null omits the whole card -- the formulas table's own rule for Next due
  // ("None -> the card and the stat are omitted, never rendered as a zero"),
  // the same convention BudgetPage.tsx's dailyPaceOk gate follows for its
  // own fourth card.
  nextDue: { billName: string; dueOn: string; overdue: boolean } | null;
}) {
  return (
    <div className="grid grid-cols-1 gap-4 sm:grid-cols-3">
      <StatCard
        testId="bills-stat-due-this-month"
        label={BILL_COPY.statDueThisMonth}
        value={formatMoney(dueThisMonthMinor, currency, symbol)}
        valueClassName="text-ink tabular"
        // State 2's own contract: a zero here explains itself rather than
        // sitting as a bare "S$0.00" -- not gated to any named "state," just
        // to the figure actually being zero, the same as the card beside it.
        subtitle={dueThisMonthMinor === 0 ? BILL_COPY.dueThisMonthZero : undefined}
      />
      <StatCard
        testId="bills-stat-paid-so-far"
        label={BILL_COPY.statPaidSoFar}
        value={formatMoney(paidSoFarMinor, currency, symbol)}
        valueClassName="text-accent tabular"
        subtitle={paidSoFarMinor === 0 ? BILL_COPY.paidSoFarZero : undefined}
      />
      {nextDue && (
        <StatCard
          testId="bills-stat-next-due"
          label={BILL_COPY.statNextDue}
          // Overdue replaces the date value outright, rather than sitting
          // beside it -- state 5's own contract: "names it as overdue rather
          // than printing a past date as though upcoming."
          value={nextDue.overdue ? BILL_COPY.nextDueOverdueValue : monthDayLabel(nextDue.dueOn)}
          valueClassName={nextDue.overdue ? "text-danger" : "text-ink"}
          subtitle={BILL_COPY.nextDueBillNameClause(nextDue.billName)}
        />
      )}
    </div>
  );
}
