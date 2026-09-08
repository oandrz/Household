-- +goose Up

-- The bot speaks first: once a day, each linked owner with Money gets a short
-- digest of bills due and budget lines running hot (stage 6 of the automation
-- roadmap, docs/superpowers/specs/2026-09-08-hearth-nudges-design.md).
--
-- nudge_deliveries is the at-most-once ledger. A row is claimed BEFORE the
-- message is sent and deleted if the send fails, so a restart mid-run, a
-- second tick in the same hour, or two ticks racing cannot deliver twice: the
-- primary key is the only lock. Keyed on household as well as chat because
-- one person can belong to several households and each household's digest is
-- its own message. Rows older than a month are pruned by the api on the same
-- schedule as telegram_link_requests.
CREATE TABLE nudge_deliveries (
    chat_id      bigint      NOT NULL,
    household_id uuid        NOT NULL REFERENCES households(id) ON DELETE CASCADE,
    day          date        NOT NULL,
    sent_at      timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (chat_id, household_id, day)
);

-- The opt-out lives on the chat binding, not the membership: it is "this
-- phone stops buzzing", set by /nudges off in that chat, and it must survive
-- the person joining a second household.
ALTER TABLE telegram_accounts
    ADD COLUMN nudges_enabled boolean NOT NULL DEFAULT true;

-- +goose Down
ALTER TABLE telegram_accounts DROP COLUMN nudges_enabled;
DROP TABLE nudge_deliveries;
