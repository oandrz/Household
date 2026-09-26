// Package fx implements the FXRateProvider port. Today the table is fixed; when
// a live source arrives it becomes another implementation of the same port.
package fx

import (
	"context"
	"fmt"

	"github.com/andreasoentoro/hearth/api/internal/domain"
	"github.com/andreasoentoro/hearth/api/internal/usecase"
)

type StaticProvider struct {
	// units maps a currency pair to how many units of the second currency one
	// unit of the first buys. Stored one way only; the inverse is exact because
	// a Rate is a fraction.
	units map[[2]string]int64
}

// The adapter honours the whole port contract, ErrNoRate included.
var _ usecase.FXRateProvider = (*StaticProvider)(nil)

func NewStaticProvider() *StaticProvider {
	return &StaticProvider{units: map[[2]string]int64{
		{"SGD", "IDR"}: 12_410, // per the design's Settings screen
	}}
}

func (p *StaticProvider) Rate(_ context.Context, from, to string) (domain.Rate, error) {
	if from == to {
		return domain.Rate{Numerator: 1, Denominator: 1}, nil
	}
	if n, ok := p.units[[2]string{from, to}]; ok {
		return domain.Rate{Numerator: n, Denominator: 1}, nil
	}
	if n, ok := p.units[[2]string{to, from}]; ok {
		return domain.Rate{Numerator: 1, Denominator: n}, nil
	}
	return domain.Rate{}, fmt.Errorf("%w: %s to %s", domain.ErrNoRate, from, to)
}
