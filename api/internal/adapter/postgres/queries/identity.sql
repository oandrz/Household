-- name: GetUserByEmail :one
SELECT id, email, password_hash, display_name, avatar_initial FROM users WHERE email = $1;

-- name: GetUserByID :one
SELECT id, email, password_hash, display_name, avatar_initial FROM users WHERE id = $1;

-- name: CreateUser :one
INSERT INTO users (email, password_hash, display_name, avatar_initial)
VALUES ($1, $2, $3, $4)
RETURNING id, email, password_hash, display_name, avatar_initial;

-- name: SetPasswordHash :exec
UPDATE users SET password_hash = $2 WHERE id = $1;

-- name: GetOrphanedCredentiallessUserByName :one
-- A credential-less user (no email, no password) with this display name
-- that currently holds no membership row at all -- the state a removed
-- membership leaves behind without deleting the user underneath it.
SELECT id, email, password_hash, display_name, avatar_initial
FROM users u
WHERE u.display_name = $1
  AND u.email IS NULL
  AND u.password_hash IS NULL
  AND NOT EXISTS (SELECT 1 FROM memberships m WHERE m.user_id = u.id)
LIMIT 1;

-- name: GetHousehold :one
SELECT id, name, family_name, primary_currency, show_secondary_currency,
       secondary_currency, fx_rate_mode
FROM households WHERE id = $1;

-- name: UpdateHousehold :one
UPDATE households
SET name = $2, family_name = $3, primary_currency = $4, show_secondary_currency = $5,
    secondary_currency = $6, fx_rate_mode = $7
WHERE id = $1
RETURNING id, name, family_name, primary_currency, show_secondary_currency,
          secondary_currency, fx_rate_mode;

-- name: CreateHousehold :one
INSERT INTO households (name, family_name) VALUES ($1, $2)
RETURNING id, name, family_name, primary_currency, show_secondary_currency,
          secondary_currency, fx_rate_mode;

-- name: ListMemberships :many
SELECT m.id, m.household_id, m.user_id, m.role, m.capabilities,
       u.email, u.display_name, u.avatar_initial
FROM memberships m JOIN users u ON u.id = m.user_id
WHERE m.household_id = $1
ORDER BY m.role DESC, u.display_name;

-- name: GetMembershipByUser :one
SELECT id, household_id, user_id, role, capabilities
FROM memberships WHERE user_id = $1 LIMIT 1;

-- name: CreateMembership :one
INSERT INTO memberships (household_id, user_id, role, capabilities)
VALUES ($1, $2, $3, $4)
RETURNING id, household_id, user_id, role, capabilities;

-- name: UpdateMembership :exec
UPDATE memberships SET role = $3, capabilities = $4 WHERE household_id = $1 AND id = $2;

-- name: DeleteMembership :exec
DELETE FROM memberships WHERE household_id = $1 AND id = $2;

-- name: CreateSession :one
INSERT INTO sessions (token_hash, user_id, household_id, expires_at)
VALUES ($1, $2, $3, $4)
RETURNING id;

-- name: GetLiveSession :one
SELECT id, user_id, household_id, expires_at FROM sessions
WHERE token_hash = $1 AND revoked_at IS NULL AND expires_at > now();

-- name: ExtendSession :exec
UPDATE sessions SET expires_at = $2 WHERE token_hash = $1;

-- name: RevokeSessionByToken :exec
UPDATE sessions SET revoked_at = now() WHERE token_hash = $1 AND revoked_at IS NULL;

-- name: RevokeSessionsForUser :exec
UPDATE sessions SET revoked_at = now() WHERE user_id = $1 AND revoked_at IS NULL;

-- name: CreateMagicLink :exec
INSERT INTO magic_links (user_id, token_hash, expires_at) VALUES ($1, $2, $3);

-- name: ConsumeMagicLink :one
UPDATE magic_links SET consumed_at = now()
WHERE token_hash = $1 AND consumed_at IS NULL AND expires_at > now()
RETURNING user_id;

-- name: CountRecentMagicLinks :one
SELECT count(*) FROM magic_links m JOIN users u ON u.id = m.user_id
WHERE u.email = $1 AND m.created_at > $2;

-- name: RecordLoginAttempt :exec
INSERT INTO login_attempts (household_id, user_id, email, succeeded, at)
VALUES ($1, $2, $3, $4, $5);

-- name: ListRecentFailures :many
SELECT at FROM login_attempts
WHERE household_id = $1 AND succeeded = false AND at > $2
ORDER BY at DESC;

-- name: ListRecentFailuresByEmail :many
SELECT at FROM login_attempts
WHERE email = $1 AND succeeded = false AND at > $2
ORDER BY at DESC;

-- name: ClearFailures :exec
DELETE FROM login_attempts WHERE household_id = $1 AND succeeded = false;

-- name: CreateInvite :one
INSERT INTO invites (household_id, email, name, role, capabilities, token_hash, invited_by, expires_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
RETURNING id;

-- name: GetInviteByTokenHash :one
SELECT i.id, i.household_id, i.email, i.name, i.role, i.capabilities,
       i.expires_at, i.accepted_at, h.family_name, u.display_name AS inviter_name
FROM invites i
JOIN households h ON h.id = i.household_id
JOIN users u ON u.id = i.invited_by
WHERE i.token_hash = $1;

-- name: GetLiveInviteForEmail :one
SELECT i.id, i.household_id, i.email, i.name, i.role, i.capabilities,
       i.expires_at, i.accepted_at, h.family_name, u.display_name AS inviter_name
FROM invites i
JOIN households h ON h.id = i.household_id
JOIN users u ON u.id = i.invited_by
WHERE i.household_id = $1 AND i.email = $2
  AND i.accepted_at IS NULL AND i.expires_at > now()
ORDER BY i.created_at DESC
LIMIT 1;

-- name: MarkInviteAccepted :one
UPDATE invites SET accepted_at = now()
WHERE id = $1 AND accepted_at IS NULL AND expires_at > now()
RETURNING id;

-- name: ListSpaces :many
SELECT id, household_id, key, name, visibility, position, is_builtin, required_capability
FROM spaces WHERE household_id = $1 ORDER BY position;

-- name: CreateSpace :one
INSERT INTO spaces (household_id, key, name, visibility, position, is_builtin, required_capability)
VALUES ($1, $2, $3, $4, $5, $6, $7)
RETURNING id, household_id, key, name, visibility, position, is_builtin, required_capability;

-- name: NextSpacePosition :one
SELECT coalesce(max(position), 0) + 1 FROM spaces WHERE household_id = $1;

-- name: GetNotificationPreferences :one
SELECT household_id, bill_reminders, overspend_alerts, retro_reminder, weekly_digest
FROM notification_preferences WHERE household_id = $1;

-- name: UpsertNotificationPreferences :one
INSERT INTO notification_preferences (household_id, bill_reminders, overspend_alerts, retro_reminder, weekly_digest)
VALUES ($1, $2, $3, $4, $5)
ON CONFLICT (household_id) DO UPDATE
SET bill_reminders = excluded.bill_reminders,
    overspend_alerts = excluded.overspend_alerts,
    retro_reminder = excluded.retro_reminder,
    weekly_digest = excluded.weekly_digest
RETURNING household_id, bill_reminders, overspend_alerts, retro_reminder, weekly_digest;
