package domain

import (
	"errors"
	"testing"
)

func TestParseCurrencyNormalisesCase(t *testing.T) {
	got, err := ParseCurrency("sgd")
	if err != nil {
		t.Fatalf("ParseCurrency(sgd): %v", err)
	}
	if got != "SGD" {
		t.Fatalf("got %q, want SGD", got)
	}
}

// ZZZ is three uppercase letters, which is all NewMoney used to check. It is
// not an ISO 4217 code, and sign-up is the first place a stranger picks this
// value, so it must be refused.
func TestParseCurrencyRejectsAWellFormedNonCurrency(t *testing.T) {
	if _, err := ParseCurrency("ZZZ"); !errors.Is(err, ErrInvalidMoney) {
		t.Fatalf("ParseCurrency(ZZZ) error = %v, want ErrInvalidMoney", err)
	}
}

func TestParseCurrencyRejectsWrongLength(t *testing.T) {
	for _, code := range []string{"", "S", "SG", "SGDX"} {
		if _, err := ParseCurrency(code); !errors.Is(err, ErrInvalidMoney) {
			t.Fatalf("ParseCurrency(%q) error = %v, want ErrInvalidMoney", code, err)
		}
	}
}

func TestCurrencyMinorUnits(t *testing.T) {
	for _, tc := range []struct {
		code string
		want int
	}{
		{"SGD", 2},
		{"IDR", 2},
		{"JPY", 0},
		{"KWD", 3},
	} {
		got, ok := CurrencyMinorUnits(tc.code)
		if !ok {
			t.Fatalf("CurrencyMinorUnits(%s): not found", tc.code)
		}
		if got != tc.want {
			t.Fatalf("CurrencyMinorUnits(%s) = %d, want %d", tc.code, got, tc.want)
		}
	}
	if _, ok := CurrencyMinorUnits("ZZZ"); ok {
		t.Fatal("CurrencyMinorUnits(ZZZ): want not found")
	}
}

// ActiveCurrencies must be sorted and must not hand out a slice the caller can
// mutate into the package's own state.
func TestActiveCurrenciesIsSortedAndCopied(t *testing.T) {
	first := ActiveCurrencies()
	if len(first) < 100 {
		t.Fatalf("ActiveCurrencies() returned %d entries, want the full ISO 4217 active set", len(first))
	}
	for i := 1; i < len(first); i++ {
		if first[i-1].Code >= first[i].Code {
			t.Fatalf("not sorted at %d: %q then %q", i, first[i-1].Code, first[i].Code)
		}
	}
	first[0].Code = "MUTATED"
	if ActiveCurrencies()[0].Code == "MUTATED" {
		t.Fatal("ActiveCurrencies() handed out its own backing array")
	}
}
