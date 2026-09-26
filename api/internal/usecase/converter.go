package usecase

import (
	"context"
	"errors"

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
// Remembering each answer for the request -- a rate, or "no rate" -- is
// load-bearing, not an optimisation. Within one Converter, every amount in
// the same currency gets the same answer, so a list cannot exclude a currency
// as "no rate" in one figure and add it into another, and two amounts in one
// currency are never converted at two rates. That holds per Converter only:
// two services, or two helpers that each build their own, can each ask once.
type Converter struct {
	fx      FXRateProvider
	primary string
	rates   map[string]domain.Rate
	noRate  map[string]error // the provider's ErrNoRate answer, per currency
}

// NewConverter returns a Converter into primary, backed by fx.
func NewConverter(fx FXRateProvider, primary string) *Converter {
	return &Converter{fx: fx, primary: primary, rates: map[string]domain.Rate{}, noRate: map[string]error{}}
}

// Convert returns m in the primary currency. An amount already in primary is
// returned unchanged without asking the provider. That is exact, and it means
// a single-currency household never depends on a rate provider being up.
//
// Most callers want TryConvert, which turns "no rate" into a value. Call
// Convert directly only when "no rate" must fail too, as the net-worth trend
// does.
//
// An error wrapping domain.ErrNoRate means m's currency has no rate. That is
// the ONLY error a caller may answer by leaving m out of a total (CONTEXT.md,
// "No rate"). Anything else (a failed lookup, domain.ErrInvalidRate,
// domain.ErrAmountOverflow) must fail the request. "No rate" is remembered
// like a rate; a failed lookup is not, so the next Convert asks again.
func (c *Converter) Convert(ctx context.Context, m domain.Money) (domain.Money, error) {
	if m.Currency == c.primary {
		return m, nil
	}
	if err, ok := c.noRate[m.Currency]; ok {
		return domain.Money{}, err
	}
	rate, ok := c.rates[m.Currency]
	if !ok {
		var err error
		rate, err = c.fx.Rate(ctx, m.Currency, c.primary)
		if errors.Is(err, domain.ErrNoRate) {
			c.noRate[m.Currency] = err
			return domain.Money{}, err
		}
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

// TryConvert is Convert for a caller that leaves "no rate" amounts out of a
// total, which is every caller except the net-worth trend. hasRate is false,
// with a nil error, when m's currency has no rate: exclude m and carry on. Any
// error it returns must fail the request.
//
// It exists so that "exclude only on domain.ErrNoRate" is written once, here.
// A caller that wrote `if err != nil { exclude }` around Convert would bring
// back the defect where a provider outage rendered as a smaller total with
// "no rate" beside it (docs/LEARNING.md, pattern 5).
func (c *Converter) TryConvert(ctx context.Context, m domain.Money) (converted domain.Money, hasRate bool, err error) {
	converted, err = c.Convert(ctx, m)
	if errors.Is(err, domain.ErrNoRate) {
		return domain.Money{}, false, nil
	}
	if err != nil {
		return domain.Money{}, false, err
	}
	return converted, true, nil
}
