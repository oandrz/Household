package domain_test

import (
	"errors"
	"math"
	"testing"

	"github.com/andreasoentoro/hearth/api/internal/domain"
)

func TestNewMoneyRejectsWrongLengthCurrencyStrings(t *testing.T) {
	if _, err := domain.NewMoney(100, "SG"); err == nil {
		t.Fatal("expected a two-letter currency to be rejected")
	}
	if _, err := domain.NewMoney(100, "SGDX"); err == nil {
		t.Fatal("expected a four-letter currency to be rejected")
	}
}

// Don't require the caller to have already uppercased the currency string --
// sign-up passes through a stranger's raw input.
func TestNewMoneyNormalisesLowercaseCurrency(t *testing.T) {
	m, err := domain.NewMoney(100, "sgd")
	if err != nil {
		t.Fatalf("NewMoney(sgd): %v", err)
	}
	if m.Currency != "SGD" {
		t.Fatalf("Currency = %q, want %q", m.Currency, "SGD")
	}
}

// ZZZ is well-formed (three uppercase letters) but not a real ISO 4217 code.
// Don't accept it: sign-up is the first place a stranger picks this value, so
// a well-formed-but-invalid code must be refused.
func TestNewMoneyRejectsAWellFormedNonCurrency(t *testing.T) {
	if _, err := domain.NewMoney(100, "ZZZ"); !errors.Is(err, domain.ErrInvalidMoney) {
		t.Fatalf("NewMoney(ZZZ) error = %v, want ErrInvalidMoney", err)
	}
}

func TestAddRefusesToMixCurrencies(t *testing.T) {
	sgd, _ := domain.NewMoney(1000, "SGD")
	idr, _ := domain.NewMoney(1000, "IDR")

	if _, err := sgd.Add(idr); !errors.Is(err, domain.ErrCurrencyMismatch) {
		t.Fatalf("err = %v, want ErrCurrencyMismatch", err)
	}
}

func TestAddSumsMinorUnits(t *testing.T) {
	a, _ := domain.NewMoney(824055, "SGD")
	b, _ := domain.NewMoney(100, "SGD")

	sum, err := a.Add(b)
	if err != nil {
		t.Fatalf("Add: %v", err)
	}
	if sum.Amount != 824155 {
		t.Fatalf("Amount = %d, want 824155", sum.Amount)
	}
}

func TestStringFormatsMinorUnits(t *testing.T) {
	m, _ := domain.NewMoney(824055, "SGD")
	if got := m.String(); got != "SGD 8240.55" {
		t.Fatalf("String() = %q, want %q", got, "SGD 8240.55")
	}
}

func TestStringPrefixesOrdinaryNegativeAmountsWithAMinus(t *testing.T) {
	m, _ := domain.NewMoney(-100, "SGD")
	if got := m.String(); got != "-SGD 1.00" {
		t.Fatalf("String() = %q, want %q", got, "-SGD 1.00")
	}
}

// TestStringHandlesTheMostNegativeInt64 guards the two's-complement trap:
// negating math.MinInt64 returns itself, so an implementation that negates
// m.Amount directly corrupts the output for this one value instead of
// erroring.
func TestStringHandlesTheMostNegativeInt64(t *testing.T) {
	m, _ := domain.NewMoney(math.MinInt64, "SGD")
	want := "-SGD 92233720368547758.08"
	if got := m.String(); got != want {
		t.Fatalf("String() = %q, want %q", got, want)
	}
}

func TestAddRejectsOverflow(t *testing.T) {
	max, _ := domain.NewMoney(math.MaxInt64, "SGD")
	one, _ := domain.NewMoney(1, "SGD")
	if _, err := max.Add(one); !errors.Is(err, domain.ErrAmountOverflow) {
		t.Fatalf("err = %v, want ErrAmountOverflow", err)
	}

	min, _ := domain.NewMoney(math.MinInt64, "SGD")
	minusOne, _ := domain.NewMoney(-1, "SGD")
	if _, err := min.Add(minusOne); !errors.Is(err, domain.ErrAmountOverflow) {
		t.Fatalf("err = %v, want ErrAmountOverflow", err)
	}
}

func TestAddDoesNotOverflowAtTheBoundary(t *testing.T) {
	// One below the boundary must still succeed -- the overflow check must
	// not be off by one in the safe direction either.
	almostMax, _ := domain.NewMoney(math.MaxInt64-1, "SGD")
	one, _ := domain.NewMoney(1, "SGD")
	sum, err := almostMax.Add(one)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if sum.Amount != math.MaxInt64 {
		t.Fatalf("Amount = %d, want %d", sum.Amount, int64(math.MaxInt64))
	}
}

func TestAddRejectsAZeroValueMoneyOnEitherSide(t *testing.T) {
	var zero domain.Money
	sgd, _ := domain.NewMoney(100, "SGD")

	if _, err := zero.Add(zero); !errors.Is(err, domain.ErrMoneyWithoutCurrency) {
		t.Fatalf("err = %v, want ErrMoneyWithoutCurrency (zero.Add(zero))", err)
	}
	if _, err := sgd.Add(zero); !errors.Is(err, domain.ErrMoneyWithoutCurrency) {
		t.Fatalf("err = %v, want ErrMoneyWithoutCurrency (sgd.Add(zero))", err)
	}
	if _, err := zero.Add(sgd); !errors.Is(err, domain.ErrMoneyWithoutCurrency) {
		t.Fatalf("err = %v, want ErrMoneyWithoutCurrency (zero.Add(sgd))", err)
	}
}
