# Hearth — Net worth, twelve-month trend

Written 2026-08-19. Closes the last 🟡 on Finances: `docs/FEATURE_TRACKER.md`
row "Net worth with 12-month trend", and the trend half of the Overview net
worth card's row.

The design draws it in two places:

- **Finances** (design line 354): the figure, `▲ 2.1%` beside it, "Last 12
  months" on the right, twelve bars below with the newest highlighted, and a
  four-tick month axis — `Aug '25 · Nov '25 · Feb '26 · Jul '26`.
- **Overview** (design line 305): the same figure with `▲ 2.1% this month`
  under it. No chart.

Everything above the bars already exists and is live. This spec is the bars
and the percentage.

---

## The finding this spec rests on

`docs/FEATURE_TRACKER.md` line 489 says:

> The trend needs balance snapshots: a second table, and a separate decision
> about when a snapshot gets written (nightly? on every balance change? on
> read?).

**That is not true, and it never was.** A balance in this codebase is already
derived, not stored — `queries/account.sql` computes it as the opening balance
plus every transaction dated on or after `opening_balance_as_of`. The same
rows that produce today's figure produce last March's. No table, no writer, no
scheduler.

That assertion sat in the tracker unchallenged and kept a shippable feature on
the shelf. It gets rewritten as part of this work, and `docs/LEARNING.md` gets
the entry.

---

## Decisions

### 1. Derive the history; do not record it

The series is recomputed on every read from transactions, not written into a
snapshot table.

**What that buys:** no migration, no second writer, no decision about when a
snapshot is taken, and no class of defect where the snapshot and the live
figure disagree. The last bar cannot drift from the headline because it *is*
the headline (decision 3).

**What it costs, stated plainly:** history is not frozen. All twelve bars move
retroactively when any of these happens —

- an account's `openingBalanceMinor` or `openingBalanceAsOf` is edited (PATCH
  exposes both)
- an account is archived — `Summary` skips `IsArchived()`, so archiving today
  removes that account from every bar, not merely from today's
- `countTowardNetWorth` is toggled
- and the same applies to `Complete(m)`, which is judged against the accounts
  that exist **now**: archiving an account today makes months that were
  genuinely incomplete read as complete, because the account that was missing
  from them is no longer counted at all
- `fx.StaticProvider` is one day replaced by a live rate source, which will
  reprice every past month at today's rate

A snapshot table would freeze all four. It is not built now because
**snapshots can be added later and backfilled from this derivation, and the
reverse is impossible** — a household that starts snapshotting in six months
would otherwise have six months of nothing. When the day comes that the
household's own history must be immutable (an audit trail, a "you said it was
this on 1 Jan" claim), that is the trigger to add the table, and this
paragraph is why it was not added first.

### 2. Rates are today's rates, for every month

Each account's historical balance is converted into the household's primary
currency at the **current** rate, not the rate that held in that month. There
is no historical rate table and `fx.StaticProvider` has one number in it.

Consequence: a mixed-currency household's trend shows how their *balances*
moved, with the exchange rate held still. That is the more useful of the two
charts anyway — an IDR account whose balance never changed should not appear
to rise and fall because the currency did. Say so in the spec, not in the UI:
this is a one-rate-in-the-provider product today, and a UI note about
historical FX would describe machinery that does not exist.

### 3. The last bar is the headline figure, by construction

Walk backwards from the live balance rather than computing each month-end
independently:

```
bal(a, current) = AccountView.Balance          // the repository's own figure
bal(a, m − 1)   = bal(a, m) − movement(a, m)
```

The independent form — `SUM(...) WHERE occurred_on <= month_end` — is
rejected. `ListAccounts`'s balance expression has **no upper bound**, so a
transaction dated next week is already inside today's headline; a `<=
month_end` bar would leave it out, and the last bar would disagree with the
figure printed directly above it. `account.sql`'s own comment already warns
about this class: *"Get and List must compute this the same way, or the two
disagree on the same account's balance."* The trend is the third computation
of that number, and anchoring is what keeps it honest.

Future-dated transactions are therefore bucketed into the current month (the
formulas below), so the first step backwards removes them exactly once and
they never leak into older bars.

**The trend is built inside `AccountService.Summary`, not beside it.** The
newest point for each account is the *same converted value* that loop already
added to the headline — the variable, not a second computation that arrives at
the same answer. This matters beyond tidiness: `fx.StaticProvider` returns one
rate forever, so two independent conversion passes agree today by coincidence,
and a live provider could return two different rates within one request and
make the last bar disagree with the figure printed directly above it. A test
against the static provider would never see it. Sharing the value closes it by
construction rather than by luck.

The same reasoning forces one small refactor: rates are looked up **once per
distinct currency pair per request** and reused for every month of every
account, rather than `convert` calling the provider afresh each time. A
per-call rate map is enough; it is also what stops a twelve-month, ten-account
household from making 120 provider calls.

`Summary` therefore gains a `today time.Time` parameter —
`Summary(ctx, householdID, views, today)` — read once at the HTTP layer and
never from a clock inside the usecase, the same rule `RetroService.List`
follows. That is a signature change with existing callers and existing tests
to update.

### 4. An untracked month is a gap, never a zero

A month before an account's `openingBalanceAsOf` has no honest figure for that
account. This codebase's rule for that state is already written twice —
`NetWorthSummary.Computable` ("zero is a claim about the household's money"),
and `MoodPoint.HasMood`, which refuses to draw `0` for "no mood recorded". The
trend follows it.

Three states per month, and the middle one is the decision:

| State | When | Drawn as |
|---|---|---|
| Known and complete | every counted account was tracked by that month's end | a full bar |
| Known but incomplete | at least one counted account was tracked, at least one was not | a bar in the muted tint, with a note under the chart |
| Unknown | no counted account was tracked yet | nothing — no bar, no baseline stub |

The rejected alternatives, so nobody re-litigates them:

- **Strict** (gap unless every counted account is tracked) is the most
  defensible number and the worst product: adding one account today would wipe
  all twelve bars, and adding accounts is a thing households do.
- **Flat carry-back** (assume an account held its opening balance all along)
  draws a smooth chart out of a figure nobody asserted. It invents money.
- **Known-accounts-only with no marking** draws a cliff on the month an
  account was added, which reads as "we got richer" when it means "we started
  counting something." Marking the incomplete months is what makes this
  honest, and the marking is the whole difference.

**Fewer than two known months is not a trend, and is not drawn.** The chart is
replaced by the same "not enough history yet" line `MoodChart` uses; the figure
above it is still correct and still shown. The threshold is two, not one,
because of the state every new customer is in on their first day: a household
that signs up today, with every account opened today, has exactly one known
month. Drawing that as a single bar pinned to the right-hand edge with eleven
blank slots beside it and no percentage under it is worse than saying plainly
that there is not enough history yet.

### 5. The percentage is suppressed more often than it is shown

`▲ 2.1%` compares the current month with the previous one, and the current
month's bar is **now**, not the end of the month — the newest bar is the live
balance (decision 3), so the percentage reads as month-to-date, which is what
the design's own "▲ 2.1% this month" says. Every older bar is that month's
closing figure.

The percentage is omitted unless **all** of these hold:

- both months are known
- **both months are complete** — a jump caused by starting to track an account
  is not growth, and this is the guard that stops the product claiming it is
- the previous month's net worth is strictly greater than zero — a percentage
  off zero is undefined, and off a negative base it inverts its own sign (a
  household moving from −10,000 to −5,000 would be shown as −50%)
- the arithmetic does not overflow `int64` (below)

Omitted means *nothing is rendered* — no `0.0%`, no `—`. The same rule as
everywhere else here.

### 6. It rides on `GET /accounts`, not a new endpoint

The trend is a field on `summaryDTO`. `handleListAccounts`'s own comment gives
the reason: one endpoint for one screen, so the limited-member redaction is
written once. A new route would mean a second copy of the role check, and a
limited member must receive no trend at all — the trend is amounts, and
amounts are exactly what that role may not see.

It also inherits the `Computable` gate for free: an incomputable summary
carries no figures, so it carries no trend.

### 7. Overview shares the card, not the chart

`OverviewPage` already renders `NetWorthCard` directly. `NetWorthCard` gains
the percentage — so both screens get it from one change — and an optional
`chart` slot that only `FinancesPage` fills. No second component, no copy of
the not-computable branch.

---

## Data model

**No migration. No new table. No new column.**

One new query in `api/internal/adapter/postgres/queries/account.sql`:

```sql
-- name: ListAccountMonthlyMovements :many
```

It returns `(account_id uuid, month date, delta_minor bigint)` — one row per
account per calendar month that has any movement, `month` being the first of
that month, `delta_minor` in **that account's own currency**. No conversion
happens in SQL and none can: the FX provider lives in the usecase layer, which
is why `MonthTotalsQuery` returns rows rather than sums too.

Its `WHERE` clause must mirror `ListAccounts`'s balance expression exactly:

- outgoing side: `-t.amount_minor` for `t.from_account_id = a.id`
- incoming side: `+COALESCE(t.received_amount_minor, t.amount_minor)` for
  `t.to_account_id = a.id` — what actually landed, in this account's currency
- both sides filtered `t.occurred_on >= a.opening_balance_as_of`
- scoped `a.household_id = $1`, and lower-bounded at the window start `$2`

Any divergence between this filter and `ListAccounts`'s makes the walk-back
disagree with the headline **silently** — the numbers stay plausible. The test
in §Testing exists for precisely this.

A `UNION ALL` of the two sides, grouped by account and
`date_trunc('month', occurred_on)`, is the shape. There is no upper bound on
`occurred_on`, deliberately: see decision 3.

### Port

`AccountRepository` gains one method, contract in `usecase/ports.go`:

```go
// MonthlyMovements returns each account's net movement per calendar month
// from since onward, in that account's own currency, counting only
// transactions dated on or after that account's own opening_balance_as_of --
// the same filter AccountView.Balance is computed with, and it must stay the
// same or the two disagree. Months with no movement are absent rather than
// zero-valued. There is no upper bound: a future-dated transaction is
// already inside AccountView.Balance, so it must be inside this too.
MonthlyMovements(ctx context.Context, householdID string, since time.Time) ([]AccountMonthMovement, error)
```

```go
type AccountMonthMovement struct {
	AccountID string
	Month     time.Time // first of the month, midnight, UTC
	Delta     domain.Money
}
```

---

## The formulas, pinned

`today` is read once at the HTTP layer and passed in, never read from a clock
inside the usecase — the house rule, and `RetroService.List` is the model.

| Name | Formula |
|---|---|
| `current` | `startOfMonth(today)` (budget.go's helper, reused) |
| window | twelve months, `months[i] = current.AddDate(0, -(11-i), 0)`, oldest first |
| counted account | non-archived **and** `CountTowardNetWorth` **and** convertible to the primary currency — the same three filters `Summary` applies |
| bucket of a movement | `startOfMonth(m.Month)`, clamped: any month after `current` is counted as `current` |
| `bal(a, current)` | `AccountView.Balance` — the repository's figure, untouched |
| `bal(a, months[i-1])` | `bal(a, months[i]) − movement(a, months[i])` |
| `tracked(a, m)` | `startOfMonth(a.OpeningBalanceAsOf) <= m` |
| `NetWorth(m)` | `Σ SignedNetWorthAmount(rate.Apply(bal(a, m)))` over counted accounts with `tracked(a, m)`; **nil** when that set is empty |
| `NetWorth(current)` | the headline itself — each account contributes the very `inPrimary` value `Summary`'s own loop added, not a re-conversion of it |
| `Complete(m)` | every counted account satisfies `tracked(a, m)` |
| `changeBasisPoints` | `round((NetWorth(current) − NetWorth(prev)) × 10000 / NetWorth(prev))`, half away from zero; **nil** unless decision 5's four conditions all hold |

**Conversion order matches `Summary` exactly**, because it *is* `Summary`: the
rate is fetched once per distinct currency pair per request, applied per
account per month (`Rate.Apply` rounds half away from zero), and the converted
figures are summed. Never sum first — `domain.Money.Add` refuses two
currencies, deliberately. The newest month reuses the headline's own converted
value rather than recomputing it (decision 3), so the last bar equals the
figure above it by construction, not by arithmetic that happens to agree.

**Overflow.** `delta × 10000` overflows `int64` above roughly 9.2 × 10^14 in
minor units. The guard is the same fail-closed rule as everywhere: if the
multiplication would overflow, `changeBasisPoints` is nil. `ports.go`'s
`mulOverflows` is the existing helper and the existing precedent.

---

## API

`summaryDTO` gains one optional field, present only on the computable branch:

```json
{
  "currency": "SGD",
  "computable": true,
  "netWorthMinor": 24835000,
  "trend": {
    "points": [
      { "month": "2025-09", "netWorthMinor": null,     "complete": false },
      { "month": "2025-10", "netWorthMinor": 21140000, "complete": false },
      { "month": "2026-08", "netWorthMinor": 24835000, "complete": true }
    ],
    "changeBasisPoints": 210
  }
}
```

- `points` is always exactly twelve, oldest first — the middle nine are
  elided in the example above.
- `month` is `YYYY-MM`, matching the retros mood chart's own wire format.
- `netWorthMinor` is `null` for an unknown month — null, never `0`, and never
  omitted, because the frontend needs the slot to keep the axis aligned.
- `complete` is `false` on an unknown month too; the frontend branches on
  `netWorthMinor === null` first.
- `changeBasisPoints` is an integer in basis points, absent when suppressed.
  Integer rather than a float percent: a percentage is not money, but there is
  no reason to put a float on this wire when `210` says `2.10%` exactly.
- `trend` itself is absent when the summary is incomputable, and the whole
  summary is absent for a limited member. Neither state needs a new guard.

---

## Screens and states

### Finances

`NetWorthCard` renders, in one bordered card, matching design line 354:

```
Net worth                                        Last 12 months
S$248,350  ▲ 2.1%

█ █ █ █ █ █ █ █ █ █ █ █
Aug '25   Nov '25   Feb '26   Jul '26
```

The grid widens from `md:grid-cols-2` to `md:grid-cols-[1.7fr_1fr]` so the
chart column matches the design's proportions beside the breakdown card. One
column on mobile, unchanged.

New file `web/src/features/money/NetWorthChart.tsx` — inline SVG, `viewBox`
plus `w-full` so it scales, no charting dependency. `MoodChart.tsx`'s comment
carries the reasoning and this follows it exactly, including `role="img"` with
an `aria-label` that names the range and how many of the twelve months carry a
figure. Bars, not a line: the design draws bars, and a bar's height from a
zero baseline is the right encoding for a stock quantity.

States:

| State | What is drawn |
|---|---|
| Twelve known months | twelve bars, newest in the accent colour, the rest muted |
| Some months incomplete | those bars in a lighter tint, plus one line under the axis: *"Bars before Mar '26 are missing accounts added later."* |
| Some months unknown | those slots left empty, axis unchanged |
| Fewer than two known months | the "not enough history yet" line in place of the chart; the figure above stays. The day-one household lands here |
| Not computable | unchanged — the existing branch returns before any of this |
| Negative net worth | bars run downward from a zero baseline; the axis is drawn at zero, not at the minimum |

A month label every third tick, as `MoodChart` does — twelve labels overlap at
this width. `Aug '25`, short month plus apostrophe-year, is its own helper in
`money/copy.ts`; `retroCopy`'s `monthShortLabel` is a different format (no
year) and is not shared across features for this.

### Overview

The card gains one line under the figure: `▲ 2.1% this month`. Same
suppression rules, same component, no chart.

---

## Error handling

- Repository error from the new query — `MapDomainError`, the same as every
  other read on this handler. No partial summary is returned: the screen shows
  one set of numbers or none.
- Rate lookup failure for a currency — that account is already in
  `ExcludedNoRate` and is excluded from every bar, exactly as it is excluded
  from the headline. It never contributes a partially-converted figure.
- Overflow on any `Add` or `Apply` — returns the error, as `Summary` already
  does. No bar is drawn from an arithmetic result that overflowed.
- A movement row for an account not in the views list (archived between the
  two reads, in principle) — ignored. The chart describes the accounts the
  summary describes.

---

## Testing

**The mutation-checked one** (`proving-tests-can-fail`): *the last point equals
the summary's own net worth*, in a household with an SGD account and an IDR
account, with a future-dated transaction present. Break it by changing the new
query's `>=` to `>`, or by dropping the future-month clamp, and it must go red.
This single assertion is what stops the third computation of net worth from
drifting from the first two — and note it can only *stay* true because the
newest point reuses the headline's own converted value (decision 3). A test
alone cannot guarantee this: `fx.StaticProvider` returns one rate forever, so
two independent conversion passes would agree in the suite and could disagree
in production.

Usecase, against in-memory doubles:

- a month before every account's opening date is `nil`, not `0`
- a household with exactly one known month yields a trend the frontend renders
  as the empty state, not as a single bar
- a month with one tracked account and one untracked is `complete: false` and
  still carries a figure
- a transfer between two of the household's own accounts moves the household
  total by nothing in that month
- a liability's balance pulls the bar down (`SignedNetWorthAmount`)
- an archived account contributes to no bar, not merely to no headline
- `countTowardNetWorth: false` likewise
- `changeBasisPoints` is nil when the previous month is incomplete, nil at a
  zero or negative base, and `210` for 2.10% growth

Postgres, testcontainers: the new query returns per-account monthly deltas
with the opening-date filter applied, on both the outgoing and incoming sides,
including the `received_amount_minor` case.

HTTP: a limited member's response carries no `summary` and therefore no
`trend`; an incomputable summary carries no `trend`.

Frontend: twelve bars render; an unknown month leaves its slot empty; an
incomplete month is tinted and the note appears; the percentage is absent when
the server omits it; the empty state replaces the chart when nothing is known.

**Browser walk** at `http://localhost:5173`, per CLAUDE.md and the
`verifying-in-the-real-environment` skill — the seeded household for the happy
path, then a fresh account added today to see the incomplete-month marking and
the suppressed percentage with real data.

---

## Out of scope

- **Snapshot table / immutable history.** Decision 1 names the trigger.
- **Historical FX rates.** Decision 2.
- **Choosing the window.** Twelve months, as the design draws. No range picker.
- **Hover tooltips on bars.** The design has none; the axis and the headline
  carry the reading.
- **The Overview card's assets/liabilities split.** Tracker row 427's other
  half, unrelated to the trend, stays 🟡 with its gap renamed.

---

## Definition of done

- `make lint && make test` green
- the last-bar-equals-headline test mutation-checked, red on a broken filter
- browser walk done, including one account added mid-window
- `docs/FEATURE_TRACKER.md`: Finances row "Net worth with 12-month trend"
  🟡 → ✅; row 427's gap narrowed to the assets/liabilities split; **line 489's
  prose rewritten** — it asserts a snapshot table is required and it is wrong;
  summary table recounted, not guessed
- `docs/SYSTEM_DESIGN.md` updated via the `maintaining-system-design` skill —
  the new query, the new port method, the new DTO field, and the Finances
  request flow
- `docs/LEARNING.md`: a document asserted an implementation constraint nobody
  verified, and the assertion, not the difficulty, is what deferred this
  feature
- `hunting-sibling-defects` pass: any other place that computes a balance or a
  net worth from these same rows
