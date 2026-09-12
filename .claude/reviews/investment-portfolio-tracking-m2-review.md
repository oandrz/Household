# Code review: investment portfolio tracking, milestone 2

**Reviewed**: 2026-09-13
**Scope**: `b435132..HEAD` on `hearth-portfolio-holdings` — 55 files, +6495/−121
**Base**: `main` (branch unmerged, no PR open, so this is a local review with no GitHub publish)
**Decision**: **REQUEST CHANGES** — one HIGH, three MEDIUM. Nothing CRITICAL; no security finding.
**Status**: H1, M1, M2 and M3 are fixed on this branch (`2d0a614`, `3ee11f4`, `0a41a69`, `c91c171`). See *Resolution* at the end.

The working tree is clean, so the skill's "uncommitted changes" scope would have
reported nothing. Reviewed the milestone's committed work instead.

## Summary

The arithmetic is the strongest part of this change and I could not break it:
the cost-carried fold makes "buying more must never read as profit" true by
construction, both ends of every period are folded from the beginning of the
holding's life, and blanking is per component with a reason attached. Guards,
currency handling and SQL parameterisation are all correct. What the review
found is on the edges: one silent failure in the income panel, and three places
where a rule the codebase states is not applied here.

## Findings

### HIGH

**H1 — A failed income delete fails silently.**
`web/src/features/money/HoldingIncomePanel.tsx:172-178`

```tsx
onClick={async () => {
  await deleteIncome.mutateAsync({ id: holding.id, incomeId: row.id });
  setConfirmingDelete(null);
}}
```

No `try`/`catch`. A rejected mutation (network drop, 404 on an already-deleted
row, 422) leaves the row on screen, the panel stuck in its "Really remove /
Keep" state, no message anywhere, and an unhandled promise rejection in the
console. The person is left to conclude the button is broken, or to click it
again.

The file's own header says it mirrors `HoldingLotsPanel.tsx`, and that file's
delete does the right thing eleven lines of the same shape away
(`HoldingLotsPanel.tsx:179-187`): catches, sets `eventError` from
`ApiError.message`, and clears the confirm state either way. The submit handler
in this very file also catches. Only the delete path was missed.

This is `docs/LEARNING.md` pattern 5 (silent partial success is worse than
loud failure) and pattern 1 (the sibling kept the bug).

**Fix** — mirror the lots panel:

```tsx
onClick={async () => {
  try {
    await deleteIncome.mutateAsync({ id: holding.id, incomeId: row.id });
  } catch (err) {
    setError(err instanceof ApiError ? err.message : "That entry could not be removed.");
  } finally {
    setConfirmingDelete(null);
  }
}}
```

`setError` already exists (`:53`) and is already rendered with `role="alert"`
(`:140`) — in the form section above the list, which is exactly where the lots
panel puts its own delete error too.

### MEDIUM

**M1 — `DeleteIncome` does not scope the delete to the holding in the URL.**
`api/internal/usecase/holding.go:416-421`, `queries/holding_income.sql:33-34`

```go
if _, err := s.d.Holdings.Get(ctx, householdID, holdingID); err != nil { return err }
return s.d.Income.Delete(ctx, householdID, incomeID)
```

The holding is fetched and then not used. `DELETE FROM holding_income WHERE
household_id = $1 AND id = $2` means `DELETE /holdings/A/income/{row-of-B}`
succeeds and removes a row belonging to holding B. The household boundary
holds, so this is not a data-disclosure or cross-tenant bug — it is an
integrity gap inside one household, reachable only by hand-crafting a request
the UI never sends.

It exactly mirrors the shipped `DeleteHoldingEvent`
(`queries/holding.sql:109-110`), so the milestone copied an existing shape
rather than inventing one.

**Correction, established while fixing this**: I wrote that the event path was
"arguably worse". It is not — it was not broken at all. `DeleteWithFold` lists
the named holding's events and returns `ErrNotFound` for an id that is not among
them, so the mismatched pair was already refused there. Only income was
reachable. The event query was scoped anyway, as a second lock, and the test
records that mutating it does not go red.

**Fix**: add `AND holding_id = $3` to both queries and pass it. Two-line
change per query, plus a test that a mismatched pair is a 404.

**M2 — `scope, _ := RequestScope(r)` drops the "is there a scope" answer.**
`api/internal/adapter/http/holding_handlers.go:718` (and twelve siblings in the
same file, all from milestone 1)

Every handler in this file discards the boolean. `household_handlers.go:69-73`
does the opposite and writes a 401. Today the routes sit behind
`requireSession`, so the value cannot be absent and nothing is exploitable —
this is defence in depth, not a live hole. It matters more for the new report
route than for the M1 routes: with an empty household id the report returns
**200 and an empty body** rather than refusing, which is the failure shape
nobody notices. `CLAUDE.md` states "fail closed on values you did not
construct".

**Fix**: one helper (`scopeOr401(w, r) (Scope, bool)`), thirteen call sites,
no behaviour change while the middleware stays as it is.

**M3 — The confirm/remove controls in the income panel are below the touch
target size every other control on the screen uses.**
`HoldingIncomePanel.tsx:170-196`

The Remove / Really remove / Keep buttons are bare `text-[12px]` spans with no
`min-h-11`. Every other interactive element in this milestone — the form's
button, the period toggle, the page links — carries `min-h-11 … sm:min-h-0`,
which is the repo's 44px-on-phones convention. `HoldingLotsPanel.tsx` gives its
equivalent buttons the full treatment. The report was walked at 320px, so this
is a real phone surface.

### LOW

- **L1 — `ErrPeriodIndexOutOfRange` has no mapping in `errors.go`**, so it
  would surface as `INTERNAL`/500. It is unreachable through HTTP today
  (`PeriodContaining` and `PeriodsEndingOn` only ever build valid indices), but
  its three siblings added in this milestone are all mapped and this one was
  missed. One `case`.
- **L2 — `deps.Clock.Now()` is called twice in `handleHoldingReport`**
  (`holding_handlers.go:736` and `:749`), once for the computation and once per
  period for the `current` flag. A call that straddles midnight on a period's
  last day would compute the report for one period and mark a different one
  current. Hoist it into a local.
- **L3 — `subtractComponent` negates with `-b.Native.Amount`**
  (`holding_return.go:290-296`), which overflows for `math.MinInt64`. Every
  other overflow in this path is caught inside `Money.Add`; this one happens
  before it. Not reachable with real money.
- **L4 — The income `note` is unbounded.** Only the global
  `http.MaxBytesReader` caps it (`errors.go:70`). Consistent with holding
  events and valuations, which are also unbounded; `agreement.go` bounds its
  notes with `utf8.RuneCountInString`. Worth one decision covering all four
  rather than a patch here.
- **L5 — "Quarter by period" / "Half-year by period"** is the section heading
  the chart sits under (`PortfolioReportPage.tsx:119-121`). It reads as a typo
  even though it is not. "By quarter" / "By half-year" / "By year" would say
  the same thing in the owner's words.
- **L6 — The chart encodes holdings by hue alone** in the bars
  (`PeriodReturnChart.tsx:36-44`). Mitigated: each `rect` carries a `<title>`,
  the legend pairs each colour with its name, and the table below repeats every
  figure — so nothing is available only in colour. Noting it because six hues
  is already the honest limit and a seventh holding silently reuses the first.
- **L7 — Bars can overflow the plot** when `barWidth` hits its `Math.max(1, …)`
  floor (`PeriodReturnChart.tsx:96-99`), which needs roughly 150 holdings in one
  household. Recording the bound, not asking for a fix.

## What was checked and found sound

- **Security**: no credentials, no string-built SQL (every query is sqlc-generated
  and parameterised), no `dangerouslySetInnerHTML`, no path handling. The new
  routes sit inside the existing `money` space + `requireOwner` group, with
  `requireCSRF` on all four writes (`router.go:384-409`). Authorisation is at
  the inbound edge only, and no service takes an actor — ADR 8 holds.
- **Money**: `int64` minor units and an ISO 4217 code everywhere; no `float64`
  in any monetary path. The second cost pool stores amounts, never rates.
- **Period arithmetic**: inclusive ends via `AddDate(0, months, 0).AddDate(0,0,-1)`
  rather than 90-day addition; `Previous()` decrements the year; `startOfDayUTC`
  reads the date in the value's own location (the defect this milestone caught
  RED).
- **Blanking**: `Unrealised`/`Total` are pointers with a `BlankReason`, never a
  zero; `Realised`/`Income`/`Fees` are never blanked, because no price is
  involved in computing them. The Zod schema and the page both honour it.
- **Index alignment**: the chart's `offset + i` and the table's
  `periods.length - 1 - reversedIndex` both map to the right period; checked by
  hand at both ends of both lists.
- **Fail-closed switches**: `ParsePeriodKind`, `ParseIncomeKind` and
  `sumIncome`'s `default` all refuse rather than assume.
- **N+1**: none. `Report` issues five reads for the whole household and folds in
  memory, and the cost of doing so is written down at the call site along with
  the scale at which it stops being free.
- **Migration**: `CHECK (kind IN ('income','fee'))`, `CHECK (amount_minor > 0)`,
  paired-NULL check on the primary amount, and an index matching the read the
  report actually performs. The absence of a fold-shaped index is deliberate and
  explained in the file.

## Validation

| Check | Result |
|---|---|
| Arch lint (`scripts/arch-lint.sh`) | Pass |
| Type check (`tsc --noEmit`) | Pass |
| ESLint | Pass |
| `go vet ./...` | Pass |
| Go tests (`go test ./...`) | Pass — exit 0, zero FAIL. Ran at `5ca4c5a`; `git diff --name-only 5ca4c5a..HEAD -- api/` is empty, so the result still describes this tree. |
| Frontend tests (`vitest run`) | Pass — 95 files, 872 tests |
| Browser | Walked: figures hand-checked, cache invalidation via in-app navigation, blanking with reason, Q/H/Y toggle, 320px, 401 signed out, clean console on a fresh load |

## Files reviewed in full

`domain/period.go`, `domain/holding_return.go`, `domain/holding_income.go`,
`usecase/holding.go` (income + `Report`), `usecase/household.go` (currency
guard), `http/holding_handlers.go` (report + income), `http/router.go` (guards),
`http/errors.go` (mappings), `migrations/00020_holding_income.sql`,
`queries/holding_income.sql`, `PeriodReturnChart.tsx`,
`PortfolioReportPage.tsx`, `HoldingIncomePanel.tsx`, `usePortfolioReport.ts`,
`holdingSchemas.ts`, `useHoldings.ts`. Generated sqlc output and the test files
were read for shape rather than line by line.

## Recommendation

Fix **H1** before this merges — it is eight lines and it is a shipped silent
failure on a destructive action. **M1** and **M2** are each a small change that
also repairs the milestone-1 siblings they were copied from, so they are worth
doing in this branch rather than filing. **M3** is a one-class edit. Everything
under LOW can be recorded and left.


## Resolution

All four actionable findings are fixed on this branch, test-first, each
mutation-checked.

| # | Fix | Commit | RED evidence |
|---|---|---|---|
| H1 | The failed income delete catches, shows `ApiError.message`, clears the confirmation in a `finally` | `0a41a69` | New `HoldingIncomePanel.test.tsx` — the assertion failed and vitest printed the unhandled `ApiError` the product was leaking |
| M1 | Both child-row deletes scope on `holding_id` (queries, sqlc, repositories, port, doubles) | `2d0a614` | `TestDeletingAChildRowThroughTheWrongHoldingIsNotFound` — "income delete via the wrong holding = 204, want 404" |
| M2 | `requireScope` helper; thirteen call sites | `3ee11f4` | `TestAHoldingHandlerWithNoScopeRefuses` — the report handler did not merely fail to refuse, it **panicked** on a nil dependency |
| M3 | The three row buttons carry `min-h-11` | `0a41a69` | Measured in the browser at 390px: 44px each, and 44px again in the confirm state |

Mutations run for these fixes: six, five killed. The survivor is the event
half of M1, for the reason in the correction above, and it is annotated at the
assertion rather than papered over.

The LOW findings (L1–L7) are recorded and not fixed; none is reachable through
the product.

Two incidental defects were caught by `make lint` while fixing, both of the
same class and both in test stubs that vitest happily ran: a partial
`UpdateHoldingBody` and a `heldNano` typed as a string. Green tests and a green
build remain two different claims.
