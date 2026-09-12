package domain

import (
	"fmt"
	"time"
)

// PeriodKind is the length of a reporting period. The three the PRD names --
// calendar quarters, calendar half-years and the calendar year -- and no
// others: a month is too short to read a portfolio in, and anything custom is
// a different feature with a date picker in it.
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
// Its fields are unexported so that an impossible period -- a fifth quarter, a
// zeroth half -- cannot be constructed at all, which is the same reason
// Quantity hides its nano field. Build one with NewPeriod or
// PeriodContaining; read it with Kind, Year, Index, Start, End and Label.
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

// Start is the first day of the period, at midnight UTC.
//
// UTC is the same normalisation budget.go's startOfMonth applies, and for the
// same reason: no household in Hearth stores a timezone
// (usecase/account.go:165, transaction_repo.go:318 both record this), so a
// period boundary that depended on the caller's location would put the same
// trade in two different quarters depending on who opened the page.
func (p Period) Start() time.Time {
	firstMonth := time.Month((p.index-1)*p.monthsEach() + 1)
	return time.Date(p.year, firstMonth, 1, 0, 0, 0, 0, time.UTC)
}

// End is the LAST DAY of the period, inclusive, at midnight UTC.
//
// Inclusive, not the first day of the next period: every figure in the report
// is measured over [Start, End], and the last day of a quarter is the day a
// household is most likely to have recorded a closing price on. Computed by
// stepping a whole month count forward and a day back, never by adding 90
// days -- February is why.
func (p Period) End() time.Time {
	return p.Start().AddDate(0, p.monthsEach(), 0).AddDate(0, 0, -1)
}

// Previous is the period of the same kind immediately before this one. It is
// what supplies a period's OPENING value: a quarter opens at the value it
// closed the previous quarter on, so the two chain and no separate rule is
// needed for the first day of a year.
func (p Period) Previous() Period {
	index, year := p.index-1, p.year
	if index < 1 {
		index = periodsPerYear[p.kind]
		year--
	}
	// The index came from a valid Period and is back in range, so this cannot
	// fail; the error is dropped rather than propagated into every caller.
	previous, err := NewPeriod(p.kind, year, index)
	if err != nil {
		return p
	}
	return previous
}

// Contains judges the DAY, not the instant. A timestamp recorded at 23:00 in
// Singapore on 30 June is 15:00 UTC on 30 June, and both are the last day of
// H1; comparing instants would put it in H2 for anyone who typed it after
// 08:00 local. Truncating first is what makes the answer independent of who
// is asking.
func (p Period) Contains(t time.Time) bool {
	day := startOfDayUTC(t)
	return !day.Before(p.Start()) && !day.After(p.End())
}

// IsCurrent says whether the household is still living in this period, which
// is what lets a screen label it "to date" rather than presenting a
// half-finished quarter as a closed one.
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
// It ends with the current period rather than the last closed one because the
// owner's first question is "how am I doing now" -- with one price recorded,
// a closed-periods-only report would be empty until a quarter ended, and the
// feature could not be judged for three months.
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
// It reads Date() in the value's OWN location and does NOT convert to UTC
// first. Converting first moves 00:30 on 1 January in Singapore back to 31
// December, which would file a trade in the wrong year for the eight hours a
// day this household is ahead of UTC -- the defect class docs/LEARNING.md
// pattern 1 has now recorded six times. budget.go's startOfMonth reads
// t.Year() and t.Month() the same way and for the same reason.
func startOfDayUTC(t time.Time) time.Time {
	year, month, day := t.Date()
	return time.Date(year, month, day, 0, 0, 0, 0, time.UTC)
}
