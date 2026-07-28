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
