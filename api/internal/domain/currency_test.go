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

// SelectableCurrencies must include the common two-minor-unit currencies and
// exclude every currency Money.String()'s hard-coded two decimal places would
// render wrong.
func TestSelectableCurrenciesOffersOnlyTwoMinorUnitCodes(t *testing.T) {
	got := SelectableCurrencies()
	if len(got) < 100 {
		t.Fatalf("got %d currencies, want the two-minor-unit majority of ISO 4217", len(got))
	}
	units := map[string]int{}
	for _, c := range got {
		units[c.Code] = c.MinorUnits
	}
	for _, code := range []string{"SGD", "IDR", "USD"} {
		if u, ok := units[code]; !ok || u != 2 {
			t.Fatalf("%s missing or has %d minor units, want present with 2", code, u)
		}
	}
	for _, code := range []string{"JPY", "KWD", "ISK"} {
		if _, ok := units[code]; ok {
			t.Fatalf("%s is selectable, but Money.String() renders its minor units wrong", code)
		}
	}
}

func TestParseSelectableCurrency(t *testing.T) {
	if got, err := ParseSelectableCurrency("sgd"); err != nil || got != "SGD" {
		t.Fatalf("ParseSelectableCurrency(sgd) = (%q, %v), want (SGD, nil)", got, err)
	}
	// JPY is a well-formed, active ISO 4217 code -- ParseCurrency alone would
	// accept it -- but its zero minor units is exactly what this gate exists
	// to refuse.
	if _, err := ParseSelectableCurrency("JPY"); !errors.Is(err, ErrInvalidMoney) {
		t.Fatalf("ParseSelectableCurrency(JPY) error = %v, want ErrInvalidMoney", err)
	}
	if _, err := ParseSelectableCurrency("ZZZ"); !errors.Is(err, ErrInvalidMoney) {
		t.Fatalf("ParseSelectableCurrency(ZZZ) error = %v, want ErrInvalidMoney", err)
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
