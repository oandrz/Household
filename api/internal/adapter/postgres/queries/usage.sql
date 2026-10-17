-- Counts for the per-household caps (domain/limits.go). Each WHERE leads
-- with household_id so an index serves it (migration 00024).

-- name: CountTransactions :one
SELECT count(*) FROM transactions WHERE household_id = $1;

-- name: CountTransactionsInMonth :one
SELECT count(*) FROM transactions
WHERE household_id = $1 AND occurred_on >= sqlc.arg(month_start)::date AND occurred_on < sqlc.arg(next_month_start)::date;

-- name: CountAccounts :one
SELECT count(*) FROM accounts WHERE household_id = $1;

-- Categories use CountCategories in transaction.sql: the same count.

-- name: CountGoals :one
SELECT count(*) FROM goals WHERE household_id = $1;

-- name: CountBills :one
SELECT count(*) FROM bills WHERE household_id = $1;

-- name: CountHoldings :one
SELECT count(*) FROM holdings WHERE household_id = $1;

-- name: CountSpaces :one
SELECT count(*) FROM spaces WHERE household_id = $1;

-- name: CountAgreementSections :one
SELECT count(*) FROM agreement_sections WHERE household_id = $1;

-- name: CountGoalContributions :one
SELECT count(*) FROM goal_contributions WHERE household_id = $1;

-- name: CountBillPayments :one
SELECT count(*) FROM bill_payments WHERE household_id = $1;

-- name: CountHoldingEvents :one
SELECT count(*) FROM holding_events WHERE household_id = $1;

-- name: CountHoldingValuations :one
SELECT count(*) FROM holding_valuations WHERE household_id = $1;

-- name: CountHoldingIncome :one
SELECT count(*) FROM holding_income WHERE household_id = $1;

-- name: CountRetroActions :one
SELECT count(*) FROM retro_actions a JOIN retros r ON r.id = a.retro_id WHERE r.household_id = $1;

-- name: CountAgreementProposals :one
SELECT count(*) FROM agreement_proposals WHERE household_id = $1;

-- name: CountMembers :one
SELECT count(*) FROM memberships WHERE household_id = $1;

-- name: CountPendingInvitesInHousehold :one
SELECT count(*) FROM invites WHERE household_id = $1 AND accepted_at IS NULL AND expires_at > now();

-- name: CountActiveAPITokens :one
SELECT count(*) FROM api_tokens WHERE household_id = $1 AND revoked_at IS NULL AND expires_at > now();
