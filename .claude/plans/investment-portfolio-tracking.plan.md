# Plan: Investment portfolio tracking

**Source PRD**: `.claude/prds/investment-portfolio-tracking.prd.md`
**Selected Milestone**: 1 — Holdings exist
**Complexity**: Medium (large for a first slice; the schema and the arithmetic are the hard parts, the screens are ordinary)

## Summary

Milestone 1 gives a household a place to record **what it actually holds** —
instrument, quantity, what each lot cost, when it was acquired — and a dated
**valuation** per instrument, so a portfolio page can show current market value.
It writes no net worth code, touches no existing balance, and adds no external
dependency.

Two things make this bigger than a CRUD slice and both are settled below before
any code: **fractional quantity arithmetic that does not overflow**, and **how a
foreign-currency figure gets into the household's primary currency without an FX
source that exists**.

---

## Strategy across the whole PRD

This plan details milestone 1 only. The other three, and what gates each:

| # | Milestone | Gated on | Shape when it comes |
|---|---|---|---|
| 1 | **Holdings exist** | Nothing — this plan | Schema, arithmetic, CRUD, portfolio page |
| 2 | The period report | M1's data shape only | Pure read-side: a period query over M1's events. No new tables. The formulas are already pinned in the PRD's Definitions — do not re-decide them |
| 3 | Net worth tells the truth | **The cash-component question** (PRD open question 7) | Changes `ListAccounts`, the 12-month trend, and Budget's blast radius. Deserves its own spec and its own browser walk. M1 deliberately gives it every input it needs and touches none of its code |
| 4 | moomoo | Three unanswered questions, cheapest first: **does it need a locally-running gateway process** | May not survive. Costs nothing to find out; costs a lot to discover late |

The order is not preference. M2 is the milestone that tests the hypothesis; M1
exists to feed it. M3 is separate precisely so the report can ship without
risking a shipped, walked net worth feature.

---

## Decisions this plan makes

These are the two the PRD left open and marked *"needs a decision before
`/plan`"*. Both are taken here rather than asked, so the gate at the bottom is
where they get ratified or overridden.

### Decision A — foreign currency travels as an amount, not a rate

**The problem.** `api/internal/adapter/fx/static.go:22` holds exactly one pair,
`SGD↔IDR` at 12,410. A USD-listed stock cannot be converted by anything in this
codebase. Worse, `usecase.FXRateProvider` (`ports.go:755`) takes no date, so it
could not give a historic rate even if it had one — and unrealised profit needs
the rate at *acquisition* for cost and at *period end* for value, which are
different dates.

**The decision.** Every money-bearing event stores **two amounts**: the native
one and, when the currencies differ, the primary-currency one the owner
supplies. No rate is stored and no dated-rate port is invented.

This is not a new idea in this codebase — it is exactly
`domain.Transaction.ReceivedAmount` (`api/internal/domain/transaction.go:45-49`),
which already solves the identical problem for a cross-currency transfer: *"It
is nil when nothing but the amount sent is known, which is the ordinary
same-currency case. It is required for a transfer whose two accounts differ in
currency."* Mirror that contract exactly, including the nil-means-same-currency
convention.

**Why an amount beats a rate.** The owner knows what a lot cost them in SGD,
because that is the number that left their bank. A rate is derived from that,
and storing the derived thing loses the fact. It also means a live FX provider,
if one ever arrives, becomes a *convenience that pre-fills a field* rather than
a correctness dependency — which is the cheapest possible relationship to have
with an external service.

**What it costs.** More typing per event. Accepted: the PRD's whole MVP is
manual entry, and if the owner is already typing a price they can type a total.

### Decision B — milestone 1 does not touch account balances at all

**The decision.** Holdings are invisible to `ListAccounts`, to net worth, to the
12-month trend and to Budget. The portfolio page shows its own figure and says
plainly on screen that it is **not yet in net worth**.

**Why.** The PRD's open question 7 — does an investment account hold uninvested
cash alongside holdings, and is buying a *conversion* of that cash — is genuinely
unsettled, and answering it wrong silently double-counts the household's money.
It is milestone 3's question. Deciding it here, inside a schema task, is how it
would get decided badly.

**What M1 must do so M3 is not blocked:** record `cost` on every acquisition and
`proceeds` on every disposal, and anchor every holding to a real investment
account by foreign key. That gives M3 every input it needs whichever
reconciliation it picks. Anchoring later would mean a backfill.

**Day-one honesty.** The "not yet in net worth" note on the page is a
requirement, not a nicety. A number on screen that the headline figure disagrees
with, with nothing saying why, is exactly the first-run surprise this project
has already been burned by.

---

## Patterns to Mirror

| Category | Source | Pattern |
|---|---|---|
| Fail-closed parser | `api/internal/domain/goal.go:29-42` | `ParseContributionSource` — a `switch` over a value arriving from a database column, with a `default` that returns `Err…` rather than carrying it. Every new enum here gets one |
| Migration prose | `api/migrations/00007_goals.sql:3-12` | The comment says *why* a goal carries its own currency and ends **"Do not 'fix' this by dropping the column."** Write the non-obvious decision where someone would try to change it |
| Currency per row | `api/migrations/00007_goals.sql:19` | `currency char(3) NOT NULL` on the row, not inherited — a goal accumulates for years and a primary-currency change must not restate it. A holding has the same property, more so |
| Value constraints in SQL *and* Go | `api/migrations/00007_goals.sql:50-51` + the Go parser | Both layers refuse an unknown value; the SQL `CHECK (source IN (…))` and `ParseContributionSource` are deliberately redundant |
| Exact integer money arithmetic | `api/internal/usecase/ports.go:718-733` | `Rate.Apply` — rounds half away from zero, and returns `domain.ErrAmountOverflow` rather than wrapping. `mulOverflows` (`:735-751`) is the overflow idiom, with its `MinInt64` edge case written out |
| Cross-currency amount | `api/internal/domain/transaction.go:45-49` | `ReceivedAmount *Money` — nil for the same-currency case, required when they differ |
| Route guard stack | `api/internal/adapter/http/router.go:288-300` | The `txn` group stacks `requireCapability(domain.CapMoney)` **and** `requireOwner` on reads as well as writes, with a comment saying why a blank table "reads as broken". Its final paragraph warns against leaning on an invariant enforced in another layer |
| Repository file naming | `api/internal/adapter/postgres/goal_repo.go` + `queries/goal.sql` | One `<thing>_repo.go` beside one `queries/<thing>.sql`, each with a `_test.go` |
| Feature page + panel + hook | `web/src/features/money/GoalsPage.tsx`, `GoalContributionsPanel.tsx`, `useGoals.ts` | A page, a panel for the child ledger, one hook per resource, each with a co-located `.test.tsx` |

No existing code holds a fractional quantity or a market price. That pattern
does not exist here and is invented in Task 1 — stated rather than pretended.

---

## The arithmetic problem, and its fix

**Quantity is not money.** `float64` is banned from monetary paths
(`CLAUDE.md`), but a quantity is genuinely fractional: 300.5 grams, 0.5 of a
share. Representing it as `int64` scaled by 10⁹ (**nano units**) is the natural
extension of the codebase's minor-units instinct.

**The naive version is broken, and not in an exotic case.** Valuing a holding
is `quantity_nano × unit_price_minor / 10⁹`, and that intermediate overflows an
ordinary Indonesian position:

```
10,000 shares          = 10_000 × 10⁹     = 1.0e13 quantity_nano
Rp 10,000 per share    = 10_000 × 10²     = 1.0e6  price_minor   (IDR exponent 2,
                                                     domain/currency.go:58)
product                                    = 1.0e19
int64 max                                  = 9.223e18   ← overflows
```

`mulOverflows` catches it, so nothing wraps — but the symptom is a refusal to
value a perfectly normal holding, which is a defect all the same.

**The fix: a 128-bit intermediate via `math/bits`.** `math/bits` is the standard
library, so `internal/domain` may import it. Multiply into a 128-bit pair, divide
back down, and the *result* — a value in minor units — always fits comfortably.

```
hi, lo := bits.Mul64(|quantity_nano|, |price_minor|)   // exact 128-bit product
if hi >= 10⁹ { return ErrAmountOverflow }              // else Div64 panics
quo, rem := bits.Div64(hi, lo, 10⁹)
// round half away from zero using rem, restore the sign, check ≤ MaxInt64
```

Rounding **half away from zero**, matching `Rate.Apply` — one rounding rule in
the codebase, not two. The `hi >= 10⁹` guard is load-bearing: `bits.Div64`
*panics* when the quotient would not fit, and a panic in a monetary path is
worse than the overflow it replaces.

The IDR figures above are the test case, named in the test.

---

## Files to Change

| File | Action | Why |
|---|---|---|
| `api/internal/domain/quantity.go` | CREATE | `Quantity` (int64 nano) and `Value(unitPrice Money) (Money, error)` — the 128-bit multiply, one type, one job |
| `api/internal/domain/quantity_test.go` | CREATE | The IDR overflow case, rounding at the half, negative quantities refused, zero |
| `api/internal/domain/holding.go` | CREATE | `Holding`, `HoldingLot`, `Valuation`, `InstrumentKind`, `HoldingEventKind` + their fail-closed parsers |
| `api/internal/domain/holding_test.go` | CREATE | Parsers refuse unknown values; average-cost basis across two lots at different prices |
| `api/migrations/00019_holdings.sql` | CREATE | `holdings`, `holding_events`, `holding_valuations`; CHECKs mirroring the Go parsers |
| `api/internal/adapter/postgres/queries/holding.sql` | CREATE | Queries, beside every other `queries/<thing>.sql` |
| `api/internal/adapter/postgres/holding_repo.go` | CREATE | Implements the ports; maps a missing row to `domain.ErrNotFound` at this boundary, never `pgx.ErrNoRows` further up |
| `api/internal/adapter/postgres/holding_repo_test.go` | CREATE | Testcontainers, mirroring `goal_repo_test.go` |
| `api/internal/usecase/ports.go` | UPDATE | `HoldingRepository` + `HoldingValuationRepository` — narrow, per interface segregation. Doc comments are the contract |
| `api/internal/usecase/holding.go` | CREATE | `HoldingService` — validity only, never an actor (authorisation is the HTTP edge's, ADR 8) |
| `api/internal/usecase/holding_test.go` | CREATE | Against in-memory doubles, as every other service is |
| `api/internal/adapter/http/holding.go` | CREATE | Handlers. Every 2xx except 204 carries a JSON body |
| `api/internal/adapter/http/holding_test.go` | CREATE | Includes: a member without `money` is refused **before** any service call |
| `api/internal/adapter/http/router.go` | UPDATE | Mount under the `money`+`owner` group — see Task 5 |
| `api/cmd/hearthctl/…` | UPDATE | The routes table a test diffs against `router.go` **both ways**; the suite goes red until this is updated |
| `docs/CLI.md` | UPDATE | New routes documented |
| `web/src/features/money/PortfolioPage.tsx` (+`.test.tsx`) | CREATE | The page; carries the "not yet in net worth" note |
| `web/src/features/money/HoldingModal.tsx` (+`.test.tsx`) | CREATE | Add/edit a holding, mirroring `GoalModal.tsx` |
| `web/src/features/money/HoldingLotsPanel.tsx` (+`.test.tsx`) | CREATE | The child ledger, mirroring `GoalContributionsPanel.tsx` |
| `web/src/features/money/ValuationModal.tsx` (+`.test.tsx`) | CREATE | Record a price as of a date |
| `web/src/features/money/useHoldings.ts` (+`.test.ts`) | CREATE | One hook per resource, mirroring `useGoals.ts` |
| `web/src/routes/router.tsx` | UPDATE | `/money/portfolio` |
| `docs/FEATURE_TRACKER.md` | UPDATE | **New rows** — the design draws no portfolio screen — plus a recount of the summary table, never a delta |
| `docs/SYSTEM_DESIGN.md` | UPDATE | New tables, routes and a request flow. Via the `maintaining-system-design` skill |
| `docs/LEARNING.md` | UPDATE | The overflow defect and what would have caught it sooner |

---

## Tasks

### Task 1: `domain.Quantity` and exact valuation
- **Action**: `Quantity` as int64 nano units with a constructor refusing negatives; `Value(unitPrice Money) (Money, error)` doing the 128-bit multiply above. Write the scale choice and the `bits.Div64` panic guard as comments **at the point someone would change them**.
- **Mirror**: `ports.go:718-751` — `Rate.Apply`'s rounding and `mulOverflows`' written-out edge case.
- **Validate**: `go test ./internal/domain -run 'Quantity|Value'` — the narrower `-run Quantity` silently matches only the constructor tests, not the `TestValue*` ones. Must include the 10,000 × Rp 10,000 case (produces a value, not `ErrAmountOverflow`) **and** a 100,000-share case, whose product passes 2⁶⁴ and so actually exercises the 128-bit high word — the smaller case alone passes a 64-bit truncation.

### Task 2: The domain types
- **Action**: `Holding` (account, name, instrument kind, unit label, currency), `HoldingLot` (an acquisition or a disposal: quantity, native amount, optional primary amount, date), `Valuation` (unit price, as-of date). `InstrumentKind` = stock | gold | other. Average-cost basis as a pure function over lots. "Other" is quantity 1 × its value, so there is **one** arithmetic path, not two.
- **Mirror**: `domain/goal.go:29-42` for every parser; `domain/transaction.go:45-49` for the optional primary amount.
- **Validate**: `go test ./internal/domain -run Holding`. One test asserts average cost across two lots at different prices.

### Task 3: Migration and repository
- **Action**: `00019_holdings.sql` with `CHECK` constraints mirroring each Go parser, currency on the holding row, and prose saying why. Repository + queries file.
- **Mirror**: `00007_goals.sql` throughout — including that a holding is **archived, never deleted**, because lots and valuations reference it.
- **Validate**: `go test ./internal/adapter/postgres -run Holding` (needs Docker).

### Task 4: Ports and service
- **Action**: Narrow repository interfaces in `ports.go` with doc comments that carry the contract. `HoldingService` enforces what is *valid*: the account exists, **is of type `investment`** (fail closed on the type — a `default` that refuses), the lot's currency matches its holding, a primary amount is present exactly when the currencies differ.
- **Mirror**: `usecase/goal.go`; the "no service takes an actor parameter" rule from `CLAUDE.md` and ADR 8.
- **Validate**: `go test ./internal/usecase -run Holding`.

### Task 5: Routes, behind the existing guard
- **Action**: Mount under the **`money` + `owner`** group — reads included, not just writes. `router.go:288-300` already makes this argument for the ledger: a table whose every figure is blank "reads as broken." A portfolio page is that table, so a limited member gets **403, not a redacted body**. This satisfies the PRD's "not for limited members" and writes zero redaction code.
- **Mirror**: the `txn` group verbatim, including stacking both guards rather than leaning on an invariant enforced in another layer for another reason — that group's own comment warns against exactly that.
- **Validate**: `go test ./internal/adapter/http -run Holding`. One test proves a member without `money` is refused **before** the service is reached.

### Task 6: `hearthctl` and CLI docs
- **Action**: Add the routes to the hand-kept table and a typed `holding add`; update `docs/CLI.md`.
- **Mirror**: the existing typed inserts. Goes **through** the guards, never around them (ADR 6).
- **Validate**: `go test ./cmd/hearthctl` — the routes test diffs against `router.go` both ways and is red until this is done. This also gives the browser walk a way to seed data.

### Task 7: The screens
- **Action**: Portfolio page, holding modal, lots panel, valuation modal, `useHoldings`. Current market value per holding, in primary currency with native beside it. A holding with no valuation shows **no figure and the reason** — never a zero. Valuation age is displayed, because the PRD's top risk is valuations quietly going stale. The "not yet in net worth" note ships on this page.
- **Mirror**: `GoalsPage.tsx` + `GoalContributionsPanel.tsx` + `useGoals.ts`.
- **Validate**: `npm test` in `web/`; `make lint` for typecheck and eslint.

### Task 8: Documentation, as part of the work
- **Action**: New `docs/FEATURE_TRACKER.md` rows (the design describes no portfolio screen) and a **recount** of the summary table — this file records that adjusting by delta has produced wrong numbers before. `docs/SYSTEM_DESIGN.md` via `maintaining-system-design`. A `docs/LEARNING.md` entry for the overflow.
- **Validate**: The columns sum to the stated totals.

### Task 9: The browser walk
- **Action**: Drive `http://localhost:5173` by hand. Create an investment account, add a gold holding in grams and a foreign-currency stock, record two lots at different prices, record a valuation, read the value. Confirm net worth is **unchanged** and the page says so. Confirm a limited member gets 403.
- **Mirror**: the `verifying-in-the-real-environment` skill; the walk records in `docs/superpowers/plans/`.
- **Validate**: Tests passing is not this claim. Nothing is done until this walk has run.

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

Definition of done (`CLAUDE.md`): `make lint && make test` green, **at least one
new test mutation-checked**, `FEATURE_TRACKER.md` and `LEARNING.md` updated, and
the browser walk run.

---

## Risks

| Risk | Likelihood | Mitigation |
|---|---|---|
| **Quantity × price overflows on a normal position** | Certain at nano scale without the fix | Task 1's 128-bit multiply, with the IDR case as a named test. This is *the* technical risk of the milestone |
| **`bits.Div64` panics instead of erroring** | Medium if the guard is forgotten | The `hi >= 10⁹` check precedes every divide; a test drives a quotient past `MaxInt64` |
| **M1 quietly grows into M3** | High — "while I'm here, net worth should include this" | Decision B is a named non-goal. Holdings are invisible to `ListAccounts` in this milestone, full stop |
| **`hearthctl` routes test blocks the suite** | Certain, by design | Task 6 is sequenced before the walk, not after |
| **Average cost is wrong across currencies** | Medium | Basis is computed per holding in the holding's own currency; the primary figure is carried per lot, never re-derived by averaging rates |
| **Valuations go stale and nobody notices** | High (the PRD's top product risk) | Valuation age on screen from day one; a holding with no valuation blanks with a reason rather than showing zero |
| **Two docs go stale** | Medium — it is the failure this repo documents most | Task 8 is a task, not a tidy-up afterwards |

---

## Acceptance

- [ ] All nine tasks complete
- [ ] `make lint && make test` green
- [ ] At least one new test mutation-checked
- [ ] The IDR overflow case is a named test and passes
- [ ] A limited member is refused by a test that proves the guard runs before the service
- [ ] `FEATURE_TRACKER.md` (new rows + recount), `SYSTEM_DESIGN.md`, `LEARNING.md` updated
- [ ] Browser walk run and recorded in `docs/superpowers/plans/`
- [ ] Net worth confirmed **unchanged** by this milestone, and the page says so

---

**WAITING FOR CONFIRMATION.** A "yes" ratifies Decision A (foreign currency
travels as an amount, mirroring `ReceivedAmount` — no FX provider, no dated-rate
port) and Decision B (milestone 1 touches no balance; the cash-component question
stays open for milestone 3).
