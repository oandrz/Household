package httpadapter

import (
	"context"
	"fmt"
	"time"

	"github.com/andreasoentoro/hearth/api/internal/domain"
)

// householdToday is the calendar day it is, at instant now, in the zone the
// household keeps its calendar in. Both auth middlewares call it once per
// request and store the answer on Scope.Today, so every handler in one
// request agrees on the date, and a session and a token agree with each
// other.
//
// The household is read on every request rather than cached, for the reason
// the flags are: an owner who changes the zone in Settings must see the new
// date on the next request, and one indexed read is cheap on one box. If a
// measurement ever says otherwise, carry the zone on the membership query.
//
// An error here is never a domain answer. A household whose zone cannot be
// loaded has no "today", and a caller must not turn domain.ErrNotFound from
// this read into a 404, which on an authenticated route means "this does not
// exist". Callers answer it with logAndWriteInternal.
func householdToday(ctx context.Context, deps Deps, householdID string, now time.Time) (time.Time, error) {
	household, err := deps.Households.Get(ctx, householdID)
	if err != nil {
		return time.Time{}, fmt.Errorf("load the household to work out its day: %w", err)
	}
	today, err := domain.TodayIn(now, household.Timezone)
	if err != nil {
		return time.Time{}, fmt.Errorf("household %s: %w", householdID, err)
	}
	return today, nil
}
