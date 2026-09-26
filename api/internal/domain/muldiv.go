package domain

import (
	"math"
	"math/bits"
)

// mulDivRoundHalfAway returns a*num/den, rounded half away from zero. It is
// the one rounding rule for every monetary multiply-then-divide in Hearth:
// currency conversion (Rate.Apply), a holding's value (Quantity.Value) and a
// cost pool's share (Money.Prorate). It exists so that those three stop each
// carrying a copy with a comment promising the copies match.
//
// a may be negative. num must be >= 0 and den must be > 0; every caller checks
// that first and reports its own error, so ok=false here means only "the
// answer does not fit in an int64". The product is taken in 128 bits, so an
// intermediate that is too big never causes a refusal on its own; only the
// answer can.
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
