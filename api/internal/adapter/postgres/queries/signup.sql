-- name: CreateSignup :exec
INSERT INTO signups (email, token_hash, expires_at)
VALUES ($1, $2, $3);

-- name: GetSignupByTokenHash :one
SELECT id, email, expires_at, consumed_at
FROM signups
WHERE token_hash = $1;

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
RETURNING id, email;

-- name: PruneSignups :execrows
DELETE FROM signups
WHERE created_at < $1
  AND (consumed_at IS NOT NULL OR expires_at <= now());
