-- +goose Up

-- retros is one household-month's check-in. One row per calendar month
-- (UNIQUE below), month stored as the first of the month -- the same
-- convention budgets and TransactionRepository.MonthTotals already use.
--
-- A retro is ONE shared draft that both partners write into, and its lines
-- carry no author: the design renders no name against any bullet, and
-- attribution would need an author column, a second mood, and a rule for what
-- "4/5" means when two people disagree (spec decision 1).
CREATE TABLE retros (
    id           uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    household_id uuid        NOT NULL REFERENCES households(id) ON DELETE CASCADE,
    month        date        NOT NULL,
    -- NULL means nobody has picked an emoji yet. Never 0: zero is a claim,
    -- the same reasoning budgets.expected_income_minor carries.
    mood         smallint    CHECK (mood BETWEEN 1 AND 5),
    went_well    text        NOT NULL DEFAULT '',
    was_hard     text        NOT NULL DEFAULT '',
    notes        text        NOT NULL DEFAULT '',
    -- NULL is the whole draft concept (spec decision 2). No status column: one
    -- nullable timestamp answers "is it done" and "when", and a status enum
    -- would let a row claim finished with no completion time.
    completed_at timestamptz,
    -- Optimistic concurrency. Both partners can have the same draft open, and
    -- a whole-column save would otherwise silently eat whatever the other one
    -- just typed (spec decision 6). Bumped by the retro update path only --
    -- ticking an action writes a different table and must not collide.
    version      integer     NOT NULL DEFAULT 1,
    created_at   timestamptz NOT NULL DEFAULT now(),
    updated_at   timestamptz NOT NULL DEFAULT now(),
    UNIQUE (household_id, month)
);

-- retro_actions is what a retro decided to do next month. No position column
-- on purpose: an ordering integer needs a writer, the only safe writer is
-- max(position)+1 inside the insert, and two partners adding an action at the
-- same moment can still collide on it -- the version guard above covers the
-- retro's text, not its actions. The design draws no reordering control, so
-- the column would exist only to create that race. Insertion order is the
-- order; ORDER BY created_at, id.
CREATE TABLE retro_actions (
    id           uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    retro_id     uuid        NOT NULL REFERENCES retros(id) ON DELETE CASCADE,
    body         text        NOT NULL,
    -- NULL means open; this is the tick, and it is ticked all month long,
    -- which is why it lives on the page rather than inside the modal.
    done_at      timestamptz,
    -- Provenance for an action carried from last month (spec decision 4).
    -- ON DELETE SET NULL: deleting July's action must never delete August's
    -- copy of it.
    carried_from uuid        REFERENCES retro_actions(id) ON DELETE SET NULL,
    created_at   timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX retro_actions_retro_id_idx ON retro_actions (retro_id);

-- An action is assigned to one owner or both -- the design's June retro shows
-- "A C" on one action and "A" on another. A join table rather than two boolean
-- columns: nothing in this product caps a household at two owners (the invite
-- modal offers "Parent" freely; last-owner protection only guarantees at
-- least one), so two columns would encode a limit that does not exist.
CREATE TABLE retro_action_assignees (
    action_id     uuid NOT NULL REFERENCES retro_actions(id) ON DELETE CASCADE,
    membership_id uuid NOT NULL REFERENCES memberships(id) ON DELETE CASCADE,
    PRIMARY KEY (action_id, membership_id)
);

-- +goose Down
DROP TABLE retro_action_assignees;
DROP TABLE retro_actions;
DROP TABLE retros;
