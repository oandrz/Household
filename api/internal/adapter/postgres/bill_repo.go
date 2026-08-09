package postgres

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/andreasoentoro/hearth/api/internal/adapter/postgres/sqlcgen"
	"github.com/andreasoentoro/hearth/api/internal/domain"
	"github.com/andreasoentoro/hearth/api/internal/usecase"
)

// BillRepo keeps the pool alongside the pool-backed *sqlcgen.Queries, like
// GoalRepo and BudgetRepo, because Task 5's RecordPayment and UndoPayment
// each need to begin their own transaction -- something a *sqlcgen.Queries
// built once at construction time cannot do on its own.
type BillRepo struct {
	q    *sqlcgen.Queries
	pool *pgxpool.Pool
}

func NewBillRepo(db *DB) *BillRepo {
	return &BillRepo{q: sqlcgen.New(db.Pool()), pool: db.Pool()}
}

func (r *BillRepo) List(ctx context.Context, householdID string, includeArchived bool) ([]usecase.BillRecord, error) {
	rows, err := r.q.ListBills(ctx, sqlcgen.ListBillsParams{
		HouseholdID:     uuid(householdID),
		IncludeArchived: includeArchived,
	})
	if err != nil {
		return nil, translate(err, "list bills")
	}
	out := make([]usecase.BillRecord, 0, len(rows))
	for _, row := range rows {
		rec, err := toBillRecord(sqlcgen.Bill{
			ID: row.ID, HouseholdID: row.HouseholdID, Name: row.Name, AmountMinor: row.AmountMinor,
			Cadence: row.Cadence, NextDue: row.NextDue, DueAnchorDay: row.DueAnchorDay,
			CategoryID: row.CategoryID, PayFromAccountID: row.PayFromAccountID,
			PaidByMembershipID: row.PaidByMembershipID, Autopay: row.Autopay,
			IsSubscription: row.IsSubscription, ArchivedAt: row.ArchivedAt,
		}, row.CategoryName, row.AccountName, row.Currency)
		if err != nil {
			return nil, err
		}
		out = append(out, rec)
	}
	return out, nil
}

func (r *BillRepo) Get(ctx context.Context, householdID, billID string) (usecase.BillRecord, error) {
	row, err := r.q.GetBill(ctx, sqlcgen.GetBillParams{
		HouseholdID: uuid(householdID),
		ID:          uuid(billID),
	})
	if err != nil {
		return usecase.BillRecord{}, translate(err, "get bill")
	}
	return toBillRecord(sqlcgen.Bill{
		ID: row.ID, HouseholdID: row.HouseholdID, Name: row.Name, AmountMinor: row.AmountMinor,
		Cadence: row.Cadence, NextDue: row.NextDue, DueAnchorDay: row.DueAnchorDay,
		CategoryID: row.CategoryID, PayFromAccountID: row.PayFromAccountID,
		PaidByMembershipID: row.PaidByMembershipID, Autopay: row.Autopay,
		IsSubscription: row.IsSubscription, ArchivedAt: row.ArchivedAt,
	}, row.CategoryName, row.AccountName, row.Currency)
}

// Create writes one row. queries/bill.sql's own comment explains why
// CreateBill joins accounts in the same statement (via a CTE) rather than a
// bare RETURNING: bills has no currency column, and BillRecord requires
// Bill.Amount.Currency populated on every return, Create included.
func (r *BillRepo) Create(ctx context.Context, in usecase.NewBillRow) (usecase.BillRecord, error) {
	row, err := r.q.CreateBill(ctx, sqlcgen.CreateBillParams{
		HouseholdID:        uuid(in.HouseholdID),
		Name:               in.Name,
		AmountMinor:        in.AmountMinor,
		Cadence:            string(in.Cadence),
		NextDue:            dateOnly(in.NextDue),
		DueAnchorDay:       int16(in.DueAnchorDay),
		CategoryID:         nullableUUID(optionalID(in.CategoryID)),
		PayFromAccountID:   uuid(in.PayFromAccountID),
		PaidByMembershipID: nullableUUID(optionalID(in.PaidByMembershipID)),
		Autopay:            in.Autopay,
		IsSubscription:     in.IsSubscription,
	})
	if err != nil {
		return usecase.BillRecord{}, translate(err, "create bill")
	}
	return toBillRecord(sqlcgen.Bill{
		ID: row.ID, HouseholdID: row.HouseholdID, Name: row.Name, AmountMinor: row.AmountMinor,
		Cadence: row.Cadence, NextDue: row.NextDue, DueAnchorDay: row.DueAnchorDay,
		CategoryID: row.CategoryID, PayFromAccountID: row.PayFromAccountID,
		PaidByMembershipID: row.PaidByMembershipID, Autopay: row.Autopay,
		IsSubscription: row.IsSubscription, ArchivedAt: row.ArchivedAt,
	}, row.CategoryName, row.AccountName, row.Currency)
}

// Update replaces every mutable column -- name, amount, cadence, next due
// date, due anchor day, category, pay-from account, payer, autopay and
// is_subscription. BillService is what turns a partial PATCH into a complete
// domain.Bill; this port never merges (ports.go's own doc comment). Scoped by
// household_id AND id together (UpdateBill's own SQL comment) -- an id from
// another household matches no row and translate turns the resulting
// pgx.ErrNoRows into domain.ErrNotFound. Same name-collision contract as
// Create.
func (r *BillRepo) Update(ctx context.Context, b domain.Bill) (usecase.BillRecord, error) {
	row, err := r.q.UpdateBill(ctx, sqlcgen.UpdateBillParams{
		HouseholdID:        uuid(b.HouseholdID),
		ID:                 uuid(b.ID),
		Name:               b.Name,
		AmountMinor:        b.Amount.Amount,
		Cadence:            string(b.Cadence),
		NextDue:            nullableDate(b.NextDue),
		DueAnchorDay:       int16(b.DueAnchorDay),
		CategoryID:         nullableUUID(optionalID(b.CategoryID)),
		PayFromAccountID:   uuid(b.PayFromAccountID),
		PaidByMembershipID: nullableUUID(optionalID(b.PaidByMembershipID)),
		Autopay:            b.Autopay,
		IsSubscription:     b.IsSubscription,
	})
	if err != nil {
		return usecase.BillRecord{}, translate(err, "update bill")
	}
	return toBillRecord(sqlcgen.Bill{
		ID: row.ID, HouseholdID: row.HouseholdID, Name: row.Name, AmountMinor: row.AmountMinor,
		Cadence: row.Cadence, NextDue: row.NextDue, DueAnchorDay: row.DueAnchorDay,
		CategoryID: row.CategoryID, PayFromAccountID: row.PayFromAccountID,
		PaidByMembershipID: row.PaidByMembershipID, Autopay: row.Autopay,
		IsSubscription: row.IsSubscription, ArchivedAt: row.ArchivedAt,
	}, row.CategoryName, row.AccountName, row.Currency)
}

// SetArchived stamps archived_at with at, or clears it when archived is
// false. SetBillArchived's own COALESCE is "first stamp wins": archiving an
// already-archived bill keeps its original archived_at instead of moving it
// forward to at -- the same GoalRepository.SetArchived convention.
func (r *BillRepo) SetArchived(ctx context.Context, householdID, billID string, archived bool, at time.Time) (usecase.BillRecord, error) {
	row, err := r.q.SetBillArchived(ctx, sqlcgen.SetBillArchivedParams{
		HouseholdID: uuid(householdID),
		ID:          uuid(billID),
		Archived:    archived,
		At:          timestamptz(at),
	})
	if err != nil {
		return usecase.BillRecord{}, translate(err, "set bill archived")
	}
	return toBillRecord(sqlcgen.Bill{
		ID: row.ID, HouseholdID: row.HouseholdID, Name: row.Name, AmountMinor: row.AmountMinor,
		Cadence: row.Cadence, NextDue: row.NextDue, DueAnchorDay: row.DueAnchorDay,
		CategoryID: row.CategoryID, PayFromAccountID: row.PayFromAccountID,
		PaidByMembershipID: row.PaidByMembershipID, Autopay: row.Autopay,
		IsSubscription: row.IsSubscription, ArchivedAt: row.ArchivedAt,
	}, row.CategoryName, row.AccountName, row.Currency)
}

// RecordPayment writes the bill_payments row, the expense transaction and
// the advanced next_due in ONE database transaction
// (BillRepository.RecordPayment's own doc comment): a bill left advanced
// with no payment, or a payment with no expense, is not a state this method
// can produce. The transaction is begun and committed by hand, not via
// pgx.BeginFunc (contrast GoalRepo.Create) -- Step 6's mutation check
// (removing the deferred rollback and committing after each statement to
// prove TestRecordPaymentIsAtomic actually catches a partial write) is only
// expressible against this shape.
func (r *BillRepo) RecordPayment(ctx context.Context, in usecase.PaymentWrite) (usecase.BillPaymentRecord, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return usecase.BillPaymentRecord{}, translate(err, "begin record payment")
	}
	defer tx.Rollback(ctx)
	q := r.q.WithTx(tx)

	// 1. The expense. Its currency is the account's, resolved by the service
	//    through AccountLookup -- the same rule TransactionService.Create
	//    applies at usecase/transaction.go:232. occurred_on is PaidOn, not
	//    DueOn: the money left the account on the day it was actually paid,
	//    which may be later than the occurrence it settles.
	txn, err := q.CreateTransaction(ctx, sqlcgen.CreateTransactionParams{
		HouseholdID:            uuid(in.HouseholdID),
		Kind:                   "expense",
		OccurredOn:             dateOnly(in.PaidOn),
		Description:            in.Description,
		CategoryID:             nullableUUID(optionalID(in.CategoryID)),
		PaidByMembershipID:     nullableUUID(optionalID(in.PaidByMembershipID)),
		FromAccountID:          uuid(in.PayFromAccountID),
		ToAccountID:            pgtype.UUID{}, // NULL: an expense has no destination account
		AmountMinor:            in.AmountMinor,
		AmountCurrency:         in.Currency,
		ReceivedAmountMinor:    nil, // an expense is never a transfer
		ReceivedAmountCurrency: nil,
	})
	if err != nil {
		return usecase.BillPaymentRecord{}, translate(err, "create bill expense")
	}
	// 2. The payment row. UNIQUE (bill_id, due_on) is what refuses a second
	//    payment of one occurrence; translate() maps it to ErrAlreadyExists.
	pay, err := q.CreateBillPayment(ctx, sqlcgen.CreateBillPaymentParams{
		BillID:        uuid(in.BillID),
		HouseholdID:   uuid(in.HouseholdID),
		DueOn:         dateOnly(in.DueOn),
		PaidOn:        dateOnly(in.PaidOn),
		AmountMinor:   in.AmountMinor,
		TransactionID: txn.ID,
	})
	if err != nil {
		return usecase.BillPaymentRecord{}, translate(err, "create bill payment")
	}
	// 3. The advance. A bill left advanced with no payment is exactly the
	//    partial state this transaction exists to make impossible. NextDue is
	//    nil for a settled one-off (PaymentWrite's own doc comment);
	//    nullableDate carries that through as SQL NULL, which the schema
	//    allows only for a one-off. SetBillNextDue returns the row's id
	//    (RETURNING, not a bare :exec) so a household/bill mismatch that
	//    matches zero rows surfaces as pgx.ErrNoRows -> domain.ErrNotFound
	//    and rolls this transaction back, instead of committing writes 1 and
	//    2 while silently leaving next_due untouched.
	if _, err := q.SetBillNextDue(ctx, sqlcgen.SetBillNextDueParams{
		HouseholdID: uuid(in.HouseholdID),
		ID:          uuid(in.BillID),
		NextDue:     nullableDate(in.NextDue),
	}); err != nil {
		return usecase.BillPaymentRecord{}, translate(err, "advance bill next due")
	}
	if err := tx.Commit(ctx); err != nil {
		return usecase.BillPaymentRecord{}, translate(err, "commit record payment")
	}
	// BillName is in.Description: PaymentWrite's own doc comment says
	// Description IS the bill's name. Autopay is left false on purpose --
	// BillPaymentRecord's own doc comment explains why RecordPayment does not
	// join it back.
	return toRecordedBillPayment(pay, in.Description, in.Currency)
}

// UndoPayment reverses RecordPayment's three writes in ONE database
// transaction: deletes the payment, deletes its transaction when
// transaction_id is still non-NULL, and rewinds next_due to the payment's
// due_on. Every read and write here is scoped by household_id AND bill_id
// together, never by payment id alone -- bill_payments carries no database
// constraint tying its household_id to its bill's, so an id from a
// mismatched household or bill would otherwise leak across the boundary.
func (r *BillRepo) UndoPayment(ctx context.Context, householdID, billID, paymentID string) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return translate(err, "begin undo payment")
	}
	defer tx.Rollback(ctx)
	q := r.q.WithTx(tx)

	pay, err := q.GetBillPayment(ctx, sqlcgen.GetBillPaymentParams{
		HouseholdID: uuid(householdID),
		BillID:      uuid(billID),
		ID:          uuid(paymentID),
	})
	if err != nil {
		return translate(err, "get bill payment")
	}

	mostRecent, err := q.MostRecentBillPaymentDueOn(ctx, sqlcgen.MostRecentBillPaymentDueOnParams{
		HouseholdID: uuid(householdID),
		BillID:      uuid(billID),
	})
	if err != nil {
		return translate(err, "most recent bill payment due on")
	}
	// GetBillPayment above already proved at least one payment -- this one --
	// exists for this bill, so MAX(due_on) cannot come back NULL. A stray
	// invalid value here is a bug in the query, not a real "no payments"
	// case, and is refused rather than let the comparison below pass on a
	// zero time by accident.
	if !mostRecent.Valid {
		return fmt.Errorf("postgres: most recent bill payment due_on is NULL for bill %s with a payment already confirmed to exist", billID)
	}
	// Only the bill's most recent payment can be undone: undoing an older
	// one would rewind next_due behind a later occurrence that is still
	// paid, and the screen would show a due date for money already spent.
	if !dateToTime(pay.DueOn).Equal(dateToTime(mostRecent)) {
		return domain.ErrForbidden
	}

	if _, err := q.DeleteBillPayment(ctx, sqlcgen.DeleteBillPaymentParams{
		HouseholdID: uuid(householdID),
		BillID:      uuid(billID),
		ID:          uuid(paymentID),
	}); err != nil {
		return translate(err, "delete bill payment")
	}
	// transaction_id is nullable: a payment whose expense was already
	// deleted from the Transactions page (bill_payments.transaction_id ON
	// DELETE SET NULL) has nothing left here to delete.
	if pay.TransactionID.Valid {
		if _, err := q.DeleteTransaction(ctx, sqlcgen.DeleteTransactionParams{
			HouseholdID: uuid(householdID),
			ID:          pay.TransactionID,
		}); err != nil {
			return translate(err, "delete bill expense")
		}
	}
	// Rewind to the undone payment's own due_on. SetBillNextDue never writes
	// due_anchor_day -- see its own doc comment in queries/bill.sql for the
	// worked example of what writing it here would destroy. RETURNING id
	// (not a bare :exec) is what stops a household/bill mismatch from
	// committing both deletions above while silently leaving next_due
	// un-rewound -- see SetBillNextDue's own comment.
	if _, err := q.SetBillNextDue(ctx, sqlcgen.SetBillNextDueParams{
		HouseholdID: uuid(householdID),
		ID:          uuid(billID),
		NextDue:     pay.DueOn,
	}); err != nil {
		return translate(err, "rewind bill next due")
	}
	if err := tx.Commit(ctx); err != nil {
		return translate(err, "commit undo payment")
	}
	return nil
}

// ListPayments returns one household's payments due in month, newest paid_on
// first, ties by bill name -- ListBillPaymentsForMonth's own ordering.
func (r *BillRepo) ListPayments(ctx context.Context, householdID string, month time.Time) ([]usecase.BillPaymentRecord, error) {
	start, next := monthBounds(month)
	rows, err := r.q.ListBillPaymentsForMonth(ctx, sqlcgen.ListBillPaymentsForMonthParams{
		HouseholdID: uuid(householdID),
		MonthStart:  dateOnly(start),
		NextMonth:   dateOnly(next),
	})
	if err != nil {
		return nil, translate(err, "list bill payments for month")
	}
	out := make([]usecase.BillPaymentRecord, 0, len(rows))
	for _, row := range rows {
		rec, err := toBillPaymentRecord(row)
		if err != nil {
			return nil, err
		}
		out = append(out, rec)
	}
	return out, nil
}

// MonthTotals returns the two figures the stat cards pair: paidMinor sums
// bill_payments due in the month, and dueMinor is that PLUS every unarchived
// bill still due in it. BillRepository's own package doc comment explains why
// this cannot come from bills alone: a monthly bill paid on 8 July has
// next_due = 8 August, so a query filtering bills.next_due into the month
// misses every bill already paid. The two halves also filter archived bills
// differently, on purpose -- see BillMonthDueTotals and BillMonthUnpaidTotals
// in queries/bill.sql for why.
func (r *BillRepo) MonthTotals(ctx context.Context, householdID string, month time.Time) (dueMinor, paidMinor map[string]int64, err error) {
	start, next := monthBounds(month)
	paidRows, err := r.q.BillMonthDueTotals(ctx, sqlcgen.BillMonthDueTotalsParams{
		HouseholdID: uuid(householdID), MonthStart: dateOnly(start), NextMonth: dateOnly(next),
	})
	if err != nil {
		return nil, nil, translate(err, "bill month paid totals")
	}
	unpaidRows, err := r.q.BillMonthUnpaidTotals(ctx, sqlcgen.BillMonthUnpaidTotalsParams{
		HouseholdID: uuid(householdID), MonthStart: dateOnly(start), NextMonth: dateOnly(next),
	})
	if err != nil {
		return nil, nil, translate(err, "bill month unpaid totals")
	}
	paid := map[string]int64{}
	for _, row := range paidRows {
		paid[row.Currency] = row.Minor
	}
	// due is paid PLUS still-unpaid: the whole month's obligation, which is
	// what makes the two stat cards read as one fraction (spec decision 5).
	due := map[string]int64{}
	for cur, minor := range paid {
		due[cur] = minor
	}
	for _, row := range unpaidRows {
		due[row.Currency] += row.Minor
	}
	return due, paid, nil
}

// monthBounds returns the first day of month's calendar month and the first
// day of the following month, in UTC -- the half-open [start, next) range
// BillMonthDueTotals, BillMonthUnpaidTotals and ListBillPaymentsForMonth all
// filter into. Built on startOfMonth (transaction_repo.go) rather than a
// second normalisation, so a month always means the same thing across every
// repository in this package.
func monthBounds(month time.Time) (start, next time.Time) {
	start = startOfMonth(month)
	next = start.AddDate(0, 1, 0)
	return start, next
}

// toBill converts one bills row's columns (plus the currency joined in from
// its pay-from account) into a domain.Bill. cadence goes through
// domain.ParseCadence rather than a bare cast -- a value this code did not
// construct is refused here, not carried up, the same rule toGoalContribution
// applies to goal_contributions.source.
func toBill(b sqlcgen.Bill, currency string) (domain.Bill, error) {
	cadence, err := domain.ParseCadence(b.Cadence)
	if err != nil {
		return domain.Bill{}, fmt.Errorf("postgres: bill: %w", err)
	}
	amount, err := domain.NewMoney(b.AmountMinor, currency)
	if err != nil {
		return domain.Bill{}, fmt.Errorf("postgres: bill money: %w", err)
	}
	return domain.Bill{
		ID:                 uuidToString(b.ID),
		HouseholdID:        uuidToString(b.HouseholdID),
		Name:               b.Name,
		Amount:             amount,
		Cadence:            cadence,
		NextDue:            dateToTimePtr(b.NextDue),
		DueAnchorDay:       int(b.DueAnchorDay),
		CategoryID:         optionalIDToString(b.CategoryID),
		PayFromAccountID:   uuidToString(b.PayFromAccountID),
		PaidByMembershipID: optionalIDToString(b.PaidByMembershipID),
		Autopay:            b.Autopay,
		IsSubscription:     b.IsSubscription,
		ArchivedAt:         timePtrOf(b.ArchivedAt),
	}, nil
}

// toBillRecord is toBill plus the category and account names ListBills,
// GetBill, CreateBill, UpdateBill and SetBillArchived all join in alongside
// the same bill columns -- every query that returns a bill, per
// queries/bill.sql's own header comment.
func toBillRecord(b sqlcgen.Bill, categoryName, accountName, currency string) (usecase.BillRecord, error) {
	bill, err := toBill(b, currency)
	if err != nil {
		return usecase.BillRecord{}, err
	}
	return usecase.BillRecord{Bill: bill, CategoryName: categoryName, AccountName: accountName}, nil
}

// billPaymentRecord builds one usecase.BillPaymentRecord from bill_payments'
// own columns plus the three facts a caller joins or already holds --
// billName, autopay and currency. transaction_id maps through
// optionalIDToString: "" once the ledger row has been deleted, per
// domain.BillPayment's own doc comment, matching bill_payments.transaction_id's
// ON DELETE SET NULL. Shared by toBillPaymentRecord (ListBillPaymentsForMonth,
// which joins all three) and toRecordedBillPayment (RecordPayment, whose
// caller already holds billName and currency and leaves autopay false) the
// same way GoalRepo's toGoal underlies toGoalRecord.
func billPaymentRecord(id, billID, householdID pgtype.UUID, dueOn, paidOn pgtype.Date,
	amountMinor int64, transactionID pgtype.UUID, billName string, autopay bool, currency string) (usecase.BillPaymentRecord, error) {
	amount, err := domain.NewMoney(amountMinor, currency)
	if err != nil {
		return usecase.BillPaymentRecord{}, fmt.Errorf("postgres: bill payment money: %w", err)
	}
	return usecase.BillPaymentRecord{
		Payment: domain.BillPayment{
			ID:            uuidToString(id),
			BillID:        uuidToString(billID),
			HouseholdID:   uuidToString(householdID),
			DueOn:         dateToTime(dueOn),
			PaidOn:        dateToTime(paidOn),
			Amount:        amount,
			TransactionID: optionalIDToString(transactionID),
		},
		BillName: billName,
		Autopay:  autopay,
	}, nil
}

// toBillPaymentRecord converts one ListBillPaymentsForMonth row into a
// usecase.BillPaymentRecord.
func toBillPaymentRecord(p sqlcgen.ListBillPaymentsForMonthRow) (usecase.BillPaymentRecord, error) {
	return billPaymentRecord(p.ID, p.BillID, p.HouseholdID, p.DueOn, p.PaidOn, p.AmountMinor, p.TransactionID,
		p.BillName, p.Autopay, p.Currency)
}

// toRecordedBillPayment converts CreateBillPayment's row into a
// usecase.BillPaymentRecord. billName and currency come from the caller,
// not a join: RecordPayment's own service caller has just read the whole
// bill and already holds both (BillPaymentRecord's own doc comment). Autopay
// is always false here -- only ListPayments' join populates it.
func toRecordedBillPayment(p sqlcgen.BillPayment, billName, currency string) (usecase.BillPaymentRecord, error) {
	return billPaymentRecord(p.ID, p.BillID, p.HouseholdID, p.DueOn, p.PaidOn, p.AmountMinor, p.TransactionID,
		billName, false, currency)
}
