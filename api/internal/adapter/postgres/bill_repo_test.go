package postgres_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/andreasoentoro/hearth/api/internal/adapter/postgres"
	"github.com/andreasoentoro/hearth/api/internal/domain"
	"github.com/andreasoentoro/hearth/api/internal/usecase"
)

// day parses a plain "2026-08-08" date into UTC midnight, the same shape
// every other date fixture in this package uses (july, august above). It
// panics rather than taking a *testing.T, matching those two: the strings
// below are fixed test literals, so a parse failure can only mean a typo in
// this file, not a real runtime condition to report through *testing.T.
func day(s string) time.Time {
	t, err := time.Parse("2006-01-02", s)
	if err != nil {
		panic("day: " + err.Error())
	}
	return t
}

// createBill goes through repo.Create, deriving due_anchor_day from nextDue's
// day-of-month the way BillService will (insertBill's own comment, schema_test.go),
// so a test only states what it actually varies.
func createBill(t *testing.T, ctx context.Context, repo *postgres.BillRepo, householdID, accountID, name string,
	cadence domain.Cadence, nextDue time.Time, amountMinor int64) usecase.BillRecord {
	t.Helper()
	rec, err := repo.Create(ctx, usecase.NewBillRow{
		HouseholdID:      householdID,
		Name:             name,
		AmountMinor:      amountMinor,
		Cadence:          cadence,
		NextDue:          nextDue,
		DueAnchorDay:     nextDue.Day(),
		PayFromAccountID: accountID,
	})
	if err != nil {
		t.Fatalf("createBill %s: %v", name, err)
	}
	return rec
}

// payBillDirectly writes a bill_payments row AND advances the bill's own
// next_due, both by raw SQL. RecordPayment -- the real, single-transaction
// way this happens -- is Task 5's job and is stubbed in bill_repo.go today
// ("not implemented: Task 5"). MonthTotals only reads bill_payments and
// bills.next_due; it does not care which code path wrote them, and
// schema_test.go's own "one occurrence can be paid only once" subtest already
// inserts into bill_payments the same direct way. Advancing next_due here too
// is what a real RecordPayment would also do in the same transaction --
// skipping it would leave the paid bill still reading as "due this month" and
// double count it in BillMonthUnpaidTotals, which is exactly the wrong-number
// failure MonthTotals exists to avoid.
func payBillDirectly(t *testing.T, ctx context.Context, db *postgres.DB, householdID, billID string,
	dueOn, paidOn time.Time, amountMinor int64, nextDue time.Time) {
	t.Helper()
	if _, err := db.Pool().Exec(ctx,
		`INSERT INTO bill_payments (bill_id, household_id, due_on, paid_on, amount_minor)
		 VALUES ($1, $2, $3, $4, $5)`,
		billID, householdID, dueOn, paidOn, amountMinor); err != nil {
		t.Fatalf("insert bill payment: %v", err)
	}
	if _, err := db.Pool().Exec(ctx,
		`UPDATE bills SET next_due = $1 WHERE id = $2`, nextDue, billID); err != nil {
		t.Fatalf("advance bill next_due: %v", err)
	}
}

// TestMonthTotalsCountsABillAlreadyPaidThisMonth is BillRepository.MonthTotals'
// own contract test: the naive "bills whose next_due is in this month" query
// returns a wrong number here, because paying the bill already advanced it
// into next month.
func TestMonthTotalsCountsABillAlreadyPaidThisMonth(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	repo := postgres.NewBillRepo(db)
	h, acct := seedHouseholdAndAccount(t, ctx, db) // SGD

	// Due 8 Aug, S$142.30, monthly.
	bill := createBill(t, ctx, repo, h, acct, "SP utilities", domain.CadenceMonthly, day("2026-08-08"), 14230)
	// A second bill due 24 Aug, unpaid.
	createBill(t, ctx, repo, h, acct, "Income tax", domain.CadenceMonthly, day("2026-08-24"), 23000)

	next := day("2026-09-08")
	payBillDirectly(t, ctx, db, h, bill.Bill.ID, day("2026-08-08"), day("2026-08-08"), 14230, next)

	due, paid, err := repo.MonthTotals(ctx, h, day("2026-08-01"))
	if err != nil {
		t.Fatalf("MonthTotals: %v", err)
	}
	if paid["SGD"] != 14230 {
		t.Fatalf("paid = %d, want 14230", paid["SGD"])
	}
	// 14230 already paid + 23000 still due. A query over bills.next_due alone
	// would return 23000 and look right.
	if due["SGD"] != 37230 {
		t.Fatalf("due = %d, want 37230 (the paid bill still counts)", due["SGD"])
	}
}

// TestMonthTotalsPaidHalfIncludesAnArchivedBill pins the asymmetry
// queries/bill.sql documents on BillMonthDueTotals: the paid half has no
// archived_at filter, unlike the unpaid half, because the money already left
// the household -- archiving the bill afterwards must not retroactively empty
// the month it was paid in. The unpaid half of the same archived bill must
// NOT reappear, or a re-archived bill would look like it is still owed.
func TestMonthTotalsPaidHalfIncludesAnArchivedBill(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	repo := postgres.NewBillRepo(db)
	h, acct := seedHouseholdAndAccount(t, ctx, db)

	bill := createBill(t, ctx, repo, h, acct, "One-off refund fee", domain.CadenceOneOff, day("2026-08-05"), 5000)
	// A one-off settles with no next due date at all -- insert the payment and
	// clear next_due directly (payBillDirectly's "advance to a new next_due"
	// shape does not fit a settled one-off, which has none).
	if _, err := db.Pool().Exec(ctx,
		`INSERT INTO bill_payments (bill_id, household_id, due_on, paid_on, amount_minor)
		 VALUES ($1, $2, $3, $4, $5)`,
		bill.Bill.ID, h, day("2026-08-05"), day("2026-08-05"), 5000); err != nil {
		t.Fatalf("insert bill payment: %v", err)
	}
	if _, err := db.Pool().Exec(ctx, `UPDATE bills SET next_due = NULL WHERE id = $1`, bill.Bill.ID); err != nil {
		t.Fatalf("settle the one-off: %v", err)
	}

	if _, err := repo.SetArchived(ctx, h, bill.Bill.ID, true, day("2026-08-06")); err != nil {
		t.Fatalf("SetArchived: %v", err)
	}

	due, paid, err := repo.MonthTotals(ctx, h, day("2026-08-01"))
	if err != nil {
		t.Fatalf("MonthTotals: %v", err)
	}
	if paid["SGD"] != 5000 {
		t.Fatalf("paid = %d, want 5000 -- archiving afterwards must not empty the month it was paid in", paid["SGD"])
	}
	if due["SGD"] != 5000 {
		t.Fatalf("due = %d, want 5000 -- the archived bill's own settled occurrence, not doubled and not zeroed", due["SGD"])
	}
}

// All three writes or none. Guarding-partial-writes exists because four
// defects in this project returned success for work that had only partly
// happened.
func TestRecordPaymentIsAtomic(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	repo := postgres.NewBillRepo(db)
	h, acct := seedHouseholdAndAccount(t, ctx, db)
	bill := createBill(t, ctx, repo, h, acct, "SP utilities", domain.CadenceMonthly, day("2026-08-08"), 14230)

	// A category id that does not exist fails the transactions FK, which is
	// the second of the three writes.
	next := day("2026-09-08")
	_, err := repo.RecordPayment(ctx, usecase.PaymentWrite{
		HouseholdID: h, BillID: bill.Bill.ID, DueOn: day("2026-08-08"), PaidOn: day("2026-08-08"),
		AmountMinor: 14230, Currency: "SGD", Description: "SP utilities",
		CategoryID:       "00000000-0000-0000-0000-000000000001",
		PayFromAccountID: acct, NextDue: &next,
	})
	if err == nil {
		t.Fatal("expected the bad category to fail the write")
	}

	after, err := repo.Get(ctx, h, bill.Bill.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !after.Bill.NextDue.Equal(day("2026-08-08")) {
		t.Fatalf("next_due = %s, want it unmoved at 2026-08-08", after.Bill.NextDue.Format("2006-01-02"))
	}
	payments, err := repo.ListPayments(ctx, h, day("2026-08-01"))
	if err != nil {
		t.Fatalf("ListPayments: %v", err)
	}
	if len(payments) != 0 {
		t.Fatalf("got %d payments, want none -- a failed write left one behind", len(payments))
	}
}

// TestRecordPaymentLeavesNoOrphanExpenseWhenTheSecondWriteFails is
// TestRecordPaymentIsAtomic's sibling for the one failure mode that test
// cannot reach. TestRecordPaymentIsAtomic's bad category fails on
// CreateTransaction -- the FIRST of the three writes -- so nothing before it
// has ever been written, and it stays green whether or not the transaction
// actually protects anything (it even passes vacuously against the
// not-implemented stub). This test instead fails on CreateBillPayment, the
// SECOND write, via UNIQUE (bill_id, due_on): the first write (the expense)
// has already gone through by the time the second one is rejected, so this
// is the only one of the two tests that can catch an orphan expense left
// behind by a payment that never landed.
func TestRecordPaymentLeavesNoOrphanExpenseWhenTheSecondWriteFails(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	repo := postgres.NewBillRepo(db)
	h, acct := seedHouseholdAndAccount(t, ctx, db)
	bill := createBill(t, ctx, repo, h, acct, "SP utilities", domain.CadenceMonthly, day("2026-08-08"), 14230)

	next := day("2026-09-08")
	write := usecase.PaymentWrite{
		HouseholdID: h, BillID: bill.Bill.ID, DueOn: day("2026-08-08"), PaidOn: day("2026-08-08"),
		AmountMinor: 14230, Currency: "SGD", Description: "SP utilities",
		PayFromAccountID: acct, NextDue: &next,
	}
	if _, err := repo.RecordPayment(ctx, write); err != nil {
		t.Fatalf("first payment: %v", err)
	}

	// Same DueOn again: the expense (write 1) succeeds -- it has no
	// UNIQUE constraint of its own -- but the payment (write 2) collides
	// with the one already recorded for 8 Aug.
	if _, err := repo.RecordPayment(ctx, write); !errors.Is(err, domain.ErrAlreadyExists) {
		t.Fatalf("second RecordPayment = %v, want ErrAlreadyExists", err)
	}

	var txns int
	if err := db.Pool().QueryRow(ctx,
		`SELECT count(*) FROM transactions WHERE household_id = $1`, h).Scan(&txns); err != nil {
		t.Fatalf("count transactions: %v", err)
	}
	if txns != 1 {
		t.Fatalf("got %d transactions, want 1 -- the second call's expense must not survive its own rejected payment", txns)
	}
}

// TestRecordPaymentRefusesAHouseholdBillMismatchInsteadOfLeavingNextDueBehind
// pins a review finding: neither bill_payments nor transactions carries a
// database constraint tying its household_id to the bill's own (this file's
// header comment on UndoPayment repeats why), so writes 1 and 2 succeed
// regardless of whether the household actually owns the bill -- only
// SetBillNextDue's own WHERE clause on bills can catch the mismatch, and
// only if it is written to fail loud on a zero-row match. Before the fix,
// SetBillNextDue was a bare :exec; a zero-row UPDATE returns success with no
// error in Postgres, so RecordPayment would have committed the expense and
// the payment row while silently leaving next_due untouched -- exactly the
// partial state this transaction exists to make impossible, arriving
// through a silent no-op instead of a caught error.
func TestRecordPaymentRefusesAHouseholdBillMismatchInsteadOfLeavingNextDueBehind(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	repo := postgres.NewBillRepo(db)
	h, acct := seedHouseholdAndAccount(t, ctx, db)
	other, _ := seedHouseholdAndAccount(t, ctx, db)
	bill := createBill(t, ctx, repo, h, acct, "SP utilities", domain.CadenceMonthly, day("2026-08-08"), 14230)

	// other is a real household, just not the one this bill belongs to.
	next := day("2026-09-08")
	_, err := repo.RecordPayment(ctx, usecase.PaymentWrite{
		HouseholdID: other, BillID: bill.Bill.ID, DueOn: day("2026-08-08"), PaidOn: day("2026-08-08"),
		AmountMinor: 14230, Currency: "SGD", Description: "SP utilities",
		PayFromAccountID: acct, NextDue: &next,
	})
	if !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("RecordPayment(mismatched household) = %v, want ErrNotFound", err)
	}

	// The whole transaction must have rolled back: no orphan payment row, no
	// orphan expense, and the real bill's next_due untouched.
	payments, err := repo.ListPayments(ctx, h, day("2026-08-01"))
	if err != nil {
		t.Fatalf("ListPayments: %v", err)
	}
	if len(payments) != 0 {
		t.Fatalf("got %d payments, want 0 -- the payment row must not survive the mismatch", len(payments))
	}
	var txns int
	if err := db.Pool().QueryRow(ctx,
		`SELECT count(*) FROM transactions WHERE household_id = $1 OR household_id = $2`, h, other).Scan(&txns); err != nil {
		t.Fatalf("count transactions: %v", err)
	}
	if txns != 0 {
		t.Fatalf("got %d transactions, want 0 -- the expense must not survive the mismatch", txns)
	}
	after, err := repo.Get(ctx, h, bill.Bill.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !after.Bill.NextDue.Equal(day("2026-08-08")) {
		t.Fatalf("next_due = %s, want it unmoved at 2026-08-08 -- SetBillNextDue's zero-row match must not silently no-op", after.Bill.NextDue.Format("2006-01-02"))
	}
}

func TestUndoRefusesAnythingButTheMostRecentPayment(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	repo := postgres.NewBillRepo(db)
	h, acct := seedHouseholdAndAccount(t, ctx, db)
	bill := createBill(t, ctx, repo, h, acct, "Netflix", domain.CadenceMonthly, day("2026-07-05"), 1998)

	aug := day("2026-08-05")
	july, err := repo.RecordPayment(ctx, usecase.PaymentWrite{
		HouseholdID: h, BillID: bill.Bill.ID, DueOn: day("2026-07-05"), PaidOn: day("2026-07-05"),
		AmountMinor: 1998, Currency: "SGD", Description: "Netflix",
		PayFromAccountID: acct, NextDue: &aug,
	})
	if err != nil {
		t.Fatalf("july payment: %v", err)
	}
	sep := day("2026-09-05")
	if _, err := repo.RecordPayment(ctx, usecase.PaymentWrite{
		HouseholdID: h, BillID: bill.Bill.ID, DueOn: day("2026-08-05"), PaidOn: day("2026-08-05"),
		AmountMinor: 1998, Currency: "SGD", Description: "Netflix",
		PayFromAccountID: acct, NextDue: &sep,
	}); err != nil {
		t.Fatalf("august payment: %v", err)
	}

	// Undoing July would rewind next_due to 5 July -- behind August, which is
	// still paid -- and the screen would show a due date for money spent.
	err = repo.UndoPayment(ctx, h, bill.Bill.ID, july.Payment.ID)
	if !errors.Is(err, domain.ErrForbidden) {
		t.Fatalf("UndoPayment(older) = %v, want ErrForbidden", err)
	}
	// The HTTP layer's own "409, naming which payment is undoable" (Task
	// 10) reaches for this richer type via errors.As -- pinning that it
	// carries the August due date, not just the bare sentinel, is what
	// proves that path has real data to name, not just a status code.
	var notLatest *domain.BillPaymentNotLatestError
	if !errors.As(err, &notLatest) {
		t.Fatalf("UndoPayment(older) = %v, want a *domain.BillPaymentNotLatestError", err)
	}
	if !notLatest.MostRecentDueOn.Equal(day("2026-08-05")) {
		t.Fatalf("MostRecentDueOn = %s, want 2026-08-05 (August's, the payment that IS undoable)",
			notLatest.MostRecentDueOn.Format("2006-01-02"))
	}
}

func TestUndoReversesAllThreeWrites(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	repo := postgres.NewBillRepo(db)
	h, acct := seedHouseholdAndAccount(t, ctx, db)
	bill := createBill(t, ctx, repo, h, acct, "SP utilities", domain.CadenceMonthly, day("2026-08-08"), 14230)

	next := day("2026-09-08")
	pay, err := repo.RecordPayment(ctx, usecase.PaymentWrite{
		HouseholdID: h, BillID: bill.Bill.ID, DueOn: day("2026-08-08"), PaidOn: day("2026-08-08"),
		AmountMinor: 14230, Currency: "SGD", Description: "SP utilities",
		PayFromAccountID: acct, NextDue: &next,
	})
	if err != nil {
		t.Fatalf("RecordPayment: %v", err)
	}
	if err := repo.UndoPayment(ctx, h, bill.Bill.ID, pay.Payment.ID); err != nil {
		t.Fatalf("UndoPayment: %v", err)
	}

	// 1. The payment row is gone.
	payments, err := repo.ListPayments(ctx, h, day("2026-08-01"))
	if err != nil {
		t.Fatalf("ListPayments: %v", err)
	}
	if len(payments) != 0 {
		t.Fatalf("got %d payments after undo, want 0", len(payments))
	}
	// 2. The expense is gone from the ledger. Counted in SQL rather than
	//    through a repository, so this asserts the row's absence and not some
	//    other layer's filtering of it.
	var txns int
	if err := db.Pool().QueryRow(ctx,
		`SELECT count(*) FROM transactions WHERE household_id = $1`, h).Scan(&txns); err != nil {
		t.Fatalf("count transactions: %v", err)
	}
	if txns != 0 {
		t.Fatalf("got %d transactions after undo, want 0 -- the expense survived", txns)
	}
	// 3. The due date is back where it was.
	after, err := repo.Get(ctx, h, bill.Bill.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !after.Bill.NextDue.Equal(day("2026-08-08")) {
		t.Fatalf("next_due = %s, want it rewound to 2026-08-08", after.Bill.NextDue.Format("2006-01-02"))
	}
}

// TestUndoDoesNotDestroyTheDueAnchorDay is the sibling of Task 2's clamp
// test: that one proves the arithmetic, this proves the anchor is not
// quietly overwritten by the one write that has no business touching it.
// SetBillNextDue runs on both the advance (RecordPayment) and the rewind
// (UndoPayment) paths and must never write due_anchor_day on either --
// bill_repo.go's own comment on RecordPayment/UndoPayment works the 31 Jan
// -> 28 Feb -> 31 Mar -> undo -> 28 Feb example this test pins.
func TestUndoDoesNotDestroyTheDueAnchorDay(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	repo := postgres.NewBillRepo(db)
	h, acct := seedHouseholdAndAccount(t, ctx, db)
	// Due on the 31st: the only anchor that can be lost.
	bill := createBill(t, ctx, repo, h, acct, "Rent", domain.CadenceMonthly, day("2026-01-31"), 250000)

	feb := day("2026-02-28")
	if _, err := repo.RecordPayment(ctx, usecase.PaymentWrite{
		HouseholdID: h, BillID: bill.Bill.ID, DueOn: day("2026-01-31"), PaidOn: day("2026-01-31"),
		AmountMinor: 250000, Currency: "SGD", Description: "Rent",
		PayFromAccountID: acct, NextDue: &feb,
	}); err != nil {
		t.Fatalf("january payment: %v", err)
	}
	mar := day("2026-03-31")
	febPay, err := repo.RecordPayment(ctx, usecase.PaymentWrite{
		HouseholdID: h, BillID: bill.Bill.ID, DueOn: day("2026-02-28"), PaidOn: day("2026-02-28"),
		AmountMinor: 250000, Currency: "SGD", Description: "Rent",
		PayFromAccountID: acct, NextDue: &mar,
	})
	if err != nil {
		t.Fatalf("february payment: %v", err)
	}

	if err := repo.UndoPayment(ctx, h, bill.Bill.ID, febPay.Payment.ID); err != nil {
		t.Fatalf("UndoPayment: %v", err)
	}

	after, err := repo.Get(ctx, h, bill.Bill.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !after.Bill.NextDue.Equal(day("2026-02-28")) {
		t.Fatalf("next_due = %s, want it rewound to 2026-02-28", after.Bill.NextDue.Format("2006-01-02"))
	}
	// The whole point: undo rewound the date but must not have taken the
	// anchor with it. An anchor of 28 here means the bill has silently lost
	// its 31st, and the next advance would land on 28 March.
	if after.Bill.DueAnchorDay != 31 {
		t.Fatalf("due_anchor_day = %d, want 31 -- undo overwrote the anchor", after.Bill.DueAnchorDay)
	}
}

// TestUndoPaymentIsAtomic is TestRecordPaymentIsAtomic's missing other
// direction. The design requires atomicity BOTH ways, and until this test
// existed, deleting `defer tx.Rollback(ctx)` from UndoPayment left the whole
// suite green: every other undo test drives the happy path, which commits.
//
// The lever is the same one TestRecordPaymentRefusesAHouseholdBillMismatch...
// uses, applied one write later. bill_payments carries no constraint tying
// its household_id to its bill's, so moving the bill to another household
// leaves the payment and its expense perfectly findable under the ORIGINAL
// household while the final rewind -- the only statement that touches
// `bills` -- matches zero rows and fails. That puts the failure AFTER both
// deletions, which is the only place a missing rollback could do damage.
//
// Two things are asserted, because a missing rollback and a mistaken commit
// break differently:
//
//  1. The payment row and its expense are still there. This is what fails if
//     the deferred call is ever changed to Commit -- the two deletions would
//     land despite the error.
//  2. The pool has no connection still checked out. This is what fails if the
//     deferred call is simply DELETED: the writes stay invisible either way
//     (nothing commits them), but the transaction is never ended, so its
//     connection never returns to the pool and its row locks are never
//     released -- every later write to those rows blocks forever, and
//     db.Close() at teardown blocks with them.
func TestUndoPaymentIsAtomic(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	repo := postgres.NewBillRepo(db)
	h, acct := seedHouseholdAndAccount(t, ctx, db)
	other, _ := seedHouseholdAndAccount(t, ctx, db)
	bill := createBill(t, ctx, repo, h, acct, "SP utilities", domain.CadenceMonthly, day("2026-08-08"), 14230)

	next := day("2026-09-08")
	pay, err := repo.RecordPayment(ctx, usecase.PaymentWrite{
		HouseholdID: h, BillID: bill.Bill.ID, DueOn: day("2026-08-08"), PaidOn: day("2026-08-08"),
		AmountMinor: 14230, Currency: "SGD", Description: "SP utilities",
		PayFromAccountID: acct, NextDue: &next,
	})
	if err != nil {
		t.Fatalf("RecordPayment: %v", err)
	}

	// The bill moves households; the payment and the expense do not. Now the
	// two deletions still match, and only the rewind cannot.
	if _, err := db.Pool().Exec(ctx,
		`UPDATE bills SET household_id = $1 WHERE id = $2`, other, bill.Bill.ID); err != nil {
		t.Fatalf("move the bill to another household: %v", err)
	}

	if err := repo.UndoPayment(ctx, h, bill.Bill.ID, pay.Payment.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("UndoPayment = %v, want ErrNotFound from the rewind matching no bill", err)
	}

	var payments int
	if err := db.Pool().QueryRow(ctx,
		`SELECT count(*) FROM bill_payments WHERE household_id = $1`, h).Scan(&payments); err != nil {
		t.Fatalf("count bill payments: %v", err)
	}
	if payments != 1 {
		t.Fatalf("got %d payments, want 1 -- the deletion must not survive a failed rewind", payments)
	}
	var txns int
	if err := db.Pool().QueryRow(ctx,
		`SELECT count(*) FROM transactions WHERE household_id = $1`, h).Scan(&txns); err != nil {
		t.Fatalf("count transactions: %v", err)
	}
	if txns != 1 {
		t.Fatalf("got %d transactions, want 1 -- the expense must not be deleted by an undo that failed", txns)
	}
	// The bill itself is untouched: its due date never rewound, so the household
	// is left with exactly the state it started in, not half of an undo.
	var nextDue time.Time
	if err := db.Pool().QueryRow(ctx, `SELECT next_due FROM bills WHERE id = $1`, bill.Bill.ID).Scan(&nextDue); err != nil {
		t.Fatalf("read next_due: %v", err)
	}
	if !nextDue.Equal(day("2026-09-08")) {
		t.Fatalf("next_due = %s, want it still advanced at 2026-09-08", nextDue.Format("2006-01-02"))
	}

	// See this test's own comment: a connection still checked out here means
	// the failed call left its transaction open rather than rolling it back.
	// The queries above each acquired and released their own connection
	// synchronously, so anything still held is UndoPayment's.
	if held := db.Pool().Stat().AcquiredConns(); held != 0 {
		t.Fatalf("%d connection(s) still checked out after a failed undo, want 0 -- the transaction was never rolled back", held)
	}
}

// TestUndoMostRecentIsScopedToTheBillNotTheHousehold pins which set the
// "only the most recent payment" guard compares against.
// MostRecentBillPaymentDueOn filters on bill_id as well as household_id;
// with that filter dropped, undoing a legitimately-latest
// payment on one bill is refused because a DIFFERENT bill happens to carry a
// later one. Every other undo test uses a household with a single paying
// bill, which is exactly why none of them can see it.
func TestUndoMostRecentIsScopedToTheBillNotTheHousehold(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	repo := postgres.NewBillRepo(db)
	h, acct := seedHouseholdAndAccount(t, ctx, db)

	netflix := createBill(t, ctx, repo, h, acct, "Netflix", domain.CadenceMonthly, day("2026-08-05"), 1998)
	rent := createBill(t, ctx, repo, h, acct, "Rent", domain.CadenceMonthly, day("2026-09-01"), 250000)

	sep := day("2026-09-05")
	netflixPay, err := repo.RecordPayment(ctx, usecase.PaymentWrite{
		HouseholdID: h, BillID: netflix.Bill.ID, DueOn: day("2026-08-05"), PaidOn: day("2026-08-05"),
		AmountMinor: 1998, Currency: "SGD", Description: "Netflix",
		PayFromAccountID: acct, NextDue: &sep,
	})
	if err != nil {
		t.Fatalf("netflix payment: %v", err)
	}
	// Due AFTER Netflix's, and on a different bill: the household's own
	// MAX(due_on) is now September, while Netflix's is still August.
	oct := day("2026-10-01")
	if _, err := repo.RecordPayment(ctx, usecase.PaymentWrite{
		HouseholdID: h, BillID: rent.Bill.ID, DueOn: day("2026-09-01"), PaidOn: day("2026-09-01"),
		AmountMinor: 250000, Currency: "SGD", Description: "Rent",
		PayFromAccountID: acct, NextDue: &oct,
	}); err != nil {
		t.Fatalf("rent payment: %v", err)
	}

	// Netflix's August payment IS its own most recent, so this undo is
	// legitimate. Unscoped, the guard compares it against Rent's September
	// and refuses.
	if err := repo.UndoPayment(ctx, h, netflix.Bill.ID, netflixPay.Payment.ID); err != nil {
		t.Fatalf("UndoPayment = %v, want success -- the guard must compare against THIS bill's payments", err)
	}

	after, err := repo.Get(ctx, h, netflix.Bill.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !after.Bill.NextDue.Equal(day("2026-08-05")) {
		t.Fatalf("next_due = %s, want it rewound to 2026-08-05", after.Bill.NextDue.Format("2006-01-02"))
	}
	// Rent is untouched: undoing one bill's payment must not disturb another's.
	rentAfter, err := repo.Get(ctx, h, rent.Bill.ID)
	if err != nil {
		t.Fatalf("Get rent: %v", err)
	}
	if !rentAfter.Bill.NextDue.Equal(day("2026-10-01")) {
		t.Fatalf("rent next_due = %s, want it still at 2026-10-01", rentAfter.Bill.NextDue.Format("2006-01-02"))
	}
}

func TestGetHidesABillFromAnotherHousehold(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	repo := postgres.NewBillRepo(db)
	mine, myAcct := seedHouseholdAndAccount(t, ctx, db)
	theirs, theirAcct := seedHouseholdAndAccount(t, ctx, db)
	other := createBill(t, ctx, repo, theirs, theirAcct, "Theirs", domain.CadenceMonthly, day("2026-08-08"), 1000)

	_, err := repo.Get(ctx, mine, other.Bill.ID)
	if !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("Get across households = %v, want ErrNotFound (not forbidden, not a row)", err)
	}
	_ = myAcct
}

// TestListIncludesArchivedBillsAsAUnionWhenRequested pins
// BillRepository.List's own doc comment: includeArchived is a UNION, not a
// filter swap. false must return only the live bill; true must return BOTH
// the live one AND the archived one together, not the archived one instead.
func TestListIncludesArchivedBillsAsAUnionWhenRequested(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	repo := postgres.NewBillRepo(db)
	h, acct := seedHouseholdAndAccount(t, ctx, db)

	live := createBill(t, ctx, repo, h, acct, "Live bill", domain.CadenceMonthly, day("2026-08-08"), 5000)
	archived := createBill(t, ctx, repo, h, acct, "Archived bill", domain.CadenceMonthly, day("2026-08-10"), 6000)
	if _, err := repo.SetArchived(ctx, h, archived.Bill.ID, true, day("2026-08-11")); err != nil {
		t.Fatalf("SetArchived: %v", err)
	}

	liveOnly, err := repo.List(ctx, h, false)
	if err != nil {
		t.Fatalf("List(false): %v", err)
	}
	if len(liveOnly) != 1 || liveOnly[0].Bill.ID != live.Bill.ID {
		t.Fatalf("List(false) = %+v, want only the live bill %s", liveOnly, live.Bill.ID)
	}

	all, err := repo.List(ctx, h, true)
	if err != nil {
		t.Fatalf("List(true): %v", err)
	}
	if len(all) != 2 {
		t.Fatalf("List(true) returned %d bills, want 2 -- live AND archived together, a union not a swap", len(all))
	}
}

// TestCreateDuplicateNameIsErrBillNameTakenEvenArchived pins
// BillRepository.Create's own doc comment: a name colliding with UNIQUE
// (household_id, name) is domain.ErrBillNameTaken, and an archived bill still
// occupies its name -- the same categories/goals gotcha.
func TestCreateDuplicateNameIsErrBillNameTakenEvenArchived(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	repo := postgres.NewBillRepo(db)
	h, acct := seedHouseholdAndAccount(t, ctx, db)

	first := createBill(t, ctx, repo, h, acct, "Netflix", domain.CadenceMonthly, day("2026-08-08"), 1799)

	dup := usecase.NewBillRow{
		HouseholdID: h, Name: "Netflix", AmountMinor: 1799, Cadence: domain.CadenceMonthly,
		NextDue: day("2026-08-08"), DueAnchorDay: 8, PayFromAccountID: acct,
	}
	if _, err := repo.Create(ctx, dup); !errors.Is(err, domain.ErrBillNameTaken) {
		t.Fatalf("err = %v, want domain.ErrBillNameTaken", err)
	}

	if _, err := repo.SetArchived(ctx, h, first.Bill.ID, true, day("2026-08-09")); err != nil {
		t.Fatalf("SetArchived: %v", err)
	}

	// An archived bill still occupies its unique key.
	if _, err := repo.Create(ctx, dup); !errors.Is(err, domain.ErrBillNameTaken) {
		t.Fatalf("err after archiving the first = %v, want still domain.ErrBillNameTaken", err)
	}
}

// TestUpdateWritesEveryMutableColumn pins UpdateBill's own contract: an
// unconditional full-row SET, every mutable column including due_anchor_day
// (which must move together with a changed NextDue, per the brief's own
// prose). Every field below is changed to a value that disagrees with what
// Create wrote, so a SET list silently missing one column -- is_subscription
// or paid_by_membership_id, say -- cannot pass by accident agreeing with the
// original.
func TestUpdateWritesEveryMutableColumn(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	repo := postgres.NewBillRepo(db)
	h, acct := seedHouseholdAndAccount(t, ctx, db)
	otherAcct := insertTestAccount(t, db, h, "Second account", "SGD")
	category := insertTestCategory(t, db, h, "Subscriptions")
	payer := insertTestMembership(t, db, h, "Andreas")

	created := createBill(t, ctx, repo, h, acct, "Gym", domain.CadenceMonthly, day("2026-08-01"), 8000)

	b := created.Bill
	b.Name = "Gym membership"
	b.Amount.Amount = 9500
	b.Cadence = domain.CadenceYearly
	next := day("2026-09-15")
	b.NextDue = &next
	b.DueAnchorDay = 15
	b.CategoryID = category
	b.PayFromAccountID = otherAcct
	b.PaidByMembershipID = payer
	b.Autopay = true
	b.IsSubscription = true

	updated, err := repo.Update(ctx, b)
	if err != nil {
		t.Fatalf("Update: %v", err)
	}

	got := updated.Bill
	if got.Name != "Gym membership" {
		t.Fatalf("Name = %q, want %q", got.Name, "Gym membership")
	}
	if got.Amount.Amount != 9500 {
		t.Fatalf("Amount = %d, want 9500", got.Amount.Amount)
	}
	if got.Cadence != domain.CadenceYearly {
		t.Fatalf("Cadence = %q, want yearly", got.Cadence)
	}
	if got.NextDue == nil || !got.NextDue.Equal(next) {
		t.Fatalf("NextDue = %v, want %v", got.NextDue, next)
	}
	if got.DueAnchorDay != 15 {
		t.Fatalf("DueAnchorDay = %d, want 15 -- it must move together with NextDue", got.DueAnchorDay)
	}
	if got.CategoryID != category {
		t.Fatalf("CategoryID = %q, want %q", got.CategoryID, category)
	}
	if got.PayFromAccountID != otherAcct {
		t.Fatalf("PayFromAccountID = %q, want %q", got.PayFromAccountID, otherAcct)
	}
	if got.PaidByMembershipID != payer {
		t.Fatalf("PaidByMembershipID = %q, want %q", got.PaidByMembershipID, payer)
	}
	if !got.Autopay {
		t.Fatal("Autopay = false, want true")
	}
	if !got.IsSubscription {
		t.Fatal("IsSubscription = false, want true")
	}
	if updated.AccountName != "Second account" {
		t.Fatalf("AccountName = %q, want %q -- the join must follow the new pay_from_account_id", updated.AccountName, "Second account")
	}
	if updated.CategoryName != "Subscriptions" {
		t.Fatalf("CategoryName = %q, want %q", updated.CategoryName, "Subscriptions")
	}

	// Get must agree with what Update returned -- proves the write actually
	// landed, not just that UpdateBill's own RETURNING echoed the input back
	// unwritten.
	fetched, err := repo.Get(ctx, h, b.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if fetched.Bill.Name != "Gym membership" || fetched.AccountName != "Second account" {
		t.Fatalf("Get after Update = %+v, want it to reflect the new name and account too", fetched)
	}
}
