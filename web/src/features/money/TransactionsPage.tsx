// The Transactions page (design's "All transactions" screen). Follows
// FinancesPage.tsx's own shape: a loading/error gate, then the five screen
// states from the design's section 7.1 -- first run, no matches, an
// excluded-no-rate line, a marked pre-opening row, and a disabled Add button
// when there are no accounts at all.
//
// The design's own subtitle also reads "auto-imported from linked accounts."
// Dropped, on purpose: nothing here is imported (Task 40's manual entry is
// the only way a transaction gets created), and copy describing a sync this
// product cannot do is a promise it cannot keep.
import { useState } from "react";
import { Link } from "@tanstack/react-router";
import { apiFetch, ApiError } from "../../api/client";
import { useCurrencies } from "../auth/useAuth";
import { useHouseholdMembers } from "../settings/useHouseholdMembers";
import { formatMoney } from "./formatMoney";
import { TransactionFilters, type TransactionFilterValues } from "./TransactionFilters";
import { TransactionModal, type TransactionFormValues } from "./TransactionModal";
import { TRANSACTIONS_COPY } from "./transactionCopy";
import { transactionsResponseSchema, type Transaction, type TransactionsResponse } from "./transactionSchemas";
import { useAccounts } from "./useAccounts";
import {
  toQueryString,
  useCategories,
  useCreateTransaction,
  useDeleteTransaction,
  useTransactions,
  useUpdateTransaction,
  type TransactionFilters as TransactionQueryFilters,
} from "./useTransactions";
import type { Account } from "./schemas";

// Owned here, not by TransactionFilters.tsx: that component holds no state of
// its own (see its own file comment), and a value export sitting alongside a
// component export there trips eslint's react-refresh/only-export-components
// rule, the same reason copy.ts/transactionCopy.ts are plain .ts modules.
const EMPTY_TRANSACTION_FILTERS: TransactionFilterValues = {
  kind: "",
  accountId: "",
  categoryId: "",
  paidBy: "",
  month: "",
};

// A one-off fetch for "Load older transactions", deliberately not routed
// through useTransactions/useQuery. That hook's query key includes `cursor`,
// so letting the page's *reactive* query advance its own cursor would mean
// each older page displaces the last shown page rather than adding to it --
// and every mutation's invalidateLedger (useTransactions.ts) would then only
// refetch whichever page happens to be "current," leaving earlier ones stale
// in a second, disconnected way. This reuses the exact URL-building and
// parsing the hook itself uses (toQueryString, transactionsResponseSchema),
// so the two paths cannot disagree about what the querystring or the
// response shape means -- only "keep this page pinned in local state instead
// of the query cache" is different.
async function fetchOlderPage(
  filters: TransactionQueryFilters,
  cursor: string,
): Promise<TransactionsResponse> {
  const body = await apiFetch<unknown>(
    `/api/v1/transactions${toQueryString({ ...filters, cursor })}`,
  );
  return transactionsResponseSchema.parse(body);
}

// summary.month arrives as "YYYY-MM" (transaction_handlers.go's monthLayout).
// Parsed onto day 2 of the month, not day 1 -- day 1 at a negative UTC offset
// can read back as the *previous* month once `new Date(year, month, day)`
// applies the runtime's local timezone, and day 2 has no such edge for any
// real-world offset. The same reasoning as AccountModal's own today().
function monthLabel(month: string): string {
  const [year, monthNum] = month.split("-").map(Number);
  return new Date(year, monthNum - 1, 2).toLocaleDateString("en-US", {
    month: "long",
    year: "numeric",
  });
}

function formatDateHeading(occurredOn: string): string {
  const [year, month, day] = occurredOn.split("-").map(Number);
  const date = new Date(year, month - 1, day);
  const label = date.toLocaleDateString("en-US", { month: "short", day: "numeric" });
  const now = new Date();
  const isToday =
    date.getFullYear() === now.getFullYear() &&
    date.getMonth() === now.getMonth() &&
    date.getDate() === now.getDate();
  return isToday ? `Today · ${label}` : label;
}

// Assumes the server's own order is already day-grouped (keyset pagination
// walks the ledger newest-first, so same-day rows are adjacent) -- this does
// not re-sort, only collapses adjacent same-date rows under one heading.
function groupByDay(rows: Transaction[]): { date: string; items: Transaction[] }[] {
  const groups: { date: string; items: Transaction[] }[] = [];
  for (const row of rows) {
    const current = groups[groups.length - 1];
    if (current && current.date === row.occurredOn) {
      current.items.push(row);
    } else {
      groups.push({ date: row.occurredOn, items: [row] });
    }
  }
  return groups;
}

// The PATCH route's fields are all pointers server-side
// (updateTransactionRequest, transaction_handlers.go): a field the JSON body
// omits, or sends as `null`, decodes to nil and means "leave this alone."
// TransactionFormValues models an unset category/person/account as `null`,
// which is exactly the value that would be silently read as "no change"
// instead of "clear it" if forwarded as-is -- so every nullable field here is
// translated to "" (the same empty-string sentinel the create route's own
// zero-value default already uses for "no category"/"no one paid") before it
// reaches the request.
//
// receivedAmountMinor can't use that trick: an amount's "empty" sentinel
// would be 0, which is also a real (if invalid) amount, so the API gives it
// its own boolean instead (TransactionUpdate.ClearReceivedAmount,
// transaction.go). clearReceivedAmount is derived here rather than left for
// the caller to guess, because "the new value is null" is ambiguous by
// itself -- true both when a transfer's fee was just cleared (which must
// delete the stored figure) and every time a non-transfer's form submits
// (which never had a figure to delete in the first place). Comparing against
// what the transaction had *before* this edit tells the two apart: if it
// never had a received amount, this evaluates to `false` and the omitted
// receivedAmountMinor is correctly read as "leave alone" -- a no-op, since
// there was nothing to leave alone; if it did, and the new value is null,
// this evaluates to `true` and actually removes it. That second case is
// reached by a transfer whose destination account just changed out of the
// currency pair that required the figure, or whose kind changed away from
// transfer entirely -- both leave the stored figure attached to a
// transaction that no longer has any honest use for it.
function toUpdateBody(values: TransactionFormValues, initial: Transaction) {
  const hadReceivedAmount = initial.receivedAmount !== null;
  return {
    kind: values.kind,
    occurredOn: values.occurredOn,
    description: values.description,
    categoryId: values.categoryId ?? "",
    paidByMembershipId: values.paidByMembershipId ?? "",
    fromAccountId: values.fromAccountId ?? "",
    toAccountId: values.toAccountId ?? "",
    amountMinor: values.amountMinor,
    ...(values.receivedAmountMinor !== null
      ? { receivedAmountMinor: values.receivedAmountMinor }
      : {}),
    clearReceivedAmount: hadReceivedAmount && values.receivedAmountMinor === null,
  };
}

function subtitleParts(t: Transaction): string[] {
  if (t.kind === "transfer") {
    return [t.fromAccountName ?? "—", "→", t.toAccountName ?? "—"];
  }
  if (t.kind === "income") {
    return [t.toAccountName ?? "—", t.categoryName ?? TRANSACTIONS_COPY.noCategory];
  }
  return [
    t.fromAccountName ?? "—",
    t.paidByName ?? "Unassigned",
    t.categoryName ?? TRANSACTIONS_COPY.noCategory,
  ];
}

function AmountCell({
  transaction,
  symbolFor,
}: {
  transaction: Transaction;
  symbolFor: (currency: string) => string | undefined;
}) {
  if (transaction.kind === "income") {
    return (
      <span className="whitespace-nowrap text-[13.5px] font-semibold text-accent">
        +{formatMoney(transaction.amount.amountMinor, transaction.amount.currency, symbolFor(transaction.amount.currency))}
      </span>
    );
  }
  if (transaction.kind === "transfer") {
    // A transfer shows both sides -- what left the source account and what
    // the destination actually received -- because the two can differ (a
    // cross-currency conversion, a bank fee), and showing only one would be
    // half the transaction.
    const received = transaction.receivedAmount ?? transaction.amount;
    return (
      <span className="whitespace-nowrap text-[13.5px] font-semibold text-ink">
        {formatMoney(-transaction.amount.amountMinor, transaction.amount.currency, symbolFor(transaction.amount.currency))}
        {" → "}
        <span className="text-accent">
          +{formatMoney(received.amountMinor, received.currency, symbolFor(received.currency))}
        </span>
      </span>
    );
  }
  return (
    <span className="whitespace-nowrap text-[13.5px] font-semibold text-ink">
      {formatMoney(-transaction.amount.amountMinor, transaction.amount.currency, symbolFor(transaction.amount.currency))}
    </span>
  );
}

function TransactionRow({
  transaction,
  symbolFor,
  onClick,
}: {
  transaction: Transaction;
  symbolFor: (currency: string) => string | undefined;
  onClick: () => void;
}) {
  return (
    <button
      type="button"
      onClick={onClick}
      className="flex w-full items-center justify-between gap-3 border-b border-hairline py-3 text-left last:border-b-0"
    >
      <div>
        <div className="text-[13.5px] font-semibold text-ink">{transaction.description}</div>
        <div className="text-[11.5px] text-muted">{subtitleParts(transaction).join(" · ")}</div>
        {/* Two independent markers, not one: a transfer can predate one
            account's opening balance and not the other's, and "does not
            change the balance" would otherwise be half true. */}
        {transaction.beforeFromAccountOpeningBalance && transaction.fromAccountName && (
          <div className="mt-0.5 text-[11px] text-muted">
            {TRANSACTIONS_COPY.beforeOpeningBalance(transaction.fromAccountName)}
          </div>
        )}
        {transaction.beforeToAccountOpeningBalance && transaction.toAccountName && (
          <div className="mt-0.5 text-[11px] text-muted">
            {TRANSACTIONS_COPY.beforeOpeningBalance(transaction.toAccountName)}
          </div>
        )}
      </div>
      <AmountCell transaction={transaction} symbolFor={symbolFor} />
    </button>
  );
}

export function TransactionsPage() {
  const [filters, setFilters] = useState<TransactionFilterValues>(EMPTY_TRANSACTION_FILTERS);
  const [addOpen, setAddOpen] = useState(false);
  const [editingTransaction, setEditingTransaction] = useState<Transaction | null>(null);

  // Rows loaded via "Load older transactions", beyond the page's own
  // reactive first page. See fetchOlderPage's own comment for why these live
  // outside the query cache.
  const [olderRows, setOlderRows] = useState<Transaction[]>([]);
  const [olderNextCursor, setOlderNextCursor] = useState<string | null>(null);
  const [loadingOlder, setLoadingOlder] = useState(false);

  const filterSignature = `${filters.kind}|${filters.accountId}|${filters.categoryId}|${filters.paidBy}|${filters.month}`;
  const [lastFilterSignature, setLastFilterSignature] = useState(filterSignature);
  // Computed during render, not an effect -- the same "derive until the
  // input changes" pattern TransactionModal's own currencyPairKey uses. Rows
  // loaded via "Load older" under the *old* filters must never survive to
  // sit glued to rows fetched under new ones.
  if (filterSignature !== lastFilterSignature) {
    setLastFilterSignature(filterSignature);
    setOlderRows([]);
    setOlderNextCursor(null);
  }

  const queryFilters: TransactionQueryFilters = {
    kind: filters.kind || undefined,
    accountId: filters.accountId || undefined,
    categoryId: filters.categoryId || undefined,
    paidBy: filters.paidBy || undefined,
    month: filters.month || undefined,
  };

  const transactionsQuery = useTransactions(queryFilters);
  const accountsQuery = useAccounts(false);
  const categoriesQuery = useCategories();
  const membersQuery = useHouseholdMembers();
  const currencies = useCurrencies();

  const createTransaction = useCreateTransaction();
  const updateTransaction = useUpdateTransaction();
  const deleteTransaction = useDeleteTransaction();

  // Two essential queries, checked separately (not one `||`'d condition) so
  // each one's own `.data` narrows to defined below without either needing a
  // fallback -- the same shape FinancesPage.tsx uses for its own single
  // query.
  if (transactionsQuery.isPending) {
    return <p className="p-9 text-xs text-muted">Loading…</p>;
  }
  if (accountsQuery.isPending) {
    return <p className="p-9 text-xs text-muted">Loading…</p>;
  }
  if (transactionsQuery.isError) {
    // GET /transactions is money AND owner-gated, identically to GET
    // /goals and GET /bills (router.go's own comment on the whole `txn`
    // group) -- a limited member holding money reaches this route (the
    // sidebar link and the /money route guard both check only the
    // capability, never the role) and the request answers 403. Branching
    // on the real status, not a second useMe() role check -- GoalsPage.tsx's
    // own comment on its identical branch: a role check here would be a
    // second source of truth that could disagree with what the server
    // actually decided. Found and fixed alongside the identical gap in
    // BillsPage.tsx and BudgetPage.tsx during Bills' Task 18 walk
    // (docs/LEARNING.md pattern 1: fixing one instance is not fixing the
    // class) -- this file's own error branch had never been given the
    // GoalsPage.tsx-style split either.
    const status = transactionsQuery.error instanceof ApiError ? transactionsQuery.error.status : undefined;
    if (status === 403) {
      return (
        <section data-testid="transactions-owner-only" className="m-9 rounded-xl border border-hairline bg-card p-[22px]">
          <h1 className="text-[23px] font-semibold tracking-[-0.02em] text-ink">{TRANSACTIONS_COPY.title}</h1>
          <h2 className="mt-4 text-xs text-muted">{TRANSACTIONS_COPY.ownerOnlyHeading}</h2>
          <p className="mt-1.5 text-[13px] text-ink">{TRANSACTIONS_COPY.ownerOnlyBody}</p>
        </section>
      );
    }
    return (
      <p role="alert" data-testid="transactions-load-error" className="p-9 text-xs text-danger">
        Couldn't load your transactions.
      </p>
    );
  }
  if (accountsQuery.isError) {
    return (
      <p role="alert" className="p-9 text-xs text-danger">
        Couldn't load your accounts.
      </p>
    );
  }

  const accounts: Account[] = accountsQuery.data.accounts;
  const categories = categoriesQuery.data ?? [];
  const members = (membersQuery.data ?? []).map((m) => ({ id: m.id, name: m.user.displayName }));
  const { summary } = transactionsQuery.data;
  const rows = [...transactionsQuery.data.transactions, ...olderRows];
  const nextCursor = olderRows.length > 0 ? olderNextCursor : transactionsQuery.data.nextCursor;
  const filtersActive = Object.values(filters).some((v) => v !== "");
  const symbolFor = (currency: string) =>
    currencies.data?.currencies.find((c) => c.code === currency)?.symbol;
  const excludedCurrencies = [...new Set(summary.excludedNoRate.map((e) => e.currency))].join(", ");

  async function handleLoadOlder() {
    if (nextCursor === null) return;
    setLoadingOlder(true);
    try {
      const page = await fetchOlderPage(queryFilters, nextCursor);
      setOlderRows((prev) => [...prev, ...page.transactions]);
      setOlderNextCursor(page.nextCursor);
    } finally {
      setLoadingOlder(false);
    }
  }

  // createTransactionRequest's fields match TransactionFormValues one for
  // one (TransactionModal's own comment), so the create path needs no
  // translation the way the edit path below does -- there is no "leave this
  // alone" state to accidentally collide with on a brand-new row.
  async function handleCreate(values: TransactionFormValues) {
    await createTransaction.mutateAsync(values);
  }

  async function handleUpdate(values: TransactionFormValues) {
    if (!editingTransaction) return;
    const id = editingTransaction.id;
    const updated = await updateTransaction.mutateAsync({
      id,
      body: toUpdateBody(values, editingTransaction),
    });
    // olderRows sits outside the query cache invalidateLedger refreshes (see
    // fetchOlderPage's own comment): a transaction edited while it happens to
    // be showing on an appended "Load older" page would otherwise keep
    // displaying its pre-edit description and amount, in place, until a full
    // reload -- the row a household just corrected still showing the old
    // figure. Patched directly with the server's own response (not
    // recomputed from `values`) so this can never disagree with what was
    // actually persisted.
    setOlderRows((prev) => prev.map((row) => (row.id === id ? updated : row)));
  }

  async function handleDelete() {
    if (!editingTransaction) return;
    const id = editingTransaction.id;
    await deleteTransaction.mutateAsync(id);
    // Same reasoning as handleUpdate above: a deleted row sitting in
    // olderRows is untouched by invalidateLedger's refetch of the reactive
    // first page, and would otherwise keep showing a transaction that no
    // longer exists.
    setOlderRows((prev) => prev.filter((row) => row.id !== id));
  }

  const groups = groupByDay(rows);

  return (
    <div className="flex flex-col gap-5 px-9 py-8">
      <div className="flex items-start justify-between gap-4">
        <div>
          <Link to="/money" className="mb-1 inline-block text-xs font-semibold text-accent">
            {TRANSACTIONS_COPY.backToFinances}
          </Link>
          <h1 className="text-[23px] font-semibold tracking-[-0.02em] text-ink">
            {TRANSACTIONS_COPY.title}
          </h1>
          <p className="mt-1 text-[13px] text-muted">
            {TRANSACTIONS_COPY.countInMonth(summary.count, monthLabel(summary.month))}
          </p>
        </div>
        <div className="flex flex-col items-end gap-1">
          <button
            type="button"
            disabled={accounts.length === 0}
            onClick={() => setAddOpen(true)}
            className="rounded-lg bg-accent px-3.5 py-2 text-[13px] font-semibold text-white disabled:cursor-not-allowed disabled:opacity-60"
          >
            {TRANSACTIONS_COPY.add}
          </button>
          {/* Disabled with an explanation, not a modal whose account dropdown
              is empty -- that is a dead end reached after four clicks. */}
          {accounts.length === 0 && (
            <p className="max-w-[220px] text-right text-[11px] text-muted">
              {TRANSACTIONS_COPY.noAccountsYet}
            </p>
          )}
        </div>
      </div>

      <div className="rounded-[10px] border border-callout-border bg-callout px-3.5 py-3 text-[12.5px] leading-relaxed text-accent">
        {TRANSACTIONS_COPY.categoriesFeedBudget}
      </div>

      <div className="flex flex-wrap items-end gap-3">
        <TransactionFilters
          values={filters}
          onChange={setFilters}
          accounts={accounts}
          categories={categories}
          members={members}
        />
        <div className="ml-auto pb-1.5 text-[12.5px] text-muted">
          {TRANSACTIONS_COPY.spentThisMonth}{" "}
          <b className="text-ink">
            {formatMoney(summary.spentMinor, summary.currency, symbolFor(summary.currency))}
          </b>
        </div>
      </div>

      {summary.excludedNoRate.length > 0 && (
        <p className="text-xs text-muted">
          {TRANSACTIONS_COPY.excludedNoRate(summary.excludedNoRate.length, excludedCurrencies)}
        </p>
      )}

      <div className="rounded-xl border border-hairline bg-card px-[22px] py-2">
        {rows.length === 0 ? (
          filtersActive ? (
            <div className="flex flex-col items-center gap-3 py-14 text-center">
              <p className="text-sm font-semibold text-ink">{TRANSACTIONS_COPY.noMatchesTitle}</p>
              <button
                type="button"
                onClick={() => setFilters(EMPTY_TRANSACTION_FILTERS)}
                className="rounded-lg border border-hairline px-3 py-1.5 text-xs font-semibold text-accent"
              >
                {TRANSACTIONS_COPY.noMatchesAction}
              </button>
            </div>
          ) : accounts.length === 0 ? (
            <div className="flex flex-col items-center gap-3 py-14 text-center">
              <p className="text-sm font-semibold text-ink">
                {TRANSACTIONS_COPY.noAccountsTitle}
              </p>
              <p className="max-w-sm text-[13px] text-muted">
                {TRANSACTIONS_COPY.noAccountsBody}
              </p>
              {/* The way out lives here, in the middle of the screen, not only
                  in the header's hint beside the disabled button. */}
              <Link
                to="/money"
                className="rounded-lg bg-accent px-3.5 py-2 text-[13px] font-semibold text-white"
              >
                {TRANSACTIONS_COPY.noAccountsAction}
              </Link>
            </div>
          ) : (
            <div className="flex flex-col items-center gap-3 py-14 text-center">
              <p className="text-sm font-semibold text-ink">{TRANSACTIONS_COPY.emptyTitle}</p>
              <p className="max-w-sm text-[13px] text-muted">{TRANSACTIONS_COPY.emptyBody}</p>
            </div>
          )
        ) : (
          <>
            {groups.map((group, index) => (
              <div key={`${group.date}-${index}`}>
                <p className="pb-1.5 pt-3.5 text-[11px] uppercase tracking-[0.08em] text-muted">
                  {formatDateHeading(group.date)}
                </p>
                {group.items.map((t) => (
                  <TransactionRow
                    key={t.id}
                    transaction={t}
                    symbolFor={symbolFor}
                    onClick={() => setEditingTransaction(t)}
                  />
                ))}
              </div>
            ))}
            {/* null, not a row count, is what this hides on -- a page that
                happens to be exactly full would otherwise wrongly look like
                the last one (transactionsResponseSchema's own comment). */}
            {nextCursor !== null && (
              <button
                type="button"
                disabled={loadingOlder}
                onClick={handleLoadOlder}
                className="w-full py-3.5 text-center text-[12.5px] font-semibold text-accent disabled:opacity-60"
              >
                {TRANSACTIONS_COPY.loadOlder}
              </button>
            )}
          </>
        )}
      </div>

      {/* Mounted only while actually open, matching AccountsPanel's own
          AccountModal -- this modal's own categories query would otherwise
          fire on every page load whether or not anyone opens it. */}
      {addOpen && (
        <TransactionModal
          open
          onClose={() => setAddOpen(false)}
          onSubmit={handleCreate}
          accounts={accounts}
          members={members}
        />
      )}
      {/* A single "currently editing" slot, not one modal per row: closing it
          (onClose sets editingTransaction back to null) fully unmounts this
          instance before any other row's click could ever mount a new one,
          so no mounted instance ever carries a previous row's state into the
          next -- the trap a single always-mounted modal with a swapped
          `initial` prop would fall into. */}
      {editingTransaction && (
        <TransactionModal
          open
          onClose={() => setEditingTransaction(null)}
          onSubmit={handleUpdate}
          onDelete={handleDelete}
          initial={editingTransaction}
          accounts={accounts}
          members={members}
        />
      )}
    </div>
  );
}
