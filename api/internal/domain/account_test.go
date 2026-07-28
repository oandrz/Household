package domain_test

import (
	"errors"
	"math"
	"testing"

	"github.com/andreasoentoro/hearth/api/internal/domain"
)

func TestParseAccountTypeAcceptsTheFiveKnownTypes(t *testing.T) {
	for _, want := range []domain.AccountType{
		domain.AccountCash, domain.AccountInvestment, domain.AccountProperty,
		domain.AccountLoan, domain.AccountCreditCard,
	} {
		got, err := domain.ParseAccountType(string(want))
		if err != nil {
			t.Fatalf("ParseAccountType(%q): %v", want, err)
		}
		if got != want {
			t.Errorf("ParseAccountType(%q) = %q", want, got)
		}
	}
}

// TestParseAccountTypeRefusesAnythingElse is the fail-closed rule: an account
// type arrives from a request body or a database column, so it is a value this
// code did not construct. Guessing at an unrecognised one would put an account
// on the wrong side of the net worth subtraction.
func TestParseAccountTypeRefusesAnythingElse(t *testing.T) {
	for _, input := range []string{"", "savings", "CASH", "cash ", "crypto"} {
		if _, err := domain.ParseAccountType(input); !errors.Is(err, domain.ErrUnknownAccountType) {
			t.Errorf("ParseAccountType(%q) err = %v, want ErrUnknownAccountType", input, err)
		}
	}
}

func TestIsLiabilityIsTrueOnlyForDebts(t *testing.T) {
	assets := []domain.AccountType{domain.AccountCash, domain.AccountInvestment, domain.AccountProperty}
	debts := []domain.AccountType{domain.AccountLoan, domain.AccountCreditCard}

	for _, a := range assets {
		if a.IsLiability() {
			t.Errorf("%q.IsLiability() = true, want false", a)
		}
	}
	for _, d := range debts {
		if !d.IsLiability() {
			t.Errorf("%q.IsLiability() = false, want true", d)
		}
	}
}

// TestSignedNetWorthAmountNegatesOnlyDebts pins the rule that stops a car loan
// from making a household look richer. The stored figure is the sum owed, as a
// positive number; the minus sign is produced here.
func TestSignedNetWorthAmountNegatesOnlyDebts(t *testing.T) {
	owed, err := domain.NewMoney(1_450_000, "SGD") // S$14,500.00
	if err != nil {
		t.Fatalf("NewMoney: %v", err)
	}

	got, err := domain.AccountLoan.SignedNetWorthAmount(owed)
	if err != nil {
		t.Fatalf("SignedNetWorthAmount: %v", err)
	}
	if got.Amount != -1_450_000 || got.Currency != "SGD" {
		t.Errorf("loan = %+v, want {-1450000 SGD}", got)
	}

	got, err = domain.AccountCash.SignedNetWorthAmount(owed)
	if err != nil {
		t.Fatalf("SignedNetWorthAmount: %v", err)
	}
	if got.Amount != 1_450_000 {
		t.Errorf("cash = %+v, want {1450000 SGD}", got)
	}
}

// TestSignedNetWorthAmountRefusesMinInt64 guards the two's-complement edge:
// negating math.MinInt64 returns math.MinInt64 itself, so a naive negation
// would turn the largest possible debt into the largest possible asset.
func TestSignedNetWorthAmountRefusesMinInt64(t *testing.T) {
	worst := domain.Money{Amount: math.MinInt64, Currency: "SGD"}

	if _, err := domain.AccountLoan.SignedNetWorthAmount(worst); !errors.Is(err, domain.ErrAmountOverflow) {
		t.Fatalf("err = %v, want ErrAmountOverflow", err)
	}
}
