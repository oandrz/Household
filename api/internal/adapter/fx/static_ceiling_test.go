package fx

import (
	"context"
	"testing"

	"github.com/andreasoentoro/hearth/api/internal/domain"
)

// domain.MaxAmountMinor was chosen so that the largest amount a person may
// enter still fits in an int64 after conversion. That promise depends on the
// rates in this table, so it is checked here, against every pair in both
// directions: adding a rate large enough to break it fails this test rather
// than a household's net worth.
//
// An internal test (package fx, not fx_test) because it has to read the
// table itself; naming the pairs again here would miss the one added later.
func TestTheLargestAmountAPersonMayEnterConvertsAtEveryRateInTheTable(t *testing.T) {
	p := NewStaticProvider()
	if len(p.units) == 0 {
		t.Fatal("the rate table is empty, so this test would check nothing")
	}
	for pair := range p.units {
		for _, direction := range [][2]string{{pair[0], pair[1]}, {pair[1], pair[0]}} {
			from, to := direction[0], direction[1]
			rate, err := p.Rate(context.Background(), from, to)
			if err != nil {
				t.Fatalf("Rate(%s, %s): %v", from, to, err)
			}
			if _, err := rate.Apply(domain.MaxAmountMinor); err != nil {
				t.Errorf("%s to %s: converting domain.MaxAmountMinor failed: %v", from, to, err)
			}
		}
	}
}
