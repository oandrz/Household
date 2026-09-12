-- name: InsertHoldingIncome :one
INSERT INTO holding_income (holding_id, household_id, kind, amount_minor,
                            primary_amount_minor, primary_currency, received_on, note)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
RETURNING id, holding_id, household_id, kind, amount_minor,
          primary_amount_minor, primary_currency, received_on, note, created_at;

-- ListHoldingIncome is one holding's rows, newest first, the way a panel under
-- the holding reads them. The order is a PRESENTATION choice here and nothing
-- computes with it -- summing a period is commutative, unlike the event fold
-- whose ORDER BY is a contract.
-- name: ListHoldingIncome :many
SELECT i.id, i.holding_id, i.household_id, i.kind, i.amount_minor,
       i.primary_amount_minor, i.primary_currency, i.received_on, i.note, i.created_at,
       h.currency
FROM holding_income i
JOIN holdings h ON h.id = i.holding_id
WHERE i.household_id = $1 AND i.holding_id = $2
ORDER BY i.received_on DESC, i.id;

-- ListHoldingIncomeForHousehold is every row the period report needs, in one
-- read rather than one query per holding -- the same shape
-- ListHoldingEventsForHousehold uses and for the same reason.
-- name: ListHoldingIncomeForHousehold :many
SELECT i.id, i.holding_id, i.household_id, i.kind, i.amount_minor,
       i.primary_amount_minor, i.primary_currency, i.received_on, i.note, i.created_at,
       h.currency
FROM holding_income i
JOIN holdings h ON h.id = i.holding_id
WHERE i.household_id = $1
ORDER BY i.holding_id, i.received_on, i.id;

-- DeleteHoldingIncome is scoped to the HOLDING as well as the household, so a
-- request naming one holding cannot remove another's row. The household alone
-- would be enough to keep the households apart -- this is about the URL and
-- the database agreeing on which holding is being edited.
-- name: DeleteHoldingIncome :execrows
DELETE FROM holding_income
WHERE household_id = $1 AND holding_id = $2 AND id = $3;
