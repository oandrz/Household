package domain

import (
	"math"
	"math/bits"
)

// mulDivRoundHalfAway returns a*num/den, rounded half away from zero -- the
// rounding rule shared by Rate.Apply, Quantity.Value and Money.Prorate, so
// the three don't each carry a copy promising to match. Two percentage
// calculations (domain.PercentUsed, usecase changeBasisPoints) still round
// independently; they aren't money, so moving them is separate work.
//
// a may be negative; num must be >= 0 and den > 0. ok=false conflates
// overflow with a broken precondition -- callers must check num and den
// themselves and treat false as overflow, or they'll misreport bad input.
// The product is taken in 128 bits, so only the final answer can overflow.
func mulDivRoundHalfAway(a, num, den int64) (int64, bool) {
	if num < 0 || den <= 0 {
		return 0, false
	}
	negative := a < 0
	magnitude := uint64(a)
	if negative {
		// |a| without negating a: -math.MinInt64 does not fit in an int64,
		// but ^a + 1 in uint64 is exactly its magnitude, 2^63.
		magnitude = uint64(^a) + 1
	}

	hi, lo := bits.Mul64(magnitude, uint64(num))
	// bits.Div64 PANICS when the quotient needs more than 64 bits, which is
	// exactly when hi >= den. Check first and report instead: a panic on a
	// monetary path is worse than the overflow it would replace.
	if hi >= uint64(den) {
		return 0, false
	}
	quo, rem := bits.Div64(hi, lo, uint64(den))

	// rem < den <= math.MaxInt64, so doubling it cannot wrap.
	if rem*2 >= uint64(den) {
		quo++
		if quo == 0 { // wrapped past the largest uint64
			return 0, false
		}
	}

	if negative {
		if quo > 1<<63 {
			return 0, false
		}
		if quo == 1<<63 {
			return math.MinInt64, true
		}
		return -int64(quo), true
	}
	if quo > math.MaxInt64 {
		return 0, false
	}
	return int64(quo), true
}
