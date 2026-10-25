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

-- ListCashflowRows returns every transaction of one household, oldest first,
-- with the two facts the cashflow classification needs from other tables: the
-- destination account's type (only meaningful for a transfer) and the cadence
-- of the bill this transaction paid. Both joins are LEFT: most rows have
-- neither. The bill_payments join also matches household_id so a payment row
-- can never pull in another household's bill. A transaction linked by more
-- than one payment comes back more than once; the repository refuses that
-- rather than count a payment twice.
-- name: ListCashflowRows :many
SELECT t.id, t.occurred_on, t.kind, t.amount_minor, t.amount_currency,
       ta.type     AS to_account_type,
       ta.nickname AS to_account_name,
       b.cadence   AS bill_cadence,
       b.name      AS bill_name
FROM transactions t
LEFT JOIN accounts ta      ON ta.id = t.to_account_id
LEFT JOIN bill_payments bp ON bp.transaction_id = t.id AND bp.household_id = t.household_id
LEFT JOIN bills b          ON b.id = bp.bill_id AND b.household_id = t.household_id
WHERE t.household_id = $1
ORDER BY t.occurred_on, t.id;
