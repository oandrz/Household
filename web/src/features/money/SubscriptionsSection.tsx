// The "Of which subscriptions" section at the foot of RecurringBillsCard.
// A subscription is a bill with a box ticked, so this total is a part of the
// recurring total above it, never an addition to it. Pure presentation:
// monthlyMinor/annualMinor are the server's subscriptionsMonthlyMinor/
// subscriptionsAnnualMinor, formatted here and never re-summed from `bills`,
// because a second client-side sum is only a second chance to disagree.
//
// `bills` is BillsPage's full `data.bills`, archived rows included once
// "Show archived" is on. This section filters it itself, so every row it
// shows is a bill its own two totals include (the server never counts an
// archived bill).
import { BILL_COPY, CADENCE_LABELS } from "./billCopy";
import { formatMoney } from "./formatMoney";
import type { Bill } from "./billSchemas";

type SymbolFor = (currency: string) => string | undefined;

export function SubscriptionsSection({
  bills,
  currency,
  symbolFor,
  monthlyMinor,
  annualMinor,
}: {
  bills: Bill[];
  // The household's primary currency -- what monthlyMinor/annualMinor are
  // already converted into server-side.
  currency: string;
  symbolFor: SymbolFor;
  monthlyMinor: number;
  annualMinor: number;
}) {
  // Keeps the server's order; this section filters, it never re-sorts.
  //
  // A one-off is excluded even when ticked: it is not a recurring cost and
  // never reaches monthlyMinor/annualMinor (domain.AnnualEquivalentMinor),
  // so a row for it would show a figure neither total moves for.
  const subscriptions = bills.filter(
    (bill) => bill.archivedAt === null && bill.isSubscription && bill.cadence !== "one_off",
  );

  if (subscriptions.length === 0) {
    // Two reasons land here and need different sentences: nothing is ticked,
    // or something is ticked that the filter excludes (a one-off, or an
    // archived bill). "Nothing is marked yet" would be false in the second
    // case, so `anyTicked` looks at every bill handed in, not the filtered
    // list.
    const anyTicked = bills.some((bill) => bill.isSubscription);
    return (
      <section data-testid="subscriptions-section" className="mt-5 border-t border-hairline pt-4">
        <h3 data-testid="subscriptions-heading" className="text-[13px] font-semibold text-ink">
          {BILL_COPY.subscriptionsEmptyHeading}
        </h3>
        <p data-testid="subscriptions-empty" className="mt-2 text-[12.5px] leading-relaxed text-muted">
          {anyTicked ? BILL_COPY.subscriptionsEmptyExcludedBody : BILL_COPY.subscriptionsEmptyBody}
        </p>
      </section>
    );
  }

  const monthlyLabel = formatMoney(monthlyMinor, currency, symbolFor(currency));
  const annualLabel = formatMoney(annualMinor, currency, symbolFor(currency));

  return (
    <section data-testid="subscriptions-section" className="mt-5 border-t border-hairline pt-4">
      <h3 data-testid="subscriptions-heading" className="text-[13px] font-semibold text-ink">
        {BILL_COPY.subscriptionsHeading(monthlyLabel)}
      </h3>

      <div className="mt-3 flex flex-col gap-3">
        {subscriptions.map((bill) => (
          <div key={bill.id} data-testid="subscription-row" className="flex items-center justify-between gap-3">
            <div>
              <div className="text-[13px] text-ink">{bill.name}</div>
              {/* Only a cadence that is not monthly is labelled: the
                  heading's "/mo" already says monthly. */}
              {bill.cadence !== "monthly" && (
                <div className="text-[11px] text-muted">{CADENCE_LABELS[bill.cadence]}</div>
              )}
            </div>
            <span className="tabular text-[13px] font-semibold text-ink">
              {formatMoney(bill.amountMinor, bill.currency, symbolFor(bill.currency))}
            </span>
          </div>
        ))}
      </div>

      <div data-testid="subscriptions-annual" className="mt-3.5 border-t border-hairline pt-3 text-[12px] text-muted">
        {BILL_COPY.subscriptionsAnnualLine(annualLabel)}
      </div>
      {/* A row shows what is actually charged (a quarterly bill's S$120)
          while the totals are monthly equivalents; without this sentence the
          two kinds of figure cannot be told apart. No "last reviewed" date:
          nothing in the product can set one. */}
      <p data-testid="subscriptions-equivalent-note" className="mt-1.5 text-[11px] text-muted">
        {BILL_COPY.subscriptionsEquivalentNote}
      </p>
    </section>
  );
}
