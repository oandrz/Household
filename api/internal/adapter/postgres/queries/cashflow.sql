-- ListCashflowConfirmations returns every confirmed month of one household,
-- oldest first. The confirmer's name comes through memberships and users; a
-- confirmation whose member has since left has no membership and reads "".
-- name: ListCashflowConfirmations :many
SELECT c.month, c.confirmed_at, COALESCE(u.display_name, '')::text AS confirmed_by_name
FROM cashflow_month_confirmations c
LEFT JOIN memberships m ON m.id = c.confirmed_by_membership_id
LEFT JOIN users u       ON u.id = m.user_id
WHERE c.household_id = $1
ORDER BY c.month;

-- ConfirmCashflowMonth is idempotent: the no-op DO UPDATE makes RETURNING
-- yield the existing row, so the first person to confirm stays the one kept.
-- name: ConfirmCashflowMonth :one
WITH c AS (
    INSERT INTO cashflow_month_confirmations (household_id, month, confirmed_by_membership_id, confirmed_at)
    VALUES ($1, sqlc.arg(month)::date, $2, $3)
    ON CONFLICT (household_id, month) DO UPDATE SET month = EXCLUDED.month
    RETURNING month, confirmed_by_membership_id, confirmed_at
)
SELECT c.month, c.confirmed_at, COALESCE(u.display_name, '')::text AS confirmed_by_name
FROM c
LEFT JOIN memberships m ON m.id = c.confirmed_by_membership_id
LEFT JOIN users u       ON u.id = m.user_id;

-- UnconfirmCashflowMonth deletes the mark; deleting nothing is not an error.
-- name: UnconfirmCashflowMonth :exec
DELETE FROM cashflow_month_confirmations
WHERE household_id = $1 AND month = sqlc.arg(month)::date;
