# TDD evidence — Investment portfolio tracking, milestone 2

**Source plan**: [`.claude/plans/investment-portfolio-tracking-m2.plan.md`](../plans/investment-portfolio-tracking-m2.plan.md)
**Source PRD**: [`.claude/prds/investment-portfolio-tracking.prd.md`](../prds/investment-portfolio-tracking.prd.md)
**Branch**: `hearth-portfolio-holdings` (unmerged; milestone 1 sits on the same branch)
**Commits**: `b435132` to the branch tip — every commit on `hearth-portfolio-holdings` after milestone 1's last. A count is deliberately not given here: it drifted twice while this file was being written, which is the same prose-drift the tracker's recount notes exist for.

This report is the index to what the tests prove. The test code is the proof;
this file says which behaviour each piece of it pins, so the answer survives a
squash merge and a new session.

## The journeys, from the plan

1. As the household's owner, I want to see whether each holding made or lost
   money **in a period** — not only since I bought it — so I can tell a good
   quarter from a good decade.
2. As the owner, I want to **switch between quarters, half-years and years**
   without re-entering anything, because the instrument I am judging changes
   which window is honest.
3. As the owner, I want the **period I am in now** included and marked, so the
   report is about today rather than about the last closed quarter.
4. As the owner, I want to record **dividends and fees**, so "did this earn
   anything" counts the cash it paid me and the custody I paid for it.
5. As the owner, I want a **bar chart** comparing those periods, because eight
   numbers in a table do not show a trend.
6. As the owner, I want to be told **when a figure cannot be computed and why**,
   rather than being shown a zero that means "no price recorded".

## Task report

Each task below names its RED evidence and its GREEN evidence. Where a mutation
survived, the mutation and the test written to kill it are recorded — eight did,
and every one of them found a real weakness.

### Task 1 — `domain.Period`: the boundaries, test-first
Calendar quarters, half-years and years as a value type with unexported fields.

- **RED**: `b435132` — `api/internal/domain/period_test.go`, 16 tests, written
  against a file that did not exist. `go test ./internal/domain/` — build
  failure naming `domain.Period`, the intended compile-time RED.
- **GREEN**: `da7475c` — `api/internal/domain/period.go`. `go test
  ./internal/domain/ -run 'Period' -count=1` → ok.
- **Defect the RED caught**: `startOfDayUTC` first converted to UTC *before*
  reading the date, which moves 00:30 SGT on 1 January into the previous year —
  so a Singapore household asking "this quarter" in the first half-hour of a
  year got the quarter before it. Fixed to read `t.Date()` in the value's own
  location, mirroring `budget.go:643`.
- **Guaranteed**: a period's end is inclusive and is the day before the next
  one starts; `Previous()` crosses a year boundary correctly (Q1 → Q4 of the
  year before); `PeriodsEndingOn` runs oldest-first and ends with the period
  containing today; an unknown kind is refused rather than defaulted.

### Task 2 — the primary-currency pool
`Position` folds two cost pools at once: the instrument's own currency and the
household's.

- **RED**: `820629c`, tests first in the same commit —
  `TestRealisedInPrimaryCurrencyUsesTheBlendedRateNotTheLatestOne`,
  `TestAHoldingAlreadyInThePrimaryCurrencyFillsBothPoolsFromOneAmount`,
  `TestAnEmptyPositionsPrimaryPoolIsInTheHouseholdsCurrency`,
  `TestTheFoldRefusesAPrimaryAmountInSomeOtherCurrency`.
- **GREEN**: same commit — the unexported `costPool` type with `acquire`/
  `dispose`, and `Position(events, primaryCurrency)`.
- **Guaranteed**: realised gain in the household's currency uses the **blended
  rate of the lots actually sold**, not the latest rate — the arithmetic that
  makes "a US stock up in USD while SGD strengthened" come out as the loss it
  was. Amounts are stored, never rates; a primary amount in a third currency is
  refused rather than coerced.

### Task 3 — income and fees in the domain
- **RED/GREEN**: `589c2a0` — `api/internal/domain/holding_income_test.go`, 8
  tests, `holding_income.go` written to satisfy them.
- **Guaranteed**: both kinds are stored **positive** and the report subtracts
  the fees; an unrecognised `kind` from a database column fails closed; the
  primary amount is whole or absent, never half-supplied.

### Task 4 — `PeriodReturn`: the report arithmetic
The milestone's hardest task, and the only one given its own RED commit.

- **RED**: `5962838` — `api/internal/domain/holding_return_test.go`, 19 tests,
  no implementation. Build failure naming `Holding.ReturnOver`.
- **GREEN**: `4c63bad`, then `801453d` for the price dates.
- **Mutations run: 9. Four survived the first pass** and each pointed at a real
  hole:
  - an event dated on the period's **first** day,
  - an event dated on the period's **last** day,
  - **two prices inside one window** (the boundary must take the latest one at
    or before the close, not the first),
  - **Q1's previous period** crossing the year boundary.
  Four tests added; all nine mutations then killed. Three further mutations
  failed to *build* (`declared and not used`) — those prove nothing and were
  rewritten to compile before being counted.
- **Guaranteed**: `unrealised = (value − cost)close − (value − cost)open`,
  folded from the beginning of the holding to each cut-off, so a period's
  answer never depends on which periods were drawn beside it. A boundary price
  must be **dated inside the period it closes**; periods chain, so one period's
  close is the next one's open; holding nothing needs no price; **blanking is
  per component** — an unpriced quarter blanks unrealised and total while
  realised and income still report.

### Task 5 — migration and repository
- **RED/GREEN**: `0d21a58` — `api/migrations/00020_holding_income.sql`,
  `queries/holding_income.sql`, and 5 repository tests including
  `TestTheSchemaRefusesAZeroIncomeAmount` (asserts the CHECK, not the Go) and
  `TestValuationsForHouseholdReturnsEveryPriceNotOnlyTheNewest` (the read the
  report needs, which the M1 query did not do).
- **Guaranteed**: income is its own table with a `kind` column, never a third
  `HoldingEventKind`; a zero row is refused by the database; every price is
  readable, not only the newest.

### Task 6 — ports and the service
- **RED/GREEN**: `0d21a58` — `usecase/holding.go` gains `RecordIncome`,
  `ListIncome`, `DeleteIncome` and `Report`; 9 service tests.
- **Guaranteed**: the report ends with the **current** period and runs
  oldest-first; one return per period per holding; **archived holdings are
  included** (a holding sold last year still earned what it earned); one
  household's rows never reach another's; a count beyond `maxReportPeriods` and
  an unknown kind are refused with typed errors, not clamped.

### Task 7 — routes, `hearthctl`, CLI docs
- **RED/GREEN**: `94ec0f5` — 6 HTTP tests.
- **Defect the tests caught**: `HoldingDeps.Income` was wired in neither
  `main.go` nor `api_test.go`, so **every new route 500ed** — the same trap
  milestone 1 hit, one commit after I wrote a comment about it. Both
  composition roots now wire it, and `api_test.go` carries the comment saying
  why it is duplicated.
- **Second defect**: four new domain sentinels were unmapped in `errors.go` and
  surfaced as `INTERNAL`. Mapped — 400 for a bad query parameter, 422 for a
  refused typed value.
- **Guaranteed**: `/holdings/report` is not shadowed by the `/holdings/{id}`
  lookup; each period kind has its own default window (quarter 6, half 4, year
  3); every component and both price dates reach the wire; future-dated income
  is refused there.

### Task 8 — the screens
- **RED/GREEN**: `f25230f` — `PortfolioReportPage.test.tsx` (6 tests),
  `holdingReportCopy.test.ts` (7).
- **Defects the tests caught**: a currency stub that omitted the required
  `name` field made the Zod schema throw, so the page rendered `SGD 200.00`
  instead of `S$200.00` — **the exact defect the milestone 1 review found**,
  reproduced in a test rather than in the browser. And an assertion used a
  hyphen where `formatMoney` emits U+2212 MINUS.
- **Guaranteed**: a blank component prints its **reason**, not a zero; the
  current period is labelled "to date"; the Q/H/Y toggle refetches rather than
  re-slicing stale data.

### Task 9 — the bar chart
Hand-built inline SVG, the third in this repo, no charting dependency.

- **RED/GREEN**: `e55d55b` — `PeriodReturnChart.test.tsx`, 7 tests.
- **Mutation survived twice**: an AI mutation to the baseline kept every test
  green, because a floating baseline preserves the proportions the tests were
  checking (`height = value / span * H`). Fixed by publishing `data-baseline`
  and `data-plot-bottom` from the component and asserting **containment** — a
  bar must sit inside the plot — rather than only relative heights.
- **Guaranteed**: bars are grouped by period, capped at 40 (`visiblePeriods()`
  drops the oldest, never the newest), and a blank return renders **nothing**
  rather than a zero-height bar sitting on the axis.

### Task 10 — documentation, as part of the work
`35047af` — six tracker rows and a recount (99 → 105 ✅, 140 → 146 total),
three new `LEARNING.md` patterns, `SYSTEM_DESIGN.md` scope/ports/routes/ER
entity and a new §5 subsection, `HANDOVER.md`, `docs/CLI.md`.

### Task 11 — the browser walk
Driven at `http://localhost:5173` before any "done" claim, per `CLAUDE.md`.

- Figures read off the screen and checked by hand: S$495 + S$1,000 + S$45 − S$5
  = **S$1,535**.
- Cache invalidation confirmed by **in-app navigation**, not a reload — the
  case the fix is for.
- Per-component blanking with its reason, while realised still reported.
- Q/H/Y toggle, 320px phone width, 401 when signed out, clean console.
- **One product finding recorded rather than hidden**: the chart's shared
  linear axis flattens smaller holdings when one is orders of magnitude larger.

## Two fixes found after the tasks, both by looking rather than by failing

| Fix | How it was found | RED |
|---|---|---|
| `ed93d26` — a price or a trade now refetches the period report | asked what the cache does before the walk | 2 of 3 new tests in `useHoldings.test.tsx` failed on the `waitFor` |
| `dacc4c1` — adding or renaming a holding does too | grepped for the **shape** of the first fix; `invalidateHoldings` was its sibling and had been missed | both new tests failed on the same `waitFor` |

The second is the checklist's step 3 (*"grep for the shape of anything you
fixed; siblings are the norm here"*) doing exactly what it is there for.

## Test specification

| # | What is guaranteed | Test | Type | Result |
|---|---|---|---|---|
| 1 | A period's end is inclusive and chains into the next period's start | `domain/period_test.go` (16) | unit | PASS |
| 2 | The current quarter is found in the caller's own timezone, not UTC | `period_test.go:TestContainsJudgesTheDayNotTheInstant` | unit | PASS |
| 3 | Realised gain in the household's currency uses the blended rate of the lots sold | `domain/holding_test.go:TestRealisedInPrimaryCurrencyUsesTheBlendedRateNotTheLatestOne` | unit | PASS |
| 4 | A primary amount in a third currency is refused, not coerced | `holding_test.go:TestTheFoldRefusesAPrimaryAmountInSomeOtherCurrency` | unit | PASS |
| 5 | Income and fees fail closed on an unknown kind and store positive | `domain/holding_income_test.go` (8) | unit | PASS |
| 6 | A period's return is folded from the beginning to two cut-offs, independent of the periods drawn beside it | `domain/holding_return_test.go` (19) | unit | PASS |
| 7 | An unpriced period blanks unrealised and total **with a reason**, while realised and income still report | `holding_return_test.go` | unit | PASS |
| 8 | The database refuses a zero income amount | `postgres/holding_repo_test.go:TestTheSchemaRefusesAZeroIncomeAmount` | integration | PASS |
| 9 | Every price is readable, not only the newest | `holding_repo_test.go:TestValuationsForHouseholdReturnsEveryPriceNotOnlyTheNewest` | integration | PASS |
| 10 | The report ends with the current period and runs oldest-first | `usecase/holding_test.go:TestReportEndsWithTheCurrentPeriodAndRunsOldestFirst` | unit | PASS |
| 11 | Archived holdings still appear in the report | `usecase/holding_test.go:TestReportIncludesArchivedHoldings` | unit | PASS |
| 12 | One household's rows never reach another's | `usecase/holding_test.go:TestReportKeepsEachHoldingsRowsToItself` | unit | PASS |
| 13 | A count or kind the report cannot draw is refused with a typed error | `usecase/holding_test.go` (2 tests) | unit | PASS |
| 14 | `/holdings/report` is not shadowed by the id lookup | `http/holdings_api_test.go:TestTheReportRouteIsNotAnIdLookup` | integration | PASS |
| 15 | Each period kind has its own default window at the wire | `holdings_api_test.go:TestTheReportDefaultsItsWindowPerPeriodKind` | integration | PASS |
| 16 | Every component and both price dates reach the wire | `holdings_api_test.go:TestTheReportCarriesEveryComponentAndThePriceDates` | integration | PASS |
| 17 | Future-dated income is refused at the wire | `holdings_api_test.go:TestFutureDatedIncomeIsRefusedAtTheWire` | integration | PASS |
| 18 | The primary currency cannot change while investments are held — **at the wire**, with its own code, and a rename still saves | `holdings_api_test.go:TestAHouseholdHoldingInvestmentsCannotChangeCurrency` | integration | PASS |
| 19 | A blank component prints its reason, never a zero | `web/…/PortfolioReportPage.test.tsx` (6) | unit | PASS |
| 20 | Money renders with the household's own symbol, not the bare code | `web/…/holdingReportCopy.test.ts` (7) | unit | PASS |
| 21 | Every bar sits inside the plot area, and a blank renders nothing | `web/…/PeriodReturnChart.test.tsx` (7) | unit | PASS |
| 22 | A trade, a price, a dividend, a new holding or a rename all refetch the report | `web/…/useHoldings.test.tsx` (5) | unit | PASS |

## Validation actually run

```
make lint                       exit 0   (arch lint, tsc, eslint, go vet)
cd api && go test ./...         exit 0   (testcontainers, colima socket)
cd web && npx vitest run        95 files, 872 tests passed
```

Mutation testing, not coverage percentage, is this repository's bar for
"the test proves something" — `CLAUDE.md`'s definition of done asks for at
least one mutation-checked test per change. **34 mutations were run across this
milestone; 8 survived on the first pass and each one produced a new test.** No
line-coverage threshold is configured for either suite, and none is claimed
here.

## Deliberate deviations from the plan

Recorded rather than quietly dropped:

| Plan said | What happened | Why |
|---|---|---|
| `HoldingIncomePanel.test.tsx` | not written | The panel is a thin form over a mutation already covered by `useHoldings.test.tsx`; it was walked in the browser instead. A component test here would have asserted the stub, not the product. |
| `usePortfolioReport.test.ts` | not written | Covered through `PortfolioReportPage.test.tsx`, which exercises the hook against stubbed routes — testing it twice would pin the same fetch twice. |
| Use the `dataviz` skill for the chart | skill does not exist | The plan named a skill this repository does not have. Mirrored `NetWorthChart.tsx` instead, the existing hand-built SVG in this codebase; recorded in `e55d55b`'s message. |
| Typed `hearthctl` sub-commands for the report | not built | `hearthctl api` already drives every new route, and `docs/CLI.md` now carries worked examples. A typed command per route earns its place when a script needs the parsing, not before. |

## Known gaps

- **The chart's shared linear axis** flattens smaller holdings when one is
  orders of magnitude larger. Found in the walk, recorded in the tracker as a
  known gap rather than papered over. A per-holding axis or a log scale is the
  fix; neither is in this milestone.
- **Pre-existence periods** print as `S$0.00` rows for quarters before a
  holding existed. Correct — the holding provably earned nothing — but it is a
  wall of zeros on a new household's first report.
- **No PR is open.** Milestones 1 and 2 sit together on
  `hearth-portfolio-holdings`.

## Merge evidence

If this branch is squashed, the RED/GREEN summary above is the
record. The short version: every task began with a failing test (compile-time
RED for the three new domain files, runtime RED everywhere else), each fix
commit re-ran the same target to GREEN, and the eight surviving mutations —
four on the period arithmetic, two on the chart baseline, two on the cache —
are the reason to believe the tests would catch a regression rather than
merely pass.
