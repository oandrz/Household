package domain_test

import (
	"errors"
	"testing"
	"time"

	"github.com/andreasoentoro/hearth/api/internal/domain"
)

func TestParseTransactionKindRefusesAnythingElse(t *testing.T) {
	for _, in := range []string{"", "Expense", "spend", "withdrawal"} {
		if _, err := domain.ParseTransactionKind(in); !errors.Is(err, domain.ErrUnknownTransactionKind) {
			t.Fatalf("ParseTransactionKind(%q) = %v, want ErrUnknownTransactionKind", in, err)
		}
	}
	for _, in := range []domain.TransactionKind{
		domain.TransactionExpense, domain.TransactionIncome, domain.TransactionTransfer,
	} {
		got, err := domain.ParseTransactionKind(string(in))
		if err != nil || got != in {
			t.Fatalf("ParseTransactionKind(%q) = %q, %v", in, got, err)
		}
	}
}

func sgd(minor int64) domain.Money { return domain.Money{Amount: minor, Currency: "SGD"} }
func idr(minor int64) domain.Money { return domain.Money{Amount: minor, Currency: "IDR"} }

func TestBalanceEffectSubtractsFromTheSourceAndAddsToTheDestination(t *testing.T) {
	day := time.Date(2026, 7, 18, 0, 0, 0, 0, time.UTC)

	expense := domain.Transaction{
		Kind: domain.TransactionExpense, OccurredOn: day,
		FromAccountID: "dbs", Amount: sgd(5230),
	}
	got, ok := expense.BalanceEffect("dbs")
	if !ok || got != sgd(-5230) {
		t.Fatalf("expense on its own account = %v, %v; want -5230 SGD", got, ok)
	}
	if _, ok := expense.BalanceEffect("ocbc"); ok {
		t.Fatal("an expense reported an effect on an account it does not touch")
	}

	income := domain.Transaction{
		Kind: domain.TransactionIncome, OccurredOn: day,
		ToAccountID: "dbs", Amount: sgd(640000),
	}
	if got, ok := income.BalanceEffect("dbs"); !ok || got != sgd(640000) {
		t.Fatalf("income = %v, %v; want +640000 SGD", got, ok)
	}
}

// The defect this prevents: crediting the destination with the amount that
// left rather than the amount that arrived adds Singapore dollars to a rupiah
// balance, and the account ends up wrong by a factor of ten thousand.
func TestBalanceEffectCreditsTheReceivedAmountOnACrossCurrencyTransfer(t *testing.T) {
	received := idr(620000000)
	transfer := domain.Transaction{
		Kind:          domain.TransactionTransfer,
		OccurredOn:    time.Date(2026, 6, 29, 0, 0, 0, 0, time.UTC),
		FromAccountID: "dbs", ToAccountID: "bca",
		Amount: sgd(50000), ReceivedAmount: &received,
	}

	if got, ok := transfer.BalanceEffect("dbs"); !ok || got != sgd(-50000) {
		t.Fatalf("source side = %v, %v; want -50000 SGD", got, ok)
	}
	if got, ok := transfer.BalanceEffect("bca"); !ok || got != idr(620000000) {
		t.Fatalf("destination side = %v, %v; want +620000000 IDR", got, ok)
	}
}

// A same-currency transfer may still carry a received amount -- a bank fee.
// When it does not, what arrives is what left.
func TestBalanceEffectFallsBackToTheAmountSentWhenNothingElseWasRecorded(t *testing.T) {
	transfer := domain.Transaction{
		Kind:          domain.TransactionTransfer,
		OccurredOn:    time.Date(2026, 6, 29, 0, 0, 0, 0, time.UTC),
		FromAccountID: "dbs", ToAccountID: "ocbc", Amount: sgd(50000),
	}
	if got, ok := transfer.BalanceEffect("ocbc"); !ok || got != sgd(50000) {
		t.Fatalf("destination side = %v, %v; want +50000 SGD", got, ok)
	}

	fee := sgd(49800)
	withFee := transfer
	withFee.ReceivedAmount = &fee
	if got, ok := withFee.BalanceEffect("ocbc"); !ok || got != sgd(49800) {
		t.Fatalf("destination side with a fee = %v, %v; want +49800 SGD", got, ok)
	}
}
