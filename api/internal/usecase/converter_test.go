package usecase_test

import (
	"context"
	"errors"
	"testing"

	"github.com/andreasoentoro/hearth/api/internal/domain"
	"github.com/andreasoentoro/hearth/api/internal/usecase"
)

var errProviderDown = errors.New("rate provider unreachable")

// A single-currency household must keep working while the provider is down,
// so a primary-currency amount never asks it.
func TestConverterNeverAsksTheProviderForAPrimaryAmount(t *testing.T) {
	fx := newFXDouble().failWith(errProviderDown)
	c := usecase.NewConverter(fx, "SGD")

	got, err := c.Convert(context.Background(), domain.Money{Amount: 824_055, Currency: "SGD"})
	if err != nil {
		t.Fatalf("Convert: %v", err)
	}
	if got != (domain.Money{Amount: 824_055, Currency: "SGD"}) {
		t.Fatalf("Convert = %+v, want the amount unchanged", got)
	}
	if fx.calls != 0 {
		t.Fatalf("provider asked %d times, want 0", fx.calls)
	}
}

func TestConverterConvertsAtTheProvidersRate(t *testing.T) {
	c := usecase.NewConverter(newFXDouble(), "SGD")

	got, err := c.Convert(context.Background(), domain.Money{Amount: 124_100_000, Currency: "IDR"})
	if err != nil {
		t.Fatalf("Convert: %v", err)
	}
	if got != (domain.Money{Amount: 10_000, Currency: "SGD"}) {
		t.Fatalf("Convert = %+v, want S$100.00", got)
	}
}

// One rate per currency per request: two figures on one screen can never be
// converted at two different rates.
func TestConverterAsksOncePerCurrencyPerRequest(t *testing.T) {
	fx := newFXDouble()
	c := usecase.NewConverter(fx, "SGD")
	ctx := context.Background()

	for _, amount := range []int64{124_100_000, 12_410} {
		if _, err := c.Convert(ctx, domain.Money{Amount: amount, Currency: "IDR"}); err != nil {
			t.Fatalf("Convert(%d): %v", amount, err)
		}
	}
	if fx.calls != 1 {
		t.Fatalf("provider asked %d times, want 1", fx.calls)
	}
}

func TestConverterReportsNoRateAsErrNoRate(t *testing.T) {
	c := usecase.NewConverter(newFXDouble(), "SGD")

	_, err := c.Convert(context.Background(), domain.Money{Amount: 500, Currency: "EUR"})
	if !errors.Is(err, domain.ErrNoRate) {
		t.Fatalf("Convert(EUR) error = %v, want domain.ErrNoRate", err)
	}
}

// A failed lookup is not "no rate". The caller must be able to tell the two
// apart, or an outage would render as a smaller total.
func TestConverterPassesAFailedLookupThroughAsSomethingOtherThanNoRate(t *testing.T) {
	c := usecase.NewConverter(newFXDouble().failWith(errProviderDown), "SGD")

	_, err := c.Convert(context.Background(), domain.Money{Amount: 12_410, Currency: "IDR"})
	if !errors.Is(err, errProviderDown) {
		t.Fatalf("Convert error = %v, want the provider's own error", err)
	}
	if errors.Is(err, domain.ErrNoRate) {
		t.Fatal("a failed lookup must not read as ErrNoRate")
	}
}

// A failure is not remembered, so the next conversion in the same request
// asks again rather than repeating a stale answer.
func TestConverterDoesNotRememberAFailedLookup(t *testing.T) {
	fx := newFXDouble().failWith(errProviderDown)
	c := usecase.NewConverter(fx, "SGD")
	ctx := context.Background()
	idr := domain.Money{Amount: 12_410, Currency: "IDR"}

	if _, err := c.Convert(ctx, idr); err == nil {
		t.Fatal("first Convert: want the provider's error")
	}
	fx.fail = nil
	if _, err := c.Convert(ctx, idr); err != nil {
		t.Fatalf("second Convert: %v", err)
	}
	if fx.calls != 2 {
		t.Fatalf("provider asked %d times, want 2", fx.calls)
	}
}
