-- GetVision reads one household-year's parent row. Scoped by household_id:
-- a vision belonging to another household must be indistinguishable from one
-- that does not exist.
-- name: GetVision :one
SELECT id, household_id, year, theme, description, version
FROM visions
WHERE household_id = $1 AND year = $2;

-- ListVisionPillars returns a vision's pillars in the order the household
-- sees them -- the design numbers them visibly ("Pillar 1", "Pillar 2"), so
-- position is not an implementation detail.
-- name: ListVisionPillars :many
SELECT id, position, name, description
FROM vision_pillars
WHERE vision_id = $1
ORDER BY position;

-- ListVisionMeasures returns every measure of every pillar of one vision in a
-- single round trip, joined back to its pillar so the repository can group
-- them without a query per pillar.
-- name: ListVisionMeasures :many
SELECT m.id, m.pillar_id, m.position, m.label, m.current_value, m.target_value, m.goal_id
FROM vision_measures m
JOIN vision_pillars p ON p.id = m.pillar_id
WHERE p.vision_id = $1
ORDER BY p.position, m.position;

-- name: ListVisionMilestones :many
SELECT id, position, year, title, note
FROM vision_milestones
WHERE vision_id = $1
ORDER BY position;

-- CreateVision is the version-0 path: a first save for a household-year.
-- ON CONFLICT DO NOTHING rather than an upsert, so a zero-row result means
-- "someone else created it while this editor was typing" and the repository
-- can answer domain.ErrVisionChanged instead of silently overwriting a whole
-- year of pillars.
--
-- Both parent writes MUST return the post-write version: VisionRepo.Save
-- hands it straight back as the token the next save will send, so a RETURNING
-- list that gave back the version it read would make every subsequent save
-- conflict against itself. Do not trim `version` from either RETURNING.
-- name: CreateVision :one
INSERT INTO visions (household_id, year, theme, description)
VALUES ($1, $2, $3, $4)
ON CONFLICT (household_id, year) DO NOTHING
RETURNING id, household_id, year, theme, description, version;

-- UpdateVision is the version-guarded path. A zero-row result is ambiguous
-- (deleted, or the other partner saved first) and the caller re-reads to tell
-- the two apart -- RetroRepo.Update's own comment gives the reasoning.
-- name: UpdateVision :one
UPDATE visions
SET theme = $3, description = $4, version = version + 1, updated_at = now()
WHERE household_id = $1 AND year = $2 AND version = $5
RETURNING id, household_id, year, theme, description, version;

-- name: DeleteVisionPillars :exec
DELETE FROM vision_pillars WHERE vision_id = $1;

-- name: DeleteVisionMilestones :exec
DELETE FROM vision_milestones WHERE vision_id = $1;

-- name: InsertVisionPillar :one
INSERT INTO vision_pillars (vision_id, position, name, description)
VALUES ($1, $2, $3, $4)
RETURNING id;

-- name: InsertVisionMeasure :exec
INSERT INTO vision_measures (pillar_id, position, label, current_value, target_value, goal_id)
VALUES ($1, $2, $3, $4, $5, $6);

-- name: InsertVisionMilestone :exec
INSERT INTO vision_milestones (vision_id, position, year, title, note)
VALUES ($1, $2, $3, $4, $5);

-- CountGoalsInHousehold answers how many of the given goal ids actually belong
-- to this household. VisionRepo.Save compares this against the number of
-- distinct ids it asked about, inside the same transaction as the write --
-- vision_measures' own FK only proves a goal exists somewhere, never that it
-- is this household's. The identical hole CountCategoriesInHousehold closes
-- for budget lines.
-- name: CountGoalsInHousehold :one
SELECT count(*) FROM goals
WHERE id = ANY(sqlc.arg(goal_ids)::uuid[]) AND household_id = $1;
