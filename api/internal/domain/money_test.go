package domain_test

import (
	"errors"
	"math"
	"testing"

	"github.com/andreasoentoro/hearth/api/internal/domain"
)

// NewMoney's currency check is structural -- three uppercase letters -- not a
// membership check against the real ISO 4217 currency list. This test name
// used to claim it rejected "an unknown currency", which it never verified:
// it only ever tried malformed strings. TestNewMoneyAcceptsAnyWellFormedCode
// below pins down the other half of that behaviour.
func TestNewMoneyRejectsMalformedCurrencyStrings(t *testing.T) {
	if _, err := domain.NewMoney(100, "sgd"); err == nil {
		t.Fatal("expected lowercase currency to be rejected")
	}
	if _, err := domain.NewMoney(100, "SG"); err == nil {
		t.Fatal("expected a two-letter currency to be rejected")
	}
}

func TestNewMoneyAcceptsAnyWellFormedCode(t *testing.T) {
	// "QQQ" is three uppercase letters and not a real ISO 4217 currency.
	// NewMoney accepts it anyway: the domain package has no currency
	// registry and does not claim to validate against one.
	if _, err := domain.NewMoney(100, "QQQ"); err != nil {
		t.Fatalf("unexpected error for a well-formed but nonexistent code: %v", err)
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

// TestStringHandlesTheMostNegativeInt64 guards against a specific two's
// complement trap: negating math.MinInt64 returns itself, so any
// implementation of String() that negates m.Amount directly corrupts the
// output for this one value instead of panicking or erroring.
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

	if _, err := zero.Add(zero); !errors.Is(err, domain.ErrInvalidMoney) {
		t.Fatalf("err = %v, want ErrInvalidMoney (zero.Add(zero))", err)
	}
	if _, err := sgd.Add(zero); !errors.Is(err, domain.ErrInvalidMoney) {
		t.Fatalf("err = %v, want ErrInvalidMoney (sgd.Add(zero))", err)
	}
	if _, err := zero.Add(sgd); !errors.Is(err, domain.ErrInvalidMoney) {
		t.Fatalf("err = %v, want ErrInvalidMoney (zero.Add(sgd))", err)
	}
}
