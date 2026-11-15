package postgres_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/andreasoentoro/hearth/api/internal/adapter/postgres"
	"github.com/andreasoentoro/hearth/api/internal/domain"
	"github.com/andreasoentoro/hearth/api/internal/usecase"
)

// insertAccountOfType adds an SGD account of the given type.
func insertAccountOfType(t *testing.T, ctx context.Context, db *postgres.DB, householdID, nickname, accountType string) string {
	t.Helper()
	var id string
	if err := db.Pool().QueryRow(ctx,
		`INSERT INTO accounts (household_id, nickname, type, opening_balance_minor,
		                       opening_balance_currency, opening_balance_as_of)
		 VALUES ($1, $2, $3, 0, 'SGD', DATE '2026-07-01') RETURNING id`,
		householdID, nickname, accountType).Scan(&id); err != nil {
		t.Fatalf("insert account %s: %v", nickname, err)
	}
	return id
}

func payYearlyBill(t *testing.T, ctx context.Context, bills *postgres.BillRepo, h, acct, billID string, dueOn time.Time, amount int64) usecase.BillPaymentRecord {
	t.Helper()
	next := dueOn.AddDate(1, 0, 0)
	rec, err := bills.RecordPayment(ctx, usecase.PaymentWrite{
		HouseholdID: h, BillID: billID, DueOn: dueOn, PaidOn: dueOn,
		AmountMinor: amount, Currency: "SGD", Description: "Insurance",
		PayFromAccountID: acct, NextDue: &next,
	})
	if err != nil {
		t.Fatalf("RecordPayment: %v", err)
	}
	return rec
}

func rowByID(rows []usecase.CashflowRow, id string) (usecase.CashflowRow, bool) {
	for _, r := range rows {
		if r.TransactionID == id {
			return r, true
		}
	}
	return usecase.CashflowRow{}, false
}

func TestCashflowRowsCarryDestinationTypeAndBillCadence(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	txns := postgres.NewTransactionRepo(db)
	bills := postgres.NewBillRepo(db)
	repo := postgres.NewCashflowRowRepo(db)
	h, acct := seedHouseholdAndAccount(t, ctx, db)
	loan := insertAccountOfType(t, ctx, db, h, "Car loan", "loan")

	transfer, err := txns.Create(ctx, domain.Transaction{
		HouseholdID: h, Kind: domain.TransactionTransfer, OccurredOn: july(3),
		Description: "Repayment", FromAccountID: acct, ToAccountID: loan,
		Amount: domain.Money{Amount: 50000, Currency: "SGD"},
	})
	if err != nil {
		t.Fatalf("create transfer: %v", err)
	}
	plain, err := txns.Create(ctx, domain.Transaction{
		HouseholdID: h, Kind: domain.TransactionExpense, OccurredOn: july(4),
		Description: "Lunch", FromAccountID: acct,
		Amount: domain.Money{Amount: 1200, Currency: "SGD"},
	})
	if err != nil {
		t.Fatalf("create expense: %v", err)
	}
	income, err := txns.Create(ctx, domain.Transaction{
		HouseholdID: h, Kind: domain.TransactionIncome, OccurredOn: july(5),
		Description: "Pay", ToAccountID: loan,
		Amount: domain.Money{Amount: 900000, Currency: "SGD"},
	})
	if err != nil {
		t.Fatalf("create income: %v", err)
	}
	bill := createBill(t, ctx, bills, h, acct, "Home insurance", domain.CadenceYearly, day("2026-07-10"), 120000)
	payment := payYearlyBill(t, ctx, bills, h, acct, bill.Bill.ID, day("2026-07-10"), 120000)

	rows, err := repo.Rows(ctx, h)
	if err != nil {
		t.Fatalf("Rows: %v", err)
	}
	if len(rows) != 4 {
		t.Fatalf("got %d rows, want 4", len(rows))
	}

	got, _ := rowByID(rows, transfer.ID)
	if got.Kind != domain.TransactionTransfer || got.ToAccountType != domain.AccountLoan || got.ToAccountName != "Car loan" ||
		got.Amount != (domain.Money{Amount: 50000, Currency: "SGD"}) {
		t.Fatalf("transfer row = %+v", got)
	}
	got, _ = rowByID(rows, plain.ID)
	if got.ToAccountType != "" || got.ToAccountName != "" || got.BillCadence != "" || got.BillName != "" {
		t.Fatalf("plain expense row = %+v, want empty destination and bill", got)
	}
	// Income also has a to-account, but only a transfer reports it.
	got, _ = rowByID(rows, income.ID)
	if got.ToAccountType != "" || got.ToAccountName != "" {
		t.Fatalf("income row = %+v, want the destination blanked", got)
	}
	got, ok := rowByID(rows, payment.Payment.TransactionID)
	if !ok || got.BillCadence != domain.CadenceYearly || got.BillName != "Home insurance" {
		t.Fatalf("bill payment row = %+v (found %v)", got, ok)
	}
	if !got.OccurredOn.Equal(day("2026-07-10")) {
		t.Fatalf("OccurredOn = %v", got.OccurredOn)
	}
	if rows[0].TransactionID != transfer.ID {
		t.Fatalf("first row is %s, want the oldest (the 3 July transfer)", rows[0].TransactionID)
	}
}

// Two ways of reading a month must not drift apart.
func TestCashflowRowsExpenseSumMatchesMonthTotals(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	txns := postgres.NewTransactionRepo(db)
	repo := postgres.NewCashflowRowRepo(db)
	h, acct := seedHouseholdAndAccount(t, ctx, db)
	other := insertAccountOfType(t, ctx, db, h, "Savings", "cash")

	for i, c := range []struct {
		kind   domain.TransactionKind
		amount int64
		on     time.Time
	}{
		{domain.TransactionExpense, 1250, july(1)},
		{domain.TransactionExpense, 3399, july(15)},
		{domain.TransactionExpense, 700, july(31)},
		{domain.TransactionExpense, 9999, time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)}, // next month
		{domain.TransactionIncome, 500000, july(2)},
		{domain.TransactionTransfer, 20000, july(3)},
	} {
		tx := domain.Transaction{
			HouseholdID: h, Kind: c.kind, OccurredOn: c.on, Description: "Row",
			Amount: domain.Money{Amount: c.amount, Currency: "SGD"},
		}
		switch c.kind {
		case domain.TransactionExpense:
			tx.FromAccountID = acct
		case domain.TransactionIncome:
			tx.ToAccountID = acct
		default:
			tx.FromAccountID, tx.ToAccountID = acct, other
		}
		if _, err := txns.Create(ctx, tx); err != nil {
			t.Fatalf("create %d: %v", i, err)
		}
	}

	views, err := txns.MonthTotals(ctx, h, july(1))
	if err != nil {
		t.Fatalf("MonthTotals: %v", err)
	}
	var want int64
	for _, v := range views {
		if v.Transaction.Kind == domain.TransactionExpense {
			want += v.Transaction.Amount.Amount
		}
	}
	rows, err := repo.Rows(ctx, h)
	if err != nil {
		t.Fatalf("Rows: %v", err)
	}
	var got int64
	for _, r := range rows {
		if r.Kind == domain.TransactionExpense && r.OccurredOn.Year() == 2026 && r.OccurredOn.Month() == time.July {
			got += r.Amount.Amount
		}
	}
	if want != 5349 || got != want {
		t.Fatalf("expense sum from Rows = %d, from MonthTotals = %d, want both 5349", got, want)
	}
}

func TestCashflowRowsFollowTheLedgerAfterAnExpenseIsDeleted(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	txns := postgres.NewTransactionRepo(db)
	bills := postgres.NewBillRepo(db)
	repo := postgres.NewCashflowRowRepo(db)
	h, acct := seedHouseholdAndAccount(t, ctx, db)
	bill := createBill(t, ctx, bills, h, acct, "Home insurance", domain.CadenceYearly, day("2026-07-10"), 120000)

	first := payYearlyBill(t, ctx, bills, h, acct, bill.Bill.ID, day("2026-07-10"), 120000)
	if err := txns.Delete(ctx, h, first.Payment.TransactionID); err != nil {
		t.Fatalf("delete expense: %v", err)
	}
	rows, err := repo.Rows(ctx, h)
	if err != nil {
		t.Fatalf("Rows: %v", err)
	}
	if len(rows) != 0 {
		t.Fatalf("got %d rows after deleting the only expense, want none (the payment row must not resurrect it)", len(rows))
	}

	second := payYearlyBill(t, ctx, bills, h, acct, bill.Bill.ID, day("2027-07-10"), 120000)
	view, err := txns.Get(ctx, h, second.Payment.TransactionID)
	if err != nil {
		t.Fatalf("get expense: %v", err)
	}
	tx := view.Transaction
	tx.Amount = domain.Money{Amount: 99000, Currency: "SGD"}
	if _, err := txns.Update(ctx, tx); err != nil {
		t.Fatalf("update expense: %v", err)
	}
	rows, err = repo.Rows(ctx, h)
	if err != nil {
		t.Fatalf("Rows: %v", err)
	}
	if len(rows) != 1 || rows[0].Amount.Amount != 99000 || rows[0].BillCadence != domain.CadenceYearly {
		t.Fatalf("rows = %+v, want one yearly-bill row at the edited 99000", rows)
	}
}

func TestCashflowRowsRefuseADuplicatedTransaction(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	bills := postgres.NewBillRepo(db)
	repo := postgres.NewCashflowRowRepo(db)
	h, acct := seedHouseholdAndAccount(t, ctx, db)
	bill := createBill(t, ctx, bills, h, acct, "Home insurance", domain.CadenceYearly, day("2026-07-10"), 120000)
	pay := payYearlyBill(t, ctx, bills, h, acct, bill.Bill.ID, day("2026-07-10"), 120000)

	// A second payment (another occurrence) pointing at the same expense.
	if _, err := db.Pool().Exec(ctx,
		`INSERT INTO bill_payments (bill_id, household_id, due_on, paid_on, amount_minor, transaction_id)
		 VALUES ($1, $2, DATE '2027-07-10', DATE '2027-07-10', 120000, $3)`,
		bill.Bill.ID, h, pay.Payment.TransactionID); err != nil {
		t.Fatalf("insert duplicate link: %v", err)
	}

	_, err := repo.Rows(ctx, h)
	if err == nil {
		t.Fatal("Rows succeeded; a duplicated transaction would count the payment twice")
	}
	if !strings.Contains(err.Error(), pay.Payment.TransactionID) {
		t.Fatalf("error %q does not name the transaction id", err)
	}
}

func TestCashflowRowsAreScopedToTheirHousehold(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	txns := postgres.NewTransactionRepo(db)
	repo := postgres.NewCashflowRowRepo(db)
	h1, acct1 := seedHouseholdAndAccount(t, ctx, db)
	h2, acct2 := seedHouseholdAndAccount(t, ctx, db)
	for _, c := range []struct{ h, a string }{{h1, acct1}, {h2, acct2}} {
		if _, err := txns.Create(ctx, domain.Transaction{
			HouseholdID: c.h, Kind: domain.TransactionExpense, OccurredOn: july(1),
			Description: "Row", FromAccountID: c.a,
			Amount: domain.Money{Amount: 100, Currency: "SGD"},
		}); err != nil {
			t.Fatalf("create: %v", err)
		}
	}
	rows, err := repo.Rows(ctx, h1)
	if err != nil || len(rows) != 1 {
		t.Fatalf("Rows(h1) = %d rows, %v; want exactly its own one", len(rows), err)
	}
}
