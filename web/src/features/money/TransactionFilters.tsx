// The five filters above the ledger. Kind renders as the design's own
// segmented control (design/Household Dashboard.dc.html's "All / Expense /
// Income" pills) -- a first draft here built it as a native <select> instead,
// reasoning that a single settable "value" was the only way to keep it both
// keyboard-reachable and queryable by label; the product owner overruled that
// in review, on the basis that the design is the spec for every other screen
// in this product and a keyboard-reachable version of the real control is
// buildable. It is: a `<fieldset>`/`<legend>` grouping three real
// `<input type="radio">`s, one per option, each wrapped in its own `<label>`
// -- keyboard-reachable (arrow keys move between options sharing one `name`,
// same as any native radio group) and queryable by each option's own
// accessible name (`getByRole("radio", { name: "Income" })`), which is what
// replaces the old single-select "change to this value" query. The other
// four filters stay native <select>s (Month is <input type="month">) -- nothing
// in review asked for those to change.
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

// value "" is "All" -- the same empty-string-means-unset convention every
// other filter here uses, so toQueryString (useTransactions.ts) omits it from
// the request exactly when the other four filters' own blank option would.
const KIND_OPTIONS: { value: string; label: string }[] = [
  { value: "", label: "All" },
  { value: "expense", label: "Expense" },
  { value: "income", label: "Income" },
];

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
      <fieldset className="m-0 flex flex-col gap-1 border-0 p-0">
        <legend className={LABEL_CLASS}>Kind</legend>
        <div className="flex overflow-hidden rounded-lg border border-hairline">
          {KIND_OPTIONS.map((option, index) => (
            <label
              key={option.value || "all"}
              className={`cursor-pointer px-3 py-1.5 text-[12.5px] font-semibold ${
                index > 0 ? "border-l border-hairline" : ""
              } ${values.kind === option.value ? "bg-ink text-white" : "bg-card text-label"}`}
            >
              <input
                type="radio"
                name="txn-filter-kind"
                value={option.value}
                checked={values.kind === option.value}
                onChange={(event) => set("kind", event.target.value)}
                className="sr-only"
              />
              {option.label}
            </label>
          ))}
        </div>
      </fieldset>

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
