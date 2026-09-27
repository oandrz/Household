package fx_test

import (
	"context"
	"errors"
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

// The port's contract: a pair the provider does not cover is ErrNoRate, and
// only that. It is what lets a screen leave an amount out instead of failing.
func TestStaticProviderAnswersAnUnknownPairWithErrNoRate(t *testing.T) {
	p := fx.NewStaticProvider()

	_, err := p.Rate(context.Background(), "SGD", "JPY")
	if !errors.Is(err, domain.ErrNoRate) {
		t.Fatalf("Rate(SGD, JPY) error = %v, want domain.ErrNoRate", err)
	}
}
