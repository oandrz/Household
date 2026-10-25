# Cashflow Review Fixes Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Show a worked sum from the ledger's "Spent in <Month>" to Cashflow's Money out in each month's detail, and fix the chart, tabs, hover states and small polish the design review left open.

**Architecture:** The sum's inputs are recorded while `domain.BuildCashflowMonths` builds each month (it already holds every converted item), and a pure `domain.ReconcileCashflowMonth` turns a month into ordered lines plus a status (`shown`, `no_rate`, `mismatch`). The HTTP adapter maps that onto each month DTO; the React detail panel only renders what it is sent. Everything else is frontend.

**Tech Stack:** Go 1.26 (standard library only in `internal/domain`), Postgres via testcontainers for API tests, React 19 + TypeScript + Tailwind v4 + TanStack Router + zod + vitest.

**Spec:** `docs/superpowers/specs/2026-10-25-cashflow-review-fixes-design.md` (approved copy lives there; use it verbatim).

## Global Constraints

- Work on branch `feat/cashflow`; commit per task; end every commit message with `Claude-Session: https://claude.ai/code/session_01Eh29N45sF2m5bvg2R6Q5cA`.
- Clean architecture: `internal/domain` imports the standard library only; `make lint-arch` enforces it, tests included.
- Money is `int64` minor units; no `float64` in any monetary path. Sums use `addChecked`.
- A `switch` on a value that crossed a boundary (status, line kind, spread) has a refusing `default`, in Go and in TypeScript.
- Comments state the rule, never a plan, task or finding number (`make lint-comments`).
- Arrays in JSON are never null.
- Minus is U+2212 (`formatMoney` already prints it); a "+" is added by the component; amounts in the sum are never red.
- Copy is the spec's, verbatim. All user-facing strings live in `web/src/features/money/cashflowCopy.ts`.
- Before any commit: `git checkout -- web/package-lock.json` (the container's `npm install` rewrites it).
- Go: `export PATH=/Volumes/Oink_Machine/.local/opt/go-v1.24.2/bin:$PATH DOCKER_HOST=unix:///Volumes/Oink_Machine/.colima/default/docker.sock TESTCONTAINERS_DOCKER_SOCKET_OVERRIDE=/var/run/docker.sock`. Frontend typecheck is `npx tsc --noEmit` in `web/` (there is no `typecheck` script).
- `make lint-vuln` is red on `main` too (GO-2026-6809); it is out of scope. Every other lint target must pass.

## Review Focus

1. A yearly bill paid in the **current** month: shares 2–12 fall after the current month and are dropped, yet the current month's sum must still land on Money out so far. (Test in Task 1.)
2. A bill paid **before the first month** of the household's history whose shares land inside it: no "whole bill" line (it is not in that month's Spent), only shares. (Test in Task 1.)
3. Two bills with the **same name paid on the same day** in one month: each whole line is followed by exactly one share, not both shares under the first. (Test in Task 1.)
4. A hand-edited address: `?view=weird`, `?year=abc`, `?year=1999`, `?year=2099`. The first two are ignored; out-of-range years fall back to this year without an error screen. (Tests in Task 5.)
5. The 375px phone: the sum sits in a table row whose visible columns are Month, Left over, Kept, Light. It must not push the table into sideways scrolling. (Measured in Task 7's walk: `scrollWidth` equals `clientWidth` with September open.)

---

### Task 1: Domain — record the sum's inputs and reconcile a month

**Files:**
- Modify: `api/internal/domain/cashflow_month.go` (`CashflowMonth`, `CashflowContribution`, `addCashflowItem`, `addSingleMonthItem`, `addSpreadItem`)
- Create: `api/internal/domain/cashflow_reconcile.go`
- Test: `api/internal/domain/cashflow_reconcile_test.go`

**Interfaces:**
- Consumes: `BuildCashflowMonths`, `SpreadForward`, `addChecked`, `ContributionSpreadBill`, `ContributionLoan` (existing).
- Produces:
  ```go
  // added to CashflowContribution (spread-bill contributions only; 0 for loans)
  ShareIndex int // 1-based month of the spread
  Spread     int // 3 or 12

  // added to CashflowMonth
  LedgerSpentMinor   int64              // this month's expense items (CashflowOut + CashflowSpreadOut at full amount), converted
  PaidInFull         []CashflowPaidBill // CashflowSpreadOut items dated this month, converted
  UnratedHere        int                // non-ignored items dated this month with no rate
  UnratedEarlierBill bool               // a share landing here comes from an earlier, unrated bill

  type CashflowPaidBill struct { Name string; AmountMinor int64; PaidOn time.Time; Spread int }

  type CashflowReconcileStatus string // "shown", "no_rate", "mismatch"
  type CashflowLineKind string        // "bill_in_full", "bill_share", "loan"
  type CashflowSumLine struct {
      Kind        CashflowLineKind
      Name        string
      AmountMinor int64 // signed: whole bill negative, share and loan positive
      PaidOn      time.Time
      ShareIndex  int // bill_share only
      Spread      int // bill_in_full and bill_share
  }
  type CashflowReconciliation struct {
      Status             CashflowReconcileStatus
      SpentMinor         int64
      Lines              []CashflowSumLine // never nil; empty unless Status is "shown"
      UnratedCount       int
      UnratedEarlierBill bool
      UnratedCurrencies  []string // never nil
  }
  func ReconcileCashflowMonth(m CashflowMonth) CashflowReconciliation
  ```

- [ ] **Step 1: Write the failing tests** in `cashflow_reconcile_test.go`. Build months with `BuildCashflowMonths` from `CashflowItem`s (as `cashflow_month_test.go` does), then reconcile. One test per case, named for the behaviour:
  - `TestReconcileSeptemberAddsUpToMoneyOut`: Sep items: Out 309450 (several expenses summing to it), SpreadOut "Car insurance" 210000 Spread 12 on 20 Sep, LoanOut "Car loan" 80000 on 28 Sep. Expect `Status "shown"`, `SpentMinor 519450`, lines `[{bill_in_full, Car insurance, -210000, Spread 12}, {bill_share, Car insurance, 17500, ShareIndex 1, Spread 12}, {loan, Car loan, 80000}]`, and `SpentMinor + Σ AmountMinor == OutMinor == 406950`.
  - `TestReconcileLaterMonthCarriesOnlyTheShare`: same items; October has one line `{bill_share, ShareIndex 2}` and `SpentMinor` excludes the bill.
  - `TestReconcileQuarterlyBill`: Spread 3 gives `ShareIndex 1` with `Spread 3`.
  - `TestReconcileRemainderLandsOnTheFirstShare`: SpreadOut 100001, Spread 12 → first share `8338` (`SpreadForward` puts the remainder first) and the month still adds up.
  - `TestReconcileTwoBillsSameNameSameDay` (Review Focus 3): two "Insurance" 120000 yearly bills paid 5 Sep → lines alternate whole, share, whole, share.
  - `TestReconcileBillPaidInTheCurrentMonth` (Review Focus 1): bill paid in `current`; the current month shows whole + share 1 and adds up.
  - `TestReconcileBillPaidBeforeHistoryBegan` (Review Focus 2): SpreadOut dated the month before `first`; `first` has a share line, no whole line, and adds up.
  - `TestReconcileNothingToExplain`: only Out items → `shown`, `Lines` empty (non-nil), `SpentMinor == OutMinor`.
  - `TestReconcileUnratedTransactionHidesTheSum`: one Out item `Unconvertible "USD"` → `no_rate`, `UnratedCount 1`, `UnratedCurrencies ["USD"]`, `Lines` empty.
  - `TestReconcileUnratedEarlierBillHidesTheSum`: unrated SpreadOut paid in Aug → September is `no_rate`, `UnratedCount 0`, `UnratedEarlierBill true`.
  - `TestReconcileMismatchHidesTheSum`: take September's built month, add 1 to `OutMinor`, reconcile → `mismatch`, `Lines` empty.
  - `TestReconcileOrdersEarlierSharesThenLoans`: a bill paid in Jul and one paid in Aug both share into Sep, plus a loan → order: Jul share, Aug share, loan.

- [ ] **Step 2: Run them and see them fail**
  Run: `cd api && go test ./internal/domain/ -run Reconcile -v`
  Expected: FAIL (undefined: `ReconcileCashflowMonth`, missing fields).

- [ ] **Step 3: Record the inputs in `cashflow_month.go`.** In `addCashflowItem`, when `own >= 0`, the role is not `CashflowIgnore` and `item.Unconvertible != ""`, increment `UnratedHere`. In `addSingleMonthItem`'s `CashflowOut` case, add to `LedgerSpentMinor`. In `addSpreadItem`: when the share index is 0 and its month is in range and the item is convertible, add the full amount to `LedgerSpentMinor` and append a `CashflowPaidBill`; set `ShareIndex: i+1, Spread: item.Spread` on each spread contribution; when the item is unconvertible and `i > 0`, set `UnratedEarlierBill`. Use `addChecked` for every addition. Update the `CashflowMonth` doc comment for the new fields.

- [ ] **Step 4: Write `ReconcileCashflowMonth` in `cashflow_reconcile.go`.** `SpentMinor` is always `LedgerSpentMinor`. If `len(m.UnconvertibleCurrencies) > 0` → `no_rate` with `UnratedCount`, `UnratedEarlierBill`, a copy of the currencies. Otherwise build lines in the spec's order: each `PaidInFull` (sorted by `PaidOn`, then `Name`, stable) as `bill_in_full` with the negated amount, each followed by the first unused spread contribution with the same `Name`, `PaidOn` and `ShareIndex == 1`; then the remaining spread contributions sorted by `PaidOn`, then `Name`; then loan contributions sorted by `PaidOn`, then `Name`. Sum with `addChecked`; on overflow, or when `SpentMinor + Σ lines != m.OutMinor`, return `mismatch` with empty `Lines`. A contribution kind other than spread-bill or loan also returns `mismatch` (fail closed). Doc comment: the rule that keeps it exact and why a mismatch hides the sum.

- [ ] **Step 5: Run the domain tests**
  Run: `cd api && go test ./internal/domain/ -v -run 'Reconcile|Cashflow'`
  Expected: PASS, including every existing Cashflow domain test.

- [ ] **Step 6: Mutation-check one test.** Temporarily drop the `ShareIndex == 1` condition when pairing; `TestReconcileTwoBillsSameNameSameDay` or `TestReconcileLaterMonthCarriesOnlyTheShare` must fail. Restore.

- [ ] **Step 7: Commit**
  ```bash
  git add api/internal/domain/cashflow_month.go api/internal/domain/cashflow_reconcile.go api/internal/domain/cashflow_reconcile_test.go
  git commit -m "Work out how each Cashflow month's money out follows from the ledger's spent"
  ```

### Task 2: HTTP — send the sum, prove it starts from the ledger's number

**Files:**
- Modify: `api/internal/adapter/http/cashflow_handlers.go` (`cashflowMonthDTO`, `toCashflowMonthDTO`)
- Test: `api/internal/adapter/http/cashflow_api_test.go`
- Modify: `docs/CLI.md` (Cashflow section, near `hearthctl api GET '/cashflow'`), `docs/SYSTEM_DESIGN.md` (the Cashflow response description)

**Interfaces:**
- Consumes: `domain.ReconcileCashflowMonth`, `CashflowReconciliation` (Task 1).
- Produces, on every month DTO (`current`, `lastFinished`, each of `recent`, each of `year.months`):
  ```json
  "reconciliation": null | {
    "status": "shown" | "no_rate" | "mismatch",
    "spentMinor": 519450,
    "lines": [{"kind": "bill_in_full"|"bill_share"|"loan", "name": "Car insurance",
               "amountMinor": -210000, "paidOn": "2026-09-20", "shareIndex": 0, "spread": 12}],
    "unratedCount": 0, "unratedEarlierBill": false, "unratedCurrencies": []
  }
  ```
  `reconciliation` is non-null only when the month's status is `solid`, `grey` or `current`; `future` and `none` months carry `null`. `contributions` stays in the response unchanged: after Task 3 the page no longer reads it, but `hearthctl` users and `docs/CLI.md` may, and removing a field is a separate decision.

- [ ] **Step 1: Write the failing API tests** in `cashflow_api_test.go`. Extend `cashflowMonthBody` with the `reconciliation` shape.
  - `TestCashflowSumStartsFromTheLedgersSpent`: sign in as the owner; create a cash account and a loan account (`mustCreateAccount` with `"type": "loan"`, as `accounts_api_test.go` does); last month: an expense (`mustCreateExpense`), a yearly bill created and paid that month (`mustCreateBill` with a yearly cadence, `mustPayBill` with a `paidOn` in last month; see `bills_api_test.go`), and a transfer into the loan account (POST `/api/v1/transactions` with `"kind": "transfer"`). GET `/api/v1/cashflow`; find last month in `recent`. Assert `reconciliation.status == "shown"`, `reconciliation.spentMinor == listTransactions(t, session, "/api/v1/transactions?month=YYYY-MM").summary.spentMinor`, and `spentMinor + Σ lines.amountMinor == outMinor`. This is the parity the spec requires.
  - `TestCashflowSumIsNullForMonthsWithoutFigures`: every `recent` month with status `none` and every `year.months` entry with status `future` has `reconciliation == null`; every other month has a non-null one whose `lines` and `unratedCurrencies` are arrays (not null).

- [ ] **Step 2: Run them and see them fail**
  Run: `cd api && go test ./internal/adapter/http/ -run 'CashflowSum' -v`
  Expected: FAIL (`reconciliation` missing).

- [ ] **Step 3: Add `cashflowReconciliationDTO` and `cashflowSumLineDTO`** to `cashflow_handlers.go` and fill `Reconciliation *cashflowReconciliationDTO` in `toCashflowMonthDTO` by calling `domain.ReconcileCashflowMonth(m)` for `solid`, `grey` and `current` months. `paidOn` uses `cashflowDayLayout`. Status and kind are converted with `string(...)`: they come from `ReconcileCashflowMonth`, which only ever builds its own constants, so no refusing `switch` is needed on this side (the rule applies to values from a database row or a request).

- [ ] **Step 4: Run the API tests**
  Run: `cd api && go test ./internal/adapter/http/ -run 'Cashflow' -v`
  Expected: PASS, including `TestCashflowPageNeverEmitsNullArrays`.

- [ ] **Step 5: Docs.** `docs/CLI.md`: one sentence and a trimmed example of `reconciliation` under the Cashflow read. `docs/SYSTEM_DESIGN.md`: the Cashflow response description gains `reconciliation`, with the rule (built from the same converted items in one request; hidden as `no_rate`/`mismatch` rather than shown wrong). Use the `maintaining-system-design` skill.

- [ ] **Step 6: Commit**
  ```bash
  git add api/internal/adapter/http/cashflow_handlers.go api/internal/adapter/http/cashflow_api_test.go docs/CLI.md docs/SYSTEM_DESIGN.md
  git commit -m "Send each Cashflow month's worked sum, starting from the ledger's spent"
  ```

### Task 3: Frontend — the worked sum in the month detail

**Files:**
- Modify: `web/src/features/money/cashflowSchemas.ts` (`cashflowMonthSchema`)
- Modify: `web/src/features/money/cashflowCopy.ts`
- Modify: `web/src/features/money/CashflowMonthDetail.tsx` (rewritten body)
- Modify: `web/src/features/money/CashflowMonthTable.tsx` (detail row id, `aria-controls`)
- Test: create `web/src/features/money/CashflowMonthDetail.test.tsx`; update the detail assertions in `CashflowPage.test.tsx` that read the old list (`detailsSpread`, `detailsLoan`).

**Interfaces:**
- Consumes: the `reconciliation` JSON from Task 2.
- Produces:
  ```ts
  // cashflowSchemas.ts
  cashflowSumLineSchema = z.object({ kind: z.enum(["bill_in_full","bill_share","loan"]), name: z.string(),
    amountMinor: z.number(), paidOn: z.string(), shareIndex: z.number(), spread: z.number() })
  cashflowReconciliationSchema = z.object({ status: z.enum(["shown","no_rate","mismatch"]), spentMinor: z.number(),
    lines: z.array(cashflowSumLineSchema), unratedCount: z.number(), unratedEarlierBill: z.boolean(),
    unratedCurrencies: z.array(z.string()) })
  // on cashflowMonthSchema:
  reconciliation: cashflowReconciliationSchema.nullable()

  // cashflowCopy.ts
  export function fullMonthName(month: string): string            // "2026-09" -> "September"
  export function cadenceWord(spread: number): "yearly" | "quarterly" | null  // 12, 3, else null
  cashflowCopy.sum = {
    heading: (m: string) => `How ${m}'s money out adds up`,
    spent: (m: string) => `Spent in ${m}`,
    spentNote: "From the Transactions page",
    wholeBill: (name: string, cadence: string) => `${name}, whole ${cadence} bill`,
    wholeBillNote: (date: string, spread: number) => `Paid ${date}, taken out here and spread over ${spread} months`,
    share: (name: string, m: string) => `${name}, ${m}'s share`,
    shareNote: (index: number, spread: number) => `Month ${index} of ${spread}`,
    shareNoteEarlier: (index: number, spread: number, cadence: string, date: string) =>
      `Month ${index} of ${spread} of the ${cadence} bill paid ${date}`,
    loan: (name: string) => `${name} repayment`,
    loanNote: (date: string) => `Transfer to a loan account, ${date}`,
    total: "= Money out",
    totalSoFar: "= Money out so far",
    noRateHere: (m: string, count: number, currencies: string) => …,      // spec's first sentence
    noRateEarlier: (m: string, currencies: string) => …,                   // spec's second
    noRateBoth: (m: string, count: number, currencies: string) => …,       // spec's combined
    mismatch: (m: string) => `Hearth can't show how ${m} adds up right now. The figures above are still the ones it uses.`,
    link: (m: string, count: number) => `See ${m}'s ${count} ${count === 1 ? "transaction" : "transactions"}`,
  }
  ```
  The three no-rate sentences are the spec's exact strings; currencies join with ", " and a final " and ". A line whose spread `cadenceWord` returns `null` for makes the panel show the mismatch sentence instead of the sum (fail closed).

- [ ] **Step 1: Write the failing tests** in `CashflowMonthDetail.test.tsx`, rendering `CashflowMonthDetail` with a month fixture (copy the `monthFixture` shape from `CashflowChartScale.test.tsx`, adding `reconciliation`) inside the router wrapper `renderWithRouter` (the panel has a `Link`):
  - `shows September's sum in order with signs on the amounts only`: reconciliation from Task 1's September case; expect the heading "How September's money out adds up", rows in order "Spent in September" S$5,194.50 / "Car insurance, whole yearly bill" "−S$2,100.00" / "Car insurance, September's share" "+S$175.00" / "Car loan repayment" "+S$800.00" / "= Money out" "S$4,069.50"; no label starts with "+" or "−".
  - `says Money out so far for the month still running`.
  - `uses the earlier-bill note for a share of a bill paid in another month`: "Month 2 of 12 of the yearly bill paid 20 Sep 2026".
  - `keeps the same shape when there is nothing to explain`: Spent and "= Money out" with equal figures plus "No yearly bills spread and no loan repayments in this month."
  - `hides the sum when a rate is missing` for each of the three no-rate cases, asserting the exact sentence and that no "= Money out" row renders.
  - `hides the sum on a mismatch` and `hides the sum on an unknown spread`.
  - `names the count in the link and keeps the month in it`: "See September's 9 transactions" with `href` ending `/money/transactions?month=2026-09`.

- [ ] **Step 2: Run them and see them fail**
  Run: `cd web && npx vitest run src/features/money/CashflowMonthDetail.test.tsx`
  Expected: FAIL.

- [ ] **Step 3: Implement** the schema, the copy, and the panel: heading row (heading + Close), then either the sum (a list capped at `max-w-[480px]`, label left with a grey note line under it, amount right with `.tabular`, total row `text-[14px] font-semibold` with a top rule) or the one sentence, then the link. A month whose `reconciliation` is `null` renders only the link. Amount signs: lines with a negative amount use `formatAmount` (prints U+2212); positive line amounts get a leading "+". Start and total are unsigned. In `CashflowMonthTable.tsx`, give the detail `<td>` `id={\`cashflow-detail-${month.month}\`}` and the month button `aria-controls` pointing at it while open.

- [ ] **Step 4: Run the frontend suite for Cashflow**
  Run: `cd web && npx vitest run src/features/money/Cashflow && npx tsc --noEmit`
  Expected: PASS after updating the old detail assertions in `CashflowPage.test.tsx` to the new rows.

- [ ] **Step 5: Mutation-check:** make positive line amounts render without "+"; the order test must fail. Restore.

- [ ] **Step 6: Commit**
  ```bash
  git checkout -- web/package-lock.json
  git add web/src/features/money/cashflowSchemas.ts web/src/features/money/cashflowCopy.ts web/src/features/money/CashflowMonthDetail.tsx web/src/features/money/CashflowMonthDetail.test.tsx web/src/features/money/CashflowMonthTable.tsx web/src/features/money/CashflowPage.test.tsx
  git commit -m "Show how each month's money out adds up from the ledger's spent"
  ```

### Task 4: Chart — fixed height, cut-off mark, readable titles, honest legend

**Files:**
- Modify: `web/src/features/money/CashflowChart.tsx`, `web/src/features/money/cashflowCopy.ts` (`chartLegend`, new `barTitle`), `web/src/features/money/CashflowMonthly.tsx` and `CashflowYearly.tsx` (pass `formatAmount`)
- Test: create `web/src/features/money/CashflowChart.test.tsx`; update the legend test in `CashflowPage.test.tsx` (labels and order change)

**Interfaces:**
- Produces: `CashflowChart({ months, currency, formatAmount }: { months: CashflowMonth[]; currency: string; formatAmount: (minor: number) => string })`; each bar keeps `data-testid="cashflow-bar"` and gains `data-clipped="true"` when cut off; the cut-off mark has `data-testid="cashflow-bar-cut"`.
  ```ts
  cashflowCopy.chartLegend = { left: "Money left over", nothing: "Nothing left over", grey: "Not enough data",
                               current: "This month so far", projected: "Projected" }
  export function barTitle(month: CashflowMonth, value: number, clipped: boolean, formatAmount: (m: number) => string): string
  ```
  `barTitle` outputs exactly (month name via `monthLabel`): solid > 0 `"July 2026: S$1,000.00 left over"`; solid ≤ 0 `"June 2026: nothing left over (−S$120.00)"` / `"(S$0.00)"`; current `"October 2026 so far: S$47,282.50 left over"` plus `" (bar cut off)"` when clipped; projected `"December 2026, projected: S$1,483.50 left over"`; grey `"August 2026: not enough data"`.

- [ ] **Step 1: Write the failing tests** in `CashflowChart.test.tsx`:
  - `titles each kind of bar with its month and amount`: one bar of each kind; assert each `<title>` text exactly as above.
  - `marks only a clipped bar as cut off`: a current bar above the finished max has `data-clipped="true"` and one `cashflow-bar-cut`; a finished bar has neither.
  - `holds the same height at every width`: the `<svg>` has a fixed-height class (`h-[200px]`) and `preserveAspectRatio="none"`; every stroked element has `vector-effect="non-scaling-stroke"`.
  - `draws the legend in the table's words and the bars' colours`: swatch styles in order `solid-positive, solid-negative, grey, current, projected`; labels as `chartLegend`; the `solid-negative` swatch has `text-danger`, the `projected` swatch `text-accent`.

- [ ] **Step 2: Run them and see them fail**
  Run: `cd web && npx vitest run src/features/money/CashflowChart.test.tsx`
  Expected: FAIL.

- [ ] **Step 3: Implement.** Height: `className="h-[200px] w-full"` with `preserveAspectRatio="none"`; give strokes `vectorEffect="non-scaling-stroke"`; make the stripe pattern not stretch (e.g. draw grey bars' stripes with `patternUnits="userSpaceOnUse"` sized in the stretched space, or replace the pattern with a solid tint plus stroke if stretching cannot be avoided; keep the legend swatch matching the bar). Cut-off: a bar whose value is outside `[min, max]` is clipped (the clamping from commit 7375f9d) and gets two white diagonal strokes (`text-card`, `stroke="currentColor"`) near its clipped end. Titles from `barTitle`. Legend per above; the `LegendItem` colour switch gets a refusing default.

- [ ] **Step 4: Run the Cashflow frontend tests and typecheck**
  Run: `cd web && npx vitest run src/features/money/Cashflow && npx tsc --noEmit`
  Expected: PASS after updating the legend test in `CashflowPage.test.tsx`.

- [ ] **Step 5: Commit**
  ```bash
  git checkout -- web/package-lock.json
  git add web/src/features/money/CashflowChart.tsx web/src/features/money/CashflowChart.test.tsx web/src/features/money/cashflowCopy.ts web/src/features/money/CashflowMonthly.tsx web/src/features/money/CashflowYearly.tsx web/src/features/money/CashflowPage.test.tsx
  git commit -m "Keep the Cashflow chart a fixed height, mark cut bars, and name each bar"
  ```

### Task 5: Tabs and year in the address, screen readers, hover states

**Files:**
- Modify: `web/src/routes/router.tsx` (`moneyCashflowRoute`)
- Modify: `web/src/features/money/CashflowPage.tsx`, `CashflowYearly.tsx` (picker buttons), `CashflowMonthly.tsx` (table heading), `CashflowMonthTable.tsx` (month button hover), `cashflowCopy.ts` (`tableHeading: "Month by month"`)
- Test: `web/src/routes/router.test.tsx` (route search), create `web/src/features/money/CashflowTabs.test.tsx`

**Interfaces:**
- Produces: `moneyCashflowRoute.validateSearch(search): { view?: "yearly"; year?: number }`. `view` is kept only when it equals `"yearly"`; `year` only when it is an integer from 2000 to 9999 (a string of four digits or a number). `CashflowPage` takes `{ initialView: "monthly" | "yearly"; initialYear?: number }` from a named route component (the `MoneyTransactionsRouteComponent` pattern in `router.tsx`, re-checking the values the same way), and writes changes back with `useNavigate` (`replace: true`). Monthly and the current year have no param.
- Tab ids: `cashflow-tab-monthly`, `cashflow-tab-yearly`; panel id `cashflow-panel`.

- [ ] **Step 1: Write the failing tests.**
  - Router: `/money/cashflow?view=yearly&year=2025` keeps both; `?view=weird` and `?year=abc` drop them; `?year=1999` drops year (Review Focus 4).
  - `CashflowTabs.test.tsx`: `opens on Yearly when the address says so`; `switching tab writes view=yearly to the address and back to nothing for Monthly`; `picking a year writes year=YYYY and the current year removes it`; `falls back to this year when the server refuses the year` (stub `GET /cashflow?year=2099` to answer 422 `CASHFLOW_YEAR_OUT_OF_RANGE`, expect the address to lose `year` and the page to show the current year, no error state); `arrow keys move between tabs` (ArrowRight from Monthly selects Yearly and focuses it; ArrowLeft back); `tabs and panel are linked` (`aria-controls="cashflow-panel"`, panel `aria-labelledby` the selected tab, unselected tab `tabIndex -1`); `the month table has the heading Month by month`.

- [ ] **Step 2: Run them and see them fail**
  Run: `cd web && npx vitest run src/routes/router.test.tsx src/features/money/CashflowTabs.test.tsx`
  Expected: FAIL.

- [ ] **Step 3: Implement** the route search, the page reading and writing it, the fallback on `ApiError` with `code === "CASHFLOW_YEAR_OUT_OF_RANGE"` (from `web/src/api/client.ts`), tab ARIA and arrow keys, and the heading. Hover states with the sibling pattern (`AccountsPanel.tsx`): `transition-colors duration-[var(--transition-state)] hover:bg-canvas` on the unselected tab and the info button, `enabled:hover:bg-canvas` on the year buttons, `hover:underline` on month buttons.

- [ ] **Step 4: Run the frontend tests and typecheck**
  Run: `cd web && npx vitest run src/routes src/features/money/Cashflow && npx tsc --noEmit`
  Expected: PASS.

- [ ] **Step 5: Commit**
  ```bash
  git checkout -- web/package-lock.json
  git add web/src/routes/router.tsx web/src/routes/router.test.tsx web/src/features/money/CashflowPage.tsx web/src/features/money/CashflowYearly.tsx web/src/features/money/CashflowMonthly.tsx web/src/features/money/CashflowMonthTable.tsx web/src/features/money/cashflowCopy.ts web/src/features/money/CashflowTabs.test.tsx
  git commit -m "Keep the Cashflow tab and year in the address, and link tabs for screen readers"
  ```

### Task 6: Small polish

**Files:**
- Modify: `web/src/features/money/cashflowCopy.ts` (`keptCell`, new `lastFinishedLabel`), `CashflowMonthly.tsx` (the `cashflow-last-month` h2), `CashflowYearly.tsx` (picker visibility, Kept so far order, `.tabular` on bills to come)
- Test: create `web/src/features/money/CashflowPolish.test.tsx`; update any `keptCell` assertion in `CashflowPage.test.tsx`

**Interfaces:**
- Produces: `keptCell(2000) === "20.00%"`, `keptCell(3758) === "37.58%"`, `keptCell(-42) === "-0.42%"`, `keptCell(null) === "—"`; `keptLine` unchanged ("20% of income kept"). `lastFinishedLabel(month: string) => \`Last finished month: ${monthLabel(month)}\``.

- [ ] **Step 1: Write the failing tests:** `keptCell` values above; the last-finished heading reads "Last finished month: September 2026" with `text-[13px] font-semibold text-ink`; `hides the year arrows when there is only one year` (`oldestYear` equals the current year: no Previous/Next buttons) and shows both otherwise; `the Kept so far card shows this year's share before last year's comparison` (DOM order); bills-to-come amounts have `.tabular`.

- [ ] **Step 2: Run them and see them fail**
  Run: `cd web && npx vitest run src/features/money/CashflowPolish.test.tsx`
  Expected: FAIL.

- [ ] **Step 3: Implement** each item as the spec describes.

- [ ] **Step 4: Run the frontend tests and typecheck**
  Run: `cd web && npx vitest run src/features/money/Cashflow && npx tsc --noEmit`
  Expected: PASS.

- [ ] **Step 5: Commit**
  ```bash
  git checkout -- web/package-lock.json
  git add web/src/features/money/cashflowCopy.ts web/src/features/money/CashflowMonthly.tsx web/src/features/money/CashflowYearly.tsx web/src/features/money/CashflowPolish.test.tsx web/src/features/money/CashflowPage.test.tsx
  git commit -m "Polish Cashflow: last finished month label, two-decimal Kept, one-year picker, card order"
  ```

### Task 7: Walk it in a real browser, then the docs

**Files:**
- Modify: `docs/FEATURE_TRACKER.md` (new ✅ row under Cashflow, "Cashflow — how money out adds up from the ledger (no mockup)", plus summary recount), `docs/LEARNING.md` (the Cashflow entry), `docs/GUIDE.md` if it describes the month detail.

- [ ] **Step 1: Restart the stack's web and api containers** (`docker restart hearth-web-1 hearth-api-1`; Vite and air miss edits under colima) and confirm `lsof -nP -iTCP:5173` is the colima forward.

- [ ] **Step 2: Walk with gstack `/browse`** at http://localhost:5173 on the seeded household (local dev magic link via Mailpit): open September; the sum reads exactly as the spec's September table and its total equals the row's Money out; click the link and confirm "Spent in September" on the Transactions page equals the sum's first line; open October and see "Month 2 of 12 …" and "= Money out so far"; hover a bar (title text), check the legend and the cut-off mark on October; switch to Yearly, reload, still Yearly; pick last year if one exists, reload, still that year; at 375px open September and check `scrollWidth === clientWidth` on the table (Review Focus 5) and that the sum reads. Screenshots to `.gstack/browse-reports/2026-10-25-cashflow-fixes/`. No new console errors.

- [ ] **Step 3: Full checks**
  Run: `make lint && make test` (with the Global Constraints environment)
  Expected: everything green except `lint-vuln` (GO-2026-6809, red on `main`).

- [ ] **Step 4: Docs.** Tracker row and recount by script (split on unescaped pipes; the previous totals must reproduce before the edit). LEARNING: what the walk or tests caught, under an existing pattern where one fits.

- [ ] **Step 5: Commit**
  ```bash
  git checkout -- web/package-lock.json
  git add docs/FEATURE_TRACKER.md docs/LEARNING.md docs/GUIDE.md
  git commit -m "Record the Cashflow review fixes in the tracker and learning log"
  ```
