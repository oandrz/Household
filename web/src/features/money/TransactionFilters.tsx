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
  // min-h-11 clears the 44px floor on a phone -- measured: py-2.5 alone
  // (padding scaled from vertical rhythm, not the floor) only reaches 39px
  // against this text's own line-height, 5px short. sm:min-h-0 lets py-1.5
  // set the desktop height again at the same breakpoint the wrapper already
  // switches width at.
  "w-full min-h-11 rounded-lg border border-hairline bg-card px-3 py-2.5 text-[12.5px] text-label sm:min-h-0 sm:w-auto sm:py-1.5";
const LABEL_CLASS =
  "text-[10.5px] font-semibold uppercase tracking-wide text-muted";
// Each filter takes the row's full width on a phone rather than sizing to
// its widest option -- a long account or category nickname would otherwise
// widen its select until the row overflowed 320px (measured: a 47-character
// account nickname alone needs 326px, wider than the 273px a filter row has
// to work with at that floor). `w-full` on both the wrapper and the select
// mirror each other rather than using `flex-1` on the wrapper: `flex-1` is
// Tailwind's `flex: 1 1 0%`, and a `0%` basis makes flex-wrap's line-breaking
// treat the item as contributing no size, so it never gets a line to itself
// -- measured, all four fields crammed onto one shared line and their labels
// (e.g. "ACCOUNT") were clipped to a 15px sliver. `w-full` forces the same
// mobile-first stack without that failure: an item demanding the full row
// can only ever share a line with nothing, so it always wraps alone.
// `min-w-0` is kept even though it measures as a no-op at `sm:w-auto` --
// there is no shared half-width slot at that breakpoint for a select to
// overflow (widths came back byte-identical with and without the class). It
// stays because it costs nothing and guards against a future layout that
// puts these fields in a shared-width slot again.
const FIELD_CLASS = "flex w-full min-w-0 flex-col gap-1 sm:w-auto";

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
          {KIND_OPTIONS.map((option, index) => {
            const selected = values.kind === option.value;
            return (
              <label
                key={option.value || "all"}
                // min-h-11/sm:min-h-0: same 44px-floor reasoning as
                // SELECT_CLASS above -- py-2.5 alone measured 5px short of
                // the floor against this text's own line-height.
                className={`flex min-h-11 cursor-pointer items-center px-3 py-2.5 text-[12.5px] font-semibold has-[:focus-visible]:ring-2 has-[:focus-visible]:ring-inset sm:min-h-0 sm:py-1.5 ${
                  // The ring colour has to change with the pill's own
                  // background: `ring-accent` (a dark green) is invisible
                  // inset against the selected pill's `bg-ink`, which is
                  // nearly the same darkness -- confirmed by two browser-walk
                  // screenshots of a focused, selected pill being pixel-
                  // identical before and after the first fix. `ring-white`
                  // reads against `bg-ink`; `ring-accent` still reads against
                  // the unselected pill's light `bg-card`.
                  selected
                    ? "has-[:focus-visible]:ring-white"
                    : "has-[:focus-visible]:ring-accent"
                } ${
                  index > 0 ? "border-l border-hairline" : ""
                } ${selected ? "bg-ink text-white" : "bg-card text-label"}`}
              >
                {/* The real, keyboard-operable radio is `sr-only` -- visually
                  hidden but still focusable -- so the pill drawn by this
                  label is the only thing a sighted user sees. Without the
                  has-[:focus-visible] ring above, arrow-keying or Tabbing
                  through this group moved focus with no visible sign of it:
                  a selected pill and a selected-and-focused pill were pixel
                  identical, an unselected-and-focused one just as blank as
                  before. The browser walk this task exists for (jsdom cannot
                  drive real Tab/arrow-key focus) is what caught it. */}
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
            );
          })}
        </div>
      </fieldset>

      <div className={FIELD_CLASS}>
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

      <div className={FIELD_CLASS}>
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

      <div className={FIELD_CLASS}>
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

      <div className={FIELD_CLASS}>
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
