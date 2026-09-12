# Plan: Investment portfolio tracking — milestone 2

**Source PRD**: `.claude/prds/investment-portfolio-tracking.prd.md`
**Selected Milestone**: 2 — The period report (plus the bar chart the owner asked for on 2026-09-12)
**Complexity**: Large — larger than milestone 1's strategy table claimed, and the
correction is explained below rather than buried

## Summary

Milestone 2 is the milestone that tests the hypothesis: the owner picks a
quarter, a half-year or a year and reads **profit per instrument, split into
unrealised, realised and income, in the household's primary currency with the
native figure beside it** — plus a bar chart comparing those periods over time.

Everything in milestone 1 was input. This is the answer.

---

## Correction to the milestone 1 plan

The M1 plan's strategy table says of this milestone:

> *"Pure read-side: a period query over M1's events. No new tables."*

**That was wrong, and it is my error in summarising the PRD rather than a change
of scope.** The PRD was right all along; the one-line characterisation of it was
optimistic. Three things M1 shipped do not carry M2:

1. **Income is not recordable.** `api/internal/domain/holding.go:41-44` says so
   deliberately — *"income does not change what is held or what it cost, so
   folding it here would corrupt the average. It arrives with the period report,
   on its own footing."* That comment was a promise to this milestone, and this
   is where it comes due. The PRD's profit definition has three components and
   one of them has nowhere to live. **New table.**
2. **`domain.Position` has no primary-currency pool.** It carries `Held`, `Cost`
   and `Realised`, all in the holding's own currency. The PRD requires every
   figure in the household's primary currency, and a realised gain in SGD cannot
   be derived after the fact from a realised gain in USD — the blend has already
   happened. **Domain change, with a signature change to the fold.**
3. **Nothing in the codebase knows what a quarter is.** `startOfMonth`
   (`api/internal/usecase/budget.go:643`) is the only period arithmetic here.
   **New domain type.**

So: two new domain files, one migration, a service method, a route, a screen and
a chart. Read-side only in the sense that it computes rather than mutates
positions — but it is not free.

---

## Decisions this plan makes

Four came from the product owner on 2026-09-12 and are recorded as answers. Four
are taken here and get ratified or overridden at the gate at the bottom.

### Answered by the owner

| # | Question | Answer |
|---|---|---|
| 1 | What does the bar chart compare? | **Periods over time, with a Quarter / Half / Year toggle.** Not three bars for Q-vs-H-vs-Y: a year contains the half contains the quarter, so those three bars would show the same money three times and read as a comparison |
| 2 | Closed periods only? | **No — include the period currently running, labelled "to date".** Closed-only means an empty report until a quarter ends, and the hypothesis could not be tested for three months |
| 3 | Income in this milestone? | **Yes.** Without it a dividend-paying stock looks worse than it is, which is precisely the wrong signal for a focus decision |
| 4 | (from M1, still binding) | Total return split three ways; primary currency with native beside; holdings replace the account's balance in net worth — that last one is **milestone 3**, not this one |

### Decision C — income and fees get their own table, never a third `HoldingEventKind`

**The decision.** A new table `holding_income`, with a `kind` column that is
`income` or `fee`. Both are stored as **positive** amounts; the period figure is
`sum(income) − sum(fee)`. `domain.Position` and its fold are not touched by
either.

**Why not a third event kind.** The code already argues this case against itself
at `domain/holding.go:41-44`. An income row has no quantity, changes nothing
about what is held, and folding it through the average-cost pool would make
every disposal after a dividend realise the wrong number. The twenty-odd tests
that pin the fold stay valid precisely because income never enters it.

**Why fees live here too.** The PRD says *"fees and commissions reduce profit in
the period they are charged."* A trading commission already is handled — M1
stores the **whole amount that left the bank** as the acquisition cost, and the
whole amount that arrived as the disposal proceeds, so brokerage is inside both
figures by construction. What has nowhere to go is a fee charged against nothing:
custody, platform, storage on a gold vault. One `kind` column carries it without
inventing negative money, which `validatePrimaryAmount` refuses on purpose.

**The cross-currency rule is reused, not rewritten.** `validatePrimaryAmount`
(`domain/holding.go:142`) already is the whole rule and already serves two
callers; income becomes the third. The future-date guard from the M1 review
(finding 3) applies identically.

### Decision D — one fold, two pools

**The decision.** `domain.Position` gains `CostPrimary` and `RealisedPrimary`,
and the fold's signature becomes:

```go
func (h Holding) Position(events []HoldingEvent, primaryCurrency string) (Position, error)
```

On an acquisition the primary pool takes `e.PrimaryAmount` when it is set, and
`e.Amount` when it is nil — nil means the holding is already in the primary
currency, which is the contract `Transaction.ReceivedAmount` established and
`validatePrimaryAmount` enforces. On a disposal the **same** `Prorate` call runs
against the primary pool.

**Why not convert afterwards.** Because there is no rate to convert with, and if
there were, applying one rate to a blended cost would be wrong anyway. Buy 10 at
USD 100 when that cost SGD 135, buy 10 more at USD 100 when it cost SGD 130, then
sell 10: the blended primary cost is SGD 132.50 per unit, and no single rate
produces that number from the USD realised figure. **This is the named test**,
and it is the one that proves the primary figure says what the trade did to
household wealth rather than merely restating the native one.

**Why one fold and not two.** Two folds over the same events, one per currency,
would be two places for the ordering contract to drift. The ordering contract is
already written in three places (`queries/holding.sql:71-80`, the port doc
comment, and the index) because it decides the realised figure.

**What it costs.** A signature change to every caller and every fold test. That
is mechanical and the compiler finds all of it — which is the cheap kind of
change to make now and the expensive kind to make once a second caller exists.

### Decision E — a boundary price must be dated inside the period it closes

**The problem.** Unrealised return over a period needs a market value at each
end, and the household's valuations land on whatever days someone typed them.
Two questions have to be pinned or every implementer picks differently: *which*
valuation serves a boundary, and *when is there no usable one*.

**The decision, in three rules:**

1. **The price at a boundary is the latest valuation dated on or before it.**
   Never the earliest after. A price recorded after a quarter closed is
   information that quarter did not have, and letting it serve would mean typing
   today's price silently rewrites a closed period's answer.
2. **A closing price must be dated inside the period it closes.** A quarter's
   opening value is the preceding quarter's closing value, so the same rule
   chains: the opening price must fall inside the preceding period. This is the
   PRD's *"refuse to compute rather than compute from stale data"* made into a
   rule that can be explained in one sentence — a July price is not what March
   was worth.
3. **A boundary where nothing is held needs no price at all; its value is
   zero.** Not blank — actually, provably zero. Without this rule a holding
   bought mid-period would blank (nothing held at the start, so no opening
   price), and that is the most common case there is; and a holding sold clean
   before the end would blank too.

**When a needed price is missing, the row blanks per component, not whole.**
Unrealised and the total show no figure and a reason; **realised and income
still show**, because they are computed from amounts that actually changed hands
and no price is involved. A quarter where the owner sold at a profit and forgot
to record a price is not an unknowable quarter.

### Decision F — the arithmetic is cost-carried, so contributions cancel themselves

**The decision.** For each holding and period:

```
open  = Position folded over events with occurred_on <  period.Start
close = Position folded over events with occurred_on <= period.End

unrealised = (valueOf(close.Held) - close.Cost) - (valueOf(open.Held) - open.Cost)
realised   =  close.Realised - open.Realised
income     =  sum(income in period) - sum(fees in period)
total      =  unrealised + realised + income
```

…and the same four lines again against the primary pool.

**Why this shape and not a net-contributions formula.** It makes the PRD's single
most important rule — *"buying more must never read as profit"* — true by
construction rather than by a correcting term. A purchase adds the same amount to
market-value-at-cost and to cost, so the unrealised difference it produces is
exactly zero on the day it happens. There is no contribution term to forget.

It also gets the PRD's two mid-period rules for free, which is the test that it
is the right shape:

- **Bought inside the period**: `open.Held` is zero, so `open` contributes
  nothing and the figure is the gain since acquisition. Exactly *"starts from its
  cost on the acquisition date"*.
- **Sold inside the period**: `close.Held` is zero, so the unrealised gain the
  holding carried at the period start is removed, and the realised term picks up
  the actual gain against basis. Exactly *"ends at its sale price, at which point
  its unrealised gain becomes realised"* — and the two components sum correctly
  rather than double-counting.

**One fold function, two cut-off dates.** Not a windowed fold: average cost
depends on everything that came before the window, so a fold that starts at the
period boundary would compute a different basis. This is the single most likely
place to get M2 wrong.

### Decision G — the chart is hand-built inline SVG, grouped by period

**The decision.** `PeriodReturnChart.tsx`, inline SVG, no dependency. It mirrors
`NetWorthChart.tsx` deliberately, including its constants and its rules.

`grep -E "recharts|d3|chart" web/package.json` returns nothing, and
`NetWorthChart.tsx:1-6` says why: *"Twelve bars is less code than a charting
dependency, and this project's own floating-dependency history is why no new
package arrives for it — MoodChart.tsx made the same call for the same reason."*
A third chart does not overturn that; it confirms it.

**What it draws.** X axis is periods, oldest to newest. Within each period, one
bar per live holding, so the eye compares instruments inside a period and
follows one instrument across periods — which is the question *"which instrument
should I focus on"*. The unrealised / realised / income split lives in the table
underneath, where three numbers can be read as three numbers.

**Four rules it inherits from `NetWorthChart`, and one it does not:**

| Rule | Where it comes from |
|---|---|
| Zero baseline, losses drawn below the line | `NetWorthChart.tsx:50-54` already does exactly this — `Math.min(0, ...values)` and a computed `baselineY`. **Losses need no new mechanism**, contrary to what a quick read of the file suggests |
| A blank figure draws **no bar**, never a zero bar | `TrendPoint.netWorth` is nil rather than zero for the same reason — "zero is a claim about the household's money" |
| Wires against data the page already holds, never a second request | `NetWorthChart.tsx:6-7` |
| One colour at varying opacity, not N hard-coded tints | `NetWorthChart.tsx:21-25`. Here the varying dimension is the holding, so: one hue per holding from a small fixed palette, newest period at full opacity |
| Ticks at a chosen few | Not inherited — six period labels fit where twelve months did not, so every period is labelled |

**Bar budget.** Periods × live holdings must stay drawable at 320px. The chart
draws at most **40 bars**, dropping the oldest periods first, and its caption
says what window it is showing. Six quarters against four holdings is 24.

**The `dataviz` skill is loaded before this file is written**, not after.

---

## Patterns to Mirror

| Category | Source | Pattern |
|---|---|---|
| Period normalisation | `api/internal/usecase/budget.go:638-645` | `startOfMonth` truncates in UTC and its comment says why comparing two periods must not depend on which instant a caller passed. `domain.Period` does the same for quarters, and for the same reason |
| A series the frontend charts | `api/internal/usecase/networth_trend.go:15-31` | `TrendPoint` — a point whose figure is `*Money` (nil = unknowable, never zero) plus a `Complete` flag so a caller reading one without the other cannot mistake an empty period for a whole one |
| Fail-closed parser | `api/internal/domain/goal.go:29-42` | `ParsePeriodKind` and the income `kind` parser each get a `default` that refuses |
| Cross-currency amount | `api/internal/domain/holding.go:142-160` | `validatePrimaryAmount` — reuse it for income, do not write a third copy |
| Future-date guard | `api/internal/usecase/holding.go` `refuseFutureDate` | Income takes the same guard as events and valuations; it is the M1 review's finding 3 and the same typo produces the same damage |
| Migration prose | `api/migrations/00019_holdings.sql` | CHECK constraints mirroring each Go parser, and the non-obvious decision written where someone would try to change it |
| Ordering as a contract | `api/internal/adapter/postgres/queries/holding.sql:71-80` | The ORDER BY that decides the realised figure, stated in the SQL, the port doc comment **and** an index built for that clause |
| Chart component | `web/src/features/money/NetWorthChart.tsx` | Inline SVG, zero baseline, nil-not-zero, no second request |
| Page + panel + hook + copy | `GoalsPage.tsx`, `GoalContributionsPanel.tsx`, `useGoals.ts`, `goalCopy.ts` | One file one job; copy in its own module so a test can assert the sentence |
| Local-date helper | `web/src/features/money/HoldingLotsPanel.tsx` `today()` | Local-time getters, never `toISOString()`. This is the repo's most-repeated defect — see `docs/LEARNING.md` pattern 1, now six instances |

---

## Files to Change

| File | Action | Why |
|---|---|---|
| `api/internal/domain/period.go` | CREATE | `PeriodKind`, `Period`, `Start`/`End`/`Label`/`Contains`/`IsCurrent`, `PeriodsEndingOn(kind, today, count)` |
| `api/internal/domain/period_test.go` | CREATE | Every boundary: Q1 starts 1 Jan and ends 31 Dec-inclusive-of-31-Mar, H2 ends 31 Dec, a leap-year February, the period containing `today` |
| `api/internal/domain/holding.go` | UPDATE | `Position` gains `CostPrimary`/`RealisedPrimary`; `Position(events, primaryCurrency)`; `HoldingIncome` type + `IncomeKind` parser |
| `api/internal/domain/holding_test.go` | UPDATE | The blended-primary-cost test; every existing fold test updated for the new signature |
| `api/internal/domain/holding_return.go` | CREATE | `PeriodReturn`, `ReturnComponent`, and the pure function that turns two `Position`s + two boundary valuations + income rows into a `PeriodReturn` |
| `api/internal/domain/holding_return_test.go` | CREATE | Bought mid-period, sold mid-period, held throughout, no opening price, no closing price, nothing held at a boundary, a period with income only |
| `api/internal/domain/errors.go` | UPDATE | `ErrUnknownPeriodKind`, `ErrUnknownIncomeKind` |
| `api/migrations/00020_holding_income.sql` | CREATE | `holding_income` with `kind` CHECK, the paired `primary_amount_minor`/`primary_currency` NULL-together CHECK, and `(holding_id, received_on)` index |
| `api/internal/adapter/postgres/queries/holding_income.sql` | CREATE | Insert / list-per-holding / list-per-household / delete |
| `api/internal/adapter/postgres/queries/holding.sql` | UPDATE | `ListAllValuationsForHousehold` — the report needs every valuation, not the latest one |
| `api/internal/adapter/postgres/holding_repo.go` | UPDATE | `HoldingIncomeRepo`; the new valuation query |
| `api/internal/adapter/postgres/holding_repo_test.go` | UPDATE | Income round-trip; the household-wide valuation ordering |
| `api/internal/usecase/ports.go` | UPDATE | `HoldingIncomeRepository`, `ListAllValuations` on the valuation port — doc comments carry the contract |
| `api/internal/usecase/holding.go` | UPDATE | `RecordIncome`, `ListIncome`, `DeleteIncome`, and `Report(ctx, householdID, kind, count, today)` |
| `api/internal/usecase/holding_test.go` | UPDATE | Report assembly against in-memory doubles, including a holding archived mid-window |
| `api/internal/adapter/http/holding_handlers.go` | UPDATE | Income routes + `GET /holdings/report`; every 2xx carries a body |
| `api/internal/adapter/http/holding_test.go` | UPDATE | A member without `money` is refused **before** the service is reached, for the report route too |
| `api/internal/adapter/http/router.go` | UPDATE | Mount under the existing `money`+`owner` group — no new authorisation concept |
| `api/cmd/hearthctl/…` | UPDATE | Routes table (the test diffs it both ways and goes red until this is done) + `holding income` and `holding report` commands |
| `docs/CLI.md` | UPDATE | The new routes and commands |
| `web/src/features/money/holdingSchemas.ts` | UPDATE | Zod schemas mirroring the new DTOs |
| `web/src/features/money/holdingReportCopy.ts` (+`.test.ts`) | CREATE | Every sentence the report can say, including each blank reason |
| `web/src/features/money/usePortfolioReport.ts` (+`.test.ts`) | CREATE | One hook for the report resource |
| `web/src/features/money/PortfolioReportPage.tsx` (+`.test.tsx`) | CREATE | The report: period toggle, per-instrument table, the chart |
| `web/src/features/money/PeriodReturnChart.tsx` (+`.test.tsx`) | CREATE | The bar chart. Inline SVG, per Decision G |
| `web/src/features/money/HoldingIncomePanel.tsx` (+`.test.tsx`) | CREATE | Record a dividend or a fee against a holding |
| `web/src/features/money/PortfolioPage.tsx` | UPDATE | A link to the report; mount the income panel beside the lots panel |
| `web/src/routes/router.tsx` | UPDATE | `/money/portfolio/report` |
| `docs/FEATURE_TRACKER.md` | UPDATE | New rows, and a **recount** of the summary table — never a delta |
| `docs/SYSTEM_DESIGN.md` | UPDATE | New table, new routes, the report flow. Via `maintaining-system-design` |
| `docs/LEARNING.md` | UPDATE | Whatever this milestone teaches, as part of the work |
| `.claude/prds/investment-portfolio-tracking.prd.md` | UPDATE | M2 row to `in-progress` with this plan; open question 1 closed (answered by M1's Decision A) |

---

## Tasks

Ordered so the arithmetic is proven before anything is built on it, and so the
migration is written after the domain knows what it needs to store.

### Task 1: `domain.Period` — the boundaries, test-first
- **Action**: `PeriodKind` (`quarter` | `half` | `year`) with a fail-closed parser; `Period{Kind, Year, Index}`; `Start()` and `End()` as **inclusive dates truncated in UTC**; `Label()` ("Q3 2026", "H2 2026", "2026"); `Contains(date)`; `IsCurrent(today)`; `PeriodsEndingOn(kind, today, count)` returning oldest-first and ending with the period containing `today`.
- **Mirror**: `budget.go:638-645` — write the UTC-truncation reason at the point someone would change it. No household stores a timezone (`usecase/account.go:165`, `transaction_repo.go:318` both record this); say so here rather than leaving it to be rediscovered.
- **Why first**: this repo has six timezone/boundary defects in `docs/LEARNING.md` pattern 1. A quarter boundary is the seventh waiting to happen, and it is the cheapest one to catch here.
- **Validate**: `go test ./internal/domain` — the whole package, never a `-run` regex. That lesson cost three rounds in M1.

### Task 2: The primary-currency pool
- **Action**: `Position` gains `CostPrimary` and `RealisedPrimary`; the fold takes `primaryCurrency` and fills both pools, using `e.PrimaryAmount` when set and `e.Amount` when nil. Same `Prorate` call on disposal.
- **Mirror**: the existing disposal branch exactly — the asymmetry that keeps average cost unchanged is already written there and must not be re-derived.
- **Validate**: `go test ./internal/domain`. **Named test**: buy 10 @ USD 100 (SGD 135), buy 10 @ USD 100 (SGD 130), sell 10 — the realised primary figure must use the blended SGD 132.50 basis. A test that only checks the native figure proves nothing about this change.

### Task 3: Income and fees in the domain
- **Action**: `HoldingIncome` (holding, kind, amount, optional primary amount, received-on, note) with `IncomeKind` = `income` | `fee`, fail-closed parser, and `Validate` delegating to `validatePrimaryAmount`.
- **Mirror**: `HoldingEvent.Validate` — same shape, same rule, no second copy of the cross-currency logic.
- **Validate**: `go test ./internal/domain`.

### Task 4: `PeriodReturn` — the report arithmetic
- **Action**: the pure function of Decision F. Blanks per component with a reason; nothing held at a boundary values at zero without a price; realised and income survive a missing price.
- **Mirror**: `TrendPoint`'s nil-not-zero contract and its "a caller reading one field without the other cannot be misled" doc comment.
- **Validate**: `go test ./internal/domain`. **This is where the mutation testing goes**: the `<` vs `<=` at each boundary, the sign on the opening term, and the zero-held short-circuit. If a mutation survives, the test is wrong before the code is.
- **Then call `advisor`** — before the migration locks anything down.

### Task 5: Migration and repository
- **Action**: `00020_holding_income.sql`; the household-wide valuation query; `HoldingIncomeRepo`.
- **Mirror**: `00019_holdings.sql` throughout, including the paired-NULL CHECK and the prose.
- **Validate**: `go test ./internal/adapter/postgres` (needs Docker; `DOCKER_HOST` is colima on this machine).

### Task 6: Ports and the service
- **Action**: narrow ports with contract-carrying doc comments. `Report` assembles: holdings (archived included — a holding sold and archived last quarter still realised money that quarter), every event, every income row, every valuation; folds twice per holding per period; returns oldest-first periods.
- **Mirror**: `usecase/goal.go`; no service takes an actor (ADR 8).
- **Note the cost**: `Report` reads the household's whole history once per request. That is right at this scale and wrong at some larger one — write the threshold in a comment, the way M1's `writeOneHolding` now does, so the next person meets a decision rather than a surprise.
- **Validate**: `go test ./internal/usecase`.

### Task 7: Routes, `hearthctl`, CLI docs
- **Action**: `GET /holdings/report?kind=&count=`, `POST|GET /holdings/{id}/income`, `DELETE /holdings/{id}/income/{incomeId}`. Mount in the existing `money`+`owner` group beside the M1 routes.
- **Mirror**: `router.go:379-397` verbatim, both guards stacked on reads as well as writes.
- **Validate**: `go test ./internal/adapter/http` and `go test ./cmd/hearthctl`. The routes table test is red by design until the table is updated — sequence it here, not after the walk.

### Task 8: The screens
- **Action**: `PortfolioReportPage` with the Q/H/Y toggle, a per-instrument table (unrealised / realised / income / total, primary with native beside), and the chart. `HoldingIncomePanel` for entry. Blank figures show the reason, never a zero. The current period is labelled **"to date"** and says which price it is using and how old it is.
- **Mirror**: `GoalsPage` + panel + hook + copy. `today()` via local-time getters — `toISOString()` is the defect this repo has now made six times.
- **Validate**: `npm test` in `web/`, `make lint`.

### Task 9: The bar chart
- **Action**: `PeriodReturnChart.tsx` per Decision G. **Load the `dataviz` skill before writing it.**
- **Mirror**: `NetWorthChart.tsx` — its zero baseline already handles negative bars; do not invent a second mechanism.
- **Validate**: `npm test`. One test asserts a blanked period draws **no** bar rather than a zero-height one; one asserts a loss draws below the baseline.

### Task 10: Documentation, as part of the work
- **Action**: `FEATURE_TRACKER.md` new rows + a recount (this file records that delta-adjusting has produced wrong numbers before, and M1 hit exactly that with the escaped-pipe row). `SYSTEM_DESIGN.md` via `maintaining-system-design` — new table in §6, new routes in §4, and the report flow. `LEARNING.md` for whatever this milestone teaches.
- **Validate**: the columns sum to the stated totals; every Mermaid block renders (M1 shipped an invalid one — `;` is a statement separator in a sequence diagram).

### Task 11: The browser walk
- **Action**: drive `http://localhost:5173`. Record a dividend and a custody fee. Open the report for the current quarter and read it "to date". Delete a valuation so a period blanks, and confirm it blanks **with a reason** while realised still shows. Toggle to half-year and to year and watch the chart rescale. Confirm a limited member gets 403.
- **Mirror**: `verifying-in-the-real-environment`. Restart the API container first — a stale binary in the dev container has already cost this project a debugging session, and `docs/LEARNING.md` records the trap.
- **Validate**: tests passing is not this claim. Nothing is done until the walk has run.

---

## Validation

```bash
export PATH=$PATH:/Volumes/Oink_Machine/.local/opt/go-v1.24.2/bin
export DOCKER_HOST=unix:///Volumes/Oink_Machine/.colima/default/docker.sock
export TESTCONTAINERS_DOCKER_SOCKET_OVERRIDE=/var/run/docker.sock

make lint      # arch lint, frontend typecheck, eslint, go vet
make test      # Go suite (needs Docker) plus the frontend tests
make dev       # then walk it at http://localhost:5173
```

Run whole packages, not `-run` regexes. That mistake was made three times in M1
and each time it hid a test that was not running.

---

## Risks

| Risk | Likelihood | Mitigation |
|---|---|---|
| **A period boundary is off by one day** | High — it is the repo's most repeated defect class, now at six instances | Task 1 is first, boundaries are inclusive and UTC-truncated in one place, and the `<`/`<=` mutations are the named mutation target in Task 4 |
| **The fold is windowed to the period and the basis comes out wrong** | Medium, and silent | Decision F: fold from the beginning to two cut-off dates. A test with a purchase *before* the window whose price differs proves the basis came from outside it |
| **Blanking swallows a computable figure** | Medium | Blanking is per component. A test asserts a period with no price still reports realised and income |
| **Primary figures are derived from native ones by a rate** | Medium — it is the obvious shortcut | Decision D's blended-cost test cannot pass under any single-rate implementation |
| **The chart grows a dependency** | Low, but permanent if it happens | Decision G, and `NetWorthChart.tsx`'s own comment as precedent |
| **Bars become unreadable with several holdings** | Medium | The 40-bar budget, oldest periods dropped first, caption says the window |
| **Scope creeps into milestone 3** | High — "the report knows the market value, net worth should use it" | M2 still touches no balance. The Portfolio page's "not yet in net worth" note stays until M3 removes it |
| **`hearthctl` routes test blocks the suite** | Certain, by design | Task 7, before the walk |
| **Stale binary in the dev container during the walk** | Medium — already happened once | Restart the API container before walking |

---

## Not in this milestone

- **Net worth** — milestone 3, gated on the PRD's uninvested-cash question.
- **moomoo** — milestone 4, gated on three unanswered questions.
- **Benchmarks (vs STI, vs S&P)** — PRD out-of-scope; same missing price-source dependency as live FX.
- **A household total across holdings** — `PortfolioView` explains why it carries none, and the report inherits that reasoning: a total would need conversions this product cannot make, and blanking makes it fragile.
- **Deleting a valuation from the UI** — `DELETE /holdings/{id}/valuations/{id}` is routed and tested but no screen calls it. Known gap carried from M1, still open, recorded rather than quietly fixed here.

---

## Open question this plan closes

PRD open question 1 — *"Does the MVP allow instruments priced in a currency the
household does not hold?"* — **was answered by milestone 1's Decision A** and the
PRD still lists it as open. Yes, it does allow them: every money-bearing row
carries its own primary-currency amount supplied by the owner, so no FX provider
is on the correctness path. Marked resolved in the PRD as part of this plan.

---

## Acceptance

- [ ] All eleven tasks complete
- [ ] `make lint && make test` green
- [ ] At least one new test mutation-checked — the period boundaries in Task 4
- [ ] The blended-primary-cost test exists and passes (Decision D)
- [ ] A period with no price blanks **with a reason** while realised and income still report
- [ ] A member without `money` is refused before the service is reached, on the report route
- [ ] The chart draws no bar for a blank figure and draws losses below the baseline
- [ ] No new frontend dependency
- [ ] `FEATURE_TRACKER.md` (new rows + recount), `SYSTEM_DESIGN.md`, `LEARNING.md` updated
- [ ] Browser walk run, including the current period "to date"

---

**WAITING FOR CONFIRMATION.** A "yes" ratifies Decision C (income and fees get
their own table, the fold is untouched), Decision D (one fold, two currency
pools, signature change), Decision E (a boundary price must be dated inside the
period it closes; blanking is per component), Decision F (cost-carried
arithmetic, folded from the beginning to two cut-offs) and Decision G (hand-built
inline SVG, grouped by period, no charting dependency).
