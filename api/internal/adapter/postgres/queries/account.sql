-- ListAccounts and ListAccountsIncludingArchived are two queries rather than
-- one with a boolean parameter, because the live-only form is what the
-- partial index accounts_household_idx covers and a `WHERE archived_at IS
-- NULL OR $2` predicate would not use it.
--
-- The LEFT JOIN is what makes a shared account (owner_membership_id IS NULL)
-- come back as a row with a NULL owner name rather than vanishing.

-- name: ListAccounts :many
SELECT a.id, a.household_id, a.nickname, a.type, a.owner_membership_id, a.opening_balance_minor, a.opening_balance_currency, a.opening_balance_as_of, a.count_toward_net_worth, a.visible_to_limited_members, a.archived_at, a.created_at, u.display_name AS owner_name,
       -- balance_minor is the opening balance plus every transaction dated
       -- ON OR AFTER opening_balance_as_of. The opening balance means the
       -- figure at the START of that day (spec 2026-07-30-hearth-finance-
       -- fixes, decision 1), so a transaction dated that same day counts —
       -- the day-one flow (create an account today, log today's dinner)
       -- must move the balance. A transaction dated strictly before stays
       -- out: that history is already inside the figure someone asserted.
       --
       -- Two filtered sums rather than one, because an account can be the
       -- source of one transfer and the destination of another. The incoming
       -- side takes received_amount_minor when there is one: that is what
       -- actually landed, in this account's own currency. Using amount_minor
       -- there would add the sending account's currency to this one's.
       --
       -- No conversion happens here and none can: every figure in this
       -- expression is already in this account's own currency.
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
SELECT a.id, a.household_id, a.nickname, a.type, a.owner_membership_id, a.opening_balance_minor, a.opening_balance_currency, a.opening_balance_as_of, a.count_toward_net_worth, a.visible_to_limited_members, a.archived_at, a.created_at, u.display_name AS owner_name,
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

-- GetAccount is scoped by household_id as well as id. Every account query in
-- this file is: an id alone would let a caller in one household read a row in
-- another by guessing a uuid, and the HTTP layer's session gives us the
-- household for free.
-- name: GetAccount :one
SELECT a.id, a.household_id, a.nickname, a.type, a.owner_membership_id, a.opening_balance_minor, a.opening_balance_currency, a.opening_balance_as_of, a.count_toward_net_worth, a.visible_to_limited_members, a.archived_at, a.created_at, u.display_name AS owner_name,
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

-- SetAccountArchived stamps or clears archived_at. There is no DELETE query in
-- this file, deliberately: transactions will reference these rows next slice,
-- and destroying an account would take its history with it.
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
