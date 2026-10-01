-- ListAccounts and ListAccountsIncludingArchived are two queries, not one
-- with a boolean parameter, because a `WHERE archived_at IS NULL OR $2`
-- predicate would not use the partial index accounts_household_idx that the
-- live-only form does.
--
-- The LEFT JOIN lets a shared account (owner_membership_id IS NULL) come
-- back as a row with a NULL owner name rather than vanishing.

-- name: ListAccounts :many
SELECT sqlc.embed(a), u.display_name AS owner_name,
       -- balance_minor is opening balance plus every transaction dated ON OR
       -- AFTER opening_balance_as_of (spec 2026-07-30-hearth-finance-fixes):
       -- the opening balance is the figure at the START of that day, so a
       -- same-day transaction still moves it, while an earlier one is
       -- already baked into that figure.
       --
       -- Two filtered sums, not one, because an account can be both a
       -- transfer's source and another transfer's destination. The incoming
       -- side prefers received_amount_minor -- what actually landed, in this
       -- account's currency -- over amount_minor, which is in the sender's
       -- currency; no conversion happens here or can, since everything above
       -- is already in this account's own currency.
       (a.opening_balance_minor
        - COALESCE((SELECT SUM(t.amount_minor) FROM transactions t
                    WHERE t.from_account_id = a.id
                      AND t.occurred_on >= a.opening_balance_as_of), 0)
        + COALESCE((SELECT SUM(COALESCE(t.received_amount_minor, t.amount_minor))
                    FROM transactions t
                    WHERE t.to_account_id = a.id
                      AND t.occurred_on >= a.opening_balance_as_of), 0)
       )::bigint AS balance_minor
FROM accounts a
LEFT JOIN memberships m ON m.id = a.owner_membership_id
LEFT JOIN users u ON u.id = m.user_id
WHERE a.household_id = $1 AND a.archived_at IS NULL
ORDER BY a.created_at;

-- name: ListAccountsIncludingArchived :many
SELECT sqlc.embed(a), u.display_name AS owner_name,
       -- See ListAccounts above for why this is two filtered sums with
       -- >= and why the incoming side prefers received_amount_minor.
       (a.opening_balance_minor
        - COALESCE((SELECT SUM(t.amount_minor) FROM transactions t
                    WHERE t.from_account_id = a.id
                      AND t.occurred_on >= a.opening_balance_as_of), 0)
        + COALESCE((SELECT SUM(COALESCE(t.received_amount_minor, t.amount_minor))
                    FROM transactions t
                    WHERE t.to_account_id = a.id
                      AND t.occurred_on >= a.opening_balance_as_of), 0)
       )::bigint AS balance_minor
FROM accounts a
LEFT JOIN memberships m ON m.id = a.owner_membership_id
LEFT JOIN users u ON u.id = m.user_id
WHERE a.household_id = $1
ORDER BY a.archived_at NULLS FIRST, a.created_at;

-- GetAccount scopes by household_id as well as id, like every query in this
-- file: id alone would let a caller in one household read another's row by
-- guessing a uuid, and the HTTP session already supplies the household id.
-- name: GetAccount :one
SELECT sqlc.embed(a), u.display_name AS owner_name,
       -- See ListAccounts above for why this is two filtered sums with
       -- >= and why the incoming side prefers received_amount_minor.
       -- Get and List must compute this the same way, or the two disagree
       -- on the same account's balance.
       (a.opening_balance_minor
        - COALESCE((SELECT SUM(t.amount_minor) FROM transactions t
                    WHERE t.from_account_id = a.id
                      AND t.occurred_on >= a.opening_balance_as_of), 0)
        + COALESCE((SELECT SUM(COALESCE(t.received_amount_minor, t.amount_minor))
                    FROM transactions t
                    WHERE t.to_account_id = a.id
                      AND t.occurred_on >= a.opening_balance_as_of), 0)
       )::bigint AS balance_minor
FROM accounts a
LEFT JOIN memberships m ON m.id = a.owner_membership_id
LEFT JOIN users u ON u.id = m.user_id
WHERE a.household_id = $1 AND a.id = $2;

-- name: CreateAccount :one
INSERT INTO accounts (
    household_id, nickname, type, owner_membership_id,
    opening_balance_minor, opening_balance_currency, opening_balance_as_of,
    count_toward_net_worth, visible_to_limited_members
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
RETURNING id, household_id, nickname, type, owner_membership_id, opening_balance_minor, opening_balance_currency, opening_balance_as_of, count_toward_net_worth, visible_to_limited_members, archived_at, created_at;

-- name: UpdateAccount :one
UPDATE accounts
SET nickname                   = $3,
    type                       = $4,
    owner_membership_id        = $5,
    opening_balance_minor      = $6,
    opening_balance_currency   = $7,
    opening_balance_as_of      = $8,
    count_toward_net_worth     = $9,
    visible_to_limited_members = $10
WHERE household_id = $1 AND id = $2
RETURNING id, household_id, nickname, type, owner_membership_id, opening_balance_minor, opening_balance_currency, opening_balance_as_of, count_toward_net_worth, visible_to_limited_members, archived_at, created_at;

-- SetAccountArchived stamps or clears archived_at. There is no DELETE query
-- in this file: transactions reference these rows, and deleting an account
-- would erase their history along with it.
-- name: SetAccountArchived :one
UPDATE accounts
SET archived_at = $3
WHERE household_id = $1 AND id = $2
RETURNING id, household_id, nickname, type, owner_membership_id, opening_balance_minor, opening_balance_currency, opening_balance_as_of, count_toward_net_worth, visible_to_limited_members, archived_at, created_at;

-- MembershipBelongsToHousehold answers whether a membership is in this
-- household, so an account can never be assigned to a member of another one.
-- name: MembershipBelongsToHousehold :one
SELECT EXISTS (
    SELECT 1 FROM memberships WHERE id = $1 AND household_id = $2
);

-- ListAccountMonthlyMovements backs the twelve-month trend: one row per
-- account per calendar month with any movement, in that account's own
-- currency. No conversion happens here and none can: the FX provider lives
-- in the usecase layer (MonthTotalsQuery says the same).
--
-- The filter mirrors ListAccounts's balance expression, including the
-- missing upper bound on occurred_on. A transaction dated after today is
-- refused when it is written, but one stored earlier may be, and it is
-- already inside the anchor balance. It must stay identical: the trend walks
-- backwards from AccountView.Balance by subtracting these deltas, so any
-- mismatch makes older bars disagree with the headline figure. The service
-- buckets any later month into the current one.
-- name: ListAccountMonthlyMovements :many
SELECT account_id,
       month,
       SUM(delta_minor)::bigint AS delta_minor,
       currency
FROM (
    SELECT t.from_account_id AS account_id,
           DATE_TRUNC('month', t.occurred_on)::date AS month,
           -t.amount_minor AS delta_minor,
           a.opening_balance_currency AS currency
    FROM transactions t
    JOIN accounts a ON a.id = t.from_account_id
    WHERE a.household_id = sqlc.arg('household_id')
      AND t.occurred_on >= a.opening_balance_as_of
      AND t.occurred_on >= sqlc.arg('since')::date
    UNION ALL
    SELECT t.to_account_id,
           DATE_TRUNC('month', t.occurred_on)::date,
           COALESCE(t.received_amount_minor, t.amount_minor),
           a.opening_balance_currency
    FROM transactions t
    JOIN accounts a ON a.id = t.to_account_id
    WHERE a.household_id = sqlc.arg('household_id')
      AND t.occurred_on >= a.opening_balance_as_of
      AND t.occurred_on >= sqlc.arg('since')::date
) movements
GROUP BY account_id, month, currency
ORDER BY account_id, month;
