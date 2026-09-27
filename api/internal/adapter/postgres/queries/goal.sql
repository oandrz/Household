-- CreateGoal inserts the goal row only -- GoalRepo.Create wraps this with
-- the opening contribution insert in one pgx.BeginFunc transaction, so a
-- name collision here rolls back before any contribution is attempted. A
-- collision -- archived rows included, since archived_at is not part of
-- goals' UNIQUE (household_id, name) -- surfaces as a 23505 that translate
-- maps to domain.ErrGoalNameTaken by constraint name, the same pattern
-- CreateCategory uses.
-- name: CreateGoal :one
INSERT INTO goals (household_id, name, target_amount_minor, currency, target_month, planned_monthly_minor)
VALUES ($1, $2, $3, $4, $5, $6)
RETURNING id, household_id, name, target_amount_minor, currency, target_month, planned_monthly_minor, archived_at, created_at, updated_at;

-- InsertGoalContribution writes one contribution row. GoalRepo.Create's
-- opening contribution (source starting_balance), GoalRepo.AddContribution's
-- manual one (source manual), and a budget rollover (source budget_rollover)
-- all share this one query -- a contribution row's shape does not depend on
-- why it was written. It carries no currency column: a contribution is its
-- goal's currency by construction (00007_goals.sql), so the caller supplies
-- the goal's currency separately when it needs a domain.Money back.
-- name: InsertGoalContribution :one
INSERT INTO goal_contributions (goal_id, household_id, amount_minor, occurred_on, note, source, source_budget_month)
VALUES ($1, $2, $3, $4, $5, $6, $7)
RETURNING id, goal_id, household_id, amount_minor, occurred_on, note, source, source_budget_month, created_at;

-- ListGoalsWithTotals returns one household's goals joined to the sum of
-- their own contributions, COALESCEd to 0 (a plain SUM over an empty LEFT
-- JOIN group is NULL, not 0). include_archived is a UNION, not a filter
-- swap (GoalRepository.List): false returns only live goals, true returns
-- live and archived together, each with its own archived_at.
--
-- Ordering is the port's pinned contract: dated goals first, newest
-- target_month first, ties (and every dateless goal, which all compare
-- equal) broken by name. NULLS LAST does the real work -- Postgres's DESC
-- default is NULLS FIRST, so without it a dateless goal would sort ahead of
-- every dated one, exactly what the port forbids.
-- name: ListGoalsWithTotals :many
SELECT g.id, g.household_id, g.name, g.target_amount_minor, g.currency, g.target_month, g.planned_monthly_minor, g.archived_at, g.created_at, g.updated_at,
       COALESCE(SUM(c.amount_minor), 0)::bigint AS contributed_minor
FROM goals g
LEFT JOIN goal_contributions c ON c.goal_id = g.id
WHERE g.household_id = $1 AND (g.archived_at IS NULL OR sqlc.arg(include_archived)::boolean)
GROUP BY g.id, g.household_id, g.name, g.target_amount_minor, g.currency, g.target_month, g.planned_monthly_minor, g.archived_at, g.created_at, g.updated_at
ORDER BY g.target_month DESC NULLS LAST, g.name;

-- GetGoalWithTotal is ListGoalsWithTotals scoped to one goal, so the two can
-- never disagree about what ContributedMinor means. household_id AND id are
-- both required in the WHERE -- a goal id from another household must match
-- no row, so it is indistinguishable from an id that never existed
-- (GoalRepository.Get's own doc comment).
-- name: GetGoalWithTotal :one
SELECT g.id, g.household_id, g.name, g.target_amount_minor, g.currency, g.target_month, g.planned_monthly_minor, g.archived_at, g.created_at, g.updated_at,
       COALESCE(SUM(c.amount_minor), 0)::bigint AS contributed_minor
FROM goals g
LEFT JOIN goal_contributions c ON c.goal_id = g.id
WHERE g.household_id = $1 AND g.id = $2
GROUP BY g.id, g.household_id, g.name, g.target_amount_minor, g.currency, g.target_month, g.planned_monthly_minor, g.archived_at, g.created_at, g.updated_at;

-- UpdateGoal replaces every mutable column -- name, target amount, target
-- month (NULL clears it) and planned monthly -- with no SET clause for
-- currency (not mutable, see GoalService.Update) or archived_at (archiving
-- has its own dedicated method). Omitting them from SET means RETURNING
-- echoes whatever the row already had, matching the in-memory double's
-- "read back off the existing row" behaviour. Same collision contract as
-- CreateGoal. Scoped by household_id AND id, so an id from another
-- household matches no row and translate turns that pgx.ErrNoRows into
-- domain.ErrNotFound.
-- name: UpdateGoal :one
UPDATE goals
SET name = $3, target_amount_minor = $4, target_month = $5, planned_monthly_minor = $6, updated_at = now()
WHERE household_id = $1 AND id = $2
RETURNING id, household_id, name, target_amount_minor, currency, target_month, planned_monthly_minor, archived_at, created_at, updated_at;

-- SetGoalArchived stamps or clears archived_at depending on the caller's
-- boolean, scoped the same way UpdateGoal is. The COALESCE keeps "archiving
-- is idempotent": an already-archived goal keeps its original archived_at
-- rather than moving to the caller's `at`, mirroring SetCategoryArchived's
-- COALESCE(archived_at, now()) with a caller-supplied timestamp standing in
-- for now() -- GoalRepository.SetArchived requires exactly this shape.
-- name: SetGoalArchived :one
UPDATE goals
SET archived_at = CASE WHEN sqlc.arg(archived)::boolean THEN COALESCE(archived_at, sqlc.arg(at)::timestamptz) ELSE NULL END,
    updated_at = now()
WHERE household_id = sqlc.arg(household_id) AND id = sqlc.arg(id)
RETURNING id, household_id, name, target_amount_minor, currency, target_month, planned_monthly_minor, archived_at, created_at, updated_at;

-- ListGoalContributions returns one goal's contributions, newest first by
-- occurred_on, tie-broken by created_at DESC ("most recently added first") --
-- matching the in-memory double, since AddContribution only appends and
-- two contributions can share a date. It joins to goals for currency: a
-- contribution row carries none of its own (00007_goals.sql), so this is
-- the one read path that must fetch it to build a domain.Money.
--
-- Scoped by household_id AND goal_id together, never either alone
-- (GoalRepository's package doc): goal_contributions has no database-level
-- guarantee that its household_id agrees with its goal_id's household, so
-- both must be checked or a contribution could leak across households.
-- name: ListGoalContributions :many
SELECT c.id, c.goal_id, c.household_id, c.amount_minor, c.occurred_on, c.note, c.source, c.source_budget_month, c.created_at,
       g.currency
FROM goal_contributions c
JOIN goals g ON g.id = c.goal_id
WHERE c.household_id = $1 AND c.goal_id = $2
ORDER BY c.occurred_on DESC, c.created_at DESC
LIMIT sqlc.arg(row_limit);

-- MonthContributionTotals sums each unarchived goal's contributions inside
-- one calendar month, excluding source = 'starting_balance': without that
-- exclusion, a household that created goals with existing balances would
-- read its opening balances as money added this month. Joined to goals so
-- an archived goal's contributions never appear.
--
-- Both g.household_id and c.household_id are checked against $1, matching
-- goalDouble.MonthContributionTotals where the port itself is silent:
-- goal_contributions.household_id has no database-level guarantee of
-- agreeing with its own goal_id's household, so a mismatched row must not
-- be summed under either household's total.
-- name: MonthContributionTotals :many
SELECT c.goal_id, SUM(c.amount_minor)::bigint AS amount_minor
FROM goal_contributions c
JOIN goals g ON g.id = c.goal_id
WHERE c.household_id = $1
  AND g.household_id = $1
  AND g.archived_at IS NULL
  AND c.occurred_on >= $2::date
  AND c.occurred_on < ($2::date + INTERVAL '1 month')
  AND c.source <> 'starting_balance'
GROUP BY c.goal_id;

-- DeleteGoalContribution removes one contribution row, scoped by id, goal_id
-- AND household_id together, the same reason ListGoalContributions gives.
-- It returns source and source_budget_month so GoalRepo.DeleteContribution
-- can decide, inside the same transaction, whether a budget's rollover
-- stamp needs clearing.
-- name: DeleteGoalContribution :one
DELETE FROM goal_contributions
WHERE id = $1 AND goal_id = $2 AND household_id = $3
RETURNING source, source_budget_month;

-- GoalProgressByIDs is Vision's one read of Goals (usecase.GoalProgressReader).
-- It returns a row only for an id belonging to THIS household, so a goal in
-- another household is indistinguishable from one that doesn't exist: a
-- missing id is a miss, not an error. ProgressByIDs turns that into a plain
-- absence from the returned map, and Vision renders a figureless measure.
--
-- Deliberately NOT filtered on archived_at: an archived goal counts as found
-- and keeps its figure ("archiving is not deletion anywhere else in this
-- product either" -- usecase.GoalProgressReader). Only a real DELETE
-- unlinks a measure, by firing goals.id's own ON DELETE SET NULL into
-- vision_measures.goal_id.
--
-- The contributed total is ListGoalsWithTotals' own expression, verbatim --
-- same LEFT JOIN, same COALESCE -- so this codebase never carries two
-- different definitions of how far along a goal is.
-- name: GoalProgressByIDs :many
SELECT g.id, g.name, g.target_amount_minor,
       COALESCE(SUM(c.amount_minor), 0)::bigint AS contributed_minor
FROM goals g
LEFT JOIN goal_contributions c ON c.goal_id = g.id
WHERE g.household_id = $1 AND g.id = ANY(sqlc.arg(goal_ids)::uuid[])
GROUP BY g.id, g.name, g.target_amount_minor;

-- ClearBudgetRollover undoes RollOverToGoal's stamp when the contribution
-- it wrote is deleted: leaving the stamp behind would strand the household --
-- money gone from the goal, a month still claiming it was rolled over,
-- and a 409 on every retry (GoalRepository.DeleteContribution). It is
-- unconditional on rolled_over_at because GoalRepo.DeleteContribution only
-- runs this after confirming the deleted row's source was budget_rollover,
-- so exactly one budget row -- this household-month -- could own the stamp.
-- name: ClearBudgetRollover :exec
UPDATE budgets
SET rolled_over_at = NULL, rollover_goal_id = NULL, updated_at = now()
WHERE household_id = $1 AND month = $2;
