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
