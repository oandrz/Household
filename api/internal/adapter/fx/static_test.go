package fx_test

import (
	"context"
	"testing"

	"github.com/andreasoentoro/hearth/api/internal/adapter/fx"
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
	if got := rate.Apply(8_540_000_000); got != 688_155 {
		t.Fatalf("Apply = %d, want 688155", got)
	}
}

func TestStaticProviderReturnsUnityForTheSameCurrency(t *testing.T) {
	p := fx.NewStaticProvider()

	rate, err := p.Rate(context.Background(), "SGD", "SGD")
	if err != nil {
		t.Fatalf("Rate: %v", err)
	}
	if rate.Apply(1234) != 1234 {
		t.Fatalf("a same-currency rate must be the identity, got %+v", rate)
	}
}

func TestStaticProviderRejectsAnUnknownPair(t *testing.T) {
	p := fx.NewStaticProvider()

	if _, err := p.Rate(context.Background(), "SGD", "JPY"); err == nil {
		t.Fatal("expected an error for a pair the static table does not cover")
	}
}
