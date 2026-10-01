package usecase

import (
	"fmt"
	"time"

	"github.com/andreasoentoro/hearth/api/internal/domain"
)

// One rule for every recorded fact: it may not be dated after the
// household's today (ADR 12). A purchase, sale, price, income row,
// transaction, bill payment and goal contribution all go through
// refuseFutureDate. A plan (a bill's next due date, a goal's target month, a
// budget month) does not: a plan is about the future.

// refuseFutureDate refuses a fact dated after the household's today with
// domain.ErrDateInFuture. today is the household's calendar day, which the
// caller works out from its time zone (domain.TodayIn); the comparison is on
// calendar days (domain.IsAfterDay), so today itself is always allowed.
//
// On an edit, call it only when the date changed (sameDay). A stored date
// can be after today without anyone having typed a future date: the row was
// written before this rule existed, or the household's zone was moved west
// afterwards. Changing that row's description must not be refused over a
// date nobody touched.
func refuseFutureDate(date, today time.Time) error {
	if domain.IsAfterDay(date, today) {
		return fmt.Errorf("%w: %s", domain.ErrDateInFuture, date.Format(time.DateOnly))
	}
	return nil
}

// sameDay reports whether two values name the same calendar day.
func sameDay(a, b time.Time) bool {
	return !domain.IsAfterDay(a, b) && !domain.IsAfterDay(b, a)
}
