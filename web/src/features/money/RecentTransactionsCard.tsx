// The Finances-page preview of the ledger the accounts spec deferred (its own
// comment: "It has no data until Transactions ships, and the design draws no
// empty state for it"). Transactions ships the data; this is the strip.
//
// Reads through useTransactions({ month: "" }), which is the wire request
// `month=all`. This card is a *recency* strip -- the five newest rows in the
// ledger -- and recency is not a month: a month-scoped request would empty
// this card on the first of every month for a household with years of
// history, which is the shape of the very defect the month contract fixed on
// the Transactions screen.
//
// It used to pass `{}` and share one cache entry with TransactionsPage's own
// default request. That stopped being the same question the day
// parseTransactionFilter gave a month-less request a meaning: an absent month
// now means the current month, for the list and the summary alike. The two
// screens therefore hold two cache entries now, deliberately -- five rows
// against a second key is the cheaper of the two wrongs.
import { Link } from "@tanstack/react-router";
import { useCurrencies } from "../auth/useAuth";
import { FINANCES_COPY } from "./copy";
import { formatMoney } from "./formatMoney";
import { TRANSACTIONS_COPY } from "./transactionCopy";
import type { Transaction } from "./transactionSchemas";
import { useTransactions } from "./useTransactions";

// The design's strip is a preview, not the ledger -- "See all" is the way to
// the rest, so this never grows past a fixed handful of rows.
const VISIBLE_ROWS = 5;

// occurredOn arrives "YYYY-MM-DD" (transaction_handlers.go). Split apart and
// rebuilt through Date's (year, monthIndex, day) constructor rather than
// `new Date(string)`, which parses a date-only ISO string as UTC midnight and
// can read back a day early once toLocaleDateString applies the runtime's
// local timezone at a negative UTC offset -- the same reasoning as
// TransactionsPage's own formatDateHeading, kept as a separate one-line copy
// here rather than imported: TransactionsPage doesn't export it, and this
// card doesn't need that function's "Today ·" special case, only a bare date.
function formatRowDate(occurredOn: string): string {
  const [year, month, day] = occurredOn.split("-").map(Number);
  return new Date(year, month - 1, day).toLocaleDateString("en-US", {
    month: "short",
    day: "numeric",
  });
}

// One account name per row, not TransactionsPage's own subtitleParts (which
// shows both sides of a transfer plus who paid): the strip's line is
// "date · category · account", three parts wide, and the source account --
// where the money actually moved from -- is what the design draws there. An
// income transaction has no fromAccount at all, so it falls back to the
// destination.
function accountNameFor(transaction: Transaction): string {
  return transaction.fromAccountName ?? transaction.toAccountName ?? "—";
}

function AmountText({
  transaction,
  symbolFor,
}: {
  transaction: Transaction;
  symbolFor: (currency: string) => string | undefined;
}) {
  // Income is the only kind that grows the household's money from this row's
  // own perspective; expense and transfer both show what left the source
  // account. TransactionsPage's own AmountCell draws a transfer's destination
  // figure too (received vs sent can differ by a fee or a conversion), but
  // this strip has one figure's worth of room, so it shows the side every
  // kind actually has: what moved out of `amount`.
  const signedMinor =
    transaction.kind === "income" ? transaction.amount.amountMinor : -transaction.amount.amountMinor;
  return (
    <span
      className={`tabular whitespace-nowrap text-[13px] font-semibold ${
        transaction.kind === "income" ? "text-accent" : "text-ink"
      }`}
    >
      {formatMoney(
        signedMinor,
        transaction.amount.currency,
        symbolFor(transaction.amount.currency),
      )}
    </span>
  );
}

export function RecentTransactionsCard() {
  const transactionsQuery = useTransactions({ month: "" });
  const currencies = useCurrencies();
  const symbolFor = (currency: string) =>
    currencies.data?.currencies.find((c) => c.code === currency)?.symbol;

  // No loading spinner or error banner of its own: FinancesPage's own
  // accounts query already gates the page's loading/error states before this
  // card ever mounts, and a second, differently-timed message on the same
  // screen for a query nobody asked about explicitly would be confusing
  // rather than informative. Folding quietly to nothing lets the rest of the
  // page stay usable while this one card's data is still in flight.
  if (transactionsQuery.isPending || transactionsQuery.isError) {
    return null;
  }

  // Trusts the server's own order rather than re-sorting -- keyset
  // pagination walks the ledger newest-first (TransactionsPage's own
  // groupByDay comment), so the first five rows of the all-months first page
  // are already the five newest.
  const rows = transactionsQuery.data.transactions.slice(0, VISIBLE_ROWS);

  return (
    <section
      aria-labelledby="recent-transactions-heading"
      className="flex flex-col rounded-xl border border-hairline bg-card p-[22px]"
    >
      <div className="flex items-center justify-between">
        <h2 id="recent-transactions-heading" className="text-xs text-muted">
          {FINANCES_COPY.recentTransactions}
        </h2>
        {/* inline-flex items-center min-h-11 sm:min-h-0: a react-router
            Link renders an <a>, inline by default and (unlike a <button>)
            never centers its own content -- TransactionFilters.tsx's own
            SELECT_CLASS comment has the measured reason this size of
            control falls short of the 44px floor on a phone. */}
        <Link
          to="/money/transactions"
          className="inline-flex min-h-11 items-center text-xs font-semibold text-accent sm:min-h-0"
        >
          {FINANCES_COPY.seeAll}
        </Link>
      </div>

      {rows.length === 0 ? (
        <p className="mt-3 text-[13px] text-muted">{TRANSACTIONS_COPY.emptyTitle}</p>
      ) : (
        <ul className="mt-2 flex flex-col">
          {rows.map((transaction) => (
            <li
              key={transaction.id}
              data-testid="recent-transaction-row"
              className="flex items-center justify-between gap-3 border-b border-hairline py-3 last:border-b-0"
            >
              <div>
                <div className="text-[13px] font-semibold text-ink">{transaction.description}</div>
                <div className="text-[11px] text-muted">
                  {[
                    formatRowDate(transaction.occurredOn),
                    transaction.categoryName ?? TRANSACTIONS_COPY.noCategory,
                    accountNameFor(transaction),
                  ].join(" · ")}
                </div>
              </div>
              <AmountText transaction={transaction} symbolFor={symbolFor} />
            </li>
          ))}
        </ul>
      )}
    </section>
  );
}
