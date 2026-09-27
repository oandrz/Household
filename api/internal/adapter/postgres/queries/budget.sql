-- CountCategoriesInHousehold answers how many of the given category ids
-- actually belong to this household. BudgetRepo.Upsert compares this against
-- the number of distinct ids it asked about, inside the same transaction as
-- the write, and refuses on a mismatch -- the categories table's own FK only
-- proves a category exists somewhere, never that it is this household's.
-- name: CountCategoriesInHousehold :one
SELECT count(*) FROM categories
WHERE id = ANY(sqlc.arg(category_ids)::uuid[]) AND household_id = $1;

-- GetHouseholdPrimaryCurrency reads the currency budgets.expected_income_minor
-- and budget_lines.cap_minor are denominated in -- both columns deliberately
-- carry no currency column of their own (migrations/00006_budgets.sql), so a
-- Budget's Money is only ever as current as this read.
-- name: GetHouseholdPrimaryCurrency :one
SELECT primary_currency FROM households WHERE id = $1;

-- GetBudget's LEFT JOIN reads back the amount a rollover actually moved --
-- goal_contributions.amount_minor for the row this household-month's stamp
-- names (source = 'budget_rollover') -- never Remaining recomputed from
-- today's transactions. Remaining is normally live (Budgeted minus Spent
-- over every transaction in the month), so without this, a backdated entry,
-- edit or delete in an already-rolled-over month would silently change a
-- "done" sentence's number after the fact. rollover_amount_minor is NULL
-- exactly when rolled_over_at is NULL (a month that was never rolled over
-- has no contribution row to join to), keeping the two in lockstep with no
-- second query, and goal_contributions_one_rollover_per_month
-- (00007_goals.sql) keeps the join from ever duplicating b's row.
-- name: GetBudget :one
SELECT b.id, b.household_id, b.month, b.expected_income_minor, h.primary_currency,
       b.rolled_over_at, b.rollover_goal_id, c.amount_minor AS rollover_amount_minor
FROM budgets b
JOIN households h ON h.id = b.household_id
LEFT JOIN goal_contributions c
       ON c.household_id = b.household_id
      AND c.source_budget_month = b.month
      AND c.source = 'budget_rollover'
WHERE b.household_id = $1 AND b.month = $2;

-- name: ListBudgetLines :many
SELECT category_id, cap_minor FROM budget_lines WHERE budget_id = $1 ORDER BY category_id;

-- UpsertBudget upserts the parent row on (household_id, month) -- the
-- constraint BudgetRepository.Upsert names -- and returns the id the lines
-- below attach to, new or already existing. It never touches budget_lines:
-- BudgetRepo.Upsert deletes and rewrites them as separate statements in the
-- same transaction, so a category-ownership failure caught before this
-- point leaves neither the parent nor the lines touched.
-- RETURNING includes rolled_over_at and rollover_goal_id even though this
-- statement never writes them (only StampBudgetRollover does): ON CONFLICT
-- DO UPDATE leaves them untouched, so RETURNING echoes the row's existing
-- stamp instead of forcing a separate re-read.
-- name: UpsertBudget :one
INSERT INTO budgets (household_id, month, expected_income_minor)
VALUES ($1, $2, $3)
ON CONFLICT (household_id, month) DO UPDATE
SET expected_income_minor = EXCLUDED.expected_income_minor, updated_at = now()
RETURNING id, household_id, month, expected_income_minor, rolled_over_at, rollover_goal_id;

-- name: DeleteBudgetLines :exec
DELETE FROM budget_lines WHERE budget_id = $1;

-- name: InsertBudgetLine :exec
INSERT INTO budget_lines (budget_id, category_id, cap_minor) VALUES ($1, $2, $3);

-- ListBudgetsInRange returns the household's budgeted months between
-- from_month and month inclusive, newest first. It filters rows that
-- already exist rather than generating one per month, so an unbudgeted
-- month is simply absent -- which gives BudgetRepository.History's "[from,
-- month), plus the viewed month if budgeted" contract for free: month is
-- inside the BETWEEN, so the viewed month's row comes back exactly when it
-- exists, with no special-casing.
-- name: ListBudgetsInRange :many
SELECT b.id, b.household_id, b.month, b.expected_income_minor, h.primary_currency,
       b.rolled_over_at, b.rollover_goal_id
FROM budgets b
JOIN households h ON h.id = b.household_id
WHERE b.household_id = $1 AND b.month BETWEEN sqlc.arg(from_month) AND sqlc.arg(to_month)
ORDER BY b.month DESC;

-- name: ListBudgetLinesForBudgets :many
SELECT budget_id, category_id, cap_minor FROM budget_lines
WHERE budget_id = ANY(sqlc.arg(budget_ids)::uuid[])
ORDER BY budget_id, category_id;

-- StampBudgetRollover sets a budget month's rollover stamp, but only if not
-- already stamped: "AND rolled_over_at IS NULL" is what stops two
-- concurrent rollovers for the same month both succeeding -- whichever
-- UPDATE commits first wins the row, and the second finds nothing left to
-- update once it re-checks the WHERE against the committed row.
--
-- Zero rows updated is ambiguous alone -- never budgeted, or already
-- stamped -- so BudgetRepo.RollOverToGoal follows it with
-- GetBudgetRolloverStamp inside the same transaction to tell the two apart,
-- rather than guessing here.
-- name: StampBudgetRollover :one
UPDATE budgets
   SET rolled_over_at = now(), rollover_goal_id = $3, updated_at = now()
 WHERE household_id = $1 AND month = $2 AND rolled_over_at IS NULL
RETURNING id;

-- GetBudgetRolloverStamp is StampBudgetRollover's own follow-up read, run
-- only when that UPDATE matches zero rows: no row at all means the month was
-- never budgeted (BudgetRepo.RollOverToGoal maps that to domain.ErrNotFound);
-- a row whose rolled_over_at is already set means a rollover beat this one to
-- it (domain.ErrRolloverAlreadyDone).
-- name: GetBudgetRolloverStamp :one
SELECT rolled_over_at FROM budgets WHERE household_id = $1 AND month = $2;
