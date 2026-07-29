package postgres

import (
	"context"
	"time"

	"github.com/andreasoentoro/hearth/api/internal/adapter/postgres/sqlcgen"
	"github.com/andreasoentoro/hearth/api/internal/domain"
	"github.com/andreasoentoro/hearth/api/internal/usecase"
)

type TransactionRepo struct{ q *sqlcgen.Queries }

func NewTransactionRepo(db *DB) *TransactionRepo {
	return &TransactionRepo{q: sqlcgen.New(db.Pool())}
}

func (r *TransactionRepo) Get(ctx context.Context, householdID, transactionID string) (usecase.TransactionView, error) {
	row, err := r.q.GetTransaction(ctx, sqlcgen.GetTransactionParams{
		HouseholdID: uuid(householdID),
		ID:          uuid(transactionID),
	})
	if err != nil {
		return usecase.TransactionView{}, translate(err, "get transaction")
	}
	return toTransactionViewFromGet(row), nil
}

func (r *TransactionRepo) Create(ctx context.Context, t domain.Transaction) (domain.Transaction, error) {
	row, err := r.q.CreateTransaction(ctx, sqlcgen.CreateTransactionParams{
		HouseholdID:            uuid(t.HouseholdID),
		Kind:                   string(t.Kind),
		OccurredOn:             dateOnly(t.OccurredOn),
		Description:            t.Description,
		CategoryID:             nullableUUID(optionalID(t.CategoryID)),
		PaidByMembershipID:     nullableUUID(optionalID(t.PaidByMembershipID)),
		FromAccountID:          nullableUUID(optionalID(t.FromAccountID)),
		ToAccountID:            nullableUUID(optionalID(t.ToAccountID)),
		AmountMinor:            t.Amount.Amount,
		AmountCurrency:         t.Amount.Currency,
		ReceivedAmountMinor:    receivedMinor(t.ReceivedAmount),
		ReceivedAmountCurrency: receivedCurrency(t.ReceivedAmount),
	})
	if err != nil {
		return domain.Transaction{}, translate(err, "create transaction")
	}
	return toTransaction(row), nil
}

func (r *TransactionRepo) Update(ctx context.Context, t domain.Transaction) (domain.Transaction, error) {
	row, err := r.q.UpdateTransaction(ctx, sqlcgen.UpdateTransactionParams{
		HouseholdID:            uuid(t.HouseholdID),
		ID:                     uuid(t.ID),
		Kind:                   string(t.Kind),
		OccurredOn:             dateOnly(t.OccurredOn),
		Description:            t.Description,
		CategoryID:             nullableUUID(optionalID(t.CategoryID)),
		PaidByMembershipID:     nullableUUID(optionalID(t.PaidByMembershipID)),
		FromAccountID:          nullableUUID(optionalID(t.FromAccountID)),
		ToAccountID:            nullableUUID(optionalID(t.ToAccountID)),
		AmountMinor:            t.Amount.Amount,
		AmountCurrency:         t.Amount.Currency,
		ReceivedAmountMinor:    receivedMinor(t.ReceivedAmount),
		ReceivedAmountCurrency: receivedCurrency(t.ReceivedAmount),
	})
	if err != nil {
		return domain.Transaction{}, translate(err, "update transaction")
	}
	return toTransaction(row), nil
}

func (r *TransactionRepo) Delete(ctx context.Context, householdID, transactionID string) error {
	_, err := r.q.DeleteTransaction(ctx, sqlcgen.DeleteTransactionParams{
		HouseholdID: uuid(householdID),
		ID:          uuid(transactionID),
	})
	if err != nil {
		// translate maps pgx.ErrNoRows to domain.ErrNotFound, which is what
		// makes deleting something that is not there indistinguishable from
		// deleting something in another household.
		return translate(err, "delete transaction")
	}
	return nil
}

// receivedMinor and receivedCurrency implement the nil <-> NULL half of the
// received amount. They are two functions rather than one returning both
// because sqlc's generated params take them as separate fields, and a single
// helper returning a pair would be unpacked at both call sites anyway.
func receivedMinor(m *domain.Money) *int64 {
	if m == nil {
		return nil
	}
	amount := m.Amount
	return &amount
}

func receivedCurrency(m *domain.Money) *string {
	if m == nil {
		return nil
	}
	currency := m.Currency
	return &currency
}

func toTransaction(t sqlcgen.Transaction) domain.Transaction {
	out := domain.Transaction{
		ID:                 uuidToString(t.ID),
		HouseholdID:        uuidToString(t.HouseholdID),
		Kind:               domain.TransactionKind(t.Kind),
		OccurredOn:         dateToTime(t.OccurredOn),
		Description:        t.Description,
		CategoryID:         optionalIDToString(t.CategoryID),
		PaidByMembershipID: optionalIDToString(t.PaidByMembershipID),
		FromAccountID:      optionalIDToString(t.FromAccountID),
		ToAccountID:        optionalIDToString(t.ToAccountID),
		Amount:             domain.Money{Amount: t.AmountMinor, Currency: t.AmountCurrency},
	}
	if t.ReceivedAmountMinor != nil && t.ReceivedAmountCurrency != nil {
		out.ReceivedAmount = &domain.Money{
			Amount:   *t.ReceivedAmountMinor,
			Currency: *t.ReceivedAmountCurrency,
		}
	}
	return out
}

// buildTransactionView is the one place a row's joined names and its two
// before-opening flags become a view, so the Get and List converters cannot
// disagree about them.
func buildTransactionView(
	t domain.Transaction,
	categoryName, paidByName, fromName, toName *string,
	beforeFrom, beforeTo *bool,
) usecase.TransactionView {
	view := usecase.TransactionView{
		Transaction:     t,
		CategoryName:    stringOrEmpty(categoryName),
		PaidByName:      stringOrEmpty(paidByName),
		FromAccountName: stringOrEmpty(fromName),
		ToAccountName:   stringOrEmpty(toName),
	}
	// nil when there is no account on that side at all -- an expense has no
	// destination, so "does this predate the destination's opening date" has
	// no answer rather than a false one.
	if t.FromAccountID != "" {
		view.BeforeFromAccountOpening = beforeFrom
	}
	if t.ToAccountID != "" {
		view.BeforeToAccountOpening = beforeTo
	}
	return view
}

func toTransactionViewFromGet(row sqlcgen.GetTransactionRow) usecase.TransactionView {
	return buildTransactionView(
		toTransaction(sqlcgen.Transaction{
			ID: row.ID, HouseholdID: row.HouseholdID, Kind: row.Kind,
			OccurredOn: row.OccurredOn, Description: row.Description,
			CategoryID: row.CategoryID, PaidByMembershipID: row.PaidByMembershipID,
			FromAccountID: row.FromAccountID, ToAccountID: row.ToAccountID,
			AmountMinor: row.AmountMinor, AmountCurrency: row.AmountCurrency,
			ReceivedAmountMinor:    row.ReceivedAmountMinor,
			ReceivedAmountCurrency: row.ReceivedAmountCurrency,
			CreatedAt:              row.CreatedAt,
		}),
		row.CategoryName, row.PaidByName, row.FromAccountName, row.ToAccountName,
		// row.BeforeFromOpening and row.BeforeToOpening are already *bool, not
		// because the LEFT JOIN makes them NULL when there is no account on
		// that side -- "fa.id IS NOT NULL AND ..." evaluates to false, not
		// NULL, in that case, so the raw SQL value is never actually NULL
		// here. sqlc still types the column as nullable because it cannot
		// prove a computed boolean expression is non-nullable, which is why
		// the field is *bool at all. buildTransactionView's own nil-ing (see
		// its comment) is what turns "false" into "no answer" for an absent
		// side -- it is load-bearing, not a belt-and-braces double-check of
		// something the query already guaranteed.
		row.BeforeFromOpening, row.BeforeToOpening,
	)
}

// List asks for one row more than the caller wanted. That extra row is the
// whole "is there another page" answer -- a COUNT(*) over a filtered ledger
// costs a scan to learn something the page itself already implies.
func (r *TransactionRepo) List(ctx context.Context, householdID string, f usecase.TransactionFilter) ([]usecase.TransactionView, error) {
	limit := f.Limit
	if limit <= 0 {
		limit = defaultTransactionLimit
	}
	if limit > maxTransactionLimit {
		limit = maxTransactionLimit
	}

	params := sqlcgen.ListTransactionsParams{
		HouseholdID: uuid(householdID),
		RowLimit:    int32(limit + 1),
	}
	if f.Kind != "" {
		kind := f.Kind
		params.Kind = &kind
	}
	if f.AccountID != "" {
		params.AccountID = nullableUUID(&f.AccountID)
	}
	if f.CategoryID != "" {
		params.CategoryID = nullableUUID(&f.CategoryID)
	}
	if f.PaidByMembershipID != "" {
		params.PaidBy = nullableUUID(&f.PaidByMembershipID)
	}
	if !f.Month.IsZero() {
		params.MonthStart = dateOnly(startOfMonth(f.Month))
	}
	// Both cursor halves or neither: a date with no id cannot order two
	// transactions on the same day, and the row-value comparison would compare
	// against a NULL id and return nothing at all.
	if !f.CursorDate.IsZero() && f.CursorID != "" {
		params.CursorDate = dateOnly(f.CursorDate)
		params.CursorID = nullableUUID(&f.CursorID)
	}

	rows, err := r.q.ListTransactions(ctx, params)
	if err != nil {
		return nil, translate(err, "list transactions")
	}
	out := make([]usecase.TransactionView, 0, len(rows))
	for _, row := range rows {
		out = append(out, toTransactionViewFromList(row))
	}
	return out, nil
}

// MonthTotals returns every transaction in one calendar month; the service
// converts each amount into the household's primary currency and sums it,
// which SQL cannot do -- the FX provider lives one layer up.
func (r *TransactionRepo) MonthTotals(ctx context.Context, householdID string, month time.Time) ([]usecase.TransactionView, error) {
	rows, err := r.q.MonthTotalsQuery(ctx, sqlcgen.MonthTotalsQueryParams{
		HouseholdID: uuid(householdID),
		Column2:     dateOnly(startOfMonth(month)),
	})
	if err != nil {
		return nil, translate(err, "month totals")
	}
	out := make([]usecase.TransactionView, 0, len(rows))
	for _, row := range rows {
		out = append(out, toTransactionViewFromMonthTotals(row))
	}
	return out, nil
}

const (
	// defaultTransactionLimit matches the ledger's own page size. maxTransactionLimit
	// is what stops a caller asking for the whole ledger in one request --
	// nothing in the UI sends a limit at all, so this only ever bounds a
	// hand-written request.
	defaultTransactionLimit = 50
	maxTransactionLimit     = 200
)

// startOfMonth normalises any instant to the first day of its month, in UTC.
// occurred_on is a date column and this product stores no timezone per
// household, so a month is a calendar month and not a range of instants.
func startOfMonth(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), 1, 0, 0, 0, 0, time.UTC)
}

func toTransactionViewFromList(row sqlcgen.ListTransactionsRow) usecase.TransactionView {
	return buildTransactionView(
		toTransaction(sqlcgen.Transaction{
			ID: row.ID, HouseholdID: row.HouseholdID, Kind: row.Kind,
			OccurredOn: row.OccurredOn, Description: row.Description,
			CategoryID: row.CategoryID, PaidByMembershipID: row.PaidByMembershipID,
			FromAccountID: row.FromAccountID, ToAccountID: row.ToAccountID,
			AmountMinor: row.AmountMinor, AmountCurrency: row.AmountCurrency,
			ReceivedAmountMinor:    row.ReceivedAmountMinor,
			ReceivedAmountCurrency: row.ReceivedAmountCurrency,
			CreatedAt:              row.CreatedAt,
		}),
		row.CategoryName, row.PaidByName, row.FromAccountName, row.ToAccountName,
		row.BeforeFromOpening, row.BeforeToOpening,
	)
}

func toTransactionViewFromMonthTotals(row sqlcgen.MonthTotalsQueryRow) usecase.TransactionView {
	return buildTransactionView(
		toTransaction(sqlcgen.Transaction{
			ID: row.ID, HouseholdID: row.HouseholdID, Kind: row.Kind,
			OccurredOn: row.OccurredOn, Description: row.Description,
			CategoryID: row.CategoryID, PaidByMembershipID: row.PaidByMembershipID,
			FromAccountID: row.FromAccountID, ToAccountID: row.ToAccountID,
			AmountMinor: row.AmountMinor, AmountCurrency: row.AmountCurrency,
			ReceivedAmountMinor:    row.ReceivedAmountMinor,
			ReceivedAmountCurrency: row.ReceivedAmountCurrency,
			CreatedAt:              row.CreatedAt,
		}),
		row.CategoryName, row.PaidByName, row.FromAccountName, row.ToAccountName,
		row.BeforeFromOpening, row.BeforeToOpening,
	)
}
