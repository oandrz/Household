-- ListBills returns one household's bills joined to the names the screen
-- displays -- the category (COALESCE'd to '' for uncategorised, the ports.go
-- NULL convention) and the pay-from account's nickname and currency. The
-- currency join is load-bearing, not decoration: bills carries no currency
-- column of its own (00008_bills.sql's own comment), so this is the only
-- place Bill.Amount's currency can come from.
--
-- include_archived is a UNION, not a filter swap -- BillRepository.List's own
-- doc comment: false returns only the live bills, true returns the live ones
-- AND the archived ones together, each carrying its own ArchivedAt.
-- name: ListBills :many
SELECT b.id, b.household_id, b.name, b.amount_minor, b.cadence, b.next_due,
       b.due_anchor_day, b.category_id, b.pay_from_account_id,
       b.paid_by_membership_id, b.autopay, b.is_subscription, b.archived_at,
       COALESCE(c.name, '')  AS category_name,
       a.nickname            AS account_name,
       a.opening_balance_currency AS currency
FROM bills b
JOIN accounts a ON a.id = b.pay_from_account_id
LEFT JOIN categories c ON c.id = b.category_id
WHERE b.household_id = $1
  AND (sqlc.arg(include_archived)::boolean OR b.archived_at IS NULL)
ORDER BY b.next_due NULLS LAST, b.name;

-- GetBill is ListBills scoped to one bill. household_id AND id are both
-- required in the WHERE -- a bill id from another household must match no
-- row, so it is indistinguishable from an id that never existed
-- (BillRepository.Get's own doc comment).
-- name: GetBill :one
SELECT b.id, b.household_id, b.name, b.amount_minor, b.cadence, b.next_due,
       b.due_anchor_day, b.category_id, b.pay_from_account_id,
       b.paid_by_membership_id, b.autopay, b.is_subscription, b.archived_at,
       COALESCE(c.name, '')  AS category_name,
       a.nickname            AS account_name,
       a.opening_balance_currency AS currency
FROM bills b
JOIN accounts a ON a.id = b.pay_from_account_id
LEFT JOIN categories c ON c.id = b.category_id
WHERE b.household_id = $1 AND b.id = $2;

-- CreateBill writes one row and joins straight back to it in the same
-- statement, via a CTE, rather than a bare RETURNING off the bills table --
-- bills has no currency column, so a bare RETURNING has nothing to build
-- Bill.Amount.Currency from, and BillRecord requires it populated (see this
-- file's header comment on ListBills). A name colliding with UNIQUE
-- (household_id, name) -- archived rows included -- surfaces as a 23505 that
-- translate maps to domain.ErrBillNameTaken, the same categoryNameUnique/
-- goalNameUnique pattern.
-- name: CreateBill :one
WITH created AS (
    INSERT INTO bills (household_id, name, amount_minor, cadence, next_due, due_anchor_day,
                        category_id, pay_from_account_id, paid_by_membership_id, autopay, is_subscription)
    VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
    RETURNING id, household_id, name, amount_minor, cadence, next_due, due_anchor_day,
              category_id, pay_from_account_id, paid_by_membership_id, autopay, is_subscription, archived_at
)
SELECT created.id, created.household_id, created.name, created.amount_minor, created.cadence, created.next_due,
       created.due_anchor_day, created.category_id, created.pay_from_account_id, created.paid_by_membership_id,
       created.autopay, created.is_subscription, created.archived_at,
       COALESCE(c.name, '')  AS category_name,
       a.nickname            AS account_name,
       a.opening_balance_currency AS currency
FROM created
JOIN accounts a ON a.id = created.pay_from_account_id
LEFT JOIN categories c ON c.id = created.category_id;

-- UpdateBill is an unconditional full-row SET -- every mutable column,
-- including due_anchor_day, no COALESCE and no dynamic SQL. BillService turns
-- a partial PATCH into a complete domain.Bill before this query ever runs
-- (ports.go's Update comment), so the anchor arrives already derived and this
-- adapter never computes a calendar day. archived_at has no SET clause here,
-- the same reason UpdateGoal excludes it: archiving is SetBillArchived's own
-- job. Scoped by household_id AND id together, same collision contract as
-- CreateBill, same join-via-CTE reason.
-- name: UpdateBill :one
WITH updated AS (
    UPDATE bills
    SET name = $3, amount_minor = $4, cadence = $5, next_due = $6, due_anchor_day = $7,
        category_id = $8, pay_from_account_id = $9, paid_by_membership_id = $10,
        autopay = $11, is_subscription = $12, updated_at = now()
    WHERE bills.household_id = $1 AND bills.id = $2
    RETURNING id, household_id, name, amount_minor, cadence, next_due, due_anchor_day,
              category_id, pay_from_account_id, paid_by_membership_id, autopay, is_subscription, archived_at
)
SELECT updated.id, updated.household_id, updated.name, updated.amount_minor, updated.cadence, updated.next_due,
       updated.due_anchor_day, updated.category_id, updated.pay_from_account_id, updated.paid_by_membership_id,
       updated.autopay, updated.is_subscription, updated.archived_at,
       COALESCE(c.name, '')  AS category_name,
       a.nickname            AS account_name,
       a.opening_balance_currency AS currency
FROM updated
JOIN accounts a ON a.id = updated.pay_from_account_id
LEFT JOIN categories c ON c.id = updated.category_id;

-- SetBillArchived stamps or clears archived_at depending on the caller's
-- boolean, scoped the same way UpdateBill is. The COALESCE is "first stamp
-- wins": archiving an already-archived bill keeps its original archived_at
-- rather than moving it forward to the caller's `at` -- the same
-- SetGoalArchived/SetCategoryArchived convention.
-- name: SetBillArchived :one
WITH archived AS (
    UPDATE bills
    SET archived_at = CASE WHEN sqlc.arg(archived)::boolean THEN COALESCE(archived_at, sqlc.arg(at)::timestamptz) ELSE NULL END,
        updated_at = now()
    WHERE bills.household_id = sqlc.arg(household_id) AND bills.id = sqlc.arg(id)
    RETURNING id, household_id, name, amount_minor, cadence, next_due, due_anchor_day,
              category_id, pay_from_account_id, paid_by_membership_id, autopay, is_subscription, archived_at
)
SELECT archived.id, archived.household_id, archived.name, archived.amount_minor, archived.cadence, archived.next_due,
       archived.due_anchor_day, archived.category_id, archived.pay_from_account_id, archived.paid_by_membership_id,
       archived.autopay, archived.is_subscription, archived.archived_at,
       COALESCE(c.name, '')  AS category_name,
       a.nickname            AS account_name,
       a.opening_balance_currency AS currency
FROM archived
JOIN accounts a ON a.id = archived.pay_from_account_id
LEFT JOIN categories c ON c.id = archived.category_id;

-- BillMonthDueTotals is half of the union BillRepository.MonthTotals' own doc
-- comment demands. A bill paid this month has already advanced past it (its
-- next_due now reads next month), so the two halves come from different
-- tables and neither alone is the figure.
--
-- No archived_at filter here, unlike BillMonthUnpaidTotals below, and that
-- asymmetry is deliberate: this money already left the household, so
-- archiving the bill afterwards must not retroactively empty the month it was
-- paid in.
-- name: BillMonthDueTotals :many
SELECT a.opening_balance_currency AS currency, SUM(p.amount_minor)::bigint AS minor
FROM bill_payments p
JOIN bills b    ON b.id = p.bill_id
JOIN accounts a ON a.id = b.pay_from_account_id
WHERE p.household_id = $1
  AND p.due_on >= sqlc.arg(month_start)::date
  AND p.due_on <  sqlc.arg(next_month)::date
GROUP BY a.opening_balance_currency;

-- BillMonthUnpaidTotals is the other half of the union: bills still due this
-- month that have not been paid yet. archived_at IS NULL here, unlike
-- BillMonthDueTotals above -- a bill nobody intends to pay again is not an
-- obligation, so an archived bill must not inflate what is still owed.
-- name: BillMonthUnpaidTotals :many
SELECT a.opening_balance_currency AS currency, SUM(b.amount_minor)::bigint AS minor
FROM bills b
JOIN accounts a ON a.id = b.pay_from_account_id
WHERE b.household_id = $1
  AND b.archived_at IS NULL
  AND b.next_due >= sqlc.arg(month_start)::date
  AND b.next_due <  sqlc.arg(next_month)::date
GROUP BY a.opening_balance_currency;

-- ListBillPaymentsForMonth returns one household's payments whose due_on
-- falls in the month, newest paid_on first, ties by bill name -- the "Paid
-- this month" list's own ordering (BillRepository.ListPayments' doc comment).
-- Joined to bills for the display name and autopay flag, and to accounts for
-- the currency, the same reason every bill-returning query above joins it.
-- name: ListBillPaymentsForMonth :many
SELECT p.id, p.bill_id, p.household_id, p.due_on, p.paid_on, p.amount_minor,
       p.transaction_id, b.name AS bill_name, b.autopay, a.opening_balance_currency AS currency
FROM bill_payments p
JOIN bills b    ON b.id = p.bill_id
JOIN accounts a ON a.id = b.pay_from_account_id
WHERE p.household_id = $1
  AND p.due_on >= sqlc.arg(month_start)::date
  AND p.due_on <  sqlc.arg(next_month)::date
ORDER BY p.paid_on DESC, b.name;

-- CreateBillPayment writes the settled-occurrence row -- the second of
-- RecordPayment's three writes (bill_repo.go's own doc comment), always
-- carrying the expense transaction's id it was written alongside in the same
-- transaction. UNIQUE (bill_id, due_on) is the backstop that refuses a
-- double-clicked Mark paid; it has no name of its own
-- (00008_bills.sql), so translate's generic 23505 branch is what turns it
-- into domain.ErrAlreadyExists, not a named-constraint case like
-- billNameUniqueConstraint.
-- name: CreateBillPayment :one
INSERT INTO bill_payments (bill_id, household_id, due_on, paid_on, amount_minor, transaction_id)
VALUES ($1, $2, $3, $4, $5, $6)
RETURNING id, bill_id, household_id, due_on, paid_on, amount_minor, transaction_id, created_at;

-- SetBillNextDue moves next_due and NOTHING else -- due_anchor_day is
-- deliberately absent from this SET list. RecordPayment calls this to
-- advance and UndoPayment calls it to rewind, and neither is the household
-- choosing a day: only CreateBill and an explicit PATCH of next_due set the
-- anchor. Writing it here would let UndoPayment destroy it -- with anchor 31,
-- due 31 Jan -> pay -> 28 Feb -> pay -> 31 Mar -> undo -> 28 Feb, and if undo
-- reset the anchor to 28 the next advance would land on 28 March, the bill
-- having silently lost its 31st forever (BillRepository's own doc comment).
-- name: SetBillNextDue :exec
UPDATE bills
SET next_due = $3, updated_at = now()
WHERE household_id = $1 AND id = $2;

-- GetBillPayment reads one payment scoped by household_id AND bill_id
-- together, never by id alone -- bill_payments carries no database
-- constraint tying its household_id to its bill's, so an id from a
-- mismatched household or bill must match no row rather than leaking across
-- the boundary (the brief's own instruction, restated on UndoPayment below).
-- name: GetBillPayment :one
SELECT id, bill_id, household_id, due_on, paid_on, amount_minor, transaction_id
FROM bill_payments
WHERE household_id = $1 AND bill_id = $2 AND id = $3;

-- MostRecentBillPaymentDueOn is UndoPayment's own guard: only the bill's most
-- recent payment can be undone, because rewinding an older one would pull
-- next_due behind a later occurrence that is still paid, and the screen
-- would show a due date for money already spent (BillRepository.UndoPayment's
-- own doc comment). MAX over an empty set is NULL, but UndoPayment only ever
-- runs this after GetBillPayment has already confirmed at least one row --
-- the payment being undone itself -- exists for this bill.
-- name: MostRecentBillPaymentDueOn :one
SELECT MAX(due_on)::date FROM bill_payments WHERE household_id = $1 AND bill_id = $2;

-- DeleteBillPayment removes one payment row, scoped by household_id AND
-- bill_id together, the same reason GetBillPayment above is. RETURNING id
-- rather than :exec turns "nothing matched" into pgx.ErrNoRows, which
-- translate maps to domain.ErrNotFound the same way DeleteTransaction does.
-- name: DeleteBillPayment :one
DELETE FROM bill_payments
WHERE household_id = $1 AND bill_id = $2 AND id = $3
RETURNING id;
