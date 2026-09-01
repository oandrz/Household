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
