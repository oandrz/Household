-- name: CreateSignup :exec
INSERT INTO signups (email, token_hash, expires_at)
VALUES ($1, $2, $3);

-- CreateConsumedSignup writes a signup row that is already consumed at insert
-- time. It exists so SignupService.Request can rate-limit a registered
-- address by the same counters (CountSignupsForEmailSince,
-- CountSignupsSince) a fresh address is limited by: those counters read this
-- table regardless of consumed_at, but nothing ever wrote a row here for the
-- registered branch before this query existed, so its counters stayed at
-- zero forever and the limit never fired for it. A row inserted through this
-- query can never provision anything -- ConsumeSignup's guard requires
-- consumed_at IS NULL -- so it exists solely to be counted, never mailed.
-- name: CreateConsumedSignup :exec
INSERT INTO signups (email, token_hash, expires_at, consumed_at)
VALUES ($1, $2, $3, now());

-- name: GetSignupByTokenHash :one
SELECT id, email, telegram_chat_id, expires_at, consumed_at
FROM signups
WHERE token_hash = $1;

-- CreateTelegramSignup is CreateSignup's Telegram twin. The two are mutually
-- exclusive per row, enforced by signups_have_exactly_one_channel.
-- name: CreateTelegramSignup :exec
INSERT INTO signups (telegram_chat_id, token_hash, expires_at)
VALUES ($1, $2, $3);

-- name: CountSignupsForEmailSince :one
SELECT count(*) FROM signups
WHERE email = $1 AND created_at >= $2;

-- name: CountSignupsSince :one
SELECT count(*) FROM signups
WHERE created_at >= $1;

-- ConsumeSignup is the transactional gate for Provision. The guard lives here,
-- in the UPDATE, rather than in the caller: a zero-rows result means the signup
-- was already consumed or has expired, and that answer is authoritative for the
-- race between SignupService.Complete's read and this write. It returns the
-- email so Provision reads the verified address from the row it is already
-- touching, rather than trusting one passed in by a caller.
-- name: ConsumeSignup :one
UPDATE signups
SET consumed_at = now()
WHERE id = $1 AND consumed_at IS NULL AND expires_at > now()
RETURNING id, email, telegram_chat_id;

-- name: PruneSignups :execrows
DELETE FROM signups
WHERE created_at < $1
  AND (consumed_at IS NOT NULL OR expires_at <= now());
