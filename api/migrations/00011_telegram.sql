-- +goose Up
CREATE TABLE telegram_link_requests (
    id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    nonce_hash  bytea       NOT NULL UNIQUE,
    expires_at  timestamptz NOT NULL,
    -- Both NULL until the nonce is redeemed, then both set in one statement.
    -- The chat is unknown when the nonce is minted: the browser has not met
    -- Telegram yet. Redemption is the only moment the two can be joined, and
    -- the per-chat rate limit counts these rows, so a redemption that failed
    -- to record its chat would be a limit that silently never fires.
    consumed_at timestamptz,
    chat_id     bigint,
    created_at  timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT consumed_rows_name_their_chat
        CHECK ((consumed_at IS NULL) = (chat_id IS NULL))
);

CREATE INDEX telegram_link_requests_chat_consumed_idx
    ON telegram_link_requests (chat_id, consumed_at DESC);

-- One Telegram account per user, and one user per Telegram account. Both
-- directions matter: without the chat_id unique, two users could bind the same
-- chat and a sign-in would be ambiguous; without the user_id unique, a user
-- could accumulate chats and a revocation would miss one.
CREATE TABLE telegram_accounts (
    id        uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id   uuid        NOT NULL UNIQUE REFERENCES users(id) ON DELETE CASCADE,
    chat_id   bigint      NOT NULL UNIQUE,
    linked_at timestamptz NOT NULL DEFAULT now()
);

-- A Telegram sign-up has no address at all, so email stops being mandatory and
-- a chat id takes its place. The CHECK is the fail-closed half: a row carrying
-- both channels, or neither, is refused by the database rather than reasoned
-- about in Go.
--
-- The backfill is a no-op. Every existing row has a non-NULL email and, after
-- the ADD COLUMN, a NULL telegram_chat_id, so the constraint holds for all of
-- them at ADD CONSTRAINT time. Run this against a restored production dump
-- before running it against production -- it is the first migration here to
-- constrain a table that already holds real rows.
ALTER TABLE signups ALTER COLUMN email DROP NOT NULL;
ALTER TABLE signups ADD COLUMN telegram_chat_id bigint;
ALTER TABLE signups ADD CONSTRAINT signups_have_exactly_one_channel
    CHECK ((email IS NULL) <> (telegram_chat_id IS NULL));

-- +goose Down
ALTER TABLE signups DROP CONSTRAINT signups_have_exactly_one_channel;
ALTER TABLE signups DROP COLUMN telegram_chat_id;
DELETE FROM signups WHERE email IS NULL;
ALTER TABLE signups ALTER COLUMN email SET NOT NULL;
DROP TABLE telegram_accounts;
DROP TABLE telegram_link_requests;
