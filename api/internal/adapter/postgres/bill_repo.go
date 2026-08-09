package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

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

// RecordPayment is Task 5's job: writing the bill_payments row, the expense
// transaction and the advanced next_due in ONE database transaction
// (BillRepository.RecordPayment's own doc comment). This stub exists only so
// the package compiles against usecase.BillRepository -- Task 5 replaces it
// with the real three-way transactional write.
func (r *BillRepo) RecordPayment(ctx context.Context, in usecase.PaymentWrite) (usecase.BillPaymentRecord, error) {
	return usecase.BillPaymentRecord{}, errors.New("not implemented: Task 5")
}

// UndoPayment is Task 5's job, for the same reason RecordPayment is stubbed
// above: deleting the payment, deleting its transaction and rewinding
// next_due all belong to one database transaction that does not exist yet.
func (r *BillRepo) UndoPayment(ctx context.Context, householdID, billID, paymentID string) error {
	return errors.New("not implemented: Task 5")
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

// toBillPaymentRecord converts one ListBillPaymentsForMonth row into a
// usecase.BillPaymentRecord. transaction_id maps through optionalIDToString:
// "" once the ledger row has been deleted, per domain.BillPayment's own doc
// comment, matching bill_payments.transaction_id's ON DELETE SET NULL.
func toBillPaymentRecord(p sqlcgen.ListBillPaymentsForMonthRow) (usecase.BillPaymentRecord, error) {
	amount, err := domain.NewMoney(p.AmountMinor, p.Currency)
	if err != nil {
		return usecase.BillPaymentRecord{}, fmt.Errorf("postgres: bill payment money: %w", err)
	}
	return usecase.BillPaymentRecord{
		Payment: domain.BillPayment{
			ID:            uuidToString(p.ID),
			BillID:        uuidToString(p.BillID),
			HouseholdID:   uuidToString(p.HouseholdID),
			DueOn:         dateToTime(p.DueOn),
			PaidOn:        dateToTime(p.PaidOn),
			Amount:        amount,
			TransactionID: optionalIDToString(p.TransactionID),
		},
		BillName: p.BillName,
		Autopay:  p.Autopay,
	}, nil
}
