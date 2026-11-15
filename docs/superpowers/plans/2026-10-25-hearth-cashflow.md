# Cashflow — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a Money › Cashflow page. It shows money in, money out and
money left over for each month and each year, for the whole household, with
a green / yellow / red / grey light that always gives its reason.

**Architecture:**
- **Domain.** The arithmetic is a set of pure, integer-only functions in
  `internal/domain/cashflow_*.go`: classify a transaction, spread a bill
  payment, build the months, apply the grey rules, judge the light,
  summarise the year.
- **Service.** A new `CashflowService` in `internal/usecase/cashflow.go`
  makes one narrow history read, converts every amount into the primary
  currency, and calls the domain functions. It is computed on every read,
  with no cache.
- **Storage.** One new table, `cashflow_month_confirmations`, holds
  "Looks right, count it".
- **API.** One `GET /api/v1/cashflow?year=` serves the whole page.
  `PUT` / `DELETE /api/v1/cashflow/confirmations/{YYYY-MM}` add and undo a
  confirmation.
- **Frontend.** A new route at `/money/cashflow`. The month drill-down links
  to the ledger through a new `?month=` URL parameter.

**Tech Stack:**
- Go (version from `api/go.mod`), chi, pgx, sqlc, goose migrations, and
  testcontainers for the Postgres tests.
- React + TypeScript, TanStack Router and TanStack Query, zod, and Vitest
  with Testing Library.

**Spec:** `docs/superpowers/specs/2026-10-25-hearth-cashflow-design.md`,
approved by the product owner on 2026-10-25. Read it in full before Task 1.
It says what the page must do and why. This plan says where each piece goes
and how each one is proven.

## Global Constraints

- **Money is `int64` minor units plus an ISO 4217 code.** `float64` never
  appears on this path.
  - Percentages are whole **basis points** (1,900 = 19.00%).
  - The cushion is in **tenths of a month** (42 = 4.2 months).
  - Every *threshold* compares exact products: "kept under 10%" is
    `10·sumLeft < sumIn`. Never compare against a rounded percentage.
  - *Displayed* figures round half away from zero, using the domain's
    existing `mulDivRoundHalfAway`.
- **Convert each amount into primary first, then add** (LEARNING pattern 12).
  Only `Converter.TryConvert`'s `hasRate == false` may leave an amount out.
  Every other error fails the read.
- **Dependencies point inward** (`make lint-arch`):
  - `domain` imports only the standard library.
  - `usecase` may import `domain`.
  - Anything that uses pgx or chi lives in `adapter/**`.
- **Fail closed** on values the code did not construct. Every `switch` over a
  transaction kind, account type or cadence read from a row has a `default`
  that returns an error.
- **Authorisation lives at the HTTP edge only** (ADR 8). The cashflow routes
  sit in the `txn` group in `router.go`, which applies
  `requireCapability(domain.CapMoney)` and then `requireOwner`. The writes
  also go through `requireCSRF`. No service takes an actor.
  `confirmedByMembershipID` is data to store, as in `AgreementService.Propose`.
- **Every 2xx except 204 carries a JSON body.**
- **Wording lives in the frontend.** Go returns reason *codes*.
  `cashflowCopy.ts` turns them into sentences.
- **Wording rule (spec anti-goal 5):** never say "safe" or give advice. Green
  reads **"On track by common guidelines"**. Reasons state facts, for example
  "You kept 6% of income on average over the last 3 months".
- **The thresholds are exact values from the spec:**

  | Check | Value |
  |---|---|
  | Kept | 10% |
  | Cushion: red below | 1 month |
  | Cushion: yellow below | 3 months |
  | Bill-due window | 60 days |
  | Average window | last 3 finished months |
  | "Usual" | median money out of the last 6 solid months, applied once there are at least 3 |
  | "Under half of usual" | `out < median / 2` |

- **Comments state rules, not plan numbers or line numbers**
  (`make lint-comments`).
- **Every task ends green.** `go build ./...` and that task's tests pass. The
  frontend tasks also pass `npm run typecheck` (from `web/`).
- **Toolchain on this machine:**
  - Go is at `/Volumes/Oink_Machine/.local/opt/go-v1.24.2/bin`. Add it to
    `PATH`. `GOTOOLCHAIN=auto` fetches the version in `go.mod`.
  - The Postgres tests need:
    `export DOCKER_HOST=unix:///Volumes/Oink_Machine/.colima/default/docker.sock`
    `export TESTCONTAINERS_DOCKER_SOCKET_OVERRIDE=/var/run/docker.sock`
- **Before Task 1,** create branch `feat/cashflow` from `main`. Commit the
  spec and this plan as its first commit:
  `docs: cashflow design and implementation plan`.

## Behaviour calls settled while planning

The architect flagged these, and the planner settled them in the direction
that never flatters a household. Don't change them without a reason.

1. **Spreading follows the ledger.** A yearly or quarterly bill payment is
   spread from its *linked transaction's current amount and date*. If that
   expense is deleted on the Transactions page, the link becomes NULL and
   nothing is spread. Cashflow keeps matching the ledger.
2. **A spread payment with no exchange rate turns every month it covers
   grey,** not only the month it was paid.
3. **A loan transfer counts `Amount`,** the figure that left the source
   account.
4. **The cushion divides by the same window as the 3-month average,** with
   grey months dropped.
5. **A due quarterly or yearly bill that can't be converted fires the yellow
   bill-due check,** instead of being skipped.

## Review Focus

These are the failures most likely to bite a real person, most likely first.
Each one is pinned by a named test in the task that owns the code.

1. **A household whose history starts this month** has no finished month.
   The light must show **grey with reason `no_finished_month`**, never green,
   and nothing may crash or divide by zero. Pinned in Task 3
   (`TestJudgeIsGreyWithNoFinishedMonth`) and Task 6
   (`TestPageForAHouseholdThatStartedThisMonth`).
2. **Exact boundaries.**
   - Kept of 999 out of 10,000 (9.99%) is **yellow**. Exactly 10% is not
     yellow.
   - A cushion of exactly 3.0 months is **not** yellow. 2.99 months is.
   Pinned in Task 3 (`TestJudgeBoundaries`).
3. **An IDR transaction with no rate** turns its month **grey**, with "IDR"
   listed in `unconvertibleCurrencies`. A spread payment with no rate greys
   every month it covers. Pinned in Task 2
   (`TestUnconvertibleSpreadGreysEveryCoveredMonth`) and Task 6
   (`TestPageGreysAMonthWithAnUnconvertibleTransaction`).
4. **A yearly bill marked paid whose expense was later deleted or edited in
   the ledger:** spreading follows the ledger, so there is no double count and
   no ghost payment. Pinned in Task 5
   (`TestCashflowRowsFollowTheLedgerAfterAnExpenseIsDeleted`).
5. **A year outside the history, or confirming an unfinished month:**
   - `?year=1999`, a future year, or a year before the oldest transaction
     gets **422 `CASHFLOW_YEAR_OUT_OF_RANGE`**. `?year=abc` gets **400**.
   - Confirming the current month, a future month or a month before
     2000-01 gets **422 `CASHFLOW_MONTH_OUT_OF_RANGE`**.
   - None of these may answer with a 500 or an empty 200.
   Pinned in Task 7 (`TestCashflowYearOutOfRange`,
   `TestConfirmRefusesUnfinishedMonth`).

---

### Task 1: Classify a transaction and spread a payment (domain)

**Files:**
- Create: `api/internal/domain/cashflow_classify.go`
- Create: `api/internal/domain/cashflow_classify_test.go`

**Interfaces:**
- Produces:
  ```go
  type CashflowRole int
  const (
      CashflowIgnore    CashflowRole = iota // transfers other than into a loan
      CashflowIn                            // income
      CashflowOut                           // expense, counted in its own month
      CashflowLoanOut                       // transfer into a loan account
      CashflowSpreadOut                     // expense linked to a quarterly/yearly bill payment
  )
  // cadence is "" when the transaction is not linked to a bill payment.
  // toType is "" unless kind is a transfer.
  func ClassifyCashflow(kind TransactionKind, toType AccountType, cadence Cadence) (CashflowRole, error)
  // SpreadMonths is 12 for yearly, 3 for quarterly, 1 for anything else.
  func SpreadMonths(c Cadence) int
  // SpreadForward splits amountMinor into months parts using integer
  // division. The remainder goes to the first part, so the parts add up
  // exactly to amountMinor.
  func SpreadForward(amountMinor int64, months int) []int64
  var ErrUnknownCashflowInput = errors.New("unrecognised cashflow input")
  ```

- [ ] **Step 1: Write the failing tests** in `cashflow_classify_test.go`:
  - `TestClassifyCashflow`: a table-driven test.
    - `(income, "", "")` → `CashflowIn`
    - `(expense, "", "")` → `CashflowOut`
    - `(expense, "", monthly)` → `CashflowOut`
    - `(expense, "", one_off)` → `CashflowOut`
    - `(expense, "", quarterly)` → `CashflowSpreadOut`
    - `(expense, "", yearly)` → `CashflowSpreadOut`
    - `(transfer, loan, "")` → `CashflowLoanOut`
    - `(transfer, credit_card, "")` → `CashflowIgnore`
    - `(transfer, cash, "")` → `CashflowIgnore`
    - `(transfer, investment, "")` → `CashflowIgnore`
  - `TestClassifyCashflowRefusesUnknownInput`. Each case must give
    `errors.Is(err, ErrUnknownCashflowInput)`:
    - kind `"refund"`
    - transfer with `toType ""`
    - transfer with `toType "boat"`
    - expense with cadence `"weekly"`
  - `TestSpreadForward`:
    - `SpreadForward(210000, 12)` gives twelve parts of `17500`.
    - `SpreadForward(100, 12)` gives `[12, 8, 8, …, 8]`, which sums to 100.
    - `SpreadForward(100, 3)` gives `[34, 33, 33]`.
    - `SpreadForward(-1200, 12)` gives twelve parts of `-100`. A refund on a
      bill spreads too.
  - `TestSpreadMonths`: yearly → 12, quarterly → 3, monthly → 1, one-off → 1.

- [ ] **Step 2: Run them and confirm they fail**
  Run: `cd api && go test ./internal/domain -run 'Cashflow|Spread' -v`
  Expected: FAIL, with undefined `ClassifyCashflow`.

- [ ] **Step 3: Implement** `ClassifyCashflow`, `SpreadMonths` and
  `SpreadForward` to the signatures above.
  - Each `switch` has a `default` that wraps `ErrUnknownCashflowInput` and
    names the value.
  - For a negative amount, the remainder (from Go's truncating `%`) still
    goes to the first part.
  - The doc comment on `CashflowLoanOut` states the owner's rule: money paid
    to a loan is money the household can't save. It also states why a
    transfer into a credit card is ignored: the card's purchases were already
    counted.

- [ ] **Step 4: Run the tests and confirm they pass** (same command).

- [ ] **Step 5: Commit**
  `git commit -m "Classify cashflow transactions and spread bill payments"`

---

### Task 2: Build the months and apply the grey rules (domain)

**Files:**
- Create: `api/internal/domain/cashflow_month.go`: items → per-month figures.
- Create: `api/internal/domain/cashflow_grey.go`: grey rules and the "usual"
  median.
- Test: `api/internal/domain/cashflow_month_test.go` and
  `api/internal/domain/cashflow_grey_test.go`

**Interfaces:**
- Consumes: `CashflowRole`, `SpreadForward` and `SpreadMonths` from Task 1.
- Produces:
  ```go
  type CashflowItem struct {
      OccurredOn    time.Time
      Role          CashflowRole
      AmountMinor   int64  // already in the primary currency; 0 when Unconvertible != ""
      Unconvertible string // the original currency code when there is no rate, else ""
      Spread        int    // SpreadMonths(cadence); 1 unless Role is CashflowSpreadOut
      Name          string // bill name or loan account name, for Contributions
  }
  type CashflowStatus string
  const (
      CashflowSolid   CashflowStatus = "solid"
      CashflowGrey    CashflowStatus = "grey"
      CashflowCurrent CashflowStatus = "current"
      CashflowFuture  CashflowStatus = "future"
      CashflowNone    CashflowStatus = "none" // before the household's first transaction
  )
  type GreyReason string
  const (
      GreyNoIncome      GreyReason = "no_income"
      GreyUnderUsual    GreyReason = "under_half_usual"
      GreyUnconvertible GreyReason = "unconvertible"
  )
  type CashflowContribution struct {
      Kind        string // "spread_bill" | "loan"
      Name        string
      AmountMinor int64  // this month's share
      PaidOn      time.Time
  }
  type CashflowMonth struct {
      Month                   time.Time // first of the month, UTC
      Status                  CashflowStatus
      InMinor, OutMinor       int64     // OutMinor includes SpreadMinor and LoanMinor
      SpreadMinor, LoanMinor  int64
      TransactionCount        int
      Grey                    []GreyReason
      UnconvertibleCurrencies []string  // sorted, unique
      Confirmed               bool
      Contributions           []CashflowContribution
  }
  func (m CashflowMonth) LeftMinor() int64
  // BuildCashflowMonths returns one month for each calendar month from first
  // through current, inclusive. current gets Status CashflowCurrent; every
  // earlier month gets CashflowSolid, until MarkCashflowGrey runs.
  func BuildCashflowMonths(items []CashflowItem, first, current time.Time) ([]CashflowMonth, error)
  // MarkCashflowGrey walks the finished months from oldest to newest and
  // sets Grey, Status and Confirmed.
  func MarkCashflowGrey(months []CashflowMonth, confirmed map[time.Time]bool)
  ```

- [ ] **Step 1: Write the failing month tests.**
  - `TestBuildMonthsCountsInOutAndLoan`: one month holds income 1,000,000,
    expense 600,000 and a loan-out of 180,000.
    - Expect `InMinor` 1,000,000, `OutMinor` 780,000, `LoanMinor` 180,000,
      `LeftMinor()` 220,000.
    - Expect one contribution of kind `loan`.
  - `TestBuildMonthsSpreadsAYearlyPaymentForward`: a yearly payment of
    210,000 on 2026-03-14, with `first` = 2026-01 and `current` = 2026-10.
    - March to October each have `SpreadMinor` 17,500.
    - January and February have 0.
    - March's `OutMinor` does **not** include the 210,000 lump.
  - `TestBuildMonthsSpreadIntoTheFutureIsCutAtCurrent`: a payment in
    2026-09 with current = 2026-10.
    - Only two months come back, and each has a 17,500 share.
    - No month after `current` is returned.
  - `TestBuildMonthsCountsEveryTransactionInTransactionCount`: an ignored
    transfer still counts toward `TransactionCount`, because it is what the
    ledger shows.
  - `TestBuildMonthsFailsOnOverflow`: two items of `math.MaxInt64 / 2 + 1`
    in one month must return `ErrAmountOverflow`. Use the domain's existing
    overflow sentinel.

- [ ] **Step 2: Write the failing grey tests.**
  - `TestGreyWhenNoIncome`: a finished month with expenses but no income →
    `Grey == [no_income]` and `Status` grey.
  - `TestUnderHalfOfUsualAppliesOnlyAfterThreeSolidMonths`:
    - With 2 earlier solid months of out 400,000, a month with out 100,000
      stays solid.
    - With 3 earlier solid months, the same month is grey `under_half_usual`.
  - `TestUsualIsTheMedianOfTheLastSixSolidMonths`:
    - With outs `[100, 400, 400, 400, 400, 400, 1000]` (oldest first, all
      solid with income), a month with out 199 is grey, because the median of
      the last six is 400 and 199 < 200. A month with out 200 is solid.
    - With an even count, the middle pair `a`, `b` makes the month grey when
      `4·out < a + b`.
  - `TestUsualSkipsGreyMonths`: a grey month between solid ones does not
    enter the median.
  - `TestConfirmationClearsNoIncomeAndUnderUsualButNotUnconvertible`:
    - A confirmed month with `no_income` becomes solid and `Confirmed` true.
    - A confirmed month with `unconvertible` stays grey.
  - `TestUnconvertibleSpreadGreysEveryCoveredMonth`: a yearly item with
    `Unconvertible: "IDR"` paid in 2026-03 makes March to October grey
    `unconvertible`, each listing `"IDR"`.
  - `TestCurrentMonthIsNeverGrey`: the current month with no income keeps
    `Status` current and an empty `Grey`.

- [ ] **Step 3: Run both test files and confirm they fail**
  Run: `cd api && go test ./internal/domain -run 'BuildMonths|Grey|Usual|Confirmation|CurrentMonth|Unconvertible' -v`

- [ ] **Step 4: Implement** `BuildCashflowMonths` and `LeftMinor` in
  `cashflow_month.go`, and `MarkCashflowGrey` in `cashflow_grey.go`.
  - Month keys are `time.Date(y, m, 1, 0, 0, 0, 0, time.UTC)`.
  - Sums use checked addition and return the overflow sentinel.
  - The median compares exactly, with no division. With an odd count and
    middle `m`, a month is grey when `2·out < m`. With an even count it is
    `4·out < a + b`.
  - An item with `Unconvertible` set adds its currency, and the
    `unconvertible` reason, to every month it covers (its own month, or all
    of its spread months), and adds no amount.

- [ ] **Step 5: Run the tests and confirm they pass.**

- [ ] **Step 6: Commit**
  `git commit -m "Build cashflow months and mark months with too little data"`

---

### Task 3: Averages, cushion, the light and the year (domain)

**Files:**
- Create: `api/internal/domain/cashflow_light.go`: averages, basis points,
  cushion, judging the light.
- Create: `api/internal/domain/cashflow_year.go`: the year summary and
  "heading for".
- Test: `api/internal/domain/cashflow_light_test.go` and
  `api/internal/domain/cashflow_year_test.go`

**Interfaces:**
- Consumes: `CashflowMonth`, `CashflowStatus` and `GreyReason` from Task 2.
- Produces:
  ```go
  type CashflowAverage struct{ Months int; SumIn, SumOut int64 } // solid months only
  func (a CashflowAverage) SumLeft() int64
  // CashflowAverageOf takes the three months before current. Grey months
  // drop out, so Months may be 0, 1, 2 or 3.
  func CashflowAverageOf(months []CashflowMonth, current time.Time) CashflowAverage
  func KeptBasisPoints(leftMinor, inMinor int64) (int64, bool)     // false when inMinor <= 0
  func CushionTenths(cashMinor, sumOut int64, n int) (int64, bool) // false when n == 0 or sumOut <= 0

  type CushionReason string // "" | "no_cash_accounts" | "unconvertible_account" | "no_solid_months" | "no_spending"
  type CashCushion struct{ Known bool; CashMinor int64; Reason CushionReason }
  type BillDue struct{ Name string; AmountMinor int64; Unconvertible bool; NextDue time.Time }

  type LightColour string // "grey" | "red" | "yellow" | "green"
  type LightReason struct {
      Code            string // see the table in Step 3
      KeptBasisPoints *int64
      CushionTenths   *int64
      BillName        string
  }
  type CashflowVerdict struct{ Colour LightColour; Reasons []LightReason } // the winning colour's reasons only
  // hasFinished is false when the household's history started in the current month.
  func JudgeCashflow(last CashflowMonth, hasFinished bool, avg CashflowAverage, c CashCushion, dueSoon []BillDue) CashflowVerdict

  type CashflowYearMonth struct {
      CashflowMonth
      ProjectedLeftMinor *int64 // only for future months of the current year, when avg.Months > 0
  }
  type CashflowYear struct {
      Year                    int
      Months                  [12]CashflowYearMonth
      KeptSoFarMinor          int64  // sum of LeftMinor over finished months in the year, grey ones included
      IncomeMinor             int64
      GreyMonths              int
      SameMonthsLastYearMinor *int64 // nil when last year has no data in those months
      HeadingForMinor         *int64 // current year only, when avg.Months > 0
  }
  func SummariseCashflowYear(months []CashflowMonth, year int, current time.Time, avg CashflowAverage) CashflowYear
  ```

- [ ] **Step 1: Write the failing light tests.** Build fixtures with a small
  helper `solidMonth(in, out int64)`.
  - `TestJudgeIsGreyWithNoFinishedMonth` (Review Focus 1):
    `hasFinished=false` → grey, reason `no_finished_month`.
  - `TestJudgeIsGreyWhenLastFinishedMonthIsGrey`: reason
    `last_month_not_enough_data`.
  - `TestJudgeIsGreyWhenNoSolidMonthToAverage`: `avg.Months == 0`, reason
    `no_months_to_average`.
  - `TestJudgeRedWhenAverageLeftIsZeroOrNegative`: sumIn equals sumOut →
    red, `left_not_positive`.
  - `TestJudgeRedWhenCushionUnderOneMonth`.
  - `TestJudgeBoundaries` (Review Focus 2). Use avg n=3 throughout.
    - sumIn 10,000, sumLeft 999 → yellow `kept_under_10`.
    - sumIn 10,000, sumLeft 1,000 → not `kept_under_10`.
    - Cushion: cash 3·sumOut/n exactly → not `cushion_under_3`. One minor
      unit less → `cushion_under_3`.
  - `TestJudgeYellowWhenCushionUnknown`: `Known=false` → yellow
    `cushion_unknown`, never green.
  - `TestJudgeYellowWhenLastMonthNegative`: the average is healthy but last
    month's `LeftMinor() < 0` → yellow.
  - `TestJudgeYellowWhenADueBillExceedsUsualLeftOver`:
    - A bill of 210,000 against an average left of 190,000 → yellow
      `bill_due_soon`, carrying the bill's name.
    - A bill with `Unconvertible` set → yellow too.
  - `TestJudgeWorstColourWinsAndKeepsOnlyItsReasons`: red and yellow
    conditions both hold → red, with only the red reasons.
  - `TestJudgeGreen`: kept 19%, cushion 4.2 months, nothing due → green with
    no reasons.
  - `TestKeptBasisPointsRoundsHalfAway`: left 1,999 of in 10,000 → 1,999.
    Left 1 of in 3 → 3,333.
  - `TestCushionTenths`: cash 84,000, sumOut 60,000, n=3 → 42.

- [ ] **Step 2: Write the failing year tests.**
  - `TestYearKeptSoFarIncludesGreyMonthsAndCountsThem`: 9 finished months,
    one of them grey. `KeptSoFarMinor` is the sum of all 9 and
    `GreyMonths` is 1.
  - `TestYearHeadingForHasNoBillSubtraction`. Current is 2026-10. Kept so far
    over January to September is 1,706,000. The average sumLeft is 570,000
    over 3 months.
    - `HeadingForMinor` = 1,706,000 + 3 × 190,000 = **2,276,000**. October,
      November and December are the remaining three months.
    - This case pins the spec's correction: nothing is subtracted for yearly
      bills still due.
  - `TestYearFutureMonthsCarryProjectionOnlyInTheCurrentYear`:
    - November and December are `future` with `ProjectedLeftMinor` 190,000.
    - October is `current`.
    - A finished year has no future months and a nil `HeadingForMinor`.
  - `TestYearMonthsBeforeHistoryAreNone`: history starts 2026-04, so
    January to March 2026 are `none`.
  - `TestYearSameMonthsLastYearComparesLikeForLike`: January to September
    2025 sums to 1,420,000 → `*SameMonthsLastYearMinor == 1,420,000`. 2025's
    October to December are not included.
  - `TestYearWithNoAverageHasNoHeadingFor`: `avg.Months == 0` →
    `HeadingForMinor` is nil.

- [ ] **Step 3: Run them and confirm they fail**
  Run: `cd api && go test ./internal/domain -run 'Judge|Kept|Cushion|Year' -v`

- [ ] **Step 4: Implement** both files. The reason codes are fixed. The
  frontend copy (Task 10) keys on them.

  | Colour | Codes (any one fires) |
  |---|---|
  | grey | `no_finished_month`, `last_month_not_enough_data`, `no_months_to_average` |
  | red | `left_not_positive` (`sumIn - sumOut <= 0`), `cushion_under_1` (`cash·n < sumOut`) |
  | yellow | `kept_under_10` (`10·sumLeft < sumIn`), `last_month_negative`, `cushion_under_3` (`cash·n < 3·sumOut`), `cushion_unknown`, `bill_due_soon` (`amount·n > sumLeft`, or the bill is unconvertible) |
  | green | none fired |

  - Every product comparison goes through one unexported 128-bit helper in
    `cashflow_light.go`, built on `math/bits`, so a large household can't
    overflow a comparison.
  - "Heading for" computes `remaining·sumLeft / n` in one
    `mulDivRoundHalfAway` call, so it rounds once.

- [ ] **Step 5: Run the tests and confirm they pass.**
  Mutation-check one case. Change `<` to `<=` in the `kept_under_10`
  comparison and confirm `TestJudgeBoundaries` fails. Then revert the change.

- [ ] **Step 6: Commit**
  `git commit -m "Judge the cashflow light and summarise the year"`

---

### Task 4: Confirmations table and repository

**Files:**
- Create: `api/migrations/00025_cashflow_confirmations.sql`
- Create: `api/internal/adapter/postgres/queries/cashflow.sql` (the
  confirmation queries; Task 5 adds the row query to the same file)
- Create: `api/internal/adapter/postgres/cashflow_confirmation_repo.go`
- Create: `api/internal/usecase/ports_money_cashflow.go` (the confirmation
  part; Task 5 and Task 6 add to it)
- Modify: `api/internal/usecase/ports.go`: add the new ports file to its
  list.
- Modify: `api/internal/domain/errors.go`: add `ErrCashflowMonthOutOfRange`
  and `ErrCashflowYearOutOfRange`.
- Test: `api/internal/adapter/postgres/cashflow_confirmation_repo_test.go`

**Interfaces:**
- Produces:
  ```go
  type CashflowConfirmation struct{ Month time.Time; ConfirmedByName string; ConfirmedAt time.Time }
  type CashflowConfirmationRepository interface {
      List(ctx context.Context, householdID string) ([]CashflowConfirmation, error)
      // Confirm is idempotent: an existing row comes back unchanged, so the
      // first person to confirm is the one kept.
      Confirm(ctx context.Context, householdID string, month time.Time, membershipID string, at time.Time) (CashflowConfirmation, error)
      // Unconfirm treats a missing row as success, not as ErrNotFound.
      Unconfirm(ctx context.Context, householdID string, month time.Time) error
  }
  func NewCashflowConfirmationRepo(db *DB) *CashflowConfirmationRepo // in package postgres
  ```

- [ ] **Step 1: Write the migration.** Table
  `cashflow_month_confirmations`:

  | Column | Definition |
  |---|---|
  | `household_id` | `uuid NOT NULL REFERENCES households(id) ON DELETE CASCADE` |
  | `month` | `date NOT NULL CHECK (month = date_trunc('month', month)::date AND month >= DATE '2000-01-01')` |
  | `confirmed_by_membership_id` | `uuid REFERENCES memberships(id) ON DELETE SET NULL` |
  | `confirmed_at` | `timestamptz NOT NULL DEFAULT now()` |

  - `PRIMARY KEY (household_id, month)`.
  - The Down migration is `DROP TABLE`.
  - A SQL comment states the ADR 13 ceiling: the service only accepts months
    from 2000-01 up to the last finished month, so a household holds at most
    one row per month in that window. No `LimitKind` is needed.

- [ ] **Step 2: Write the queries** `ListCashflowConfirmations :many`,
  `ConfirmCashflowMonth :one` and `UnconfirmCashflowMonth :exec`.
  - The list joins memberships and users to get `display_name`. A
    confirmation whose member left gives `""`.
  - Confirm is `INSERT … ON CONFLICT (household_id, month) DO UPDATE SET month = EXCLUDED.month RETURNING …`,
    so the original confirmer is returned.
  - Run `make sqlc`.

- [ ] **Step 3: Write the failing repository tests**, using the same
  testcontainers harness as `bill_repo_test.go`:
  - `TestConfirmIsIdempotentAndKeepsTheFirstConfirmer`: confirm as member A,
    then as member B. Both calls return A's name and A's `confirmed_at`.
  - `TestUnconfirmOfAMissingMonthIsNotAnError`.
  - `TestConfirmationsAreScopedToTheirHousehold`: household Y's list doesn't
    show household X's confirmation.
  - `TestTheDatabaseRefusesAMidMonthDate`: a direct insert of `2026-03-14`
    violates the CHECK constraint.

- [ ] **Step 4: Run them and confirm they fail**
  Run: `cd api && go test ./internal/adapter/postgres -run 'Confirm|Unconfirm|Confirmations|MidMonth' -v`

- [ ] **Step 5: Implement** `CashflowConfirmationRepo`. pgx types stay
  inside the adapter.

- [ ] **Step 6: Run the tests and confirm they pass.** Also run
  `make lint-arch`.

- [ ] **Step 7: Commit**
  `git commit -m "Store cashflow month confirmations"`

---

### Task 5: History row read

**Files:**
- Modify: `api/internal/adapter/postgres/queries/cashflow.sql`: add
  `ListCashflowRows :many`.
- Create: `api/internal/adapter/postgres/cashflow_row_repo.go`
- Modify: `api/internal/usecase/ports_money_cashflow.go`
- Test: `api/internal/adapter/postgres/cashflow_row_repo_test.go`

**Interfaces:**
- Produces:
  ```go
  type CashflowRow struct {
      TransactionID string
      OccurredOn    time.Time
      Kind          domain.TransactionKind
      Amount        domain.Money       // for a transfer, the from side: what left
      ToAccountType domain.AccountType // "" unless Kind is a transfer
      ToAccountName string
      BillCadence   domain.Cadence     // "" when not linked to a bill payment
      BillName      string
  }
  type CashflowRowReader interface {
      // Rows returns every transaction in the household, oldest first. The
      // count is bounded by domain.MaxTransactions. Kind, account type and
      // cadence are parsed with the domain Parse* functions, which refuse
      // unknown values.
      Rows(ctx context.Context, householdID string) ([]CashflowRow, error)
  }
  func NewCashflowRowRepo(db *DB) *CashflowRowRepo
  ```

- [ ] **Step 1: Write the query.** It selects `t.id, t.occurred_on, t.kind,
  t.amount_minor, t.amount_currency, ta.type, ta.nickname, b.cadence,
  b.name` from `transactions t`:
  - `LEFT JOIN accounts ta ON ta.id = t.to_account_id`
  - `LEFT JOIN bill_payments bp ON bp.transaction_id = t.id AND bp.household_id = t.household_id`
  - `LEFT JOIN bills b ON b.id = bp.bill_id AND b.household_id = t.household_id`
  - `WHERE t.household_id = $1 ORDER BY t.occurred_on, t.id`

  Check the column names against the migrations. Then run `make sqlc`.

- [ ] **Step 2: Write the failing tests.**
  - `TestCashflowRowsCarryDestinationTypeAndBillCadence`:
    - A transfer into a loan account gives `ToAccountType` `loan` and the
      loan's name.
    - A paid yearly bill gives a row with `BillCadence` yearly and the bill's
      name.
    - A plain expense gives empty strings in both.
  - `TestCashflowRowsExpenseSumMatchesMonthTotals`. Seed a single-currency
    month. The sum of its expense rows' `Amount` must equal the sum of the
    expense amounts that `TransactionRepository.MonthTotals` returns for the
    same month. This is the guard against two drifting ways of reading a
    month (ADR 13's concern).
  - `TestCashflowRowsFollowTheLedgerAfterAnExpenseIsDeleted` (Review
    Focus 4):
    - Pay a yearly bill, then delete its expense through the transaction
      repository. No row comes back for it.
    - Pay again and edit the expense amount. The row carries the edited
      amount.
  - `TestCashflowRowsRefuseADuplicatedTransaction`: if the join yields the
    same transaction id twice, `Rows` returns an error rather than counting
    a payment twice. Create the duplicate by inserting a second
    `bill_payments` row that links to the same transaction, directly in SQL.
  - `TestCashflowRowsAreScopedToTheirHousehold`.

- [ ] **Step 3: Run them and confirm they fail**
  Run: `cd api && go test ./internal/adapter/postgres -run CashflowRows -v`

- [ ] **Step 4: Implement** `CashflowRowRepo.Rows`. Parse every enum column
  with the domain `Parse*` functions, and return their error on an unknown
  value. Return an error naming the id if the same transaction id appears
  twice in a row.

- [ ] **Step 5: Run the tests and confirm they pass.**

- [ ] **Step 6: Commit**
  `git commit -m "Read a household's cashflow history in one query"`

---

### Task 6: CashflowService

**Files:**
- Create: `api/internal/usecase/cashflow.go`
- Modify: `api/internal/usecase/ports_money_cashflow.go`: add
  `CashflowAccounts` and `CashflowBills`.
- Modify: `api/internal/usecase/testdouble_test.go`: add in-memory doubles
  for the two new repositories.
- Test: `api/internal/usecase/cashflow_test.go`

**Interfaces:**
- Consumes: Tasks 1–5.
- Produces:
  ```go
  type CashflowAccounts interface{ List(ctx context.Context, householdID string, includeArchived bool) ([]AccountView, error) }
  type CashflowBills    interface{ List(ctx context.Context, householdID string, includeArchived bool) ([]BillRecord, error) }
  type CashflowDeps struct {
      Households    HouseholdRepository // the same port TransactionDeps and BudgetDeps carry, for PrimaryCurrency
      Rows          CashflowRowReader
      Confirmations CashflowConfirmationRepository
      Accounts      CashflowAccounts
      Bills         CashflowBills
      FX            FXRateProvider
  }
  type CashflowBillToCome struct{ BillID, Name string; AmountMinor int64; NextDue time.Time; Unconvertible bool }
  type CashflowPage struct {
      Currency        string
      HasTransactions bool
      OldestYear      int // 0 when HasTransactions is false
      Light           domain.CashflowVerdict
      Average         domain.CashflowAverage
      Cushion         domain.CashCushion
      CushionTenths   *int64
      LastFinished    *domain.CashflowMonth // nil when history started this month
      Current         domain.CashflowMonth
      Recent          [12]domain.CashflowMonth // ending at Current; padded with CashflowNone
      Year            domain.CashflowYear
      BillsToCome     []CashflowBillToCome // only when Year.Year is the current year
      Confirmations   map[time.Time]CashflowConfirmation
  }
  func NewCashflowService(d CashflowDeps) *CashflowService
  // year 0 means the current year.
  func (s *CashflowService) Page(ctx context.Context, householdID string, today time.Time, year int) (CashflowPage, error)
  func (s *CashflowService) Confirm(ctx context.Context, householdID string, month time.Time, confirmedByMembershipID string, today time.Time) (CashflowConfirmation, error)
  func (s *CashflowService) Unconfirm(ctx context.Context, householdID string, month time.Time, today time.Time) error
  ```

- [ ] **Step 1: Write the failing tests** against the in-memory doubles:
  - `TestPageForAnEmptyHousehold`: `HasTransactions` false, the light grey,
    no error.
  - `TestPageForAHouseholdThatStartedThisMonth` (Review Focus 1):
    `LastFinished` nil, light grey `no_finished_month`.
  - `TestPageMoneyOutMatchesMonthSummaryAfterAdjustments`. A mixed SGD/IDR
    month with a rate, one yearly bill payment and one loan transfer. Assert:
    `OutMinor − SpreadMinor − LoanMinor + (the yearly payment's converted lump)`
    equals `TransactionService.MonthSummary(...).Spent.Amount` for the same
    month.
  - `TestPageGreysAMonthWithAnUnconvertibleTransaction` (Review Focus 3): an
    IDR expense with no rate → the month is grey with
    `UnconvertibleCurrencies == ["IDR"]`. Its amount is not added.
  - `TestPageCushionUsesOnlyCashAccountsThatCountTowardNetWorth`. Accounts:
    - cash S$8,400, counts toward net worth
    - cash S$5,000, doesn't count
    - investment S$50,000
    - archived cash S$1,000

    Only S$8,400 enters `Cushion.CashMinor`.
  - `TestPageCushionUnknownWithNoCashAccounts` → reason `no_cash_accounts`.
    `TestPageCushionUnknownWhenACashAccountCannotConvert` → reason
    `unconvertible_account`.
  - `TestPageDueSoonTakesLiveQuarterlyAndYearlyBillsWithin60Days`:
    - A yearly bill due on today+60 is in the list. One due on today+61 is
      not.
    - A monthly bill is never in the list. An archived bill is not.
  - `TestPageBillsToComeOnlyForTheCurrentYear`.
  - `TestPageYearOutOfRange`: year 1999, year today+1, and a year before the
    oldest transaction each return `ErrCashflowYearOutOfRange`.
  - `TestPageAppliesConfirmations`: a confirmed `no_income` month is solid.
  - `TestConfirmRefusesMonthsOutsideTheWindow`: the current month, next
    month, 1999-12 and a mid-month date each return
    `ErrCashflowMonthOutOfRange`. Last month succeeds and stores the given
    membership id.
  - `TestUnconfirmRefusesTheSameWindow`.
  - `TestPagePropagatesAnFXFailure`: a provider error other than "no rate"
    fails `Page`. It must not grey the month.

- [ ] **Step 2: Run them and confirm they fail**
  Run: `cd api && go test ./internal/usecase -run 'Page|Confirm|Unconfirm' -v`

- [ ] **Step 3: Implement** `CashflowService`. `Page` works in this order:
  1. Read the household's primary currency.
  2. Read `Rows`. If there are none, return the empty page.
  3. Build a `NewConverter` once. For each row, call `TryConvert`. With no
     rate, set `Unconvertible`. Call `ClassifyCashflow` and set `Spread`.
     Use the bill name or the loan account name as `Name`.
  4. Call `BuildCashflowMonths` from the oldest row's month to today's month.
     Then call `MarkCashflowGrey` with the confirmations.
  5. Call `CashflowAverageOf`. Work out the cushion from `Accounts.List`,
     keeping non-archived `cash` accounts with `CountTowardNetWorth`,
     converted with `TryConvert`. Then call `CushionTenths`.
  6. Build the due-soon list from `Bills.List(…, false)`: quarterly and
     yearly bills whose `NextDue` is within `[today, today+60 days]`.
  7. Call `JudgeCashflow`, `SummariseCashflowYear`, and build
     `BillsToCome`.

  `Confirm` and `Unconfirm` check the window first: first-of-month,
  ≥ 2000-01-01, and before the first of today's month. Only then do they call
  the repository.

- [ ] **Step 4: Run the tests and confirm they pass.** Also run
  `make lint-arch`.

- [ ] **Step 5: Commit**
  `git commit -m "Add CashflowService"`

---

### Task 7: HTTP handlers, routes and wiring

**Files:**
- Create: `api/internal/adapter/http/cashflow_handlers.go`: the three
  handlers and their JSON shapes.
- Modify: `api/internal/adapter/http/router.go`:
  - Add `Deps.Cashflow`.
  - Add `txn.Get("/cashflow", …)` to the `txn` group.
  - Add `PUT` and `DELETE /cashflow/confirmations/{month}` in their own
    `requireCSRF` subgroup. The subgroup's comment follows the budgets
    subgroup's reasoning.
- Modify: the domain-error mapping that `MapDomainError` uses, so
  `ErrCashflowYearOutOfRange` → 422 `CASHFLOW_YEAR_OUT_OF_RANGE` and
  `ErrCashflowMonthOutOfRange` → 422 `CASHFLOW_MONTH_OUT_OF_RANGE`.
- Modify: `api/cmd/api/main.go` to wire the repositories and the service.
  Also update the test `Deps` builder the http tests use (LEARNING
  pattern 23).
- Test: `api/internal/adapter/http/cashflow_api_test.go`

**Interfaces:**
- Consumes: `CashflowService` from Task 6. In the handlers, `scope.Today` is
  `today` and `scope.Membership.ID` is the confirmer. Parse `{month}` with
  the existing `parseBudgetMonth` helper.
- Produces the response the frontend schema (Task 9) parses.

  `GET /api/v1/cashflow?year=2026` returns:
  ```json
  {"currency":"SGD","hasTransactions":true,"oldestYear":2024,
   "light":{"colour":"yellow","reasons":[{"code":"kept_under_10","keptBasisPoints":600,"cushionTenths":null,"billName":""}]},
   "average":{"months":3,"inMinor":0,"outMinor":0,"leftMinor":0,"keptBasisPoints":1200},
   "cushion":{"known":true,"tenths":42,"reason":""},
   "lastFinished":MONTH_OR_NULL,"current":MONTH,"recent":[MONTH, … 12],
   "year":{"year":2026,"months":[MONTH, … 12],"keptSoFarMinor":0,"incomeMinor":0,"greyMonths":1,
           "keptBasisPoints":1600,"sameMonthsLastYearMinor":null,"headingForMinor":null,
           "billsToCome":[{"billId":"…","name":"Car insurance","amountMinor":210000,"nextDue":"2026-12-01","unconvertible":false}]}}
  ```

  Each `MONTH` is:
  ```json
  {"month":"2026-09","status":"solid","inMinor":0,"outMinor":0,"leftMinor":0,"keptBasisPoints":null,
   "spreadMinor":0,"loanMinor":0,"transactionCount":6,"grey":[],"unconvertibleCurrencies":[],
   "projectedLeftMinor":null,"confirmation":null,
   "contributions":[{"kind":"spread_bill","name":"Car insurance","amountMinor":17500,"paidOn":"2026-03-14"}]}
  ```

  The `average` figures are per-month averages, each rounded once from the
  sums. `year.keptBasisPoints` is
  `KeptBasisPoints(KeptSoFarMinor, IncomeMinor)`, or null. `year.billsToCome`
  is `CashflowPage.BillsToCome`. The handler nests both into `year`, so the
  domain year type doesn't need either field. `confirmation` is `{"confirmedAt": "...", "confirmedByName": "..."}`
  or null.
  - `PUT /api/v1/cashflow/confirmations/2026-08` → `200` with
    `{"month":"2026-08","confirmedAt":"…","confirmedByName":"…"}`
  - `DELETE` on the same path → `204`.

- [ ] **Step 1: Write the failing API tests.**
  - `TestCashflowPageShape`: a seeded household gets 200. Assert:
    - `recent` has 12 entries, and `year.months` has 12.
    - The `light.colour` and `status` values come from the fixed vocabulary.
  - `TestCashflowYearOutOfRange` (Review Focus 5):
    - `?year=1999` → 422 `CASHFLOW_YEAR_OUT_OF_RANGE`.
    - `?year=abc` → 400.
    - A future year → 422.
  - `TestConfirmRefusesUnfinishedMonth` (Review Focus 5):
    - `PUT` the current month → 422 `CASHFLOW_MONTH_OUT_OF_RANGE`.
    - `PUT` `2026-8` → 400 `INVALID_MONTH`.
  - `TestConfirmThenUndo`:
    - `PUT` last month → 200 with the confirmer's display name, and a second
      `GET` shows `confirmation` set.
    - `DELETE` → 204, and a third `GET` shows it null.
  - `TestCashflowWritesNeedCSRF`: a cookie session `PUT` without the CSRF
    header is refused.
  - The read-guard walk (`read_guard_walk_api_test.go`) finds the new `GET`
    without being edited. Confirm that it now lists `/cashflow`. Add a
    limited member's 403 and a non-owner's 403 to whichever explicit guard
    test the transactions routes use.

- [ ] **Step 2: Run them and confirm they fail**
  Run: `cd api && go test ./internal/adapter/http -run 'Cashflow|Confirm' -v`

- [ ] **Step 3: Implement** the handlers, routes, error mapping and wiring.

- [ ] **Step 4: Run the tests and confirm they pass.** Then run the whole Go
  suite: `make test-api`.

- [ ] **Step 5: Commit**
  `git commit -m "Serve the cashflow page and month confirmations"`

---

### Task 8: Open the ledger at a month from the URL

**Files:**
- Modify: `web/src/routes/router.tsx`: add `validateSearch` to the
  transactions route. It accepts `month` only when it matches
  `^\d{4}-\d{2}$`, and otherwise drops it.
- Modify: `web/src/features/money/TransactionsPage.tsx`: use the URL `month`
  as the starting value of its month filter. It must be a string, never
  `null`. Follow the null / `""` / string rules documented on its filter
  state.
- Test: `web/src/routes/router.test.tsx` and
  `web/src/features/money/TransactionsPage.test.tsx`

- [ ] **Step 1: Write the failing tests.**
  - `opens the ledger filtered to the month in the URL`: navigate to
    `/money/transactions?month=2026-08`. The month filter shows August 2026,
    and the list request carries `month=2026-08`.
  - `ignores a malformed month in the URL`: `?month=2026-8` falls back to the
    current behaviour.

- [ ] **Step 2: Run them and confirm they fail**
  Run: `cd web && npx vitest run src/routes/router.test.tsx src/features/money/TransactionsPage.test.tsx`

- [ ] **Step 3: Implement.**

- [ ] **Step 4: Run the tests and confirm they pass**, plus
  `npm run typecheck`.

- [ ] **Step 5: Commit**
  `git commit -m "Let a link open the ledger at a given month"`

---

### Task 9: Schemas, hooks and cache invalidation

**Files:**
- Create: `web/src/features/money/cashflowSchemas.ts`: zod schemas matching
  Task 7's JSON exactly. The `colour`, `status`, `grey`, `reason` and
  contribution `kind` fields are `z.enum` of the fixed vocabularies, so an
  unknown value fails parsing instead of rendering wrongly.
- Create: `web/src/features/money/useCashflow.ts`:
  - `cashflowQueryKey(year)`, which is `["cashflow", year]`
  - `useCashflow(year)`, built on `fetchAndParse`
  - `useConfirmCashflowMonth()` and `useUnconfirmCashflowMonth()`
- Modify: add `queryClient.invalidateQueries({ queryKey: ["cashflow"] })`
  everywhere a write changes Cashflow's inputs (LEARNING pattern 22):
  - `invalidateLedger` in `useTransactions.ts`
  - `invalidateBills` in `useBills.ts` (a bill's cadence or `next_due`
    changes the due-soon list, not just a payment)
  - `invalidateAccounts` in `useAccounts.ts`
  - the household primary-currency write
- Test: `web/src/features/money/useCashflow.test.ts`, and the existing hook
  test files for each invalidation.

- [ ] **Step 1: Write the failing tests.**
  - `parses a full cashflow response`: use a fixture copied from Task 7's
    example.
  - `rejects an unknown light colour`.
  - `confirming a month refetches the cashflow page`.
  - `undoing a confirmation refetches the cashflow page`.
  - One test for each invalidation. Each asserts that after the write, a
    mounted `useCashflow` makes a second `GET /api/v1/cashflow`:
    - `creating a transaction refetches cashflow`
    - `paying a bill refetches cashflow`
    - `editing a bill refetches cashflow`
    - `editing an account refetches cashflow`
    - `changing the primary currency refetches cashflow`

- [ ] **Step 2: Run them and confirm they fail.**

- [ ] **Step 3: Implement.**

- [ ] **Step 4: Run the tests and confirm they pass**, plus
  `npm run typecheck`. Mutation-check one case: remove the `["cashflow"]`
  line from `invalidateBills` and confirm `editing a bill refetches
  cashflow` fails. Then revert.

- [ ] **Step 5: Commit**
  `git commit -m "Fetch cashflow and refresh it after every money write"`

---

### Task 10: The Cashflow page

**Files** (all under `web/src/features/money/` unless a path is given):

| File | Responsibility |
|---|---|
| `cashflowCopy.ts` | Every sentence: one function per reason code, the grey-month reasons, cushion reasons, the "i" panel text and the empty state. |
| `CashflowPage.tsx` | The Monthly / Yearly tabs and the year picker (◀ year ▶, from `oldestYear` to the current year). Loading, error and empty states. |
| `CashflowMonthly.tsx` | The light, then three figures for the last finished month (money in; money out with "includes S$X of yearly bills spread" and "includes S$Y of loan repayments" when non-zero; left over with kept %), the 3-month average ("average of N months" when N < 3), the current month's "so far" line with no colour, the chart, and the table. |
| `CashflowYearly.tsx` | The light, three figures (kept so far against the same months last year, "includes N months with not enough data" when needed; heading for, current year only; cash cushion), the chart, and the bills still to come. |
| `CashflowMonthTable.tsx` | One row per month: month, in, out, left over, kept, light. A grey row shows its reason and a **"Looks right, count it"** button. A confirmed row shows who confirmed it and an **Undo** button in the same spot. "Count it" is not offered for an `unconvertible`-only grey. |
| `CashflowMonthDetail.tsx` | Opens from a table row. Lists that month's `contributions` (spread bills and loan repayments) and links to `/money/transactions?month=YYYY-MM`. |
| `CashflowChart.tsx` | A hand-built inline SVG, drawn the way `PeriodReturnChart.tsx` draws bars above and below a zero line. Solid green or red for finished months, striped for grey, pale for the current month, dashed outline for projected months. |
| `web/src/routes/router.tsx` (modify) | `moneyCashflowRoute`, path `cashflow`, under `moneyGuardRoute`. |
| `web/src/features/shell/Sidebar.tsx` (modify) | A "Cashflow" entry next to Budget, Goals and Bills. |

`cashflowCopy.ts` is the only place wording lives. The "i" panel says that:
- the thresholds are general guidelines from MoneySense, not advice
- yearly and quarterly bills are spread over the months they cover
- a yearly cost entered as a plain expense is not spread, so enter it as a
  bill
- a loan set up as a bill shouldn't also be logged as a transfer

- [ ] **Step 1: Write the failing component tests** (Testing Library, with
  fixtures from Task 9):
  - `shows the light with its reason in words`: a yellow `kept_under_10`
    with 600 bp renders "Watch" and "You kept 6% of income on average over
    the last 3 months".
  - `never uses the word safe`: render every reason code's copy and assert
    that none matches `/\bsafe\b/i`.
  - `the current month has no colour`.
  - `a grey month offers count it and confirming calls the API`: click
    "Looks right, count it" and assert a `PUT
    /api/v1/cashflow/confirmations/2026-08`.
  - `a confirmed month shows who confirmed it and can be undone`.
  - `an unconvertible-only grey month does not offer count it`.
  - `money out names the spread bills and loan repayments`.
  - `the average says how many months it covers`.
  - `the yearly tab shows heading for only in the current year`.
  - `the empty state links to add a transaction`.
  - `month detail links to the ledger at that month`.
  - In `router.test.tsx`:
    - `mounts the Cashflow page at /money/cashflow`
    - `redirects a member without the money capability away from /money/cashflow`

- [ ] **Step 2: Run them and confirm they fail.**

- [ ] **Step 3: Implement** the files. Mobile layout and spacing follow the
  existing Money pages.

- [ ] **Step 4: Run the tests and confirm they pass.** Then run `make lint`
  and `make test-web`.

- [ ] **Step 5: Commit**
  `git commit -m "Add the Cashflow page"`

---

### Task 11: Verify in the real app, measure, and update the docs

**Files:**
- Modify: `docs/FEATURE_TRACKER.md`:
  - Add ✅ rows under Money: "Cashflow — monthly view", "Cashflow — yearly
    view", "Cashflow light with reasons", "Not-enough-data rule with 'Looks
    right, count it'", "Cash cushion", and "Open the ledger at a month from
    a link".
  - If the browser walk finds a gap, mark the row 🟡 and say what the gap is.
  - Recount the summary table rather than adjusting it by hand.
- Modify: `docs/SYSTEM_DESIGN.md`. Use the `maintaining-system-design` skill:
  - the new route
  - the new table
  - the new ports and service
  - the request flow for `GET /cashflow`
- Modify: `docs/LEARNING.md`: one entry for each defect this work found. Add
  it to an existing pattern where it fits.

- [ ] **Step 1: Walk it in a real browser** with gstack `/browse` against
  `make dev` at http://localhost:5173. Before trusting localhost, check which
  Docker engine owns port 5173. On `make seed`'s household:
  1. Open Money › Cashflow and read the light and its reason.
  2. Add an income and an expense in last month, and watch the numbers move.
  3. Pay a yearly bill and watch the spread share appear in the months after
     it.
  4. Transfer into a loan account and watch money out rise.
  5. Make a month grey by deleting its income. Press "Looks right, count it",
     then Undo.
  6. Click a month and land on the ledger filtered to it.
  7. Switch to Yearly and step back a year.
  8. Check a phone-width viewport.

  Take a screenshot at each step.

- [ ] **Step 2: Measure the read at the cap** (spec constraint, ADR 14).
  - Seed a household with 100,000 transactions, using the approach ADR 14's
    account-list measurement used.
  - Time `GET /api/v1/cashflow` under the production container's CPU limit.
  - Record the figure in `docs/LEARNING.md` or the ADR 14 follow-up notes.
  - If it goes over about 2 s, stop and report it. The fix is to bound how
    far back the walk goes, not to add a cache.

- [ ] **Step 3: Update the three documents** as listed above.

- [ ] **Step 4: Run the full gate:** `make lint && make test`. Both must be
  green.

- [ ] **Step 5: Commit**
  `git commit -m "Record Cashflow in the tracker, system design and learning log"`
