-- name: CreateTelegramLinkRequest :exec
INSERT INTO telegram_link_requests (nonce_hash, expires_at, user_id)
VALUES ($1, $2, $3);

-- ConsumeTelegramLinkRequest is the single-use gate, and it records the
-- redeeming chat in the same statement. The guard lives here rather than in
-- the caller for the same reason ConsumeSignup's does: zero rows is the
-- authoritative answer to the race between a read and this write. It now
-- returns user_id as well, because the caller's next decision -- link, sign
-- in, or sign up -- is exactly that column.
-- name: ConsumeTelegramLinkRequest :one
UPDATE telegram_link_requests
SET consumed_at = now(), chat_id = $2, chat_username = $3
WHERE nonce_hash = $1 AND consumed_at IS NULL AND expires_at > now()
RETURNING id, user_id;

-- name: GetTelegramLinkRequest :one
SELECT id, user_id, chat_id, chat_username, consumed_at, expires_at
FROM telegram_link_requests WHERE id = $1;

-- CountTelegramLinkMintsSince bounds how many link nonces one member can
-- mint. The per-chat limit below bounds redemption; this bounds minting,
-- which a signed-in session can now do with no chat involved at all.
-- name: CountTelegramLinkMintsSince :one
SELECT count(*) FROM telegram_link_requests
WHERE user_id = $1 AND created_at >= $2;

-- name: CountTelegramLinksSince :one
SELECT count(*) FROM telegram_link_requests
WHERE chat_id = $1 AND consumed_at >= $2;

-- name: GetTelegramAccountByChatID :one
SELECT user_id FROM telegram_accounts WHERE chat_id = $1;

-- name: CreateTelegramAccount :exec
INSERT INTO telegram_accounts (user_id, chat_id) VALUES ($1, $2);

-- PruneTelegramLinkRequests mirrors PruneSignups exactly: same retention
-- condition (created before the cutoff, and either already consumed or
-- expired), for the same reason -- a nonce nobody ever redeemed carries no
-- chat_id (see the table's own CHECK), so no per-chat limit ever bounds how
-- many a stranger can mint. This is the third of the three tables a stranger
-- can grow without an account; the other two (signups, login_attempts) are
-- already pruned by adminctl prune.
-- name: PruneTelegramLinkRequests :execrows
DELETE FROM telegram_link_requests
WHERE created_at < $1
  AND (consumed_at IS NOT NULL OR expires_at <= now());
