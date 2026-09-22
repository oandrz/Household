-- +goose Up

-- Telegram invites (partner-invite spec, milestone 2). An invite gains a
-- channel, and a Telegram invite carries its knock on the same row rather
-- than in a table of its own: one knock per link (spec decision 2) means
-- there is never more than one to hold.
--
-- Every existing row is an email invite with an address, so DEFAULT 'email'
-- satisfies invites_channel_matches_email at the moment it is added. Run
-- this against a restored production dump before deploying, as 00011's own
-- comment asks for any migration that constrains a table already holding
-- real rows.
ALTER TABLE invites ALTER COLUMN email DROP NOT NULL;

ALTER TABLE invites ADD COLUMN channel text NOT NULL DEFAULT 'email'
    CHECK (channel IN ('email', 'telegram'));

-- An email invite has an address; a Telegram invite has none. Written as an
-- equality between two booleans so neither direction can drift: an email
-- invite with a NULL address and a Telegram invite carrying one are both
-- refused by this single constraint.
ALTER TABLE invites ADD CONSTRAINT invites_channel_matches_email
    CHECK ((channel = 'email') = (email IS NOT NULL));

ALTER TABLE invites ADD COLUMN knock_chat_id       bigint;
ALTER TABLE invites ADD COLUMN knock_chat_username text;
ALTER TABLE invites ADD COLUMN knock_code          text;
ALTER TABLE invites ADD COLUMN knocked_at          timestamptz;

-- A knock is whole or absent. knock_chat_username stays out of it: Telegram
-- legitimately sends no username, so NULL there is data, not a half-written
-- row.
ALTER TABLE invites ADD CONSTRAINT invites_knock_is_whole
    CHECK ((knocked_at IS NULL) = (knock_chat_id IS NULL)
       AND (knocked_at IS NULL) = (knock_code IS NULL));

ALTER TABLE invites ADD CONSTRAINT invites_knock_needs_telegram
    CHECK (knocked_at IS NULL OR channel = 'telegram');

-- A group chat's id is negative. The bot already refuses a group before a
-- knock is ever recorded (adapter/telegram/update.go,
-- isPrivateChatWithItsOwner, security review 2026-09-19 finding 2); this is
-- the second gate, in the place a future caller cannot forget. It is free
-- here because no row has ever had this column.
ALTER TABLE invites ADD CONSTRAINT invites_knock_chat_is_a_person
    CHECK (knock_chat_id IS NULL OR knock_chat_id > 0);

-- +goose Down
ALTER TABLE invites DROP CONSTRAINT invites_knock_chat_is_a_person;
ALTER TABLE invites DROP CONSTRAINT invites_knock_needs_telegram;
ALTER TABLE invites DROP CONSTRAINT invites_knock_is_whole;
ALTER TABLE invites DROP COLUMN knocked_at;
ALTER TABLE invites DROP COLUMN knock_code;
ALTER TABLE invites DROP COLUMN knock_chat_username;
ALTER TABLE invites DROP COLUMN knock_chat_id;
ALTER TABLE invites DROP CONSTRAINT invites_channel_matches_email;
ALTER TABLE invites DROP COLUMN channel;
-- email stays nullable: a Telegram invite written while this migration was
-- applied would fail the old NOT NULL, and a down migration must not lose
-- rows.
