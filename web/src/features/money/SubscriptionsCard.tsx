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
// This card does its own archived/isSubscription/one-off filter internally
// (see the filter's own comment below for the one-off half), GoalsPage.tsx/
// MonthlyContributionsCard.tsx's own precedent
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
  //
  // A one-off is excluded even when ticked -- BillModal's own checkbox
  // never refuses one, but domain.AnnualEquivalentMinor's own comment is
  // categorical: a one-off "is not a recurring cost" and contributes to
  // monthlyMinor/annualMinor NEVER, ticked or not. Rendering its row anyway
  // would show a figure that visibly never moves either total above it,
  // while isSubscriptionHelp's own copy ("Included in the household's
  // subscription totals") promises the opposite. Filtering it out here
  // keeps that promise true by construction: every row on this panel is a
  // bill this panel's own two totals actually include.
  const subscriptions = bills.filter(
    (bill) => bill.archivedAt === null && bill.isSubscription && bill.cadence !== "one_off",
  );

  if (subscriptions.length === 0) {
    // Two different reasons land here, and they read very differently to a
    // household: nobody has ticked anything yet, or somebody ticked a bill
    // that this filter excludes anyway (a one-off, or one since archived).
    // `subscriptionsEmptyBody` ("No bills are marked as subscriptions yet")
    // is false in the second case -- a review round caught the first cut of
    // this panel showing that same sentence to a household that HAD just
    // ticked "Counts as a subscription" on a one-off, telling them they had
    // not done the thing they had just done. `anyTicked` checks every bill
    // handed in, not `subscriptions` (which is empty either way here), so it
    // sees a ticked-but-excluded bill even though the filter above already
    // dropped it.
    const anyTicked = bills.some((bill) => bill.isSubscription);
    return (
      <div data-testid="subscriptions-card" className="rounded-xl border border-hairline bg-card p-[22px]">
        <h2 data-testid="subscriptions-heading" className="text-[14px] font-semibold text-ink">
          {BILL_COPY.subscriptionsTitle}
        </h2>
        <p data-testid="subscriptions-empty" className="mt-3 text-[12.5px] leading-relaxed text-muted">
          {anyTicked ? BILL_COPY.subscriptionsEmptyExcludedBody : BILL_COPY.subscriptionsEmptyBody}
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
