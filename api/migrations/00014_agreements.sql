-- +goose Up
-- No position column anywhere below; ordering is created_at, id, for the reason 00009_retros.sql:34-41 gives
-- (decision 11): the only safe writer is max(position)+1 inside the insert, two owners still collide on it, and
-- no reordering control is drawn -- the column would only create that race.
--
-- A section is a label, and creating one is immediate and unsigned (decision 8). UNIQUE (household_id, name) is
-- decision 19: the adapter maps that CONSTRAINT NAME, above the generic 23505 case, to
-- ErrAgreementSectionNameTaken, so the screen can name what collided.
CREATE TABLE agreement_sections (
    id           uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    household_id uuid        NOT NULL REFERENCES households(id) ON DELETE CASCADE,
    name         text        NOT NULL,
    -- No DEFAULT now(): "Use starter set" writes four rows in one transaction, and one shared now() would tie
    -- them into a random uuid order. The writer stamps them strictly apart.
    created_at   timestamptz NOT NULL,
    UNIQUE (household_id, name)
);
-- The append-only log of every change ever proposed (decision 9): nothing is deleted and no content column is
-- rewritten -- only status, resolved_at and park_note move, so there is no updated_at.
CREATE TABLE agreement_proposals (
    id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    household_id uuid NOT NULL REFERENCES households(id) ON DELETE CASCADE,
    -- Parsed in Go on the way out; every CHECK here is a backstop, never the enforcement.
    kind         text NOT NULL CHECK (kind IN ('add', 'edit', 'remove')),
    status       text NOT NULL CHECK (status IN ('pending', 'parked', 'accepted', 'withdrawn')),
    -- Copied from the target on an edit or a remove, so a pending edit whose target is removed has a card.
    section_id   uuid NOT NULL REFERENCES agreement_sections(id),
    target_agreement_id uuid,          -- NULL for an add; its FK is added below
    body          text NOT NULL DEFAULT '',
    previous_body text NOT NULL DEFAULT '',  -- the target's wording when proposed (decision 13)
    note          text NOT NULL DEFAULT '',
    park_note     text NOT NULL DEFAULT '',
    -- A log column with NO foreign key, which is what decision 20's "no ON DELETE action" has to mean in
    -- Postgres: a bare REFERENCES defaults to NO ACTION, members are hard-deleted (queries/identity.sql:74), and
    -- an FK would then refuse to remove an owner who had ever proposed anything -- the outcome decision 20
    -- rejects RESTRICT for. CASCADE, which retro_action_assignees uses, would silently un-sign an accepted
    -- agreement. The id is verified at write time through memberships (this household, role = 'owner'), and
    -- outlives the person leaving because the record of who agreed has to.
    proposed_by_membership_id uuid NOT NULL,
    created_at  timestamptz NOT NULL,
    resolved_at timestamptz,           -- NULL means open, retros.completed_at's shape
    CONSTRAINT agreement_proposals_resolution_matches_status  -- the two cannot disagree
        CHECK ((status IN ('pending', 'parked')) = (resolved_at IS NULL)),
    -- Validate's rule again, for statements written by hand. ELSE false so an unmatched kind fails closed: a
    -- CHECK accepts NULL.
    CONSTRAINT agreement_proposals_shape CHECK (
        CASE kind
        WHEN 'add'    THEN target_agreement_id IS NULL     AND body <> '' AND previous_body =  ''
        WHEN 'edit'   THEN target_agreement_id IS NOT NULL AND body <> '' AND previous_body <> ''
        WHEN 'remove' THEN target_agreement_id IS NOT NULL AND body =  '' AND previous_body <> ''
        ELSE false END
    )
);
-- The living document, and a row stays in it forever: removal is a stamp (decision 9) -- "it stays in Version
-- history, so you can always see it was there and restore it later".
CREATE TABLE agreements (
    id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    household_id uuid NOT NULL REFERENCES households(id) ON DELETE CASCADE,
    section_id   uuid NOT NULL REFERENCES agreement_sections(id),
    body         text NOT NULL,
    -- Never NULL: propose -> sign is the only path -- "everything here is here because you both agreed".
    added_by_proposal_id   uuid NOT NULL REFERENCES agreement_proposals(id),
    removed_by_proposal_id uuid REFERENCES agreement_proposals(id),
    removed_at             timestamptz,
    created_at             timestamptz NOT NULL,
    CONSTRAINT agreements_removal_is_whole   -- a removal is one event, not half of one
        CHECK ((removed_at IS NULL) = (removed_by_proposal_id IS NULL))
);
-- Closes the cycle, at the default NO ACTION not RESTRICT: a household delete cascades into both sides of it in
-- one statement, and only NO ACTION waits for the end of that statement to check.
ALTER TABLE agreement_proposals
    ADD CONSTRAINT agreement_proposals_target_agreement_id_fkey
    FOREIGN KEY (target_agreement_id) REFERENCES agreements(id);

-- One owner's Agree. The primary key makes a double-clicked Agree idempotent (decision 16): the write is an
-- upsert and signed_at keeps the first stamp. Nothing deletes a signature either -- a departed owner's is a true
-- record that stops counting, because the set is every CURRENT owner (decision 4).
CREATE TABLE agreement_signatures (
    proposal_id   uuid NOT NULL REFERENCES agreement_proposals(id) ON DELETE CASCADE,
    membership_id uuid NOT NULL,       -- a log column with no FK, as above
    signed_at     timestamptz NOT NULL,
    PRIMARY KEY (proposal_id, membership_id)   -- scoped to a household via the proposal
);

-- Live reads carry the predicate rather than filter a growing tail of removed rows; the second index serves
-- Document's split of the log.
CREATE INDEX agreements_household_live_idx ON agreements (household_id) WHERE removed_at IS NULL;
CREATE INDEX agreement_proposals_household_status_idx ON agreement_proposals (household_id, status);

-- +goose Down
-- The ALTER goes first because the two tables name each other -- agreements.added_by_proposal_id points at
-- agreement_proposals and the constraint below points back -- so while it stands, whichever table is dropped
-- first is depended on by the other and Postgres refuses. Breaking the cycle leaves an ordinary child-first order.
ALTER TABLE agreement_proposals DROP CONSTRAINT agreement_proposals_target_agreement_id_fkey;  -- first
DROP TABLE agreement_signatures;
DROP TABLE agreements;
DROP TABLE agreement_proposals;
DROP TABLE agreement_sections;
