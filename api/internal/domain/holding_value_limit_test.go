package domain_test

import (
	"errors"
	"testing"

	"github.com/andreasoentoro/hearth/api/internal/domain"
)

// A holding's worth is quantity times unit price. Each figure is checked
// against the amount ceiling when it is typed; these tests are about the two
// TOGETHER, which no per-field check can see.

// foreignValuation is a valuation of a USD holding in an SGD household: a
// native unit price and the same price in the household's currency.
func foreignValuation(t *testing.T, day int, usdMinor, sgdMinor int64) domain.Valuation {
	t.Helper()
	primary := sgdAmount(t, sgdMinor)
	return domain.Valuation{UnitPrice: money(t, usdMinor, "USD"), PrimaryUnitPrice: &primary, AsOf: on(day)}
}

func TestAHoldingWorthExactlyTheLimitIsAccepted(t *testing.T) {
	// 100,000 units at 1,000,000,000 minor each is 1e14: the limit itself.
	err := domain.CheckHoldingValueWithinLimit(units(t, 100_000), []domain.Valuation{valuation(t, 5, 1_000_000_000)})
	if err != nil {
		t.Fatalf("error = %v, want nil at exactly the limit", err)
	}
}

func TestAHoldingWorthOneMinorUnitPastTheLimitIsRefused(t *testing.T) {
	// 121,499,449 x 823,049 = 100,000,000,000,001: both factors are ordinary
	// figures far inside the per-amount ceiling, and the product is the limit
	// plus one.
	err := domain.CheckHoldingValueWithinLimit(units(t, 121_499_449), []domain.Valuation{valuation(t, 5, 823_049)})
	if !errors.Is(err, domain.ErrHoldingValueTooLarge) {
		t.Fatalf("error = %v, want ErrHoldingValueTooLarge", err)
	}
}

// A foreign holding has two prices per day, and the report multiplies the
// quantity by each. The household-currency one is checked on its own: here
// the native value is tiny and only the primary value is at the limit.
func TestTheHouseholdCurrencyPriceIsHeldToTheSameLimit(t *testing.T) {
	atTheLimit := []domain.Valuation{foreignValuation(t, 5, 100, 1_000_000_000)}
	if err := domain.CheckHoldingValueWithinLimit(units(t, 100_000), atTheLimit); err != nil {
		t.Fatalf("error = %v, want nil at exactly the limit", err)
	}

	onePast := []domain.Valuation{foreignValuation(t, 5, 100, 823_049)}
	err := domain.CheckHoldingValueWithinLimit(units(t, 121_499_449), onePast)
	if !errors.Is(err, domain.ErrHoldingValueTooLarge) {
		t.Fatalf("error = %v, want ErrHoldingValueTooLarge for the household-currency price", err)
	}
}

// The limit is the amount ceiling, not a second number: one unit is worth
// exactly its unit price, so one unit at the ceiling passes and one unit a
// minor unit above it does not.
func TestTheHoldingValueLimitIsTheAmountCeiling(t *testing.T) {
	one := units(t, 1)
	if err := domain.CheckHoldingValueWithinLimit(one, []domain.Valuation{valuation(t, 5, domain.MaxAmountMinor)}); err != nil {
		t.Fatalf("one unit at MaxAmountMinor: error = %v, want nil", err)
	}
	err := domain.CheckHoldingValueWithinLimit(one, []domain.Valuation{valuation(t, 5, domain.MaxAmountMinor+1)})
	if !errors.Is(err, domain.ErrHoldingValueTooLarge) {
		t.Fatalf("one unit at MaxAmountMinor+1: error = %v, want ErrHoldingValueTooLarge", err)
	}
}

// 100,000 units at 1e14 minor each is 1e19, which does not fit in an int64 at
// all. That is the figure that took a household's portfolio down. It has to
// come back as the caller's mistake (a 422), never as ErrAmountOverflow, which
// the HTTP layer answers with a 500.
func TestAValueThatOverflowsIsTooLargeAndNotAnOverflow(t *testing.T) {
	err := domain.CheckHoldingValueWithinLimit(units(t, 100_000), []domain.Valuation{valuation(t, 5, 100_000_000_000_000)})
	if !errors.Is(err, domain.ErrHoldingValueTooLarge) {
		t.Fatalf("error = %v, want ErrHoldingValueTooLarge", err)
	}
	if errors.Is(err, domain.ErrAmountOverflow) {
		t.Fatalf("error = %v also matches ErrAmountOverflow, which would answer 500", err)
	}
}

// The limit must not refuse what households really own: ten million shares
// of a Rp 70 stock, and a house or a fund recorded as one unit at Rp 10bn.
func TestRealHoldingsAreFarInsideTheLimit(t *testing.T) {
	for _, tc := range []struct {
		name      string
		units     int64
		unitPrice int64
	}{
		{"ten million shares at 7,000 minor", 10_000_000, 7_000},
		{"one unit at 1e12 minor", 1, 1_000_000_000_000},
	} {
		if err := domain.CheckHoldingValueWithinLimit(units(t, tc.units), []domain.Valuation{valuation(t, 5, tc.unitPrice)}); err != nil {
			t.Errorf("%s: error = %v, want nil", tc.name, err)
		}
	}
}

// The period report multiplies by whichever price closes each period, so
// every recorded price counts, wherever it sits in the list.
func TestEveryRecordedPriceIsCheckedNotOnlyTheNewest(t *testing.T) {
	prices := []domain.Valuation{
		valuation(t, 9, 100),
		valuation(t, 2, 2_000_000_000), // 100,000 units of this is 2e14
		valuation(t, 5, 100),
	}
	err := domain.CheckHoldingValueWithinLimit(units(t, 100_000), prices)
	if !errors.Is(err, domain.ErrHoldingValueTooLarge) {
		t.Fatalf("error = %v, want ErrHoldingValueTooLarge for the price in the middle", err)
	}
}

func TestAHoldingWithNothingHeldOrNoPriceCannotBreakTheLimit(t *testing.T) {
	if err := domain.CheckHoldingValueWithinLimit(units(t, 0), []domain.Valuation{valuation(t, 5, domain.MaxAmountMinor)}); err != nil {
		t.Fatalf("nothing ever held: error = %v, want nil", err)
	}
	if err := domain.CheckHoldingValueWithinLimit(units(t, 999_999_999), nil); err != nil {
		t.Fatalf("no price recorded: error = %v, want nil", err)
	}
}

// A price this code did not build properly is a bug, not a holding that is
// too valuable. It keeps its own error so the log names the real cause.
func TestAPriceWithNoCurrencyIsNotReportedAsTooLarge(t *testing.T) {
	err := domain.CheckHoldingValueWithinLimit(units(t, 1), []domain.Valuation{{AsOf: on(5)}})
	if !errors.Is(err, domain.ErrMoneyWithoutCurrency) {
		t.Fatalf("error = %v, want ErrMoneyWithoutCurrency", err)
	}
	if errors.Is(err, domain.ErrHoldingValueTooLarge) {
		t.Fatalf("error = %v, must not read as a holding that is too valuable", err)
	}
}
