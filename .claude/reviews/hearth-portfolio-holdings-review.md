# Review: branch `hearth-portfolio-holdings`

**Reviewed**: 2026-09-12
**Branch**: `hearth-portfolio-holdings` → `main`
**Scope**: 9 commits, 39 files, +7,094 / −3
**Decision**: **REQUEST CHANGES** — one HIGH defect, three MEDIUM

## Note on scope

`git diff --name-only HEAD` reports only `.gitignore`, which was already
modified before this work began and is not part of it. By the letter of local
review mode that is "nothing to review". The useful target is the branch, so
this reviews `main...HEAD` instead — the milestone-1 portfolio feature.

This is a self-review of code written in this same session. That is worth
stating plainly: the findings below are the ones I went looking for *because*
I wrote it, and a second reader would likely find others.

## Summary

The feature is correct where it was tested hardest — the fold, the overflow
arithmetic, the guards, the wire contract — and was walked in a real browser.
The defects below are all in the places that received the least adversarial
attention: date handling, concurrency, and inputs nobody thought to make
absurd.

---

## Findings

### CRITICAL

None. No hardcoded credentials, no injection surface (every query is sqlc-
generated and parameterised), no new authorisation concept — the routes reuse
the existing `money`+`owner` group, proven at the wire by
`TestALimitedMembersCreateReachesNoService`.

### HIGH

**1. `today()` is UTC, so the date defaults to yesterday for eight hours a day.**
`web/src/features/money/HoldingLotsPanel.tsx:269`

```ts
function today(): string {
  return new Date().toISOString().slice(0, 10);   // UTC
}
```

`toISOString()` renders in UTC. This household is in Singapore (UTC+8), so
between 00:00 and 08:00 local time every purchase, sale and price defaults to
**the previous day**. A price recorded at 1am on the 10th is stamped the 9th,
and since `ListLatestValuations` orders by `as_of`, it can be silently
outranked by a price the household entered earlier.

This also breaks a convention established twice in the same directory, each
with a comment explaining itself — `AccountModal.tsx:95` and
`GoalContributionsPanel.tsx:42` both do:

```ts
const now = new Date();
return `${now.getFullYear()}-${pad(now.getMonth() + 1)}-${pad(now.getDate())}`;
```

**Fix**: use the local-time getters, matching the two siblings. Not a shared
import — those files deliberately each carry their own copy rather than
coupling three features' date handling.

Not caught by the tests because they never assert the default value, and not
caught by the browser walk because it ran at 22:00 local, where UTC and local
agree on the date.

### MEDIUM

**2. The oversell check is check-then-insert, with no transaction.**
`api/internal/usecase/holding.go` — `RecordEvent` and `DeleteEvent`

Both read the events, fold them, and then write, with nothing holding a lock
between the two. Two concurrent sales of the same holding can each validate
against a position that the other is about to consume, and both commit —
leaving a holding that cannot be folded, which is exactly the state
`DeleteEvent`'s own comment says must never be reachable ("a ledger that
cannot be folded is a screen that cannot render").

Likelihood here is genuinely low: two partners selling the same holding in the
same second. But the cost when it happens is a holding whose page throws on
every load, and the only way to fix it is the screen that is broken.

**Fix**: do the fold and the insert inside one `pgx.BeginFunc`, the way
`GoalRepo.Create` already wraps its goal-plus-contribution write. That moves
the invariant to where the row is written rather than one call above it.

**3. Nothing refuses a future-dated valuation or event.**
`api/internal/usecase/holding.go` — `RecordEvent`, `RecordValuation`

Accounts already refuse this (`ErrOpeningBalanceInFuture`,
`00004_accounts.sql`), and for the reason that applies here too: a valuation
dated 2030 wins `ORDER BY as_of DESC` forever, so one typo pins the holding's
market value to a number nobody can explain, and the `valuedAt` label reads a
date in the future without comment.

**Fix**: mirror `ErrOpeningBalanceInFuture` for both. The service already takes
a clock parameter pattern (`SetArchived(…, now)`), so there is somewhere to put
it without inventing one.

**4. Every write re-folds the entire household portfolio.**
`api/internal/adapter/http/holding_handlers.go` — `writeOneHolding`, 6 call sites

Each write answers by calling `Portfolio(…, true)`, which issues three queries
(all holdings, all events, all valuations) and folds every position, to return
one row. Recording ten lots costs ten full portfolio reads.

This is deliberate and defensible — it is why a write answers with the same
derived shape a read does, and `writeGoal` re-reads for the same reason. At a
household's scale (single-digit holdings, tens of events) it is free.

**Fix**: none now. Worth a comment naming the threshold at which it stops being
free, so the next person meets a decision rather than a surprise.

### LOW

**5. `heldNano`/`quantityNano` exceed JavaScript's safe integer range above ~9M units.**
`web/src/features/money/holdingSchemas.ts:47`

`z.number()` on a JSON integer past 2^53 (9.007e15) loses precision silently.
10 million units is 1e16 nano. Harmless today — **no component reads either
field**; `held` and `quantity` (the strings) are what render, which is the whole
point of sending both. Flagged so that a future caller reaching for the integer
knows it is display-unsafe at the top of the range.

**6. The not-in-net-worth banner flashes in.**
`web/src/features/money/PortfolioPage.tsx`

`holdings.data?.notInNetWorth` is undefined while loading, so the banner is
absent and then appears. Cosmetic.

---

## What held up

Worth recording, since a review that only lists faults misrepresents the work:

- **The arithmetic.** `Quantity.Value`'s 128-bit path, `Money.Prorate`, and the
  average-cost fold were checked by 25 mutations across four tasks; five
  survived first time and each pointed at something real, including a layering
  bug that changed the code rather than the test.
- **The ordering contract.** `(occurred_on, created_at, id)` is stated in the
  port's doc comment, the SQL, and an index built for that clause, and pinned
  by tests at both the domain and repository level.
- **The guards.** Proven at the wire, including that the capability check runs
  *before* the service (asserted at the database, not by the status code).
- **The browser walk.** Found two defects the whole test suite could not: the
  page had no styling at all (invented class names in a Tailwind project), and
  money rendered as `SGD 21,990.00` rather than `S$`.

---

## Validation

Nothing has changed since the last commit (`83bd5aa`), so these are that
commit's results, not re-run for this review:

| Check | Result |
|---|---|
| `make lint` (arch lint, tsc --noEmit, eslint, go vet) | Pass |
| `npx vitest run` | Pass — 90 files, 846 tests |
| `go test ./...` (whole module, Docker up) | Pass — every package |
| `go test ./cmd/hearthctl` (routes table diff) | Pass |
| Browser walk at :5173 | Pass — recorded in `83bd5aa` |

---

## Recommendation

Fix **1** before merge — it is a one-line change, it makes the product wrong
for a third of every day in this household's own timezone, and it contradicts a
convention the same directory documents twice.

**2** and **3** are judgement calls for the product owner: both are real, both
are cheap, neither blocks a feature that is not yet in net worth. If they are
deferred, they belong in `docs/HANDOVER.md`'s open items rather than in
someone's memory.

**4**, **5** and **6** need no code today, only a comment each.
