// The Recurring bills card on /money/bills: what the household's repeating
// bills cost a month and a year, broken down by category, with the
// subscriptions as a section at its foot. It exists so a household can see
// whether it has cut its fixed costs: archiving a cancelled bill makes the
// headline drop.
//
// Pure presentation. Every figure is the server's (BillsSummary): the
// server converts currencies and makes the rows add up to the headline, so
// nothing here sums. The breakdown shows every category at once rather than
// filtering the page, so the stat cards and the bill list never disagree
// with it.
import { BILL_COPY } from "./billCopy";
import { formatMoney } from "./formatMoney";
import type { Bill, BillsSummary } from "./billSchemas";
import { SubscriptionsSection } from "./SubscriptionsSection";

type SymbolFor = (currency: string) => string | undefined;

export function RecurringBillsCard({
  bills,
  summary,
  symbolFor,
}: {
  bills: Bill[];
  summary: BillsSummary;
  symbolFor: SymbolFor;
}) {
  const { currency } = summary;
  const money = (minor: number) => formatMoney(minor, currency, symbolFor(currency));
  const rows = summary.recurringByCategory;

  if (rows.length === 0) {
    return (
      <div data-testid="recurring-bills-card" className="rounded-xl border border-hairline bg-card p-[22px]">
        <h2 data-testid="recurring-heading" className="text-[14px] font-semibold text-ink">
          {BILL_COPY.recurringTitle}
        </h2>
        <p data-testid="recurring-empty" className="mt-3 text-[12.5px] leading-relaxed text-muted">
          {BILL_COPY.recurringEmptyBody}
        </p>
      </div>
    );
  }

  return (
    <div data-testid="recurring-bills-card" className="rounded-xl border border-hairline bg-card p-[22px]">
      <h2 data-testid="recurring-heading" className="text-[14px] font-semibold text-ink">
        {BILL_COPY.recurringTitle}
      </h2>
      {/* The total is the figure this card exists for, so it takes the stat
          cards' figure size rather than sitting inside the heading. */}
      <div data-testid="recurring-monthly" className="mt-1.5 text-2xl font-semibold tracking-[-0.02em] text-ink">
        {money(summary.recurringMonthlyMinor)}
        <span className="ml-1 text-[13px] font-normal tracking-normal text-muted">{BILL_COPY.perMonthSuffix}</span>
      </div>
      <div data-testid="recurring-annual" className="mt-0.5 text-[12px] text-muted">
        {BILL_COPY.recurringAnnualLine(money(summary.recurringAnnualMinor))}
      </div>

      <div className="mt-4 flex flex-col gap-3">
        {rows.map((row) => (
          <div
            key={row.categoryId || "none"}
            data-testid="recurring-row"
            className="flex items-start justify-between gap-3"
          >
            <div className="min-w-0">
              <div className="text-[13px] text-ink">{row.categoryId ? row.categoryName : BILL_COPY.recurringNoCategory}</div>
              {row.excludedNoRate > 0 && (
                <div className="text-[12px] text-muted">{BILL_COPY.recurringRowExcluded(row.excludedNoRate)}</div>
              )}
            </div>
            {/* A category whose every bill has no rate has no figure at all;
                "S$0.00" would read as "costs nothing". The dash is hidden from
                screen readers because the note beside it already says why. */}
            {row.annualMinor === 0 && row.excludedNoRate > 0 ? (
              <span aria-hidden="true" className="text-[13px] text-muted">
                —
              </span>
            ) : (
              <span className="tabular text-[13px] font-semibold text-ink">{money(row.monthlyMinor)}</span>
            )}
          </div>
        ))}
      </div>

      <p data-testid="recurring-equivalent-note" className="mt-3 text-[12px] text-muted">
        {BILL_COPY.recurringEquivalentNote}
      </p>

      <SubscriptionsSection
        bills={bills}
        currency={currency}
        symbolFor={symbolFor}
        monthlyMinor={summary.subscriptionsMonthlyMinor}
        annualMinor={summary.subscriptionsAnnualMinor}
      />
    </div>
  );
}
