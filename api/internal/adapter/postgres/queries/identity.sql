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
INSERT INTO households (name, family_name, primary_currency,
                        show_secondary_currency, secondary_currency)
VALUES ($1, $2, $3, $4, $5)
RETURNING id, name, family_name, primary_currency,
          show_secondary_currency, secondary_currency, fx_rate_mode;

-- name: ListMemberships :many
SELECT m.id, m.household_id, m.user_id, m.role, m.capabilities,
       u.email, u.display_name, u.avatar_initial
FROM memberships m JOIN users u ON u.id = m.user_id
WHERE m.household_id = $1
ORDER BY m.role DESC, u.display_name;

-- ByUser cannot take a household scope -- sign-in resolves the household from
-- it -- so it remains LIMIT 1 while one account belongs to exactly one
-- household. The ORDER BY makes which row it picks deterministic rather than
-- whatever the planner returns first, so if that invariant is ever broken the
-- failure is reproducible instead of intermittent.
-- name: GetMembershipByUser :one
SELECT id, household_id, user_id, role, capabilities
FROM memberships WHERE user_id = $1
ORDER BY joined_at, id
LIMIT 1;

-- name: CreateMembership :one
INSERT INTO memberships (household_id, user_id, role, capabilities)
VALUES ($1, $2, $3, $4)
RETURNING id, household_id, user_id, role, capabilities;

-- LockHouseholdMemberships takes a row lock on the household so two
-- membership changes run one after the other. It returns the id only
-- because a query must return something -- the lock is the point. Callers
-- lock first, then list and check the memberships as a new statement (see
-- MembershipRepository.UpdateWithCheck), so a racing writer always sees
-- what the first one committed.
--
-- It locks the household row, not the membership rows: one row is one
-- simple queue for anything changing who is in this household, and the
-- list that follows needs no reasoning about how Postgres re-reads a
-- locked row. FOR NO KEY UPDATE, not FOR UPDATE, because every insert into
-- a table referencing households (transactions, bills, ...) takes FOR KEY
-- SHARE on it, which FOR UPDATE would block; FOR NO KEY UPDATE still
-- excludes two membership changes from each other, since it conflicts with
-- itself.
-- name: LockHouseholdMemberships :one
SELECT id FROM households WHERE id = $1 FOR NO KEY UPDATE;

-- UpdateMembership and DeleteMembership report how many rows they touched so
-- the repository can answer domain.ErrNotFound for a membership that is not
-- this household's, instead of a silent success. Call them only through
-- MembershipRepository.UpdateWithCheck / DeleteWithCheck, which hold the lock.
-- name: UpdateMembership :execrows
UPDATE memberships SET role = $3, capabilities = $4 WHERE household_id = $1 AND id = $2;

-- name: DeleteMembership :execrows
DELETE FROM memberships WHERE household_id = $1 AND id = $2;

-- name: CreateSession :one
INSERT INTO sessions (token_hash, user_id, household_id, expires_at)
VALUES ($1, $2, $3, $4)
RETURNING id;

-- name: GetLiveSession :one
SELECT id, user_id, household_id, expires_at, admin_grant_expires_at, last_seen_at FROM sessions
WHERE token_hash = $1 AND revoked_at IS NULL AND expires_at > now();

-- name: ExtendSession :exec
UPDATE sessions SET expires_at = $2 WHERE token_hash = $1;

-- name: GrantAdminSession :exec
-- One column, deliberately: ExtendSession writes expires_at and this writes
-- the grant, so neither can silently undo the other.
UPDATE sessions SET admin_grant_expires_at = $2 WHERE token_hash = $1;

-- name: TouchSession :exec
-- One column, the same rule ExtendSession and GrantAdminSession follow:
-- expires_at, admin_grant_expires_at and last_seen_at are each written by
-- exactly one statement, so none can silently undo another.
UPDATE sessions SET last_seen_at = $2 WHERE token_hash = $1;

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

-- name: CreateTelegramInvite :one
-- No email column at all, which is what invites_channel_matches_email
-- requires of this channel (migration 00021).
INSERT INTO invites (household_id, name, role, capabilities, token_hash, invited_by, expires_at, channel)
VALUES ($1, $2, $3, $4, $5, $6, $7, 'telegram')
RETURNING id;

-- name: GetInviteByTokenHash :one
SELECT i.id, i.household_id, i.email, i.name, i.role, i.capabilities, i.channel,
       i.expires_at, i.accepted_at, h.family_name, u.display_name AS inviter_name
FROM invites i
JOIN households h ON h.id = i.household_id
JOIN users u ON u.id = i.invited_by
WHERE i.token_hash = $1;

-- name: GetLiveInviteForEmail :one
SELECT i.id, i.household_id, i.email, i.name, i.role, i.capabilities, i.channel,
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

-- name: ListPendingInvites :many
-- "Pending" is the partner-invite spec's one definition: not accepted and not
-- expired. $2 is the caller's clock rather than now(), so a test can move it,
-- the same shape ListPendingInvitesForAdmin uses.
SELECT id, email, name, role, capabilities, channel,
       knock_chat_username, knock_code, knocked_at,
       expires_at, created_at
FROM invites
WHERE household_id = $1 AND accepted_at IS NULL AND expires_at > $2
ORDER BY created_at, id;

-- name: DeleteUnacceptedInvite :one
-- Scoped by household in the SQL itself, so an id from another household
-- deletes nothing (docs/LEARNING.md pattern 24). An accepted invite is
-- history and is never deleted here.
DELETE FROM invites
WHERE id = $1 AND household_id = $2 AND accepted_at IS NULL
RETURNING id;

-- name: InviteAcceptedInHousehold :one
-- Read only after DeleteUnacceptedInvite matched nothing, to tell "already
-- accepted" apart from "no such invite in this household". The ::boolean
-- cast is the same trick holding.sql's account_archived and
-- admin_directory.sql's has_telegram use, so sqlc infers a real bool rather
-- than the untyped interface{} it falls back to for a bare IS NOT NULL.
SELECT (accepted_at IS NOT NULL)::boolean AS accepted
FROM invites
WHERE id = $1 AND household_id = $2;

-- name: RecordInviteKnock :one
-- One guarded UPDATE is the whole of "one knock per link": knocked_at IS
-- NULL is what makes the second tap -- and two taps at the same instant --
-- lose. Every other condition is here for the same reason it is in the SQL
-- and not in Go: a caller cannot forget it.
UPDATE invites
SET knock_chat_id = $2, knock_chat_username = $3, knock_code = $4, knocked_at = $5
WHERE token_hash = $1
  AND channel = 'telegram'
  AND accepted_at IS NULL
  AND expires_at > $5
  AND knocked_at IS NULL
RETURNING id;

-- name: ClaimKnockedInvite :one
-- The guard and the read in one statement: it stamps the invite accepted
-- only if it's a telegram invite, unaccepted, unexpired, and knocked, and
-- returns everything InviteRepo.Admit's transaction needs, so no separate
-- read can see a different row. Zero rows means one of those conditions
-- failed; the caller tells them apart with one more read, as Delete does
-- with InviteAcceptedInHousehold.
UPDATE invites
SET accepted_at = $3
WHERE id = $1 AND household_id = $2
  AND channel = 'telegram'
  AND accepted_at IS NULL
  AND expires_at > $3
  AND knocked_at IS NOT NULL
RETURNING name, role, capabilities, knock_chat_id, knock_chat_username;

-- name: ReplaceInviteToken :one
-- One statement replaces the token and clears the knock together, so there
-- is never an instant where a fresh link carries a stale knock. It returns
-- the chat that had knocked, if any, so the caller can tell them their link
-- died. The subselect reads the row as it stood when this statement began
-- (READ COMMITTED's snapshot rule), before the UPDATE's own SET clears it --
-- proven against a real database by
-- TestReplaceInviteTokenReturnsThePreviousKnockChatID, not just documented.
-- No expires_at condition, deliberately: an expired link is the main reason
-- an owner asks for a new one, so this must still work on it.
--
-- The UPDATE names its target "target" and the subselect its copy "prior":
-- without both aliases sqlc's analyzer (not real Postgres) reports the
-- outer WHERE's `id` as ambiguous. COALESCE(..., 0) turns "nobody had
-- knocked" into 0 inside the query, matching InviteRepository.ReplaceToken's
-- contract -- without it sqlc infers a non-nullable int64, and scanning a
-- genuine NULL into that fails at runtime the first time an owner asks for
-- a link nobody has tapped yet.
UPDATE invites AS target
SET token_hash = $3, expires_at = $4,
    knock_chat_id = NULL, knock_chat_username = NULL, knock_code = NULL, knocked_at = NULL
WHERE target.id = $1 AND target.household_id = $2
  AND target.channel = 'telegram'
  AND target.accepted_at IS NULL
RETURNING COALESCE((SELECT prior.knock_chat_id FROM invites prior WHERE prior.id = $1), 0)::bigint AS previous_knock_chat_id;

-- name: InviteChannelForReplace :one
-- Read only after ReplaceInviteToken's guarded UPDATE matched nothing, to
-- tell "this invite has no email channel" apart from "no such invite in
-- this household" -- the same fallback-read shape
-- InviteAcceptedInHousehold gives DeleteUnacceptedInvite. accepted_at IS
-- NULL sits in the WHERE, not returned as a column: a non-match here means
-- another household, unknown, or already accepted, and
-- InviteRepo.ReplaceToken answers domain.ErrNotFound for all three, the
-- same as Delete.
SELECT channel
FROM invites
WHERE id = $1 AND household_id = $2 AND accepted_at IS NULL;

-- name: ListSpaces :many
-- ORDER BY position, key: position alone has no tiebreaker -- two spaces can
-- share one (NextSpacePosition assigns it, not a unique constraint) -- so
-- without key the sidebar's order could shift between requests with no
-- write in between. key is unique per household, so it always breaks the
-- tie the same way.
SELECT id, household_id, key, name, visibility, position, is_builtin, required_capability
FROM spaces WHERE household_id = $1 ORDER BY position, key;

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

-- PruneLoginAttempts deletes attempts older than the cutoff, including the
-- NULL-household_id rows an unknown-address attempt records. ClearFailures
-- can't reach those -- it is scoped WHERE household_id = $1, which never
-- matches NULL -- so this is the only place those rows are ever deleted.
--
-- The caller must pick a cutoff well outside domain.LockoutPolicy.Window:
-- deleting a row still inside it would clear a live lockout, a security
-- regression dressed as cleanup.
-- name: PruneLoginAttempts :execrows
DELETE FROM login_attempts WHERE at < $1;
