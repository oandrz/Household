-- The operator's read-only view across every household. Nothing here
-- writes. "When was this session last used" is COALESCE(last_seen_at,
-- created_at) -- a session from before migration 00013, or one never
-- touched, counts from its creation -- and that expression appears only in
-- this file so it cannot drift.

-- name: CountHouseholds :one
SELECT COUNT(*) FROM households;

-- name: CountActiveHouseholdsSince :one
SELECT COUNT(*) FROM households h
WHERE EXISTS (
    SELECT 1 FROM sessions s
    WHERE s.household_id = h.id
      AND COALESCE(s.last_seen_at, s.created_at) >= $1
);

-- name: CountSignupsSinceForAdmin :one
-- Named ...ForAdmin (unlike the other Count* queries here) because
-- queries/signup.sql already has a CountSignupsSince -- a single-column
-- count used for per-address rate limiting -- and sqlc rejects a duplicate
-- query name across files even though the shapes differ.
-- Both channels: the table's own CHECK guarantees exactly one of email or
-- telegram_chat_id is set, so a plain count is a count of sign-ups.
SELECT
    COUNT(*)::bigint AS requested,
    COUNT(*) FILTER (WHERE consumed_at IS NOT NULL)::bigint AS completed
FROM signups
WHERE created_at >= $1;

-- name: CountPendingInvites :one
SELECT COUNT(*) FROM invites WHERE accepted_at IS NULL AND expires_at > $1;

-- name: SearchHouseholds :many
-- pattern is the caller-escaped '%q%' (see likePattern in the repo);
-- has_query is false for an empty search, which must return every
-- household and name no matched member. The first LATERAL join finds the
-- id of the first member (by joined_at) whose name or email matched, so a
-- row can say why it appeared when the household itself did not match --
-- its id alone is selected there, never its display_name/email, because
-- sqlc's nullability analysis for a LEFT JOIN onto a derived subquery
-- (LATERAL or not -- sqlc-dev/sqlc#3667) does not mark a NOT NULL source
-- column nullable, and a match_name generated as plain string then fails at
-- scan time with "cannot scan NULL into *string" on every row with no
-- match. The second, plain `LEFT JOIN users mu` fetches that member's name
-- and email; a LEFT JOIN straight onto a real table is the pattern
-- GetTransaction's paid_by_name already relies on and sqlc infers correctly.
SELECT
    h.id,
    h.name,
    h.family_name,
    h.primary_currency,
    h.created_at,
    (SELECT COUNT(*) FROM memberships m WHERE m.household_id = h.id)::bigint AS member_count,
    (SELECT MAX(COALESCE(s.last_seen_at, s.created_at)) FROM sessions s
       WHERE s.household_id = h.id)::timestamptz AS last_active_at,
    (sqlc.arg(has_query)::boolean
       AND (h.name ILIKE sqlc.arg(pattern) OR h.family_name ILIKE sqlc.arg(pattern)))::boolean AS household_matched,
    mu.display_name AS match_name,
    mu.email AS match_email
FROM households h
LEFT JOIN LATERAL (
    SELECT m.user_id
    FROM memberships m
    JOIN users u ON u.id = m.user_id
    WHERE sqlc.arg(has_query)::boolean
      AND m.household_id = h.id
      AND (u.display_name ILIKE sqlc.arg(pattern) OR u.email ILIKE sqlc.arg(pattern))
    ORDER BY m.joined_at
    LIMIT 1
) mm ON true
LEFT JOIN users mu ON mu.id = mm.user_id
WHERE NOT sqlc.arg(has_query)::boolean
   OR h.name ILIKE sqlc.arg(pattern)
   OR h.family_name ILIKE sqlc.arg(pattern)
   OR mm.user_id IS NOT NULL
ORDER BY last_active_at DESC NULLS LAST, h.created_at DESC
LIMIT sqlc.arg(row_limit)::int;

-- name: GetHouseholdForAdmin :one
SELECT id, name, family_name, primary_currency, created_at
FROM households WHERE id = $1;

-- name: ListMembersForAdmin :many
-- has_telegram comes from the join, never from email IS NULL: a user with
-- neither is a defect the screen should surface, not a state it names.
SELECT
    u.id AS user_id,
    u.display_name,
    u.email,
    m.role,
    m.capabilities,
    (ta.user_id IS NOT NULL)::boolean AS has_telegram,
    (SELECT MAX(COALESCE(s.last_seen_at, s.created_at)) FROM sessions s
       WHERE s.user_id = u.id AND s.household_id = m.household_id)::timestamptz AS last_active_at
FROM memberships m
JOIN users u ON u.id = m.user_id
LEFT JOIN telegram_accounts ta ON ta.user_id = u.id
WHERE m.household_id = $1
ORDER BY m.joined_at;

-- name: ListPendingInvitesForAdmin :many
SELECT i.name, i.email, i.role, i.expires_at, inviter.display_name AS invited_by_name
FROM invites i
JOIN users inviter ON inviter.id = i.invited_by
WHERE i.household_id = $1 AND i.accepted_at IS NULL AND i.expires_at > $2
ORDER BY i.created_at;
