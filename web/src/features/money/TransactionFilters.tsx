// The five filters above the ledger (design's "All / Expense / Income" pills
// plus three "▾" dropdowns and a month picker). All five are native <select>s
// (or, for month, a native <input type="month">) rather than the design's
// pill-and-chip look -- every field needs to be reachable and settable with
// one keyboard-and-label-driven interaction, the same reasoning AccountModal's
// Owner/Type selects already settled for this codebase, and Kind specifically
// needs to be a real <select> so its value can be read and changed as one
// thing rather than three buttons with no single accessible "current value."
//
// Holds no state of its own -- `values` and `onChange` live in
// TransactionsPage -- so the page can reset all five at once (the "Clear
// filters" action in the no-matches state) without this component needing to
// know that happened.
import type { Account } from "./schemas";
import type { Category } from "./transactionSchemas";

export type TransactionFilterValues = {
  kind: string;
  accountId: string;
  categoryId: string;
  paidBy: string;
  month: string;
};

const SELECT_CLASS =
  "rounded-lg border border-hairline bg-card px-3 py-1.5 text-[12.5px] text-label";
const LABEL_CLASS = "text-[10.5px] font-semibold uppercase tracking-wide text-muted";

export function TransactionFilters({
  values,
  onChange,
  accounts,
  categories,
  members,
}: {
  values: TransactionFilterValues;
  onChange: (next: TransactionFilterValues) => void;
  accounts: Account[];
  categories: Category[];
  members: { id: string; name: string }[];
}) {
  function set(key: keyof TransactionFilterValues, value: string) {
    onChange({ ...values, [key]: value });
  }

  return (
    <div className="flex flex-wrap items-end gap-2.5">
      <div className="flex flex-col gap-1">
        <label htmlFor="txn-filter-kind" className={LABEL_CLASS}>
          Kind
        </label>
        <select
          id="txn-filter-kind"
          value={values.kind}
          onChange={(event) => set("kind", event.target.value)}
          className={SELECT_CLASS}
        >
          <option value="">All</option>
          <option value="expense">Expense</option>
          <option value="income">Income</option>
        </select>
      </div>

      <div className="flex flex-col gap-1">
        <label htmlFor="txn-filter-account" className={LABEL_CLASS}>
          Account
        </label>
        <select
          id="txn-filter-account"
          value={values.accountId}
          onChange={(event) => set("accountId", event.target.value)}
          className={SELECT_CLASS}
        >
          <option value="">All accounts</option>
          {accounts.map((a) => (
            <option key={a.id} value={a.id}>
              {a.nickname}
            </option>
          ))}
        </select>
      </div>

      <div className="flex flex-col gap-1">
        <label htmlFor="txn-filter-category" className={LABEL_CLASS}>
          Category
        </label>
        <select
          id="txn-filter-category"
          value={values.categoryId}
          onChange={(event) => set("categoryId", event.target.value)}
          className={SELECT_CLASS}
        >
          <option value="">All categories</option>
          {categories.map((c) => (
            <option key={c.id} value={c.id}>
              {c.name}
            </option>
          ))}
        </select>
      </div>

      <div className="flex flex-col gap-1">
        <label htmlFor="txn-filter-person" className={LABEL_CLASS}>
          Person
        </label>
        <select
          id="txn-filter-person"
          value={values.paidBy}
          onChange={(event) => set("paidBy", event.target.value)}
          className={SELECT_CLASS}
        >
          <option value="">Anyone</option>
          {members.map((m) => (
            <option key={m.id} value={m.id}>
              {m.name}
            </option>
          ))}
        </select>
      </div>

      <div className="flex flex-col gap-1">
        <label htmlFor="txn-filter-month" className={LABEL_CLASS}>
          Month
        </label>
        <input
          id="txn-filter-month"
          type="month"
          value={values.month}
          onChange={(event) => set("month", event.target.value)}
          className={SELECT_CLASS}
        />
      </div>
    </div>
  );
}
