-- CreateHolding inserts one holding. A name collision -- archived rows
-- included, since archived_at is not part of the UNIQUE (account_id, name)
-- key -- surfaces as a 23505 that translate maps to domain.ErrHoldingNameTaken
-- by constraint name, the same pattern CreateGoal and CreateCategory follow.
-- name: CreateHolding :one
INSERT INTO holdings (household_id, account_id, name, instrument, unit, currency)
VALUES ($1, $2, $3, $4, $5, $6)
RETURNING id, household_id, account_id, name, instrument, unit, currency, archived_at, created_at, updated_at;

-- GetHolding is scoped by household as well as id: a holding id belonging to
-- another household must read as absent, never as forbidden, so nothing leaks
-- the existence of another household's rows.
-- name: GetHolding :one
SELECT id, household_id, account_id, name, instrument, unit, currency, archived_at, created_at, updated_at
FROM holdings
WHERE household_id = $1 AND id = $2;

-- ListHoldings returns one household's holdings in a stable name order,
-- joined to the account they sit in so a screen can name it. include_archived
-- matches ListGoalsWithTotals: false returns only live holdings, true returns
-- live and archived together, each carrying its own archived_at.
-- name: ListHoldings :many
SELECT h.id, h.household_id, h.account_id, h.name, h.instrument, h.unit, h.currency, h.archived_at, h.created_at, h.updated_at,
       a.nickname AS account_name,
       (a.archived_at IS NOT NULL)::boolean AS account_archived
FROM holdings h
JOIN accounts a ON a.id = h.account_id
WHERE h.household_id = $1 AND (h.archived_at IS NULL OR sqlc.arg(include_archived)::boolean)
ORDER BY h.name, h.id;

-- UpdateHolding changes the editable fields. Neither currency nor account_id
-- is among them: a holding's currency is what every one of its events is
-- denominated in, and moving a holding between accounts would move money
-- between accounts without a ledger row to say so. Both are recreate-only.
-- name: UpdateHolding :one
UPDATE holdings
SET name = $3, instrument = $4, unit = $5, updated_at = now()
WHERE household_id = $1 AND id = $2
RETURNING id, household_id, account_id, name, instrument, unit, currency, archived_at, created_at, updated_at;

-- name: SetHoldingArchived :one
UPDATE holdings
SET archived_at = sqlc.narg(archived_at)::timestamptz, updated_at = now()
WHERE household_id = $1 AND id = $2
RETURNING id, household_id, account_id, name, instrument, unit, currency, archived_at, created_at, updated_at;

-- CountLiveHoldingsForAccount is what stops an account's type being changed
-- out from under its holdings. usecase/account.go patches type freely, so a
-- cash account could otherwise end up holding 300g of gold.
-- name: CountLiveHoldingsForAccount :one
SELECT COUNT(*)::bigint FROM holdings
WHERE household_id = $1 AND account_id = $2 AND archived_at IS NULL;

-- LockHolding takes a row lock on one holding so that two writers folding its
-- events cannot both pass a check the other is about to invalidate. It returns
-- the currency only because a query must return something; the lock is the
-- point. Callers use it inside a transaction with InsertHoldingEvent or
-- DeleteHoldingEvent -- see HoldingEventRepository.InsertWithFold.
-- name: LockHolding :one
SELECT currency FROM holdings
WHERE household_id = $1 AND id = $2
FOR UPDATE;

-- name: InsertHoldingEvent :one
INSERT INTO holding_events (holding_id, household_id, kind, quantity_nano, amount_minor,
                            primary_amount_minor, primary_currency, occurred_on, note)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
RETURNING id, holding_id, household_id, kind, quantity_nano, amount_minor,
          primary_amount_minor, primary_currency, occurred_on, note, created_at;

-- ListHoldingEvents is the fold's input and its ORDER BY is a CONTRACT, not a
-- preference. occurred_on is a date, so two events can share one, and
-- domain.Holding.Position sorts stably -- meaning it keeps whatever order it
-- is handed for a tie. The tie is therefore broken here, by the order the
-- events were actually recorded in (created_at, then id).
--
-- Change this ordering and a household's realised gain changes with it,
-- silently: on identical same-day events, buy-then-sell realises 750 where
-- sell-then-buy realises 1000. holding_events_fold_idx exists for exactly
-- this clause.
-- name: ListHoldingEvents :many
SELECT e.id, e.holding_id, e.household_id, e.kind, e.quantity_nano, e.amount_minor,
       e.primary_amount_minor, e.primary_currency, e.occurred_on, e.note, e.created_at,
       h.currency
FROM holding_events e
JOIN holdings h ON h.id = e.holding_id
WHERE e.household_id = $1 AND e.holding_id = $2
ORDER BY e.occurred_on, e.created_at, e.id;

-- ListHoldingEventsForHousehold is the same contract across every holding, so
-- the portfolio page folds each position without one query per holding.
-- name: ListHoldingEventsForHousehold :many
SELECT e.id, e.holding_id, e.household_id, e.kind, e.quantity_nano, e.amount_minor,
       e.primary_amount_minor, e.primary_currency, e.occurred_on, e.note, e.created_at,
       h.currency
FROM holding_events e
JOIN holdings h ON h.id = e.holding_id
WHERE e.household_id = $1
ORDER BY e.holding_id, e.occurred_on, e.created_at, e.id;

-- name: DeleteHoldingEvent :execrows
DELETE FROM holding_events WHERE household_id = $1 AND id = $2;

-- UpsertValuation writes one price per holding per day. Re-entering a day's
-- price is a correction, not a second opinion, so the UNIQUE (holding_id,
-- as_of) key turns the second write into an update -- which removes the
-- question of which of two rows for one day the report should believe.
-- name: UpsertValuation :one
INSERT INTO holding_valuations (holding_id, household_id, unit_price_minor,
                                primary_unit_price_minor, primary_currency, as_of, note)
VALUES ($1, $2, $3, $4, $5, $6, $7)
ON CONFLICT (holding_id, as_of) DO UPDATE
SET unit_price_minor = EXCLUDED.unit_price_minor,
    primary_unit_price_minor = EXCLUDED.primary_unit_price_minor,
    primary_currency = EXCLUDED.primary_currency,
    note = EXCLUDED.note
RETURNING id, holding_id, household_id, unit_price_minor, primary_unit_price_minor,
          primary_currency, as_of, note, created_at;

-- name: ListValuations :many
SELECT v.id, v.holding_id, v.household_id, v.unit_price_minor, v.primary_unit_price_minor,
       v.primary_currency, v.as_of, v.note, v.created_at, h.currency
FROM holding_valuations v
JOIN holdings h ON h.id = v.holding_id
WHERE v.household_id = $1 AND v.holding_id = $2
ORDER BY v.as_of DESC, v.id;

-- ListLatestValuations is one row per holding: the newest price each has. A
-- holding with no valuation at all produces NO ROW rather than a zero one --
-- the caller reads an absent holding as "no price recorded", which is what it
-- must show on screen rather than a figure of zero.
-- name: ListLatestValuations :many
SELECT DISTINCT ON (v.holding_id)
       v.id, v.holding_id, v.household_id, v.unit_price_minor, v.primary_unit_price_minor,
       v.primary_currency, v.as_of, v.note, v.created_at, h.currency
FROM holding_valuations v
JOIN holdings h ON h.id = v.holding_id
WHERE v.household_id = $1
ORDER BY v.holding_id, v.as_of DESC, v.id;

-- name: DeleteValuation :execrows
DELETE FROM holding_valuations WHERE household_id = $1 AND id = $2;
