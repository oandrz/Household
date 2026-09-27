package domain_test

import (
	"errors"
	"math"
	"testing"

	"github.com/andreasoentoro/hearth/api/internal/domain"
)

// A holding's quantity is genuinely fractional -- 300.5 grams of gold, half
// a share -- so it can't be a plain int64 count; it's nano units, one unit
// is domain.QuantityScale.
//
// These tests exist because the obvious Value implementation (quantity *
// price / scale in int64) is wrong on an ordinary holding, not just an
// exotic one -- TestValueDoesNotOverflowOnAnOrdinaryIDRHolding is the case
// that drove the design.

func TestNewQuantityRefusesANegativeAmount(t *testing.T) {
	if _, err := domain.NewQuantity(-1); !errors.Is(err, domain.ErrQuantityNegative) {
		t.Fatalf("NewQuantity(-1) error = %v, want ErrQuantityNegative", err)
	}
}

func TestNewQuantityAcceptsZero(t *testing.T) {
	// A holding sold down to nothing is zero, not an error: its lots and its
	// realised profit still have to be readable after the last one is sold.
	q, err := domain.NewQuantity(0)
	if err != nil {
		t.Fatalf("NewQuantity(0): %v", err)
	}
	price, _ := domain.NewMoney(12345, "SGD")
	v, err := q.Value(price)
	if err != nil {
		t.Fatalf("Value: %v", err)
	}
	if v.Amount != 0 {
		t.Fatalf("Amount = %d, want 0", v.Amount)
	}
	if v.Currency != "SGD" {
		t.Fatalf("Currency = %q, want SGD -- a value is denominated in the price's currency", v.Currency)
	}
}

func TestValueOfOneWholeUnitIsTheUnitPrice(t *testing.T) {
	q, err := domain.NewQuantity(domain.QuantityScale)
	if err != nil {
		t.Fatalf("NewQuantity: %v", err)
	}
	price, _ := domain.NewMoney(4999, "SGD")

	v, err := q.Value(price)
	if err != nil {
		t.Fatalf("Value: %v", err)
	}
	if v.Amount != 4999 {
		t.Fatalf("Amount = %d, want 4999", v.Amount)
	}
}

// The case the whole design turns on: IDR has two minor units, so Rp 10,000
// per share is 1_000_000 minor units, and 10,000 shares is 1e13 nano --
// their product, 1e19, doesn't fit an int64 (max 9.223e18), so an
// int64-multiplying implementation refuses a perfectly ordinary Indonesian
// position. The 128-bit intermediate exists for this; the quotient, 1e10
// minor units, fits with room to spare.
func TestValueDoesNotOverflowOnAnOrdinaryIDRHolding(t *testing.T) {
	const shares = 10_000
	q, err := domain.NewQuantity(shares * domain.QuantityScale)
	if err != nil {
		t.Fatalf("NewQuantity: %v", err)
	}
	price, err := domain.NewMoney(10_000*100, "IDR") // Rp 10,000, two minor units
	if err != nil {
		t.Fatalf("NewMoney: %v", err)
	}

	v, err := q.Value(price)
	if err != nil {
		t.Fatalf("Value returned %v -- an ordinary IDR holding must be valuable, not refused", err)
	}
	const want = 10_000 * 10_000 * 100 // shares * price in minor units
	if v.Amount != want {
		t.Fatalf("Amount = %d, want %d", v.Amount, want)
	}
	if v.Currency != "IDR" {
		t.Fatalf("Currency = %q, want IDR", v.Currency)
	}
}

// The IDR case above overflows int64 but its product still fits a uint64, so
// it alone doesn't prove the 128-bit product's high word is used -- a
// 64-bit-truncated implementation would still pass it. 100,000 shares
// (1,000 IDX lots) at Rp 10,000 needs the high word: a product of 1e20, past
// 2^64. Without this case, truncating to one 64-bit word is a mutation the
// suite misses.
func TestValueUsesTheHighWordOfThe128BitProduct(t *testing.T) {
	const shares = 100_000
	q, err := domain.NewQuantity(shares * domain.QuantityScale)
	if err != nil {
		t.Fatalf("NewQuantity: %v", err)
	}
	price, _ := domain.NewMoney(10_000*100, "IDR")

	v, err := q.Value(price)
	if err != nil {
		t.Fatalf("Value: %v", err)
	}
	const want = int64(shares) * 10_000 * 100 // Rp 1,000,000,000 in minor units
	if v.Amount != want {
		t.Fatalf("Amount = %d, want %d", v.Amount, want)
	}
}

// Rounding is half away from zero, the same rule domain.Rate.Apply already
// uses. One rounding rule in this codebase, not two.
func TestValueRoundsHalfAwayFromZero(t *testing.T) {
	var half int64 = domain.QuantityScale / 2 // 0.5 of a unit

	cases := []struct {
		name       string
		priceMinor int64
		want       int64
	}{
		// 0.5 * 1 = 0.5 exactly -> away from zero is 1, not 0.
		{"exactly one half rounds up", 1, 1},
		// 0.5 * 3 = 1.5 exactly -> 2, not 1.
		{"one and a half rounds up", 3, 2},
		// 0.5 * 4 = 2 exactly -> no rounding to do.
		{"an exact result is untouched", 4, 2},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			q, err := domain.NewQuantity(half)
			if err != nil {
				t.Fatalf("NewQuantity: %v", err)
			}
			price, _ := domain.NewMoney(c.priceMinor, "SGD")
			v, err := q.Value(price)
			if err != nil {
				t.Fatalf("Value: %v", err)
			}
			if v.Amount != c.want {
				t.Fatalf("Amount = %d, want %d", v.Amount, c.want)
			}
		})
	}
}

// A result that genuinely can't fit must report ErrAmountOverflow, never
// panic: math/bits.Div64 panics when the quotient needs more than 64 bits,
// so the implementation must check before dividing -- a panic in a monetary
// path is worse than the overflow it replaces.
func TestValueReportsOverflowRatherThanPanicking(t *testing.T) {
	q, err := domain.NewQuantity(math.MaxInt64)
	if err != nil {
		t.Fatalf("NewQuantity: %v", err)
	}
	price, _ := domain.NewMoney(math.MaxInt64, "SGD")

	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("Value panicked (%v); it must return ErrAmountOverflow instead", r)
		}
	}()

	if _, err := q.Value(price); !errors.Is(err, domain.ErrAmountOverflow) {
		t.Fatalf("Value error = %v, want ErrAmountOverflow", err)
	}
}

// A unit price is a price: negative isn't meaningful, and accepting one
// would make a holding contribute negative market value -- a liability,
// which is what an account type is for. Fail closed on a value this
// package did not construct.
func TestValueRefusesANegativeUnitPrice(t *testing.T) {
	q, _ := domain.NewQuantity(domain.QuantityScale)
	price, _ := domain.NewMoney(-1, "SGD")

	if _, err := q.Value(price); !errors.Is(err, domain.ErrNegativeAmount) {
		t.Fatalf("Value error = %v, want ErrNegativeAmount", err)
	}
}

// A quantity crosses the wire as a STRING, the way a person types it --
// "300.5" grams, "0.5" of a share -- never as nano units for a browser to
// divide by 1e9, which is float64 arithmetic on a money-screen figure (the
// defect docs/LEARNING.md already records). ParseQuantity and FormatQuantity
// are the pair that keeps that division out of both ends.
func TestParseQuantityReadsWhatAPersonWouldType(t *testing.T) {
	cases := []struct {
		in   string
		want int64
	}{
		{"300.5", 300_500_000_000},
		{"1", domain.QuantityScale},
		{"0.5", domain.QuantityScale / 2},
		{"0.000000001", 1},                        // one billionth, the smallest representable
		{"10,000", 10_000 * domain.QuantityScale}, // thousands separators, as ParseAmount allows
		{" 2.25 ", 2_250_000_000},                 // surrounding space
		{"0", 0},
		{".5", domain.QuantityScale / 2},
	}
	for _, c := range cases {
		got, err := domain.ParseQuantity(c.in)
		if err != nil {
			t.Fatalf("ParseQuantity(%q): %v", c.in, err)
		}
		if got.Nano() != c.want {
			t.Fatalf("ParseQuantity(%q) = %d, want %d", c.in, got.Nano(), c.want)
		}
	}
}

func TestParseQuantityRefusesWhatItCannotRepresent(t *testing.T) {
	// Ten decimal places is finer than a nano unit. Silently truncating would
	// store a different number than the one typed -- worse than refusing,
	// since the household would see a figure it didn't enter.
	for _, s := range []string{"", "   ", "abc", "-1", "1.2.3", "0.0000000001", "1e9", "1.", "--1"} {
		if _, err := domain.ParseQuantity(s); err == nil {
			t.Fatalf("ParseQuantity(%q) was accepted; it must be refused", s)
		}
	}
}

// FormatQuantity is ParseQuantity's inverse for display, and trims the zeros
// nobody wants to read: 300.5 grams is not "300.500000000".
func TestFormatQuantityRoundTripsAndTrimsTrailingZeros(t *testing.T) {
	cases := []struct {
		nano int64
		want string
	}{
		{300_500_000_000, "300.5"},
		{domain.QuantityScale, "1"},
		{0, "0"},
		{1, "0.000000001"},
		{2_250_000_000, "2.25"},
	}
	for _, c := range cases {
		q, err := domain.NewQuantity(c.nano)
		if err != nil {
			t.Fatalf("NewQuantity(%d): %v", c.nano, err)
		}
		if got := domain.FormatQuantity(q); got != c.want {
			t.Fatalf("FormatQuantity(%d) = %q, want %q", c.nano, got, c.want)
		}
		// The round trip is the point: whatever a screen shows must parse back
		// to the same stored figure.
		back, err := domain.ParseQuantity(c.want)
		if err != nil {
			t.Fatalf("ParseQuantity(%q): %v", c.want, err)
		}
		if back.Nano() != c.nano {
			t.Fatalf("round trip of %d gave %d", c.nano, back.Nano())
		}
	}
}
