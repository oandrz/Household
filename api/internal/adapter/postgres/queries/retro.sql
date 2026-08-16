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

-- AddRetroAction writes one action. retro_actions carries no household_id of
-- its own (00009_retros.sql's own comment), so the INSERT...SELECT...FROM
-- retros WHERE household_id = $4 is the scoping clause, not a plain INSERT
-- with a bare retro_id: a retro_id that belongs to another household, or
-- does not exist at all, matches zero rows, and :one then reports
-- pgx.ErrNoRows -- which translate maps to domain.ErrNotFound, the same
-- "another household's row is indistinguishable from a missing one"
-- convention GetRetroByMonth already follows.
-- name: AddRetroAction :one
INSERT INTO retro_actions (retro_id, body, carried_from)
SELECT r.id, $1, $2
FROM retros r
WHERE r.id = $3 AND r.household_id = $4
RETURNING id, retro_id, body, done_at, carried_from;

-- AddRetroActionAssignee inserts one owner, scoped through memberships the
-- same way AddRetroAction scopes through retros: the SELECT's WHERE
-- requires the membership to belong to household_id, so an id that is not a
-- membership AT ALL and an id that IS a membership but of a DIFFERENT
-- household both match zero rows. :execrows lets RetroActionRepo.Add see
-- that zero and fail the whole transaction
-- (usecase.RetroActionRepository.Add's own doc comment) rather than
-- trusting retro_action_assignees.membership_id's foreign key alone, which
-- only proves the id exists SOMEWHERE in memberships -- not that it belongs
-- to this household.
-- name: AddRetroActionAssignee :execrows
INSERT INTO retro_action_assignees (action_id, membership_id)
SELECT $1, m.id
FROM memberships m
WHERE m.id = $2 AND m.household_id = $3;

-- ListRetroActions is ForRetro's whole implementation: one row per action,
-- each carrying its assignees folded into one array by a household-scoped
-- LEFT JOIN + array_agg rather than a second query, and ORDER BY
-- a.created_at, a.id because retro_actions carries no position column
-- (00009_retros.sql's own comment) -- insertion order IS the order. The
-- FILTER clause is what keeps an action with no assignees at '{}' rather
-- than array_agg's default of a one-element array holding a single NULL.
-- name: ListRetroActions :many
SELECT a.id, a.retro_id, a.body, a.done_at, a.carried_from,
       COALESCE(array_agg(asg.membership_id) FILTER (WHERE asg.membership_id IS NOT NULL), '{}')::uuid[] AS assignee_ids
FROM retro_actions a
JOIN retros r ON r.id = a.retro_id
LEFT JOIN retro_action_assignees asg ON asg.action_id = a.id
WHERE r.household_id = $1 AND a.retro_id = $2
-- a.created_at is not in this list -- legal, not an oversight: a.id is
-- (retro_actions' PRIMARY KEY), and grouping by a table's primary key lets
-- Postgres treat every other column that is functionally dependent on it,
-- created_at included, as already grouped. ORDER BY below is free to use it.
GROUP BY a.id, a.retro_id, a.body, a.done_at, a.carried_from
ORDER BY a.created_at, a.id;

-- SetRetroActionDone ticks or unticks one action. done_at is passed straight
-- through rather than branched in SQL: SetDone's own contract (clear the
-- stamp on done=false, never record a "not done" time -- the port's own doc
-- comment) is decided in Go, this query only ever sets the column to
-- whatever it is given. UPDATE...FROM retros is the scoping clause, the
-- same reason ListRetroActions joins through retros. :execrows lets
-- RetroActionRepo.SetDone see a zero-row match and refuse to call it
-- success -- the SetBillNextDue defect, docs/LEARNING.md.
-- name: SetRetroActionDone :execrows
UPDATE retro_actions a
SET done_at = $3
FROM retros r
WHERE a.id = $2 AND a.retro_id = r.id AND r.household_id = $1;

-- DeleteRetroAction hard-deletes one action, scoped through retros the same
-- way SetRetroActionDone is -- carried_from's ON DELETE SET NULL
-- (00009_retros.sql) means this can never orphan a later carried action.
-- :execrows is what lets RetroActionRepo.Remove refuse a zero-row match
-- instead of reporting success for nothing.
-- name: DeleteRetroAction :execrows
DELETE FROM retro_actions a
USING retros r
WHERE a.id = $2 AND a.retro_id = r.id AND r.household_id = $1;

-- ListOpenActionsInMonth is OpenInMonth's whole implementation: that
-- month's actions with done_at IS NULL, scoped to household_id through the
-- same retros join every query above uses. The caller is responsible for
-- month already being the first of the calendar month, midnight UTC --
-- OpenInMonth's own doc comment on RetroActionRepository -- this query does
-- not renormalise it.
-- name: ListOpenActionsInMonth :many
SELECT a.id, a.retro_id, a.body, a.done_at, a.carried_from,
       COALESCE(array_agg(asg.membership_id) FILTER (WHERE asg.membership_id IS NOT NULL), '{}')::uuid[] AS assignee_ids
FROM retro_actions a
JOIN retros r ON r.id = a.retro_id
LEFT JOIN retro_action_assignees asg ON asg.action_id = a.id
WHERE r.household_id = $1 AND r.month = $2 AND a.done_at IS NULL
-- Same GROUP BY / ORDER BY shape as ListRetroActions above, and the same
-- reason a.created_at needs no entry in the GROUP BY list.
GROUP BY a.id, a.retro_id, a.body, a.done_at, a.carried_from
ORDER BY a.created_at, a.id;
