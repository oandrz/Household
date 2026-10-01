package domain

import (
	"fmt"
	"time"
)

// PeriodKind is the length of a reporting period: calendar quarters,
// half-years or the calendar year, never a month (too short to read a
// portfolio in) or anything custom (a date-picker feature of its own).
type PeriodKind string

const (
	PeriodQuarter PeriodKind = "quarter"
	PeriodHalf    PeriodKind = "half"
	PeriodYear    PeriodKind = "year"
)

// periodsPerYear is how many of each kind fit in a year, which is also the
// largest legal index. Keeping it in one map is what stops Start, End and
// NewPeriod each carrying their own copy of "a quarter is three months".
var periodsPerYear = map[PeriodKind]int{
	PeriodQuarter: 4,
	PeriodHalf:    2,
	PeriodYear:    1,
}

// ParsePeriodKind refuses anything it does not recognise, the same way
// ParseInstrumentKind does and for the same reason: this value arrives from a
// query string, and an unrecognised one is refused rather than carried.
func ParsePeriodKind(s string) (PeriodKind, error) {
	switch PeriodKind(s) {
	case PeriodQuarter:
		return PeriodQuarter, nil
	case PeriodHalf:
		return PeriodHalf, nil
	case PeriodYear:
		return PeriodYear, nil
	default:
		return "", fmt.Errorf("%w: %q", ErrUnknownPeriodKind, s)
	}
}

// Period is one reporting window: Q3 2026, H1 2025, 2024.
//
// Fields are unexported so an impossible period -- a fifth quarter, a
// zeroth half -- can't be constructed, the same reason Quantity hides its
// nano field. Build via NewPeriod or PeriodContaining; read via Kind, Year,
// Index, Start, End and Label.
//
// index is 1-based and always 1 for a year, so that one shape covers all
// three kinds and no caller has to special-case the year.
type Period struct {
	kind  PeriodKind
	year  int
	index int
}

// NewPeriod refuses a kind it does not know and an index that does not exist
// for that kind. There is no other way to build a Period with a body, so
// every Period that reaches arithmetic has already passed this.
func NewPeriod(kind PeriodKind, year, index int) (Period, error) {
	if _, err := ParsePeriodKind(string(kind)); err != nil {
		return Period{}, err
	}
	if index < 1 || index > periodsPerYear[kind] {
		return Period{}, fmt.Errorf("%w: %s %d of %d", ErrPeriodIndexOutOfRange, kind, index, periodsPerYear[kind])
	}
	return Period{kind: kind, year: year, index: index}, nil
}

func (p Period) Kind() PeriodKind { return p.kind }
func (p Period) Year() int        { return p.year }
func (p Period) Index() int       { return p.index }

// monthsEach is how many months one period of this kind spans.
func (p Period) monthsEach() int { return 12 / periodsPerYear[p.kind] }

// Start is the first day of the period, stamped midnight UTC.
//
// A period boundary is a calendar date, not an instant, and "midnight UTC"
// is only how a date is written down here, the same shape usecase/budget.go's
// startOfMonth and TodayIn use. Which period is the CURRENT one depends on
// the household's time zone, and that is decided before this type is reached:
// callers pass the household's day (TodayIn). The boundary must never depend
// on who opened the page: a caller-local boundary would put the same trade
// in different quarters for two members of one household.
func (p Period) Start() time.Time {
	firstMonth := time.Month((p.index-1)*p.monthsEach() + 1)
	return time.Date(p.year, firstMonth, 1, 0, 0, 0, 0, time.UTC)
}

// End is the LAST DAY of the period, inclusive, at midnight UTC.
//
// Every figure in the report is measured over [Start, End], and the last day
// of a quarter is the day a household is most likely to have recorded a
// closing price on. Computed by stepping the period's whole months forward
// and a day back, never by adding 90 days -- February is why.
func (p Period) End() time.Time {
	return p.Start().AddDate(0, p.monthsEach(), 0).AddDate(0, 0, -1)
}

// Previous is the period immediately before this one, of the same kind,
// supplying a period's OPENING value: a quarter opens at what the previous
// quarter closed on, chaining with no special case for a year's first day.
// It returns an error it should never produce, rather than fall back to
// THIS period, which would silently measure the period against itself -- a
// wrong figure that looks right, in a monetary path.
func (p Period) Previous() (Period, error) {
	index, year := p.index-1, p.year
	if index < 1 {
		index = periodsPerYear[p.kind]
		year--
	}
	return NewPeriod(p.kind, year, index)
}

// Contains judges the DAY, not the instant: 23:00 in Singapore on 30 June is
// 15:00 UTC the same day, and both are the last day of H1, though comparing
// instants would put it in H2 for anyone who typed after 08:00 local.
// Truncating first makes the answer independent of who is asking.
func (p Period) Contains(t time.Time) bool {
	day := startOfDayUTC(t)
	return !day.Before(p.Start()) && !day.After(p.End())
}

// IsCurrent says whether the household is still living in this period, which
// is what lets a screen label it "to date" rather than presenting a
// half-finished quarter as a closed one. today is the household's calendar
// day (TodayIn), not the server's clock: on the first morning of a quarter
// east of Greenwich the two are in different quarters.
func (p Period) IsCurrent(today time.Time) bool { return p.Contains(today) }

// Label is the period as the owner would say it out loud.
func (p Period) Label() string {
	switch p.kind {
	case PeriodQuarter:
		return fmt.Sprintf("Q%d %d", p.index, p.year)
	case PeriodHalf:
		return fmt.Sprintf("H%d %d", p.index, p.year)
	default:
		return fmt.Sprintf("%d", p.year)
	}
}

// PeriodContaining is the period of this kind that the given day falls in.
func PeriodContaining(kind PeriodKind, t time.Time) (Period, error) {
	if _, err := ParsePeriodKind(string(kind)); err != nil {
		return Period{}, err
	}
	day := startOfDayUTC(t)
	months := 12 / periodsPerYear[kind]
	index := (int(day.Month())-1)/months + 1
	return NewPeriod(kind, day.Year(), index)
}

// PeriodsEndingOn is the series the report draws: `count` consecutive periods
// of this kind, OLDEST FIRST, ending with the one `today` falls in.
//
// It ends with the current period, not the last closed one, because the
// owner's first question is "how am I doing now": a closed-only report would
// stay empty until a quarter ended, unjudgeable for months.
//
// Walking backwards decrements the year when the index runs below one, which
// is the step that a loop doing index-- alone gets wrong once a year.
func PeriodsEndingOn(kind PeriodKind, today time.Time, count int) ([]Period, error) {
	if count < 1 {
		return nil, fmt.Errorf("%w: %d", ErrPeriodCountOutOfRange, count)
	}
	current, err := PeriodContaining(kind, today)
	if err != nil {
		return nil, err
	}
	perYear := periodsPerYear[kind]

	out := make([]Period, count)
	year, index := current.year, current.index
	for i := count - 1; i >= 0; i-- {
		p, err := NewPeriod(kind, year, index)
		if err != nil {
			return nil, err
		}
		out[i] = p
		index--
		if index < 1 {
			index = perYear
			year--
		}
	}
	return out, nil
}

// startOfDayUTC drops the clock time, leaving the calendar day the caller was
// looking at, stamped midnight UTC. Every period comparison goes through it so
// that there is one place where "which day is this" is decided.
//
// It reads Date() in the value's OWN location, never converting to UTC
// first -- that would move 00:30 on 1 January in Singapore back to 31
// December, filing a trade in the wrong year for the eight hours a day this
// household is ahead of UTC (docs/LEARNING.md pattern 1, seen six times).
// usecase/budget.go's startOfMonth reads the same way, for the same reason.
func startOfDayUTC(t time.Time) time.Time {
	year, month, day := t.Date()
	return time.Date(year, month, day, 0, 0, 0, 0, time.UTC)
}
