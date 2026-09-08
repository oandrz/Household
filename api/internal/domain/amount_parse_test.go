package domain_test

import (
	"errors"
	"testing"

	"github.com/andreasoentoro/hearth/api/internal/domain"
)

func TestParseAmountUsesIntegerArithmeticPerCurrency(t *testing.T) {
	cases := []struct {
		text  string
		units int
		want  int64
	}{
		{"84.50", 2, 8450}, {"84.5", 2, 8450}, {"84", 2, 8400}, {"0.1", 2, 10},
		{"1,250.00", 2, 125000}, {".5", 2, 50},
		{"1200", 0, 1200},                      // JPY
		{"12.345", 3, 12345}, {"12", 3, 12000}, // BHD
	}
	for _, c := range cases {
		got, err := domain.ParseAmount(c.text, c.units)
		if err != nil || got != c.want {
			t.Errorf("ParseAmount(%q, %d) = %d, %v; want %d", c.text, c.units, got, err, c.want)
		}
	}
}

func TestParseAmountRefusesWhatTheCurrencyCannotHold(t *testing.T) {
	bad := []struct {
		text  string
		units int
	}{
		{"84.505", 2}, {"12.5", 0}, {"-5", 2}, {"abc", 2}, {"", 2}, {"0", 2}, {"0.00", 2}, {"5.", 2}, {"1e3", 2},
		{"9999999999999999999", 2},
	}
	for _, c := range bad {
		if _, err := domain.ParseAmount(c.text, c.units); !errors.Is(err, domain.ErrInvalidMoney) {
			t.Errorf("ParseAmount(%q, %d) should refuse, got %v", c.text, c.units, err)
		}
	}
}

func TestFormatAmountRoundTrips(t *testing.T) {
	for _, c := range []struct {
		minor int64
		units int
		want  string
	}{{8450, 2, "84.50"}, {5, 2, "0.05"}, {1200, 0, "1200"}, {12345, 3, "12.345"}, {-8450, 2, "-84.50"}} {
		if got := domain.FormatAmount(c.minor, c.units); got != c.want {
			t.Errorf("FormatAmount(%d, %d) = %q, want %q", c.minor, c.units, got, c.want)
		}
	}
	if domain.MinorUnitsFor("JPY") != 0 || domain.MinorUnitsFor("sgd") != 2 || domain.MinorUnitsFor("BHD") != 3 {
		t.Fatal("MinorUnitsFor does not read the currency table")
	}
}
