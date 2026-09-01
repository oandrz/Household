-- name: CreateTelegramLinkRequest :exec
INSERT INTO telegram_link_requests (nonce_hash, expires_at)
VALUES ($1, $2);

-- ConsumeTelegramLinkRequest is the single-use gate, and it records the
-- redeeming chat in the same statement. The guard lives here rather than in
-- the caller for the same reason ConsumeSignup's does: zero rows is the
-- authoritative answer to the race between a read and this write.
-- name: ConsumeTelegramLinkRequest :one
UPDATE telegram_link_requests
SET consumed_at = now(), chat_id = $2
WHERE nonce_hash = $1 AND consumed_at IS NULL AND expires_at > now()
RETURNING id;

-- name: CountTelegramLinksSince :one
SELECT count(*) FROM telegram_link_requests
WHERE chat_id = $1 AND consumed_at >= $2;

-- name: GetTelegramAccountByChatID :one
SELECT user_id FROM telegram_accounts WHERE chat_id = $1;

-- name: CreateTelegramAccount :exec
INSERT INTO telegram_accounts (user_id, chat_id) VALUES ($1, $2);
