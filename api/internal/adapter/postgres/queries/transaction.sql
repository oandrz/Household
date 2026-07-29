-- SeedCategories is one statement, not a read-then-write, and that is the
-- whole point: two simultaneous first requests would both read "no categories"
-- and both insert. ON CONFLICT DO NOTHING against UNIQUE (household_id, name)
-- makes the loser of that race a no-op instead of a duplicate-key error.
--
-- Thirteen literal VALUES rows, not unnest($2::text[], $3::text[], $4::int[]):
-- sqlc's query analyzer does not carry the catalog entry for the multi-array
-- form of unnest (real Postgres resolves it specially, not as an ordinary
-- unnest(anyarray) overload), and rejects the query outright with "function
-- unnest(unknown, unknown, unknown) does not exist". One round trip either
-- way; the row count is pinned to len(domain.StarterCategories()) because
-- this is a literal list, not a loop.
-- name: SeedCategories :exec
INSERT INTO categories (household_id, name, kind, sort_order)
VALUES
    (sqlc.arg(household_id), sqlc.arg(name_1), sqlc.arg(kind_1), sqlc.arg(sort_order_1)),
    (sqlc.arg(household_id), sqlc.arg(name_2), sqlc.arg(kind_2), sqlc.arg(sort_order_2)),
    (sqlc.arg(household_id), sqlc.arg(name_3), sqlc.arg(kind_3), sqlc.arg(sort_order_3)),
    (sqlc.arg(household_id), sqlc.arg(name_4), sqlc.arg(kind_4), sqlc.arg(sort_order_4)),
    (sqlc.arg(household_id), sqlc.arg(name_5), sqlc.arg(kind_5), sqlc.arg(sort_order_5)),
    (sqlc.arg(household_id), sqlc.arg(name_6), sqlc.arg(kind_6), sqlc.arg(sort_order_6)),
    (sqlc.arg(household_id), sqlc.arg(name_7), sqlc.arg(kind_7), sqlc.arg(sort_order_7)),
    (sqlc.arg(household_id), sqlc.arg(name_8), sqlc.arg(kind_8), sqlc.arg(sort_order_8)),
    (sqlc.arg(household_id), sqlc.arg(name_9), sqlc.arg(kind_9), sqlc.arg(sort_order_9)),
    (sqlc.arg(household_id), sqlc.arg(name_10), sqlc.arg(kind_10), sqlc.arg(sort_order_10)),
    (sqlc.arg(household_id), sqlc.arg(name_11), sqlc.arg(kind_11), sqlc.arg(sort_order_11)),
    (sqlc.arg(household_id), sqlc.arg(name_12), sqlc.arg(kind_12), sqlc.arg(sort_order_12)),
    (sqlc.arg(household_id), sqlc.arg(name_13), sqlc.arg(kind_13), sqlc.arg(sort_order_13))
ON CONFLICT (household_id, name) DO NOTHING;

-- name: CountCategories :one
SELECT count(*) FROM categories WHERE household_id = $1;

-- name: ListCategories :many
SELECT id, household_id, name, kind, sort_order, archived_at, created_at
FROM categories
WHERE household_id = $1 AND archived_at IS NULL
ORDER BY sort_order;

-- name: ListCategoriesIncludingArchived :many
SELECT id, household_id, name, kind, sort_order, archived_at, created_at
FROM categories
WHERE household_id = $1
ORDER BY sort_order;

-- CategoryBelongsToHousehold answers whether a category is in this household,
-- so a transaction can never reference one from another. It mirrors
-- MembershipBelongsToHousehold, and exists for the same reason: the check must
-- not be a Get that leaks whether the id exists elsewhere.
-- name: CategoryBelongsToHousehold :one
SELECT EXISTS (
    SELECT 1 FROM categories
    WHERE id = $1 AND household_id = $2 AND archived_at IS NULL
);

-- name: GetCategoryKind :one
SELECT kind FROM categories WHERE id = $1 AND household_id = $2;

-- transactionColumns is repeated in full in each query below rather than
-- factored into a view: sqlc generates a distinct row struct per query, and a
-- view would hide which columns each one actually reads.
--
-- The three LEFT JOINs are what let an expense (no destination), a shared
-- transaction (no payer) and an uncategorised one come back as rows with NULL
-- names rather than vanishing.
--
-- before_from_opening and before_to_opening are computed here, next to the
-- dates they compare, so the rule that only transactions after an account's
-- opening date move its balance lives in one place. <= against
-- opening_balance_as_of, mirroring the strict > the balance sum will use in
-- Task 9: a transaction dated *on* the opening date is already reflected in
-- the figure someone asserted was true that day, so it must not also be
-- counted as moving the balance forward from it.

-- name: GetTransaction :one
SELECT t.id, t.household_id, t.kind, t.occurred_on, t.description,
       t.category_id, t.paid_by_membership_id, t.from_account_id, t.to_account_id,
       t.amount_minor, t.amount_currency,
       t.received_amount_minor, t.received_amount_currency, t.created_at,
       c.name AS category_name,
       u.display_name AS paid_by_name,
       fa.nickname AS from_account_name,
       ta.nickname AS to_account_name,
       (fa.id IS NOT NULL AND t.occurred_on <= fa.opening_balance_as_of) AS before_from_opening,
       (ta.id IS NOT NULL AND t.occurred_on <= ta.opening_balance_as_of) AS before_to_opening
FROM transactions t
LEFT JOIN categories  c  ON c.id  = t.category_id
LEFT JOIN memberships m  ON m.id  = t.paid_by_membership_id
LEFT JOIN users       u  ON u.id  = m.user_id
LEFT JOIN accounts    fa ON fa.id = t.from_account_id
LEFT JOIN accounts    ta ON ta.id = t.to_account_id
WHERE t.household_id = $1 AND t.id = $2;

-- name: CreateTransaction :one
INSERT INTO transactions (
    household_id, kind, occurred_on, description, category_id,
    paid_by_membership_id, from_account_id, to_account_id,
    amount_minor, amount_currency, received_amount_minor, received_amount_currency
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
RETURNING id, household_id, kind, occurred_on, description, category_id,
          paid_by_membership_id, from_account_id, to_account_id,
          amount_minor, amount_currency, received_amount_minor,
          received_amount_currency, created_at;

-- name: UpdateTransaction :one
UPDATE transactions
SET kind                     = $3,
    occurred_on              = $4,
    description              = $5,
    category_id              = $6,
    paid_by_membership_id    = $7,
    from_account_id          = $8,
    to_account_id            = $9,
    amount_minor             = $10,
    amount_currency          = $11,
    received_amount_minor    = $12,
    received_amount_currency = $13
WHERE household_id = $1 AND id = $2
RETURNING id, household_id, kind, occurred_on, description, category_id,
          paid_by_membership_id, from_account_id, to_account_id,
          amount_minor, amount_currency, received_amount_minor,
          received_amount_currency, created_at;

-- DeleteTransaction is scoped by household_id like every other query here, and
-- returns the id so the caller can tell "removed" from "there was nothing to
-- remove" without a second round trip. A transaction is hard deleted -- unlike
-- an account, nothing references it, so nothing is orphaned.
-- name: DeleteTransaction :one
DELETE FROM transactions
WHERE household_id = $1 AND id = $2
RETURNING id;
