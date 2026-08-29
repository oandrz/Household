-- +goose Up

-- visions is one household-year's theme. One row per calendar year (UNIQUE
-- below): a theme is set every January and the previous year's stays
-- readable forever, which is why this is a row per year rather than one
-- mutable row per household (spec decision 4).
CREATE TABLE visions (
    id           uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    household_id uuid        NOT NULL REFERENCES households(id) ON DELETE CASCADE,
    -- A vision is a calendar year, not a month or a day -- unlike retros.month,
    -- which is a date because a retro happens on one.
    year         smallint    NOT NULL CHECK (year BETWEEN 1900 AND 2200),
    -- Defaulted rather than merely NOT NULL because GET returns an empty
    -- vision for a year never set (spec decision 9), and the simplest shape
    -- for that is a row that can exist while still blank.
    theme        text        NOT NULL DEFAULT '',
    description  text        NOT NULL DEFAULT '',
    -- Optimistic concurrency (spec decision 10). A whole-document replace
    -- makes a lost update worse than a field-level one: a partner's entire
    -- set of pillars can be silently overwritten by a stale editor. Version 0
    -- never appears in this column -- it is the wire value meaning "I read a
    -- year that had no vision", and a create lands here at 1.
    version      integer     NOT NULL DEFAULT 1,
    created_at   timestamptz NOT NULL DEFAULT now(),
    updated_at   timestamptz NOT NULL DEFAULT now(),
    UNIQUE (household_id, year)
);

-- Children carry an explicit position, unlike retro_actions, which
-- deliberately has none: its only safe writer (max(position)+1) raced when
-- two partners added an action at once. Vision has no such race -- one save
-- writes every child of a document in one transaction, so position is
-- assigned from the submitted array's index by a single writer. The design
-- also numbers pillars visibly ("Pillar 1", "Pillar 2"), so the order is
-- something the household sees rather than an accident of insertion.
CREATE TABLE vision_pillars (
    id          uuid     PRIMARY KEY DEFAULT gen_random_uuid(),
    vision_id   uuid     NOT NULL REFERENCES visions(id) ON DELETE CASCADE,
    position    smallint NOT NULL,
    name        text     NOT NULL,
    description text     NOT NULL DEFAULT '',
    UNIQUE (vision_id, position)
);

CREATE TABLE vision_measures (
    id            uuid     PRIMARY KEY DEFAULT gen_random_uuid(),
    pillar_id     uuid     NOT NULL REFERENCES vision_pillars(id) ON DELETE CASCADE,
    position      smallint NOT NULL,
    label         text     NOT NULL,
    current_value integer,
    target_value  integer,
    -- ON DELETE SET NULL: deleting a goal must never delete a pillar's
    -- measure, let alone cascade into the vision (spec decision 8).
    goal_id       uuid     REFERENCES goals(id) ON DELETE SET NULL,
    UNIQUE (pillar_id, position),
    -- Three branches, and the third is NOT optional. A referential SET NULL
    -- is an UPDATE, and Postgres enforces CHECK constraints on UPDATE -- so
    -- with only the typed and linked branches, deleting a goal would raise a
    -- constraint violation inside the GOALS feature. The third branch is the
    -- broken-link state the Vision page renders as a label with no figure.
    -- The database permits that state only because SET NULL produces it; the
    -- domain still refuses to CREATE one, so a PUT carrying a measure with
    -- neither a goal nor a target is 422. Read tolerantly, write strictly.
    CONSTRAINT measure_is_typed_or_linked CHECK (
        -- typed
        (goal_id IS NULL     AND target_value IS NOT NULL AND target_value > 0
                             AND current_value IS NOT NULL AND current_value >= 0)
        -- linked
     OR (goal_id IS NOT NULL AND target_value IS NULL AND current_value IS NULL)
        -- link broken by a deleted goal
     OR (goal_id IS NULL     AND target_value IS NULL AND current_value IS NULL)
    )
);

-- A milestone's year is deliberately independent of its vision's year:
-- the design's own milestones sit years ahead of the vision they belong to
-- (2027, 2029 and 2032 inside a 2026 vision) -- a milestone is a future
-- waypoint the vision is aiming at, not an event that happened during the
-- vision's own year. Do not add a constraint tying this to visions.year;
-- that would make the feature impossible to use as designed.
CREATE TABLE vision_milestones (
    id        uuid     PRIMARY KEY DEFAULT gen_random_uuid(),
    vision_id uuid     NOT NULL REFERENCES visions(id) ON DELETE CASCADE,
    year      smallint NOT NULL CHECK (year BETWEEN 1900 AND 2200),
    title     text     NOT NULL,
    note      text     NOT NULL DEFAULT '',
    position  smallint NOT NULL,
    UNIQUE (vision_id, position)
);

-- +goose Down
DROP TABLE vision_milestones;
DROP TABLE vision_measures;
DROP TABLE vision_pillars;
DROP TABLE visions;
