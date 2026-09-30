package domain_test

import (
	"errors"
	"math"
	"testing"

	"github.com/andreasoentoro/hearth/api/internal/domain"
)

func TestCheckAmountWithinLimitAcceptsEverythingUpToTheCeilingOnBothSides(t *testing.T) {
	// Rp 10,000,000,000 (a house) is 1e12 minor units: a real household
	// figure, and the reason the ceiling cannot sit any lower than this.
	const houseInRupiahMinor = 1_000_000_000_000

	for _, minor := range []int64{
		0, 1, -1, 5230, houseInRupiahMinor, -houseInRupiahMinor,
		domain.MaxAmountMinor, -domain.MaxAmountMinor,
	} {
		if err := domain.CheckAmountWithinLimit(minor); err != nil {
			t.Errorf("CheckAmountWithinLimit(%d) = %v, want nil", minor, err)
		}
	}
}

func TestCheckAmountWithinLimitRefusesOneMinorUnitPastTheCeilingOnBothSides(t *testing.T) {
	for _, minor := range []int64{
		domain.MaxAmountMinor + 1, -domain.MaxAmountMinor - 1,
		// The two figures that took a household's money pages down: one
		// expense of int64 max, and S$50,000,000,000,000,000 typed in the form.
		math.MaxInt64, 5_000_000_000_000_000_000,
		// MinInt64 has no positive counterpart, so a check written with a
		// negation would let it through.
		math.MinInt64,
	} {
		if err := domain.CheckAmountWithinLimit(minor); !errors.Is(err, domain.ErrAmountTooLarge) {
			t.Errorf("CheckAmountWithinLimit(%d) = %v, want ErrAmountTooLarge", minor, err)
		}
	}
}

// The ceiling's own comment promises three things. Each is arithmetic, so each
// is pinned here: raising the constant past any of them must fail a test, not
// only contradict a comment.
func TestMaxAmountMinorKeepsThePromisesItsCommentMakes(t *testing.T) {
	// One amount at the ceiling, converted at the largest rate the FX table
	// holds (SGD to IDR), still fits in an int64.
	const largestRateNumerator = 12_410
	if domain.MaxAmountMinor > math.MaxInt64/largestRateNumerator {
		t.Errorf("MaxAmountMinor %d overflows when converted at %d", domain.MaxAmountMinor, largestRateNumerator)
	}

	// A browser holds an amount as a float64, which is exact only up to 2^53.
	const largestExactBrowserInteger = 1 << 53
	if domain.MaxAmountMinor >= largestExactBrowserInteger {
		t.Errorf("MaxAmountMinor %d is not exact in a browser", domain.MaxAmountMinor)
	}

	// No household reaches tens of thousands of rows that are each the largest
	// amount allowed, so a sum of stored amounts cannot overflow by accident.
	const fewestCeilingRowsBeforeASumOverflows = 90_000
	if math.MaxInt64/domain.MaxAmountMinor < fewestCeilingRowsBeforeASumOverflows {
		t.Errorf("only %d rows at the ceiling overflow a sum, want at least %d",
			math.MaxInt64/domain.MaxAmountMinor, fewestCeilingRowsBeforeASumOverflows)
	}
}
