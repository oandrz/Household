package domain

import (
	"fmt"
	"math"
	"math/bits"
	"strconv"
	"strings"
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

// ParseQuantity reads a quantity the way a person types it -- "300.5" grams,
// "0.5" of a share -- into nano units.
//
// It is a sibling of ParseAmount rather than a call to it, and deliberately so:
// ParseAmount caps its scale at six decimal places and refuses zero, because a
// transaction of nothing is not a transaction. Neither rule fits here. A
// quantity has nine places, and zero is a real quantity -- a holding sold down
// to nothing still has to be readable. Bending ParseAmount to cover both would
// give one function two contracts.
//
// It refuses rather than rounds, for ParseAmount's reason: "0.0000000001" means
// the person and the product disagree about what can be represented, and
// silently dropping the last digit would store a different number from the one
// that was typed. A sign is refused too -- a disposal is its own event kind,
// never a negative acquisition.
func ParseQuantity(text string) (Quantity, error) {
	const places = 9 // QuantityScale is 10^9

	s := strings.ReplaceAll(strings.TrimSpace(text), ",", "")
	if s == "" {
		return Quantity{}, fmt.Errorf("%w: %q", ErrInvalidQuantity, text)
	}
	whole, frac, hasPoint := strings.Cut(s, ".")
	if whole == "" {
		whole = "0"
	}
	if hasPoint && frac == "" {
		return Quantity{}, fmt.Errorf("%w: %q", ErrInvalidQuantity, text)
	}
	if len(frac) > places {
		return Quantity{}, fmt.Errorf("%w: %q is finer than a billionth", ErrInvalidQuantity, text)
	}
	frac += strings.Repeat("0", places-len(frac))
	digits := whole + frac
	if len(digits) > 18 {
		return Quantity{}, fmt.Errorf("%w: %q is too large", ErrInvalidQuantity, text)
	}
	var n int64
	for _, ch := range digits {
		if ch < '0' || ch > '9' {
			return Quantity{}, fmt.Errorf("%w: %q", ErrInvalidQuantity, text)
		}
		n = n*10 + int64(ch-'0')
	}
	return NewQuantity(n)
}

// FormatQuantity renders a quantity the way ParseQuantity would read it back,
// trimming the zeros nobody wants: 300.5 grams is not "300.500000000".
//
// This pair exists so that a browser NEVER divides by 1e9 to show a quantity.
// That division is float64 arithmetic on a figure a money screen displays, and
// docs/LEARNING.md already records what that costs: 333333 * 0.3 evaluates to
// 99999.90000000001 in JavaScript, and a pool floored one unit low. The string
// crosses the wire already formatted, and comes back as a string to be parsed
// here in integers.
func FormatQuantity(q Quantity) string {
	whole := q.nano / QuantityScale
	frac := q.nano % QuantityScale
	if frac == 0 {
		return strconv.FormatInt(whole, 10)
	}
	// %09d keeps the leading zeros a fraction needs -- 1 nano unit is
	// "000000001", not "1".
	digits := strings.TrimRight(fmt.Sprintf("%09d", frac), "0")
	return strconv.FormatInt(whole, 10) + "." + digits
}

// Mul is Quantity.Value read from the price's side, for a caller holding a
// price and asking what a quantity of it is worth. It is the same call and
// the same arithmetic, named for the direction the caller is thinking in.
func (m Money) Mul(q Quantity) (Money, error) { return q.Value(m) }

// Prorate returns the share of m that corresponds to part out of whole. It is
// what takes a disposal's cost out of a holding's cost pool: selling 5 of 20
// units removes exactly a quarter of what those 20 units cost, which is what
// keeps the average cost of the remainder unchanged.
//
// It is deliberately not "compute an average, then multiply". An average cost
// per nano unit is a fraction far below one minor unit -- 3000 minor over 20
// units is 0.00000015 per nano -- so computing it first truncates it to zero
// and every disposal would cost nothing. Multiplying before dividing keeps the
// precision, at the price of needing the same 128-bit intermediate
// Quantity.Value needs, and for the same reason.
func (m Money) Prorate(part, whole Quantity) (Money, error) {
	if m.Currency == "" {
		return Money{}, fmt.Errorf("%w: a Money zero value has no currency", ErrInvalidMoney)
	}
	// A cost pool is never negative here: a holding event refuses a negative
	// amount, and a disposal's cost is capped at the pool it leaves. Refusing
	// outright is better than carrying two's-complement sign handling that
	// nothing exercises -- untested cleverness on a monetary path is what the
	// house rule against it is for. The day a caller genuinely needs to
	// prorate a negative, it can be added WITH a test.
	if m.Amount < 0 {
		return Money{}, fmt.Errorf("%w: cannot prorate a negative amount, got %d", ErrInvalidMoney, m.Amount)
	}
	if whole.nano <= 0 {
		return Money{}, fmt.Errorf("%w: %d", ErrProrateWholeNotPositive, whole.nano)
	}
	if part.nano > whole.nano {
		return Money{}, fmt.Errorf("%w: %d of %d", ErrProratePartExceedsWhole, part.nano, whole.nano)
	}

	// Both operands are non-negative by the checks above, so the 128-bit
	// arithmetic stays unsigned -- the only form math/bits offers.
	hi, lo := bits.Mul64(uint64(m.Amount), uint64(part.nano))
	// part <= whole was checked above, so the quotient cannot exceed the
	// magnitude and this guard can only fire on a Money that was already
	// beyond reach. It stays because bits.Div64 panics rather than erroring,
	// and a panic on a monetary path is worse than the overflow it replaces.
	if hi >= uint64(whole.nano) {
		return Money{}, fmt.Errorf("%w: %d prorated by %d/%d", ErrAmountOverflow, m.Amount, part.nano, whole.nano)
	}
	quo, rem := bits.Div64(hi, lo, uint64(whole.nano))

	// Half away from zero, matching Quantity.Value and usecase.Rate.Apply.
	if rem*2 >= uint64(whole.nano) {
		quo++
	}
	if quo > math.MaxInt64 {
		return Money{}, fmt.Errorf("%w: %d prorated by %d/%d", ErrAmountOverflow, m.Amount, part.nano, whole.nano)
	}
	return Money{Amount: int64(quo), Currency: m.Currency}, nil
}
