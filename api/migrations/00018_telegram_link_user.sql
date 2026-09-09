-- +goose Up

-- A link nonce is minted by a signed-in member for their own account; a
-- sign-in nonce is minted by a browser that has not said who it is. The two
-- share this table, and this column is the only thing that tells them apart
-- -- deliberately not a payload prefix, which stays free for the still
-- unbuilt inv_<token> Telegram invites. Nullable, because every row written
-- since 00011 is a sign-in nonce with no user to name.
ALTER TABLE telegram_link_requests
    ADD COLUMN user_id uuid REFERENCES users(id) ON DELETE CASCADE;

-- Stamped at redemption alongside chat_id, from Telegram's message.from.
-- The confirm screen reads it so the person approving can tell their own
-- chat from a stranger's; nothing server-side ever decides anything from
-- it, because it is a display name a third party controls.
ALTER TABLE telegram_link_requests
    ADD COLUMN chat_username text;

-- The same name, carried onto the binding when it is confirmed. The link
-- request it came from is pruned within the month and the Settings panel has
-- to keep naming the connected chat long after that.
ALTER TABLE telegram_accounts ADD COLUMN chat_username text;

-- +goose Down
ALTER TABLE telegram_accounts DROP COLUMN chat_username;
ALTER TABLE telegram_link_requests DROP COLUMN chat_username;
ALTER TABLE telegram_link_requests DROP COLUMN user_id;
