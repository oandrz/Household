-- CreateRetro writes an empty draft for the month. The UNIQUE (household_id,
-- month) constraint (00009_retros.sql) is what makes a double-clicked "Start
-- retro" button harmless -- the second insert fails with 23505, which
-- translate maps to domain.ErrAlreadyExists rather than a raw pgx error
-- (RetroRepository.Create's own doc comment).
-- name: CreateRetro :one
INSERT INTO retros (household_id, month)
VALUES ($1, $2)
RETURNING id, month, mood, went_well, was_hard, notes, completed_at, version;

-- GetRetroByMonth is ByMonth's whole implementation: household_id AND month
-- both in the WHERE, so a retro belonging to another household is
-- indistinguishable from one that was never created (RetroRepository's own
-- package doc comment).
-- name: GetRetroByMonth :one
SELECT id, month, mood, went_well, was_hard, notes, completed_at, version
FROM retros
WHERE household_id = $1 AND month = $2;

-- ListRetros returns every retro for a household, newest month first, each
-- carrying its own action count via a correlated subquery rather than a
-- JOIN + GROUP BY: a retro with zero actions must still appear exactly
-- once, and a LEFT JOIN would need the same GROUP BY every other column
-- goal.sql's ListGoalsWithTotals already carries for the identical reason.
-- Deliberately unbounded -- List's own doc comment on RetroRepository.
-- name: ListRetros :many
SELECT r.id, r.month, r.mood, r.went_well, r.was_hard, r.notes, r.completed_at, r.version,
       (SELECT count(*) FROM retro_actions a WHERE a.retro_id = r.id) AS action_count
FROM retros r
WHERE r.household_id = $1
ORDER BY r.month DESC;

-- UpdateRetro is the whole optimistic-concurrency guard. The version
-- comparison is IN the WHERE clause, on purpose: a SELECT version followed
-- by an UPDATE is two statements with a race window between them, and the
-- version column exists to close exactly that window. Zero rows updated
-- means either the retro is gone or the version moved since the editor
-- loaded it, and RetroRepo.Update (not this query) is what tells the two
-- apart, with one more cheap read.
-- name: UpdateRetro :one
UPDATE retros
SET mood = $3, went_well = $4, was_hard = $5, notes = $6,
    version = version + 1, updated_at = now()
WHERE household_id = $1 AND id = $2 AND version = $7
RETURNING id, month, mood, went_well, was_hard, notes, completed_at, version;

-- CompleteRetro stamps completed_at with the caller's `at`, but only when it
-- is not already set -- coalesce keeps the FIRST completion time, the same
-- "archiving is idempotent" shape SetGoalArchived and SetCategoryArchived
-- already use, so finishing an already-finished retro is a harmless retry
-- rather than a second, later timestamp overwriting the true one.
-- name: CompleteRetro :one
UPDATE retros
SET completed_at = coalesce(completed_at, $3), updated_at = now()
WHERE household_id = $1 AND id = $2
RETURNING id, month, mood, went_well, was_hard, notes, completed_at, version;

-- DeleteDraftRetro removes a retro that has NOT been finished --
-- completed_at IS NULL lives in the WHERE clause rather than a
-- check-then-delete in Go, both because a check-then-delete can race and
-- because :execrows lets RetroRepo.DeleteDraft see a zero-row match and
-- refuse to call it success (DeleteDraft's own doc comment on
-- RetroRepository, and the SetBillNextDue defect it cites).
-- name: DeleteDraftRetro :execrows
DELETE FROM retros
WHERE household_id = $1 AND id = $2 AND completed_at IS NULL;
