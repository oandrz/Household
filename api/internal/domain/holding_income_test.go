package domain_test

import (
	"errors"
	"testing"

	"github.com/andreasoentoro/hearth/api/internal/domain"
)

// Income is the third, fold-skipping component of the PRD's profit: a dividend
// changes neither what's held nor its cost, so folding it would corrupt the
// average cost of everything sold after. It sits beside the position instead,
// summed over the period. A fee is the same shape with the opposite sign,
// hence one type, not a second table.

func income(t *testing.T, kind domain.IncomeKind, minor int64, currency string) domain.HoldingIncome {
	t.Helper()
	return domain.HoldingIncome{
		Kind:       kind,
		Amount:     money(t, minor, currency),
		ReceivedOn: on(4),
	}
}

func TestParseIncomeKindAcceptsEveryKindItShips(t *testing.T) {
	for _, want := range []domain.IncomeKind{domain.IncomeReceived, domain.IncomeFee} {
		got, err := domain.ParseIncomeKind(string(want))
		if err != nil || got != want {
			t.Fatalf("ParseIncomeKind(%q) = %q, %v", want, got, err)
		}
	}
}

func TestParseIncomeKindRefusesAnythingElse(t *testing.T) {
	for _, in := range []string{"", "dividend", "INCOME", "charge", "coupon"} {
		if _, err := domain.ParseIncomeKind(in); !errors.Is(err, domain.ErrUnknownIncomeKind) {
			t.Errorf("ParseIncomeKind(%q) error = %v, want ErrUnknownIncomeKind", in, err)
		}
	}
}

// A fee is stored positive and subtracted when the period is summed, rather
// than stored as negative money. Negative money is refused everywhere else in
// this package, and one exception is how the rule stops being a rule.
func TestIncomeRefusesANegativeAmount(t *testing.T) {
	in := income(t, domain.IncomeFee, -100, "SGD")
	if err := in.Validate("SGD", "SGD"); !errors.Is(err, domain.ErrHoldingIncomeAmountNotPositive) {
		t.Fatalf("error = %v, want ErrHoldingIncomeAmountNotPositive", err)
	}
}

// Nothing changed hands, so there is nothing to record -- the same rule a
// holding event's zero quantity follows.
func TestIncomeRefusesAZeroAmount(t *testing.T) {
	in := income(t, domain.IncomeReceived, 0, "SGD")
	if err := in.Validate("SGD", "SGD"); !errors.Is(err, domain.ErrHoldingIncomeAmountNotPositive) {
		t.Fatalf("error = %v, want ErrHoldingIncomeAmountNotPositive", err)
	}
}

func TestIncomeRefusesAnAmountInTheWrongCurrency(t *testing.T) {
	in := income(t, domain.IncomeReceived, 500, "SGD")
	if err := in.Validate("USD", "SGD"); !errors.Is(err, domain.ErrCurrencyMismatch) {
		t.Fatalf("error = %v, want ErrCurrencyMismatch", err)
	}
}

func TestIncomeRefusesAKindItDoesNotKnow(t *testing.T) {
	in := income(t, domain.IncomeKind("bonus"), 500, "SGD")
	if err := in.Validate("SGD", "SGD"); !errors.Is(err, domain.ErrUnknownIncomeKind) {
		t.Fatalf("error = %v, want ErrUnknownIncomeKind", err)
	}
}

// Income obeys the same cross-currency rule as an event and a valuation,
// through the SAME function -- a USD dividend to an SGD household is worth
// what actually landed in the bank, not what a rate says it was worth.
func TestIncomeCarriesTheSameCrossCurrencyRuleAsAnEvent(t *testing.T) {
	bare := income(t, domain.IncomeReceived, 500, "USD")
	if err := bare.Validate("USD", "SGD"); !errors.Is(err, domain.ErrHoldingPrimaryAmountRequired) {
		t.Fatalf("a USD dividend in an SGD household needs its primary amount: %v", err)
	}

	primary := money(t, 675, "SGD")
	withPrimary := bare
	withPrimary.PrimaryAmount = &primary
	if err := withPrimary.Validate("USD", "SGD"); err != nil {
		t.Fatalf("a USD dividend with its SGD amount is valid: %v", err)
	}

	same := income(t, domain.IncomeReceived, 500, "SGD")
	same.PrimaryAmount = &primary
	if err := same.Validate("SGD", "SGD"); !errors.Is(err, domain.ErrHoldingPrimaryAmountNotAllowed) {
		t.Fatalf("an SGD dividend in an SGD household must not carry a second amount: %v", err)
	}
}

// InPrimary is what the report sums: the separately recorded household-currency
// amount when there is one, the native amount when the holding is already in
// that currency.
func TestIncomeInPrimaryPrefersTheRecordedAmount(t *testing.T) {
	primary := money(t, 675, "SGD")
	withPrimary := income(t, domain.IncomeReceived, 500, "USD")
	withPrimary.PrimaryAmount = &primary
	if got := withPrimary.InPrimary(); got != primary {
		t.Errorf("InPrimary() = %v, want %v", got, primary)
	}

	same := income(t, domain.IncomeReceived, 500, "SGD")
	if got := same.InPrimary(); got != same.Amount {
		t.Errorf("InPrimary() = %v, want the native amount %v", got, same.Amount)
	}
}

// Income and fees are summed per period, so a row gets the same ceiling every
// other amount in the product has.
func TestIncomeRefusesAnAmountPastTheCeiling(t *testing.T) {
	if err := income(t, domain.IncomeReceived, domain.MaxAmountMinor, "SGD").Validate("SGD", "SGD"); err != nil {
		t.Fatalf("amount at the ceiling: %v", err)
	}
	tooLarge := income(t, domain.IncomeReceived, domain.MaxAmountMinor+1, "SGD")
	if err := tooLarge.Validate("SGD", "SGD"); !errors.Is(err, domain.ErrAmountTooLarge) {
		t.Fatalf("amount past the ceiling: error = %v, want ErrAmountTooLarge", err)
	}
}
