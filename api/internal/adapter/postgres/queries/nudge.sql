-- ListNudgeRecipients is the outbound half of ADR 8's rule: money leaves the
-- system only to a chat that could already ask /balance -- an owner with the
-- money capability -- and only while that chat has not said /nudges off. The
-- role and capability predicates are the authorisation; a test drops each one
-- and expects a stranger to appear.
-- name: ListNudgeRecipients :many
SELECT ta.chat_id, m.household_id, m.id AS membership_id, h.primary_currency
FROM telegram_accounts ta
JOIN memberships m ON m.user_id = ta.user_id
JOIN households h ON h.id = m.household_id
WHERE ta.nudges_enabled
  AND m.role = 'owner'
  AND 'money' = ANY (m.capabilities)
ORDER BY ta.chat_id, m.household_id;

-- ClaimNudge is insert-first: zero rows means today's digest for this chat and
-- household is already claimed (sent, or being sent), and the caller skips.
-- name: ClaimNudge :execrows
INSERT INTO nudge_deliveries (chat_id, household_id, day)
VALUES ($1, $2, $3)
ON CONFLICT DO NOTHING;

-- ReleaseNudge undoes a claim whose send failed, so the next tick tries again.
-- name: ReleaseNudge :exec
DELETE FROM nudge_deliveries
WHERE chat_id = $1 AND household_id = $2 AND day = $3;

-- name: SetNudgesEnabled :execrows
UPDATE telegram_accounts SET nudges_enabled = $2 WHERE chat_id = $1;

-- name: PruneNudgeDeliveries :execrows
DELETE FROM nudge_deliveries WHERE day < $1;
