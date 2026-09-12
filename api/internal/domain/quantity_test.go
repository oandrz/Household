package domain_test

import (
	"errors"
	"math"
	"testing"

	"github.com/andreasoentoro/hearth/api/internal/domain"
)

// A holding's quantity is genuinely fractional -- 300.5 grams of gold, half a
// share -- so it cannot be the plain int64 count every other number in this
// package is. It is held in nano units: one unit is domain.QuantityScale.
//
// These tests exist because the obvious implementation of Value --
// quantity * price / scale in int64 -- is wrong, and wrong on an ordinary
// holding rather than an exotic one. TestValueDoesNotOverflowOnAnOrdinaryIDRHolding
// below is the case that drove the design.

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

// The case the whole design turns on. IDR has two minor units
// (domain/currency.go), so Rp 10,000 per share is 1_000_000 minor units, and
// 10,000 shares is 1e13 nano. Their product is 1e19, which does NOT fit in an
// int64 (max 9.223e18) -- so an implementation that multiplies in int64 refuses
// to value a perfectly ordinary Indonesian position. The 128-bit intermediate
// exists for this. The quotient, 1e10 minor units (Rp 100,000,000), fits with
// room to spare: it is only the intermediate that overflows.
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

// Rounding is half away from zero, the same rule usecase.Rate.Apply already
// uses. One rounding rule in this codebase, not two.
func TestValueRoundsHalfAwayFromZero(t *testing.T) {
	half := domain.QuantityScale / 2 // 0.5 of a unit

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

// A result that genuinely cannot fit must report ErrAmountOverflow -- and must
// not panic. This case is specifically the one that makes a naive
// math/bits.Div64 blow up: Div64 panics rather than erroring when the quotient
// would not fit in 64 bits, so the implementation has to check before dividing.
// A panic in a monetary path is worse than the overflow it replaces.
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

// A unit price is a price: negative is not a meaningful one, and accepting it
// would let a holding contribute a negative market value, which is a liability,
// which is what an account type is for. Fail closed on a value this package did
// not construct.
func TestValueRefusesANegativeUnitPrice(t *testing.T) {
	q, _ := domain.NewQuantity(domain.QuantityScale)
	price, _ := domain.NewMoney(-1, "SGD")

	if _, err := q.Value(price); !errors.Is(err, domain.ErrInvalidMoney) {
		t.Fatalf("Value error = %v, want ErrInvalidMoney", err)
	}
}
