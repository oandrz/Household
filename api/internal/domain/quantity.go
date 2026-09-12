package domain

import (
	"fmt"
	"math"
	"math/bits"
)

// QuantityScale is how many stored units make one whole unit of a thing held:
// one share, one gram. A quantity is therefore an int64 count of billionths,
// the same way Money is an int64 count of minor units, and for the same reason
// -- float64 never appears anywhere a figure on a money screen comes from.
//
// Nine decimal places is far past anything real: a broker's smallest fractional
// share is a millionth, and gold is weighed to milligrams. The headroom is
// deliberate, because the alternative -- discovering the scale is too coarse
// after rows exist -- is a migration over live data.
//
// Changing this constant silently restates every quantity already stored. If it
// ever has to change, it needs a migration that rewrites the column, not a new
// value here.
const QuantityScale = 1_000_000_000

// Quantity is how much of a holding is held, in billionths of a unit. It is
// deliberately not a Money: it has no currency, because "300.5 grams" is not
// denominated in anything. Value is what turns it into money, and needs a
// price to do it.
type Quantity struct {
	// nano is unexported so that the only way to hold a Quantity is through
	// NewQuantity, which refuses a negative one. A bare struct literal would
	// let a negative quantity in through a door this type exists to close.
	nano int64
}

// NewQuantity refuses a negative amount. Zero is allowed and is a real state:
// a holding sold down to nothing still has lots and realised profit to show,
// so it must remain readable rather than becoming unrepresentable.
func NewQuantity(nano int64) (Quantity, error) {
	if nano < 0 {
		return Quantity{}, fmt.Errorf("%w: %d", ErrQuantityNegative, nano)
	}
	return Quantity{nano: nano}, nil
}

// Nano returns the raw count of billionths, for a repository storing it.
func (q Quantity) Nano() int64 { return q.nano }

// Value is what this quantity is worth at unitPrice, in unitPrice's currency.
//
// The arithmetic is the whole reason this type exists. The obvious version --
// q.nano * unitPrice.Amount / QuantityScale in int64 -- overflows on an
// entirely ordinary holding, not an exotic one:
//
//	10,000 shares                  = 1e13 nano units
//	Rp 10,000 each (IDR, 2 places) = 1e6 minor units
//	product                        = 1e19, and an int64 stops at 9.223e18
//
// The answer, 1e10 minor units, fits with room to spare; it is only the
// intermediate that does not. So the product is taken in 128 bits via
// math/bits and divided back down, which keeps the whole calculation in
// integers -- the same commitment Money.Add and usecase.Rate.Apply make.
func (q Quantity) Value(unitPrice Money) (Money, error) {
	if unitPrice.Currency == "" {
		return Money{}, fmt.Errorf("%w: a Money zero value has no currency", ErrInvalidMoney)
	}
	// A unit price is a price. A negative one would make a holding contribute
	// negative market value, which is a debt -- and a debt is an account type,
	// not a holding. Fail closed rather than quietly producing that figure.
	if unitPrice.Amount < 0 {
		return Money{}, fmt.Errorf("%w: a unit price cannot be negative, got %d", ErrInvalidMoney, unitPrice.Amount)
	}

	// Both operands are non-negative by the checks above and by NewQuantity,
	// so the unsigned product below needs no sign handling at all. That is
	// why those two refusals come first: they buy the rest of this function.
	hi, lo := bits.Mul64(uint64(q.nano), uint64(unitPrice.Amount))

	// bits.Div64 PANICS when the quotient will not fit in 64 bits, rather
	// than reporting it. Checking hi against the divisor first is what turns
	// that panic into an error: the quotient is (hi*2^64+lo)/QuantityScale,
	// which needs 64 bits or more exactly when hi >= QuantityScale. A panic
	// in a monetary path is worse than the overflow it would replace, so this
	// guard must stay ahead of the divide.
	if hi >= QuantityScale {
		return Money{}, fmt.Errorf("%w: %d nano units at %d", ErrAmountOverflow, q.nano, unitPrice.Amount)
	}
	quo, rem := bits.Div64(hi, lo, QuantityScale)

	// Round half away from zero, matching usecase.Rate.Apply. Both operands
	// are non-negative, so "away from zero" is always up, and doubling the
	// remainder compares it against the divisor without leaving integers.
	if rem*2 >= QuantityScale {
		quo++
		// The increment is the one step that can carry a quotient which was
		// exactly math.MaxInt64 over the edge, so it is checked after, not
		// before.
		if quo == 0 {
			return Money{}, fmt.Errorf("%w: rounding %d nano units at %d", ErrAmountOverflow, q.nano, unitPrice.Amount)
		}
	}
	if quo > math.MaxInt64 {
		return Money{}, fmt.Errorf("%w: %d nano units at %d", ErrAmountOverflow, q.nano, unitPrice.Amount)
	}

	return Money{Amount: int64(quo), Currency: unitPrice.Currency}, nil
}
