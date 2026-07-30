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

-- ORDER BY sort_order, name: the name is a deterministic tie-break for two
-- rows that share a sort_order, which CreateCategory's own comment explains
-- can happen -- without it, a tie's relative order would depend on
-- whichever physical row order Postgres happens to scan, which can differ
-- between two reads of the exact same data.
-- name: ListCategories :many
SELECT id, household_id, name, kind, sort_order, archived_at, created_at
FROM categories
WHERE household_id = $1 AND archived_at IS NULL
ORDER BY sort_order, name;

-- name: ListCategoriesIncludingArchived :many
SELECT id, household_id, name, kind, sort_order, archived_at, created_at
FROM categories
WHERE household_id = $1
ORDER BY sort_order, name;

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

-- CreateCategory appends the new row to the end of the household's sort
-- order, computed by the same statement as the insert rather than a separate
-- read-max-then-write -- that closes the round-trip window where a second
-- request's read could land between this one's read and its write.
--
-- It does not close the window between two concurrent transactions: under
-- READ COMMITTED, two INSERTs that each begin before the other commits both
-- see the same pre-insert MAX(sort_order) and can both compute and commit
-- the same value -- there is no UNIQUE constraint on sort_order to make the
-- second one fail. Closing that window would need either an advisory lock
-- or a unique constraint, both rejected here: sort_order has no correctness
-- requirement, only a display one, so a tied value is cosmetic (two
-- categories drawn in the same slot, ListCategories' own ORDER BY sort_order,
-- name breaking the tie deterministically rather than leaving it to
-- whatever order Postgres happens to scan rows in) -- not a bug worth a lock
-- around every category creation.
--
-- A name collision (including against an archived row, which keeps its slot
-- in UNIQUE (household_id, name)) surfaces as a 23505 that
-- CategoryRepo.Create maps through translate to domain.ErrCategoryNameTaken.
-- name: CreateCategory :one
INSERT INTO categories (household_id, name, kind, sort_order)
VALUES (
    sqlc.arg(household_id),
    sqlc.arg(name),
    sqlc.arg(kind),
    (SELECT COALESCE(MAX(sort_order), 0) + 1 FROM categories WHERE household_id = sqlc.arg(household_id))
)
RETURNING id, household_id, name, kind, sort_order, archived_at, created_at;

-- RenameCategory scopes the UPDATE to household_id as well as id, so a
-- category id from another household returns no row -- CategoryRepo.Rename
-- turns that into domain.ErrNotFound via translate's pgx.ErrNoRows case, the
-- same as every other single-row lookup in this package -- rather than
-- silently renaming (impossible, the WHERE excludes it) or leaking whether
-- the id exists elsewhere.
-- name: RenameCategory :one
UPDATE categories
SET name = sqlc.arg(name)
WHERE id = sqlc.arg(id) AND household_id = sqlc.arg(household_id)
RETURNING id, household_id, name, kind, sort_order, archived_at, created_at;

-- SetCategoryArchived stamps or clears archived_at depending on the caller's
-- boolean, scoped the same way RenameCategory is. The COALESCE is the whole
-- of "archiving is idempotent": archiving an already-archived row keeps its
-- first archived_at rather than moving it forward to now(), so two calls
-- (or a retry) never disagree about when a category was actually archived.
-- name: SetCategoryArchived :one
UPDATE categories
SET archived_at = CASE WHEN sqlc.arg(archived)::boolean THEN COALESCE(archived_at, now()) ELSE NULL END
WHERE id = sqlc.arg(id) AND household_id = sqlc.arg(household_id)
RETURNING id, household_id, name, kind, sort_order, archived_at, created_at;

-- transactionColumns is repeated in full in each query below rather than
-- factored into a view: sqlc generates a distinct row struct per query, and a
-- view would hide which columns each one actually reads.
--
-- The three LEFT JOINs are what let an expense (no destination), a shared
-- transaction (no payer) and an uncategorised one come back as rows with NULL
-- names rather than vanishing.
--
-- before_from_opening and before_to_opening are computed here, next to the
-- dates they compare, so the rule lives in one place. Strict < against
-- opening_balance_as_of, mirroring the >= the balance sum uses in
-- queries/account.sql: the opening balance is the figure at the START of
-- its day (spec 2026-07-30, decision 1), so only a transaction dated
-- strictly before it is already inside that figure and excluded.

-- name: GetTransaction :one
SELECT t.id, t.household_id, t.kind, t.occurred_on, t.description,
       t.category_id, t.paid_by_membership_id, t.from_account_id, t.to_account_id,
       t.amount_minor, t.amount_currency,
       t.received_amount_minor, t.received_amount_currency, t.created_at,
       c.name AS category_name,
       u.display_name AS paid_by_name,
       fa.nickname AS from_account_name,
       ta.nickname AS to_account_name,
       (fa.id IS NOT NULL AND t.occurred_on < fa.opening_balance_as_of) AS before_from_opening,
       (ta.id IS NOT NULL AND t.occurred_on < ta.opening_balance_as_of) AS before_to_opening
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

-- ListTransactions serves the ledger and all five of its filters.
--
-- Each filter is written as `(sqlc.narg(x)::type IS NULL OR column = ...)`, so
-- an unset filter is a no-op inside one prepared statement rather than a
-- separate query per combination. Thirty-two hand-written variants would drift
-- from each other, and a concatenated string would be an injection surface.
--
-- The account filter matches EITHER side: a transfer belongs in the ledger of
-- both accounts it touches.
--
-- The keyset predicate is the row-value comparison (occurred_on, id) < (cursor
-- date, cursor id), which matches transactions_household_date_idx exactly.
-- Comparing the pair rather than the date alone is what makes two transactions
-- on the same day page correctly.
--
-- LIMIT is $N + 1 in the caller, not here: the extra row is how the caller
-- learns another page exists without counting the table.
-- name: ListTransactions :many
SELECT t.id, t.household_id, t.kind, t.occurred_on, t.description,
       t.category_id, t.paid_by_membership_id, t.from_account_id, t.to_account_id,
       t.amount_minor, t.amount_currency,
       t.received_amount_minor, t.received_amount_currency, t.created_at,
       c.name AS category_name,
       u.display_name AS paid_by_name,
       fa.nickname AS from_account_name,
       ta.nickname AS to_account_name,
       (fa.id IS NOT NULL AND t.occurred_on < fa.opening_balance_as_of) AS before_from_opening,
       (ta.id IS NOT NULL AND t.occurred_on < ta.opening_balance_as_of) AS before_to_opening
FROM transactions t
LEFT JOIN categories  c  ON c.id  = t.category_id
LEFT JOIN memberships m  ON m.id  = t.paid_by_membership_id
LEFT JOIN users       u  ON u.id  = m.user_id
LEFT JOIN accounts    fa ON fa.id = t.from_account_id
LEFT JOIN accounts    ta ON ta.id = t.to_account_id
WHERE t.household_id = $1
  AND (sqlc.narg('kind')::text IS NULL OR t.kind = sqlc.narg('kind')::text)
  AND (sqlc.narg('account_id')::uuid IS NULL
       OR t.from_account_id = sqlc.narg('account_id')::uuid
       OR t.to_account_id   = sqlc.narg('account_id')::uuid)
  AND (sqlc.narg('category_id')::uuid IS NULL OR t.category_id = sqlc.narg('category_id')::uuid)
  AND (sqlc.narg('paid_by')::uuid IS NULL OR t.paid_by_membership_id = sqlc.narg('paid_by')::uuid)
  AND (sqlc.narg('month_start')::date IS NULL
       OR (t.occurred_on >= sqlc.narg('month_start')::date
           AND t.occurred_on < (sqlc.narg('month_start')::date + INTERVAL '1 month')))
  AND (sqlc.narg('cursor_date')::date IS NULL
       OR (t.occurred_on, t.id) < (sqlc.narg('cursor_date')::date, sqlc.narg('cursor_id')::uuid))
ORDER BY t.occurred_on DESC, t.id DESC
LIMIT sqlc.arg('row_limit');

-- MonthTotalsQuery returns every transaction in one calendar month. The
-- service converts each amount into the household's primary currency before
-- summing, which SQL cannot do -- the FX provider lives in the usecase layer.
-- name: MonthTotalsQuery :many
SELECT t.id, t.household_id, t.kind, t.occurred_on, t.description,
       t.category_id, t.paid_by_membership_id, t.from_account_id, t.to_account_id,
       t.amount_minor, t.amount_currency,
       t.received_amount_minor, t.received_amount_currency, t.created_at,
       c.name AS category_name,
       u.display_name AS paid_by_name,
       fa.nickname AS from_account_name,
       ta.nickname AS to_account_name,
       (fa.id IS NOT NULL AND t.occurred_on < fa.opening_balance_as_of) AS before_from_opening,
       (ta.id IS NOT NULL AND t.occurred_on < ta.opening_balance_as_of) AS before_to_opening
FROM transactions t
LEFT JOIN categories  c  ON c.id  = t.category_id
LEFT JOIN memberships m  ON m.id  = t.paid_by_membership_id
LEFT JOIN users       u  ON u.id  = m.user_id
LEFT JOIN accounts    fa ON fa.id = t.from_account_id
LEFT JOIN accounts    ta ON ta.id = t.to_account_id
WHERE t.household_id = $1
  AND t.occurred_on >= $2::date
  AND t.occurred_on < ($2::date + INTERVAL '1 month')
ORDER BY t.occurred_on DESC, t.id DESC;
