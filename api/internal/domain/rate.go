package domain

import "fmt"

// Rate is how many units of one currency one unit of another is worth, held
// as a fraction rather than a scaled decimal. SGD to IDR is {12410, 1}; IDR to
// SGD is {1, 12410}. A scaled decimal cannot represent the second direction --
// 0.0000806 truncates to zero at any sane scale -- and IDR to SGD is precisely
// the direction the design's Finances screen uses.
type Rate struct {
	Numerator   int64
	Denominator int64
}

// Apply converts an amount of minor units, rounding half away from zero.
//
// A rate whose numerator or denominator is not positive is refused with
// ErrInvalidRate. It comes from a provider this code did not construct, and a
// zero denominator would otherwise divide by zero. An answer that does not fit
// in an int64 is refused with ErrAmountOverflow rather than silently wrapping.
func (r Rate) Apply(minorUnits int64) (int64, error) {
	if r.Numerator <= 0 || r.Denominator <= 0 {
		return 0, fmt.Errorf("%w: %d/%d", ErrInvalidRate, r.Numerator, r.Denominator)
	}
	out, ok := mulDivRoundHalfAway(minorUnits, r.Numerator, r.Denominator)
	if !ok {
		return 0, fmt.Errorf("%w: %d at %d/%d", ErrAmountOverflow, minorUnits, r.Numerator, r.Denominator)
	}
	return out, nil
}
