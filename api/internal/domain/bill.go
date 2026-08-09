package domain

import (
	"fmt"
	"time"
)

// Cadence is how often a bill repeats. It arrives from a database column and
// from a request body, so ParseCadence refuses anything else. A fifth cadence
// needs a migration as well as a case here -- both layers refusing an unknown
// value is the house pattern (transactions.kind, goal_contributions.source).
type Cadence string

const (
	CadenceOneOff    Cadence = "one_off"
	CadenceMonthly   Cadence = "monthly"
	CadenceQuarterly Cadence = "quarterly"
	CadenceYearly    Cadence = "yearly"
)

func ParseCadence(s string) (Cadence, error) {
	switch Cadence(s) {
	case CadenceOneOff:
		return CadenceOneOff, nil
	case CadenceMonthly:
		return CadenceMonthly, nil
	case CadenceQuarterly:
		return CadenceQuarterly, nil
	case CadenceYearly:
		return CadenceYearly, nil
	default:
		return "", fmt.Errorf("%w: %q", ErrUnknownCadence, s)
	}
}

// MonthsPerPeriod is how many calendar months one period spans. A one-off has
// no period at all and returns 0, which is why NextDue refuses it rather than
// advancing by nothing and returning the same date forever.
func (c Cadence) MonthsPerPeriod() int {
	switch c {
	case CadenceMonthly:
		return 1
	case CadenceQuarterly:
		return 3
	case CadenceYearly:
		return 12
	default:
		return 0
	}
}

// PeriodsPerYear is how many times a cadence recurs in a year. Used only for
// the subscriptions rollup, which multiplies rather than divides.
func (c Cadence) PeriodsPerYear() int {
	switch c {
	case CadenceMonthly:
		return 12
	case CadenceQuarterly:
		return 4
	case CadenceYearly:
		return 1
	default:
		return 0
	}
}

type Bill struct {
	ID                 string
	HouseholdID        string
	Name               string
	Amount             Money
	Cadence            Cadence
	NextDue            *time.Time // nil only for a settled one-off
	DueAnchorDay       int
	CategoryID         string // "" when uncategorised, the ports.go NULL convention
	PayFromAccountID   string
	PaidByMembershipID string // "" when unattributed
	Autopay            bool
	IsSubscription     bool
	ArchivedAt         *time.Time
}

func (b Bill) IsArchived() bool { return b.ArchivedAt != nil }

type BillPayment struct {
	ID            string
	BillID        string
	HouseholdID   string
	DueOn         time.Time
	PaidOn        time.Time
	Amount        Money
	TransactionID string // "" once the ledger row has been deleted
}

// NextDue advances a due date by one period of the cadence, clamping to the
// last day of the destination month.
//
// The clamp is why this function exists. Go's time.Time.AddDate(0, 1, 0) on
// 31 January returns 3 March -- it normalises "31 February" forward instead of
// refusing it -- so a bill due on the 31st would walk off the end of every
// short month. 31 Jan -> 28 Feb (29 in a leap year) -> 31 Mar is what a
// household means.
//
// anchorDay, not from.Day(), is what gets clamped. Clamping the clamped value
// is one-way: 31 Jan lands on 28 Feb, and advancing from 28 would give 28
// March, so a bill due on the 31st would silently become a bill due on the
// 28th forever after its first February.
//
// It advances from `from`, never from today: a bill paid three days late must
// not shift its due date three days every month.
//
// ok is false for a one-off, which has no next date at all.
func NextDue(c Cadence, from time.Time, anchorDay int) (time.Time, bool) {
	months := c.MonthsPerPeriod()
	if months == 0 {
		return time.Time{}, false
	}
	// Year/Month/Day report components in from's own Location, not UTC.
	// Converting first means the calendar date read below is the same one
	// UTC would show, regardless of what zone the caller built from in.
	from = from.UTC()
	// Move to the first of the destination month, so the day never
	// participates in the month arithmetic and cannot overflow it.
	first := time.Date(from.Year(), from.Month(), 1, 0, 0, 0, 0, time.UTC).AddDate(0, months, 0)
	day := anchorDay
	if last := lastDayOf(first.Year(), first.Month()); day > last {
		day = last
	}
	return time.Date(first.Year(), first.Month(), day, 0, 0, 0, 0, time.UTC), true
}

// lastDayOf is the zeroth day of the following month, which time.Date
// normalises backwards to the previous month's last day. This is the one place
// that normalisation is exactly what is wanted.
func lastDayOf(year int, month time.Month) int {
	return time.Date(year, month+1, 0, 0, 0, 0, 0, time.UTC).Day()
}

// IsOverdue reports whether an unpaid due date has passed. A bill due today is
// not overdue: the household has the whole day to pay it.
func IsOverdue(nextDue, today time.Time) bool {
	return startOfDay(nextDue).Before(startOfDay(today))
}

func startOfDay(t time.Time) time.Time {
	// Same reason as NextDue's from.UTC(): Year/Month/Day read t's own
	// Location, and wrapping the result in time.UTC only labels it, it
	// never converts the input.
	t = t.UTC()
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
}

// AnnualEquivalentMinor is what a bill costs in a year at its cadence. It only
// ever multiplies: the subscriptions panel divides exactly once, at the very
// end, when it turns the annual total into a monthly one. ok is false for a
// one-off, which is not a recurring cost and is excluded from the rollup.
func AnnualEquivalentMinor(c Cadence, amountMinor int64) (int64, bool) {
	periods := c.PeriodsPerYear()
	if periods == 0 {
		return 0, false
	}
	return amountMinor * int64(periods), true
}
