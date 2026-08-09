// The Subscriptions panel on /money/bills, beside the Due soon/Later/Paid
// lists on BillsPage's own two-column grid -- the design's "Subscriptions"
// card. Pure presentation over already-computed figures, MonthlyContributionsCard.tsx's
// own convention: monthlyMinor/annualMinor are BillsSummary's own
// subscriptionsMonthlyMinor/subscriptionsAnnualMinor, formatted here and
// never re-summed from `bills` -- bill.go's own comment on why: the rollup
// is integer-first with exactly one division, done once, server-side; a
// second client-side sum of the same bills would only ever be a second
// chance for the two figures to disagree.
//
// `bills` is BillsPage's own full `data.bills` -- the union that includes
// archived rows once "Show archived" is on -- not a pre-filtered list.
// This card does its own `archivedAt === null && isSubscription` filter
// internally, GoalsPage.tsx/MonthlyContributionsCard.tsx's own precedent
// for the identical shape (goals filtered live inside the card, not by the
// page before the prop is passed): the server's own subscriptionsMonthlyMinor/
// subscriptionsAnnualMinor already exclude an archived bill (usecase/bill.go's
// own `if b.IsArchived() { continue }`, before the subscription rollup ever
// runs), so a row rendered for one here would show a bill this card's own
// two totals do not include.
import { BILL_COPY, CADENCE_LABELS } from "./billCopy";
import { formatMoney } from "./formatMoney";
import type { Bill } from "./billSchemas";

type SymbolFor = (currency: string) => string | undefined;

export function SubscriptionsCard({
  bills,
  currency,
  symbolFor,
  monthlyMinor,
  annualMinor,
}: {
  bills: Bill[];
  // The household's primary currency -- what monthlyMinor/annualMinor are
  // already converted into server-side (billsSummarySchema's own comment).
  currency: string;
  symbolFor: SymbolFor;
  monthlyMinor: number;
  annualMinor: number;
}) {
  // Preserves the server's own order (next_due ascending, nulls last, ties
  // by name -- bill.go's own sort) -- BillsPage.tsx's own rule for
  // dueSoonBills/laterBills, restated here: this card filters, it never
  // re-sorts.
  const subscriptions = bills.filter((bill) => bill.archivedAt === null && bill.isSubscription);

  if (subscriptions.length === 0) {
    return (
      <div data-testid="subscriptions-card" className="rounded-xl border border-hairline bg-card p-[22px]">
        <h2 data-testid="subscriptions-heading" className="text-[14px] font-semibold text-ink">
          {BILL_COPY.subscriptionsTitle}
        </h2>
        <p data-testid="subscriptions-empty" className="mt-3 text-[12.5px] leading-relaxed text-muted">
          {BILL_COPY.subscriptionsEmptyBody}
        </p>
      </div>
    );
  }

  const monthlyLabel = formatMoney(monthlyMinor, currency, symbolFor(currency));
  const annualLabel = formatMoney(annualMinor, currency, symbolFor(currency));

  return (
    <div data-testid="subscriptions-card" className="rounded-xl border border-hairline bg-card p-[22px]">
      <h2 data-testid="subscriptions-heading" className="text-[14px] font-semibold text-ink">
        {BILL_COPY.subscriptionsHeading(monthlyLabel)}
      </h2>

      <div className="mt-4 flex flex-col gap-3">
        {subscriptions.map((bill) => (
          <div key={bill.id} data-testid="subscription-row" className="flex items-center justify-between gap-3">
            <div>
              <div className="text-[13px] text-ink">{bill.name}</div>
              {/* Cadence shown only when it is NOT monthly -- the heading's
                  own "/mo" already tells a household these totals are
                  monthly, so a monthly row repeating "Monthly" would be
                  the one case with nothing to disambiguate (task brief's
                  own point 2: the row's job is telling apart a cadence
                  that is NOT what the heading already implies). */}
              {bill.cadence !== "monthly" && (
                <div className="text-[11px] text-muted">{CADENCE_LABELS[bill.cadence]}</div>
              )}
            </div>
            <span className="text-[13px] font-semibold text-ink">
              {formatMoney(bill.amountMinor, bill.currency, symbolFor(bill.currency))}
            </span>
          </div>
        ))}
      </div>

      <div data-testid="subscriptions-annual" className="mt-3.5 border-t border-hairline pt-3 text-[12px] text-muted">
        {BILL_COPY.subscriptionsAnnualLine(annualLabel)}
      </div>
      {/* Point 2's second half: without this sentence a household has no
          way to tell a row's own charged amount (a quarterly bill's S$120)
          apart from the heading/this line's monthly-equivalent totals.
          Deliberately no "last reviewed" date beside it -- the design draws
          one, but nothing in this product can ever set it (point 3), so
          printing one would be a figure no household could make true. */}
      <p data-testid="subscriptions-equivalent-note" className="mt-1.5 text-[11px] text-muted">
        {BILL_COPY.subscriptionsEquivalentNote}
      </p>
    </div>
  );
}
