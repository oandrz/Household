package domain_test

import (
	"errors"
	"math"
	"testing"

	"github.com/andreasoentoro/hearth/api/internal/domain"
)

func TestRateApplyRoundsHalfAwayFromZero(t *testing.T) {
	cases := []struct {
		name string
		rate domain.Rate
		in   int64
		want int64
	}{
		{"SGD to IDR, whole multiple", domain.Rate{Numerator: 12_410, Denominator: 1}, 10_000, 124_100_000},
		{"IDR to SGD, exact", domain.Rate{Numerator: 1, Denominator: 12_410}, 124_100_000, 10_000},
		{"zero stays zero", domain.Rate{Numerator: 1, Denominator: 12_410}, 0, 0},
		{"exact half rounds up", domain.Rate{Numerator: 1, Denominator: 2}, 5, 3},
		{"below half rounds down", domain.Rate{Numerator: 1, Denominator: 3}, 4, 1},
		{"above half rounds up", domain.Rate{Numerator: 1, Denominator: 3}, 5, 2},
		// A credit card or loan balance is negative. "Away from zero" must
		// mean the same distance from zero on both sides, never "towards
		// minus infinity".
		{"negative exact half rounds away from zero", domain.Rate{Numerator: 1, Denominator: 2}, -5, -3},
		{"negative below half", domain.Rate{Numerator: 1, Denominator: 3}, -4, -1},
		{"negative above half", domain.Rate{Numerator: 1, Denominator: 3}, -5, -2},
		{"most negative amount at the identity rate", domain.Rate{Numerator: 1, Denominator: 1}, math.MinInt64, math.MinInt64},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := tc.rate.Apply(tc.in)
			if err != nil {
				t.Fatalf("Apply(%d): %v", tc.in, err)
			}
			if got != tc.want {
				t.Fatalf("Apply(%d) = %d, want %d", tc.in, got, tc.want)
			}
		})
	}
}

func TestRateApplyRefusesAnAnswerTooBigForInt64(t *testing.T) {
	cases := []struct {
		name string
		rate domain.Rate
		in   int64
	}{
		{"largest amount into IDR", domain.Rate{Numerator: 12_410, Denominator: 1}, math.MaxInt64},
		{"most negative amount doubled", domain.Rate{Numerator: 2, Denominator: 1}, math.MinInt64},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := tc.rate.Apply(tc.in); !errors.Is(err, domain.ErrAmountOverflow) {
				t.Fatalf("Apply(%d) error = %v, want domain.ErrAmountOverflow", tc.in, err)
			}
		})
	}
}

// The old usecase.Rate.Apply multiplied in 64 bits and refused whenever the
// intermediate product overflowed, even when the answer fitted. The product
// is now taken in 128 bits, so only the answer can be refused. This is the one
// deliberate behaviour change in the move.
func TestRateApplyAcceptsAnAmountWhoseAnswerFitsEvenWhenTheProductDoesNot(t *testing.T) {
	rate := domain.Rate{Numerator: 3, Denominator: 2}
	in := int64(math.MaxInt64 / 2) // 4_611_686_018_427_387_903; times 3 overflows int64

	got, err := rate.Apply(in)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	// 4_611_686_018_427_387_903 * 3 / 2 = 6_917_529_027_641_081_854.5, rounded away from zero.
	if want := int64(6_917_529_027_641_081_855); got != want {
		t.Fatalf("Apply = %d, want %d", got, want)
	}
}

// A rate arrives from a provider this code did not construct, so it fails
// closed. A zero denominator used to divide by zero and panic.
func TestRateApplyRefusesARateThatIsNotPositive(t *testing.T) {
	for _, r := range []domain.Rate{
		{Numerator: 0, Denominator: 1},
		{Numerator: 1, Denominator: 0},
		{Numerator: -1, Denominator: 1},
		{Numerator: 1, Denominator: -1},
	} {
		if _, err := r.Apply(100); !errors.Is(err, domain.ErrInvalidRate) {
			t.Fatalf("%+v.Apply error = %v, want domain.ErrInvalidRate", r, err)
		}
	}
}
