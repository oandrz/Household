package usecase

import (
	"context"

	"github.com/andreasoentoro/hearth/api/internal/domain"
)

// Converter turns amounts into one household's primary currency for the
// length of one request. Build a new one per request with NewConverter. Never
// keep one between requests, or a changed rate would never be seen.
//
// Why one type rather than a convert method on each service: the algorithm
// (skip the provider when the amount is already primary, look the rate up,
// apply it, remember it) has no reason to differ between screens, and it
// drifted when each service had its own copy; only net worth remembered its
// rates. Each service still declares FXRateProvider in its own Deps, so no
// service gains a reason to change when another's FX needs do. Only the
// algorithm is shared. Do not copy it back into the services.
//
// Remembering each rate for the request is load-bearing, not an
// optimisation. Every figure on one screen must use the same rate for the same
// currency, or, the day a live provider returns two different rates within one
// request, a headline and the chart beneath it would disagree.
type Converter struct {
	fx      FXRateProvider
	primary string
	rates   map[string]domain.Rate
}

// NewConverter returns a Converter into primary, backed by fx.
func NewConverter(fx FXRateProvider, primary string) *Converter {
	return &Converter{fx: fx, primary: primary, rates: map[string]domain.Rate{}}
}

// Convert returns m in the primary currency. An amount already in primary is
// returned unchanged without asking the provider. That is exact, and it means
// a single-currency household never depends on a rate provider being up.
//
// An error wrapping domain.ErrNoRate means m's currency has no rate. That is
// the ONLY error a caller may answer by leaving m out of a total (CONTEXT.md,
// "No rate"). Anything else (a failed lookup, domain.ErrInvalidRate,
// domain.ErrAmountOverflow) must fail the request. A failed lookup is not
// remembered, so the next Convert asks again.
func (c *Converter) Convert(ctx context.Context, m domain.Money) (domain.Money, error) {
	if m.Currency == c.primary {
		return m, nil
	}
	rate, ok := c.rates[m.Currency]
	if !ok {
		var err error
		rate, err = c.fx.Rate(ctx, m.Currency, c.primary)
		if err != nil {
			return domain.Money{}, err
		}
		c.rates[m.Currency] = rate
	}
	amount, err := rate.Apply(m.Amount)
	if err != nil {
		return domain.Money{}, err
	}
	return domain.Money{Amount: amount, Currency: c.primary}, nil
}
