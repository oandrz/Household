# Cashflow review fixes: the worked sum, the chart, and polish

Date: 2026-10-25. Branch: `feat/cashflow` (built on top of the Cashflow
feature, `2026-10-25-hearth-cashflow-design.md`, before it is merged).
Approved in conversation with the product owner; mockups in
`.superpowers/brainstorm/63869-1792932702/content/` (`worked-sum-v2.html`,
`chart-and-polish.html`).

## Why

A design review of the Cashflow page found eleven open items. Most are
polish. One is a trust problem. Cashflow says September's **Money out** was
S$4,069.50. Its link opens the Transactions page, which says **Spent in
September S$5,194.50**. Both numbers are correct, for two reasons:

- Cashflow spreads a yearly or quarterly bill: S$2,100 paid on 20 Sep counts
  as S$175 in each of the 12 months it covers. Transactions counts the
  S$2,100 in full on the day it was paid.
- Cashflow counts a transfer into a loan account (S$800) as money out.
  Transactions leaves transfers out of "Spent".

Neither page says any of this. The owner is not a finance professional;
their rule is "money left over each month means good cashflow", and their
top fear is the page looking fine when it is wrong. Two totals that
disagree, with nothing to bridge them, is exactly that fear.

The owner chose to show the full arithmetic (a worked sum) rather than a
one-line explanation, knowing it costs backend work.

## Goals

1. Opening a month on Cashflow shows, step by step, how the Transactions
   page's "Spent in <Month>" becomes Cashflow's Money out, and every step
   can be checked by hand.
2. The sum is never shown unless it is exact. A sum that does not land on
   Money out is worse than no sum.
3. The chart is readable at every width, and nothing on it misleads.
4. The remaining review items are fixed (list under "The other fixes").

## Non-goals

- No change to how Money out, the light or any average is calculated.
- No change to the Transactions page.
- No new endpoint: the sum travels in the existing `GET /cashflow` response.
- No way for the user to add exchange rates (rates come from the built-in
  table in `api/internal/adapter/fx/static.go`).
- The green row lights under an amber "Watch" headline stay as they are
  (a separate, open owner decision).

## Anti-goals (a failure even if it works)

- A worked sum whose lines do not add up to the Money out printed in it.
- A "Spent in <Month>" figure that differs from what the Transactions page
  shows for that month.
- A bill shown in red or as a loss. Paying a bill is not bad news.

## The worked sum

### Where it lives

In the month detail panel that opens under a month row in the Cashflow
table (`CashflowMonthDetail.tsx`). It **replaces** today's list of spread
bills and loan repayments, because each of those becomes a line of the sum.
The panel stays flush inside the table (no card inside the card), and the
sum is capped at about 480px wide so a label and its amount stay close on
desktop. On a 375px phone the table hides Money in and Money out, so the
sum's total line is the first place Money out appears for that month: it
must read clearly there.

### What it says (approved copy)

Heading, replacing today's month-name heading: **How September's money out
adds up**, with Close on the right as today.

Lines, in this order, label on the left, amount on the right, grey second
line under the label:

| Line | Label | Grey line | Amount |
|---|---|---|---|
| Start | Spent in September | From the Transactions page | S$5,194.50 |
| A bill paid this month (one line each) | Car insurance, whole yearly bill | Paid 20 Sep 2026, taken out here and spread over 12 months | −S$2,100.00 |
| A share landing in this month (one line each) | Car insurance, September's share | Month 1 of 12 | +S$175.00 |
| A share of a bill paid earlier | Car insurance, October's share | Month 2 of 12 of the yearly bill paid 20 Sep 2026 | +S$175.00 |
| A loan repayment (one line each) | Car loan repayment | Transfer to a loan account, 28 Sep 2026 | +S$800.00 |
| Total | = Money out | — | S$4,069.50 |

- "yearly" / "12" become "quarterly" / "3" for a quarterly bill.
- Order: Start; then each bill paid this month, each followed directly by
  its own share for this month; then shares of bills paid in earlier months,
  oldest payment first; then loan repayments by date; then the total.
- Signs sit on the amount only, never before the label. Minus is U+2212
  (as `formatMoney.ts` already prints); plus is added by the component,
  since `formatMoney` never prints one. All amounts in normal ink, never
  red. Amounts use `.tabular`.
- The total line is 14px semibold with a rule above it.
- For the month still running, the total reads **= Money out so far**.
- A month with nothing to explain keeps the same shape: Start, then
  `= Money out` with the same figure, then the existing `detailsNone`
  sentence in grey ("No yearly bills spread and no loan repayments in this
  month.").
- The link becomes **See September's 9 transactions** (the count moves out
  of the sum: it counts income and transfers too, so under "Spent" it read
  as "9 purchases"). It still opens `/money/transactions?month=YYYY-MM`.

### When the sum is hidden

The panel keeps its heading and link, and shows one sentence instead of the
lines:

- **A transaction or bill in the month has no exchange rate** (the month's
  `unconvertibleCurrencies` is non-empty): "Hearth can't add up July: 1
  transaction is not counted because Hearth has no exchange rate for USD."
  The count is the number of this month's transactions left out; when the
  missing rate belongs only to an earlier bill whose share lands here, say
  "Hearth can't add up July: a bill spread into this month is in USD, and
  Hearth has no exchange rate for it." When both happen in one month:
  "Hearth can't add up July: it has no exchange rate for USD, so 1
  transaction and a bill spread into this month are not counted." Several
  currencies are listed with "and".
- **The lines do not add up to Money out.** Review found this cannot
  happen today: Spent − whole bills + shares + loans equals `OutMinor` by
  construction in `BuildCashflowMonths`, and the only break is a missing
  rate, which the case above catches. The check stays as a guard against a
  future change to either calculation: "Hearth can't show how June adds up right now. The figures
  above are still the ones it uses."

### How it is worked out (the rule that keeps it exact)

The server builds the sum in the same request as the rest of the page, from
the same converted items, so both numbers use the same exchange rates and
the same per-transaction rounding.

- **Spent** for a month is the sum of the month's expense items (roles
  `CashflowOut` and `CashflowSpreadOut`), converted per transaction exactly
  as `MonthSummary` does. It must equal what `GET /transactions` returns as
  `summary.spentMinor` for that month with no filters.
- **Whole bill** lines are the month's `CashflowSpreadOut` items at full
  amount, subtracted.
- **Share** lines are the existing `CashflowContribution`s of kind
  spread-bill landing in this month, with which month of the spread this is
  (1-based, counted from the month the bill was paid) and how many months it
  covers.
- **Loan** lines are the existing loan contributions.
- The server then checks: Spent − whole bills + shares + loans = OutMinor,
  exactly, in minor units. If not, or if the month has an unconvertible
  currency, it sends the hidden state instead of the lines. The frontend
  never re-derives or re-checks the arithmetic; it renders what it is sent,
  and the frontend tests prove it shows nothing when told the sum is hidden.

### Response shape

Each month in `GET /cashflow` gains a `reconciliation` object. The builder
may refine names, but it carries at least: a status (`shown`, `no_rate`,
`mismatch`), `spentMinor`, the ordered `lines` (each with a kind: whole
bill, share or loan; name; signed amount in minor units; paid-on date; and
for shares the month index and spread length; for whole bills the spread
length), and for `no_rate` the left-out count and currencies. Arrays are
never null (as elsewhere in this API). The schema in `cashflowSchemas.ts`
is updated to match, and `hearthctl` and `docs/CLI.md` reflect the new
field.

## The other fixes

**Chart (`CashflowChart.tsx`)**

- About 200px tall at every width (today it scales with width to ~470px at
  1440). Bars may stretch horizontally; strokes and the stripe pattern must
  not distort.
- A bar cut off by the scale (the running month, since finished months now
  set the scale) gets two white diagonal slashes near its top.
- Hovering a bar shows the month name and amount: "July 2026: S$1,000.00
  left over", "October 2026 so far: S$47,282.50 left over (bar cut off)",
  "December 2026, projected: S$1,483.50 left over", "August 2026: not
  enough data", and for zero or below "June 2026: nothing left over
  (−S$120.00)" / "June 2026: nothing left over (S$0.00)" (never "loss").
  Today's titles are raw keys ("2026-07").
- Hover only (the owner's choice): the bar keeps an SVG `<title>`, which a
  mouse shows and a tap does not. On a phone every month's Left over is in
  the table under the chart, so no tap-to-show piece is built.
- Legend uses the table's words: "Money left over" (was "Finished"), a red
  "Nothing left over" entry for loss months, "Not enough data", "This month
  so far", and "Projected" drawn green dashed to match its bars (today the
  swatch is grey).

**Tabs and screen readers (`CashflowPage.tsx`, `CashflowMonthTable.tsx`)**

- The selected tab is kept in the address (a `view=yearly` search param;
  Monthly is the default and has no param), so a reload or shared link
  keeps it. The year picked on Yearly is kept too (`year=2025`; the current
  year has no param). A year the address names but the server would refuse
  (before the oldest year with a transaction, or after this year) falls
  back to the current year instead of showing an error. *Builder's call,
  made in review: without it a shared link to 2025 opens this year.*
- Cashflow gains no `?month=` of its own. The month detail's link to
  `/money/transactions?month=YYYY-MM` keeps working as today.
- Tabs get ids and `aria-controls`, the panel `aria-labelledby`, and left
  and right arrow keys move between tabs.
- Each month row's button gets `aria-controls` pointing at its detail row.
- The month table gets a visible heading, "Month by month", matching the
  chart's "Left over each month".

**Hover states.** Tabs, the info button, the year arrows and the month
names get hover states with the app's `--transition-state` timing, matching
siblings (`AccountsPanel.tsx` uses `hover:bg-canvas`).

**Small polish**

- "September 2026" above the cards becomes "Last finished month: September
  2026", 13px semibold ink (today a 12px muted label, smaller than body
  text).
- The Kept column always shows two decimals ("20.00%", "37.58%") so the
  points line up. Sentences keep the short form ("20% of income kept").
- The year arrows are hidden when there is only one year to show.
- The "Kept so far" card shows this year's share first and the comparison
  with last year second.
- `.tabular` on the worked sum amounts (above) and the "Bills still to come
  this year" amounts.

## Constraints

- Clean architecture as `CLAUDE.md` describes: the sum's rules belong in
  `internal/domain` (standard library only), orchestration in `usecase`,
  JSON shape in `adapter/http`. Money stays `int64` minor units; no float.
- A `switch` on the new status or line kind has a refusing `default` on
  both sides.
- Comments state rules, never plan or finding numbers (`make lint-comments`).
- Definition of done from `CLAUDE.md`: `make lint && make test` green
  (`lint-vuln` is red on `main` too, GO-2026-6809, and is out of scope), at
  least one new test mutation-checked, a real-browser walk at 375px and
  desktop, and `FEATURE_TRACKER`, `LEARNING` and `SYSTEM_DESIGN` updated
  (the response shape changes).
- Performance: the page currently reads in ~92 ms p50 at 100k transactions
  (ADR 14 follow-up). The sum is built from items already in memory; it
  must not add a query per month.

## Tests that must exist

- Domain: September's example adds up exactly; a month carrying only a
  share of an earlier bill; a quarterly bill; two bills paid in one month;
  a bill whose `SpreadForward` remainder lands in the first share; a month
  with nothing to explain; a month with an unconvertible currency (hidden,
  `no_rate`, count and currencies right, including the earlier-bill case);
  a forced mismatch (hidden, `mismatch`).
- Parity, against a real database: for a seeded month with an ordinary
  expense, a yearly bill payment and a loan transfer, the sum's `spentMinor`
  equals `MonthSummary.Spent` for the same month.
- Frontend: lines render in order with signs on the amounts only (U+2212
  and "+"), the total reads "so far" for the current month, the hidden
  states show their sentence and no amounts, the link names the count.
- Chart: tooltip text for each bar kind, the cut-off mark only on a
  clipped bar, legend entries and colours.
- Tabs: reload with `view=yearly` opens Yearly; arrow keys switch tabs.

## Left to the builder

Exact type and field names; how the chart holds a fixed height; whether the
current month's "so far" wording is chosen by the server or the component;
how hover styles are shared; the order of work. Also, settled in review as
the builder's call: only the twelve recent months (the ones the table can
open) carry `reconciliation`, and projected months never do; whether the
old `contributions` field stays in the response; how the cut-off slashes
sit on a clipped bar below zero.
