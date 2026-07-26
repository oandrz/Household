package fx_test

import (
	"context"
	"errors"
	"math"
	"testing"

	"github.com/andreasoentoro/hearth/api/internal/adapter/fx"
	"github.com/andreasoentoro/hearth/api/internal/domain"
)

func TestStaticProviderKnowsTheDesignsRate(t *testing.T) {
	p := fx.NewStaticProvider()

	rate, err := p.Rate(context.Background(), "SGD", "IDR")
	if err != nil {
		t.Fatalf("Rate: %v", err)
	}
	if rate.Numerator != 12_410 || rate.Denominator != 1 {
		t.Fatalf("rate = %+v, want {12410, 1} (S$1 = Rp 12,410)", rate)
	}
}

func TestStaticProviderInvertsExactly(t *testing.T) {
	p := fx.NewStaticProvider()

	rate, err := p.Rate(context.Background(), "IDR", "SGD")
	if err != nil {
		t.Fatalf("Rate: %v", err)
	}
	if rate.Numerator != 1 || rate.Denominator != 12_410 {
		t.Fatalf("rate = %+v, want {1, 12410}", rate)
	}

	// The design's Finances screen: Rp 85,400,000 shown as approximately
	// S$6,880. In minor units that is 8_540_000_000 IDR.
	// 8_540_000_000 / 12_410 = 688_154.7…, which rounds to 688_155 → S$6,881.55.
	got, err := rate.Apply(8_540_000_000)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if got != 688_155 {
		t.Fatalf("Apply = %d, want 688155", got)
	}
}

func TestStaticProviderReturnsUnityForTheSameCurrency(t *testing.T) {
	p := fx.NewStaticProvider()

	rate, err := p.Rate(context.Background(), "SGD", "SGD")
	if err != nil {
		t.Fatalf("Rate: %v", err)
	}
	got, err := rate.Apply(1234)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if got != 1234 {
		t.Fatalf("a same-currency rate must be the identity, got %d", got)
	}
}

func TestStaticProviderRejectsAnUnknownPair(t *testing.T) {
	p := fx.NewStaticProvider()

	if _, err := p.Rate(context.Background(), "SGD", "JPY"); err == nil {
		t.Fatal("expected an error for a pair the static table does not cover")
	}
}

func TestApplyConvertsAnOrdinaryAmount(t *testing.T) {
	p := fx.NewStaticProvider()

	rate, err := p.Rate(context.Background(), "SGD", "IDR")
	if err != nil {
		t.Fatalf("Rate: %v", err)
	}

	// S$100.00 (10_000 minor units) at 12,410 should be exactly Rp 1,241,000.00.
	got, err := rate.Apply(10_000)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if want := int64(10_000 * 12_410); got != want {
		t.Fatalf("Apply = %d, want %d", got, want)
	}
}

func TestApplyReturnsErrAmountOverflowOnOverflow(t *testing.T) {
	p := fx.NewStaticProvider()

	rate, err := p.Rate(context.Background(), "SGD", "IDR")
	if err != nil {
		t.Fatalf("Rate: %v", err)
	}

	// math.MaxInt64 / 12_410 is about 743 billion minor units; the design's
	// households never approach that, but Apply is a general-purpose
	// conversion on a port a live rate provider will feed later, and it must
	// refuse to wrap rather than hand back a silently negative amount.
	if _, err := rate.Apply(math.MaxInt64); !errors.Is(err, domain.ErrAmountOverflow) {
		t.Fatalf("Apply(math.MaxInt64) error = %v, want domain.ErrAmountOverflow", err)
	}
}
