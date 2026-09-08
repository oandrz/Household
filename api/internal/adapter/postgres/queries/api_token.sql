-- name: CreateAPIToken :one
INSERT INTO api_tokens (user_id, household_id, name, token_hash, prefix, expires_at)
VALUES ($1, $2, $3, $4, $5, $6)
RETURNING *;

-- The live lookup: revoked or expired is unfindable, the same shape as
-- GetLiveSession, so no caller ever holds a dead token and forgets to check.
-- name: GetLiveAPIToken :one
SELECT * FROM api_tokens
WHERE token_hash = $1 AND revoked_at IS NULL AND expires_at > now();

-- name: ListAPITokensForUser :many
SELECT * FROM api_tokens
WHERE user_id = $1 AND revoked_at IS NULL
ORDER BY created_at DESC;

-- User-scoped on purpose: revoking by id alone would let one member revoke
-- another's token by guessing. A miss (wrong user, already revoked) affects
-- no row and the repository turns that into ErrNotFound.
-- name: RevokeAPIToken :execrows
UPDATE api_tokens SET revoked_at = now()
WHERE id = $1 AND user_id = $2 AND revoked_at IS NULL;

-- name: RevokeAPITokensForUser :exec
UPDATE api_tokens SET revoked_at = now()
WHERE user_id = $1 AND revoked_at IS NULL;

-- name: TouchAPIToken :exec
UPDATE api_tokens SET last_used_at = $2 WHERE id = $1;
