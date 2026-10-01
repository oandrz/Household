# Keep a holding's worth inside the amount ceiling — design

Date: 2026-10-01. Source: hearth-architect. Sibling of QA ISSUE-001 (the amount
ceiling, `domain.MaxAmountMinor`). Product-owner decisions at the end.

## Problem

A holding's market value is `held quantity × unit price`. Both factors can pass
`CheckAmountWithinLimit` while the product overflows int64: a price is stored,
its own `POST` answers 500, and `GET /holdings` fails for the whole household
afterwards. Probe: 100,000 units, price 1e14 minor →
`amount overflows a signed 64-bit integer`.

The reads never multiply by the *current* quantity alone: the period report
multiplies a price by the quantity held at a period boundary, possibly long ago.
So the rule must bound the **largest quantity the holding has ever held**.

## Constraints found

- `Quantity.Value` returns `ErrAmountOverflow` when the result does not fit
  (`api/internal/domain/quantity.go`), mapped to an internal 500
  (`api/internal/adapter/http/errors.go`).
- Two reads multiply: `Portfolio` (latest price × final held,
  `usecase/holding.go`) and `ReturnOver` (latest price inside a window × held at
  the window's end, up to 12 periods, `domain/holding_return.go`). The report
  then adds market value to cost, realised and income, so "the product fits
  int64" is not enough headroom.
- Write-then-fail: every holding write answers by re-reading the whole portfolio
  (`adapter/http/holding_handlers.go`), and `RecordValuation` stores with no
  fold and no lock.
- The lock already exists for events: `InsertWithFold` / `DeleteWithFold` lock
  the holding row, list events in the same transaction and call the caller's
  rule (`adapter/postgres/holding_repo.go`, contract in
  `usecase/ports_money_holding.go`).
- "Real figures stay valid" is the ceiling's first criterion; lump sums are
  quantity 1, so a unit-price cap is also the lump-sum cap; large quantities are
  expected (`web/src/features/money/holdingSchemas.ts`).
- One multiply in the package; the new check must go through `Quantity.Value`.
- The held add in `domain/holding.go` is unchecked and safe only by accident
  (overflow lands negative, `NewQuantity` refuses) with a mis-worded 422.
- Exactly one write edge: HTTP. `hearthctl` and Telegram write no holdings.
- The frontend shows `ApiError.message` for event, price and delete failures
  (`HoldingLotsPanel.tsx`); no change needed for a new refusal.
- `errors_test.go` forbids reusing `ErrAmountTooLarge` for a second meaning.

## Options

- **A. Two static caps (max held, max unit price).** ~40 lines. Refuses real
  data: ten million shares of a Rp 70 stock (~S$56k), or Rp 10bn lump sums.
- **B. One value rule under the holding lock (chosen).** Peak quantity ever held
  × every recorded price (native and household-currency) must not exceed
  `MaxAmountMinor`. ~150 lines, one port contract change, no migration, no
  frontend change. Conservative: refuses a pair no read computes (10M units held
  in January and sold, then a price of 1e12 in June).
- **C. Read tolerance only ("value unavailable").** Does not fix the defect: the
  price is still stored and answers 200.

Why prices must reach the fold from the repository: if the service read prices
before calling `InsertWithFold`, a price write could commit between that read
and the lock. Prices must be read inside the lock.

## Files and symbols

| Layer | File | Change |
|---|---|---|
| domain | `api/internal/domain/holding.go` | `Position` gains `PeakHeld Quantity`, tracked in the existing loop. The held add gets an explicit overflow check returning `ErrInvalidQuantity`. |
| domain | `api/internal/domain/holding_value_limit.go` (new) | `CheckHoldingValueWithinLimit(peak Quantity, prices []Valuation) error`. For each price and primary price: `peak.Value(price)`, compare to `MaxAmountMinor`. An `ErrAmountOverflow` from `Value` is also "too large". |
| domain | `api/internal/domain/errors.go` | New sentinel `ErrHoldingValueTooLarge`. |
| domain | `api/internal/domain/amount_limit.go` | Comment only: name the holding rule as the one computed figure held to the limit. |
| usecase | `api/internal/usecase/ports_money_holding.go` | New named type `HoldingFold func(events []domain.HoldingEvent, prices []domain.Valuation) error`, documented once: "both as they WOULD be after this write, read inside the lock". `InsertWithFold`/`DeleteWithFold` take it. `Upsert` is replaced by `UpsertWithFold(ctx, v, fold HoldingFold)` so there is no unguarded door. `Delete` gets one sentence: removing a price cannot break the rule. |
| usecase | `api/internal/usecase/holding.go` | One private closure builder (`Position`, then `CheckHoldingValueWithinLimit(position.PeakHeld, prices)`) passed at the three write sites. Leave `today` parameters and `refuseFutureDate` alone. |
| adapter | `api/internal/adapter/postgres/holding_repo.go` | `InsertWithFold` and `DeleteWithFold` list the holding's valuations in the same transaction and pass them. `HoldingValuationRepo` gains the pool. `UpsertWithFold` locks, lists events and prices, swaps out any same-day row for the new one, folds, upserts. "Same day" must use the same conversion the upsert key uses, or a corrective lower price is judged against the row it replaces. No new SQL. |
| adapter | `api/internal/adapter/http/errors.go` | One row: 422 `HOLDING_VALUE_TOO_LARGE`, "That would make this holding worth more than Hearth can record. Check the quantity and the price for extra digits." Must read correctly from a price write, a purchase, and deleting a sale. |
| tests | `api/internal/usecase/holding_double_test.go` | Doubles take the new callback; the valuation double gets `UpsertWithFold`. |
| docs | `SYSTEM_DESIGN.md`, `FEATURE_TRACKER.md`, `LEARNING.md` | Portfolio sequence diagram and prose, ports table; tracker gap 1 closed (row stays partly built for gaps 2 and 3); LEARNING pattern 1. |

## Build order

1. Port shape only: `HoldingFold`, pass prices, rename to `UpsertWithFold`,
   update doubles. Closures ignore prices. Every existing test stays green,
   including `TestTwoRacingDisposalsCannotBothCommit`.
2. Domain: `PeakHeld`, the sentinel, `CheckHoldingValueWithinLimit`, the explicit
   held add, with their tests.
3. Service and error row: one closure, three call sites. Usecase, HTTP and
   racing tests.
4. Mutation check, browser walk, docs.
5. Separate, later release (not this branch): CHECK constraints for the
   per-row ceiling, once the production audit returns zero rows.

## Tests that pin it

- Domain: `Position` reports the peak, not the final held (buy 100, sell 90).
  The check accepts exactly at the limit and refuses one minor unit past, on
  native and primary price. A product that overflows int64 answers
  `ErrHoldingValueTooLarge`, not `ErrAmountOverflow`. Real figures pass:
  10,000,000 units at 7,000 minor, and 1 unit at 1e12. The limit equals
  `MaxAmountMinor`.
- Usecase, on the doubles (refuse BEFORE storing): a refused `RecordValuation`
  leaves the valuation double with zero rows; a refused acquisition leaves the
  event double unchanged; deleting a sale that would restore too large a
  quantity is refused and the sale is still there; a back-dated price is judged
  against the peak; correcting a day's price downward succeeds.
- Postgres: a racing test shaped on `TestTwoRacingDisposalsCannotBothCommit`:
  one goroutine writes a large quantity, one a large price, each legal alone;
  exactly one is refused. The fold receives the price list with the same-day
  row replaced.
- HTTP, the inverse of the repro: buy 100,000 units, then `POST …/valuations` at
  1e14 answers 422 `HOLDING_VALUE_TOO_LARGE`; `GET …/valuations` is empty;
  `GET /holdings` and `GET /holdings/report?kind=quarter` answer 200. The
  reverse order: price first, then the purchase is refused.
- Mutation: delete the check call from the closure; the HTTP test must fail.

## Other decisions in the design

- Read tolerance: never for accounts (a balance feeds net worth, a total; the
  `ErrNoRate` rule applies). For holdings not built now.
- CHECK constraints: yes, per-row only, in a later release, plain validated
  `ADD CONSTRAINT`; `goal_contributions` must exempt
  `source = 'budget_rollover'`; pin with a schema test that formats
  `domain.MaxAmountMinor` into the probe.
- Unchecked multiplications: `domain/bill.go` accepted (capped × ≤12, then
  checked convert/add). `domain/budget.go`, `domain/goal.go`,
  `usecase/nudge.go` accepted for this slice (need 923 ceiling-sized rows; wrong
  percentage, not a failed page): file one follow-up issue.

## Side findings (not part of this fix)

- `docs/SYSTEM_DESIGN.md` and `docs/HANDOVER.md` say
  `DELETE /holdings/{id}/valuations/{valuationId}` is routed; `router.go`
  registers no such route. Recovery from a bad price today is re-posting the
  same date.
- `docs/HANDOVER.md` says the portfolio branch is unmerged; migration 00019 is
  an ancestor of `origin/main`.

## Risks

- A holding already in violation refuses every write except the repair
  (deleting the bad purchase, or re-entering that day's price lower). The
  production audit must be clean before deploy.
- `writeOneHolding` still re-reads the whole portfolio, so another holding's
  unreadable ledger makes every holding write answer 500 after committing;
  unreachable for this defect after the audit.

## Product-owner decisions (taken on the architect's recommendation)

1. No holding may be worth more than `MaxAmountMinor` (1e14 minor: S$1 trillion,
   Rp 1 trillion ≈ S$80M). One number, inherits the FX and browser promises.
2. Portfolio does not get a per-holding "value unavailable" state now. Revisit
   if the production audit finds a row the operator cannot correct.

## Build record (2026-10-01, branch `fix/qa-2026-10-01`)

Build-order steps 1 to 4 are built. Step 5 (CHECK constraints) is not, by
decision: it waits for the production audit below. No migration.

How the build differs from the table above, and why:

- The service's closure builder is named `holdingRule`.
- The prices handed to the fold are in no promised order. The rule checks
  every price, so it needs none, and promising one would be a contract
  nothing uses.
- `HoldingValuationRepo.ListByHolding` and the guarded writes share one
  `listValuations` helper, and all three guarded writes share
  `lockHoldingAndRead`, so there is one copy of "lock, then read both".
- `LockHolding`'s SQL comment named only the event writes. It now names the
  price write, and sqlc was regenerated for that comment. No query changed.
- The explicit held add returns `ErrInvalidQuantity` as specified. Its
  sentence on the wire is still the generic quantity one ("Enter a quantity
  as a number, up to nine decimal places"), which does not describe the
  case. Left as specified; a better sentence needs its own sentinel.
- `HoldingEventRepository.Insert` and `Delete` (unguarded) are still on the
  port. Only tests call them. The design removed the unguarded price write
  and did not mention these two, so they were left and reported.

## Audit SQL for the operator (run read-only against production before deploy)

Ran clean (zero rows) against the local dev database. If the preflight returns
NULL, production predates the holdings migration: skip query 4 and the
`holding_*` lines of query 1. Query 4 divides exactly where Go rounds half away,
so a product within half a minor unit of the limit is flagged but would pass.

```sql
BEGIN TRANSACTION READ ONLY;

-- 0. Preflight: NULL means the holdings tables are not deployed.
SELECT to_regclass('public.holding_valuations') AS holdings_deployed;

-- 1. Single rows past the ceiling. Expect zero rows.
--    A 'budget_rollover' contribution is computed and may legitimately appear.
SELECT r.* FROM (
  SELECT 'accounts.opening_balance_minor' AS field, a.household_id, a.id, a.opening_balance_minor AS value FROM accounts a
  UNION ALL SELECT 'transactions.amount_minor', t.household_id, t.id, t.amount_minor FROM transactions t
  UNION ALL SELECT 'transactions.received_amount_minor', t.household_id, t.id, t.received_amount_minor
    FROM transactions t WHERE t.received_amount_minor IS NOT NULL
  UNION ALL SELECT 'budgets.expected_income_minor', b.household_id, b.id, b.expected_income_minor
    FROM budgets b WHERE b.expected_income_minor IS NOT NULL
  UNION ALL SELECT 'budget_lines.cap_minor', b.household_id, l.id, l.cap_minor
    FROM budget_lines l JOIN budgets b ON b.id = l.budget_id
  UNION ALL SELECT 'goals.target_amount_minor', g.household_id, g.id, g.target_amount_minor FROM goals g
  UNION ALL SELECT 'goals.planned_monthly_minor', g.household_id, g.id, g.planned_monthly_minor FROM goals g
  UNION ALL SELECT 'goal_contributions.amount_minor (' || c.source || ')', c.household_id, c.id, c.amount_minor
    FROM goal_contributions c
  UNION ALL SELECT 'bills.amount_minor', b.household_id, b.id, b.amount_minor FROM bills b
  UNION ALL SELECT 'bill_payments.amount_minor', p.household_id, p.id, p.amount_minor FROM bill_payments p
  UNION ALL SELECT 'holding_events.amount_minor', e.household_id, e.id, e.amount_minor FROM holding_events e
  UNION ALL SELECT 'holding_events.primary_amount_minor', e.household_id, e.id, e.primary_amount_minor
    FROM holding_events e WHERE e.primary_amount_minor IS NOT NULL
  UNION ALL SELECT 'holding_valuations.unit_price_minor', v.household_id, v.id, v.unit_price_minor FROM holding_valuations v
  UNION ALL SELECT 'holding_valuations.primary_unit_price_minor', v.household_id, v.id, v.primary_unit_price_minor
    FROM holding_valuations v WHERE v.primary_unit_price_minor IS NOT NULL
  UNION ALL SELECT 'holding_income.amount_minor', i.household_id, i.id, i.amount_minor FROM holding_income i
  UNION ALL SELECT 'holding_income.primary_amount_minor', i.household_id, i.id, i.primary_amount_minor
    FROM holding_income i WHERE i.primary_amount_minor IS NOT NULL
) r
WHERE r.value > 100000000000000 OR r.value < -100000000000000
ORDER BY r.field, r.household_id;

-- 2. Account balances past the ceiling. Same expression as ListAccounts,
--    without the ::bigint cast that fails. Above 9223372036854775807
--    means GET /accounts is failing today.
SELECT * FROM (
  SELECT a.household_id, a.id, a.nickname, a.opening_balance_currency AS currency,
         a.opening_balance_minor::numeric
         - COALESCE((SELECT SUM(t.amount_minor::numeric) FROM transactions t
                     WHERE t.from_account_id = a.id AND t.occurred_on >= a.opening_balance_as_of), 0)
         + COALESCE((SELECT SUM(COALESCE(t.received_amount_minor, t.amount_minor)::numeric) FROM transactions t
                     WHERE t.to_account_id = a.id AND t.occurred_on >= a.opening_balance_as_of), 0) AS balance_minor
  FROM accounts a
) b
WHERE abs(b.balance_minor) > 100000000000000
ORDER BY abs(b.balance_minor) DESC;

-- 3. Goal totals past the ceiling.
SELECT g.household_id, g.id, g.name, SUM(c.amount_minor::numeric) AS contributed_minor
FROM goals g JOIN goal_contributions c ON c.goal_id = g.id
GROUP BY g.household_id, g.id, g.name
HAVING abs(SUM(c.amount_minor::numeric)) > 100000000000000;

-- 4. Holdings that break, or would be refused by, the new rule.
--    lowest_nano < 0 is a separate defect: an oversold ledger that cannot fold.
WITH running AS (
  SELECT e.holding_id,
         SUM(CASE e.kind WHEN 'acquisition' THEN e.quantity_nano::numeric
                         ELSE -e.quantity_nano::numeric END)
           OVER (PARTITION BY e.holding_id
                 ORDER BY e.occurred_on, e.created_at, e.id
                 ROWS BETWEEN UNBOUNDED PRECEDING AND CURRENT ROW) AS held_nano
  FROM holding_events e
), peak AS (
  SELECT holding_id, MAX(held_nano) AS peak_nano, MIN(held_nano) AS lowest_nano
  FROM running GROUP BY holding_id
), price AS (
  SELECT holding_id,
         MAX(unit_price_minor)::numeric         AS max_price_minor,
         MAX(primary_unit_price_minor)::numeric AS max_primary_price_minor
  FROM holding_valuations GROUP BY holding_id
)
SELECT h.household_id, h.id, h.name, h.currency,
       p.peak_nano, p.lowest_nano, pr.max_price_minor, pr.max_primary_price_minor,
       round(p.peak_nano * COALESCE(pr.max_price_minor, 0) / 1000000000)         AS worst_value_minor,
       round(p.peak_nano * COALESCE(pr.max_primary_price_minor, 0) / 1000000000) AS worst_primary_value_minor
FROM holdings h
JOIN peak p ON p.holding_id = h.id
LEFT JOIN price pr ON pr.holding_id = h.id
WHERE p.lowest_nano < 0
   OR p.peak_nano > 9223372036854775807
   OR p.peak_nano * GREATEST(COALESCE(pr.max_price_minor, 0), COALESCE(pr.max_primary_price_minor, 0))
        / 1000000000 > 100000000000000
ORDER BY h.household_id, h.name;

ROLLBACK;
```
