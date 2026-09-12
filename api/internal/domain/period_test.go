package domain_test

import (
	"errors"
	"testing"
	"time"

	"github.com/andreasoentoro/hearth/api/internal/domain"
)

// utcDay is the period package's own date helper. `day` and `date` are already
// taken by bill_test.go and budget_test.go, which share this package.
func utcDay(y int, m time.Month, d int) time.Time {
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
}

func mustPeriod(t *testing.T, kind domain.PeriodKind, year, index int) domain.Period {
	t.Helper()
	p, err := domain.NewPeriod(kind, year, index)
	if err != nil {
		t.Fatalf("NewPeriod(%s, %d, %d): %v", kind, year, index, err)
	}
	return p
}

func TestParsePeriodKindAcceptsEveryKindItShips(t *testing.T) {
	for _, want := range []domain.PeriodKind{domain.PeriodQuarter, domain.PeriodHalf, domain.PeriodYear} {
		got, err := domain.ParsePeriodKind(string(want))
		if err != nil || got != want {
			t.Fatalf("ParsePeriodKind(%q) = %q, %v", want, got, err)
		}
	}
}

func TestParsePeriodKindRefusesAnythingElse(t *testing.T) {
	for _, in := range []string{"", "month", "QUARTER", "semester", "q"} {
		if _, err := domain.ParsePeriodKind(in); !errors.Is(err, domain.ErrUnknownPeriodKind) {
			t.Fatalf("ParsePeriodKind(%q) error = %v, want ErrUnknownPeriodKind", in, err)
		}
	}
}

// The four quarters, both halves and the year, each with its first and last
// day written out. These are the numbers every other figure in the report is
// measured between, so they are asserted literally rather than computed -- a
// test that derives the boundary the same way the code does proves nothing.
func TestQuarterStartsAndEndsOnTheCalendarQuarter(t *testing.T) {
	cases := []struct {
		index      int
		start, end time.Time
	}{
		{1, utcDay(2026, time.January, 1), utcDay(2026, time.March, 31)},
		{2, utcDay(2026, time.April, 1), utcDay(2026, time.June, 30)},
		{3, utcDay(2026, time.July, 1), utcDay(2026, time.September, 30)},
		{4, utcDay(2026, time.October, 1), utcDay(2026, time.December, 31)},
	}
	for _, c := range cases {
		p := mustPeriod(t, domain.PeriodQuarter, 2026, c.index)
		if !p.Start().Equal(c.start) {
			t.Errorf("Q%d start = %s, want %s", c.index, p.Start(), c.start)
		}
		if !p.End().Equal(c.end) {
			t.Errorf("Q%d end = %s, want %s", c.index, p.End(), c.end)
		}
	}
}

func TestHalfYearStartsAndEndsOnTheCalendarHalf(t *testing.T) {
	h1 := mustPeriod(t, domain.PeriodHalf, 2026, 1)
	if !h1.Start().Equal(utcDay(2026, time.January, 1)) || !h1.End().Equal(utcDay(2026, time.June, 30)) {
		t.Errorf("H1 = %s..%s", h1.Start(), h1.End())
	}
	h2 := mustPeriod(t, domain.PeriodHalf, 2026, 2)
	if !h2.Start().Equal(utcDay(2026, time.July, 1)) || !h2.End().Equal(utcDay(2026, time.December, 31)) {
		t.Errorf("H2 = %s..%s", h2.Start(), h2.End())
	}
}

func TestYearStartsAndEndsOnTheCalendarYear(t *testing.T) {
	y := mustPeriod(t, domain.PeriodYear, 2026, 1)
	if !y.Start().Equal(utcDay(2026, time.January, 1)) || !y.End().Equal(utcDay(2026, time.December, 31)) {
		t.Errorf("2026 = %s..%s", y.Start(), y.End())
	}
}

// February is where a quarter boundary computed by adding days rather than
// months goes wrong, and it goes wrong only every fourth year.
func TestTheFirstQuarterOfALeapYearStillEndsOnThirtyFirstMarch(t *testing.T) {
	p := mustPeriod(t, domain.PeriodQuarter, 2024, 1)
	if !p.End().Equal(utcDay(2024, time.March, 31)) {
		t.Fatalf("Q1 2024 end = %s, want 2024-03-31", p.End())
	}
	if !p.Contains(utcDay(2024, time.February, 29)) {
		t.Fatal("29 February 2024 should fall inside Q1 2024")
	}
}

// The end is INCLUSIVE. Every figure in the report is measured over
// [Start, End], so an exclusive end would drop the last day of every period --
// and the last day of a quarter is the day a household is most likely to have
// recorded a price on.
func TestAPeriodContainsBothOfItsOwnBoundariesAndNeitherNeighbour(t *testing.T) {
	p := mustPeriod(t, domain.PeriodQuarter, 2026, 2)

	if !p.Contains(p.Start()) {
		t.Error("the first day of a period is inside it")
	}
	if !p.Contains(p.End()) {
		t.Error("the last day of a period is inside it")
	}
	if p.Contains(p.Start().AddDate(0, 0, -1)) {
		t.Error("the day before the period is not inside it")
	}
	if p.Contains(p.End().AddDate(0, 0, 1)) {
		t.Error("the day after the period is not inside it")
	}
}

// A timestamp is a moment, not a day: 23:00 on 30 June in Singapore is 15:00
// on 30 June in UTC, and both are the last day of H1. Contains truncates
// rather than comparing instants, for the reason budget.go's startOfMonth
// does -- no household in Hearth stores a timezone.
func TestContainsJudgesTheDayNotTheInstant(t *testing.T) {
	p := mustPeriod(t, domain.PeriodHalf, 2026, 1)
	singapore := time.FixedZone("+08", 8*60*60)

	lastMoment := time.Date(2026, time.June, 30, 23, 59, 0, 0, singapore)
	if !p.Contains(lastMoment) {
		t.Errorf("%s is still 30 June and belongs to H1", lastMoment)
	}
	firstMoment := time.Date(2026, time.January, 1, 0, 30, 0, 0, singapore)
	if !p.Contains(firstMoment) {
		t.Errorf("%s is still 1 January and belongs to H1", firstMoment)
	}
}

func TestNewPeriodRefusesAnIndexThatDoesNotExist(t *testing.T) {
	cases := []struct {
		kind  domain.PeriodKind
		index int
	}{
		{domain.PeriodQuarter, 0},
		{domain.PeriodQuarter, 5},
		{domain.PeriodQuarter, -1},
		{domain.PeriodHalf, 0},
		{domain.PeriodHalf, 3},
		{domain.PeriodYear, 0},
		{domain.PeriodYear, 2},
	}
	for _, c := range cases {
		if _, err := domain.NewPeriod(c.kind, 2026, c.index); !errors.Is(err, domain.ErrPeriodIndexOutOfRange) {
			t.Errorf("NewPeriod(%s, 2026, %d) error = %v, want ErrPeriodIndexOutOfRange", c.kind, c.index, err)
		}
	}
}

func TestNewPeriodRefusesAKindItDoesNotKnow(t *testing.T) {
	if _, err := domain.NewPeriod(domain.PeriodKind("decade"), 2026, 1); !errors.Is(err, domain.ErrUnknownPeriodKind) {
		t.Fatalf("error = %v, want ErrUnknownPeriodKind", err)
	}
}

func TestLabelReadsTheWayTheOwnerWouldSayIt(t *testing.T) {
	cases := []struct {
		period domain.Period
		want   string
	}{
		{mustPeriod(t, domain.PeriodQuarter, 2026, 3), "Q3 2026"},
		{mustPeriod(t, domain.PeriodHalf, 2026, 2), "H2 2026"},
		{mustPeriod(t, domain.PeriodYear, 2026, 1), "2026"},
	}
	for _, c := range cases {
		if got := c.period.Label(); got != c.want {
			t.Errorf("Label() = %q, want %q", got, c.want)
		}
	}
}

func TestPeriodContainingFindsTheOneTheDayFallsIn(t *testing.T) {
	cases := []struct {
		kind  domain.PeriodKind
		day   time.Time
		label string
	}{
		{domain.PeriodQuarter, utcDay(2026, time.January, 1), "Q1 2026"},
		{domain.PeriodQuarter, utcDay(2026, time.March, 31), "Q1 2026"},
		{domain.PeriodQuarter, utcDay(2026, time.April, 1), "Q2 2026"},
		{domain.PeriodQuarter, utcDay(2026, time.December, 31), "Q4 2026"},
		{domain.PeriodHalf, utcDay(2026, time.June, 30), "H1 2026"},
		{domain.PeriodHalf, utcDay(2026, time.July, 1), "H2 2026"},
		{domain.PeriodYear, utcDay(2026, time.August, 9), "2026"},
	}
	for _, c := range cases {
		p, err := domain.PeriodContaining(c.kind, c.day)
		if err != nil {
			t.Fatalf("PeriodContaining(%s, %s): %v", c.kind, c.day.Format(time.DateOnly), err)
		}
		if p.Label() != c.label {
			t.Errorf("PeriodContaining(%s, %s) = %q, want %q", c.kind, c.day.Format(time.DateOnly), p.Label(), c.label)
		}
	}
}

// The report's series ends with the period the household is living in, because
// the owner's first question is "how am I doing now" -- see the plan's decision
// 2. Oldest first, because that is the order a chart draws them in.
func TestPeriodsEndingOnRunsOldestFirstAndEndsWithTodaysPeriod(t *testing.T) {
	got, err := domain.PeriodsEndingOn(domain.PeriodQuarter, utcDay(2026, time.August, 9), 3)
	if err != nil {
		t.Fatalf("PeriodsEndingOn: %v", err)
	}
	want := []string{"Q1 2026", "Q2 2026", "Q3 2026"}
	if len(got) != len(want) {
		t.Fatalf("got %d periods, want %d", len(got), len(want))
	}
	for i, label := range want {
		if got[i].Label() != label {
			t.Errorf("period %d = %q, want %q", i, got[i].Label(), label)
		}
	}
}

// Walking back from Q1 has to cross into the previous year. A loop that
// decrements the index without decrementing the year produces Q0 and Q-1,
// which NewPeriod would refuse -- so this test fails loudly rather than
// silently returning the wrong quarter.
func TestPeriodsEndingOnWalksBackThroughTheYearBoundary(t *testing.T) {
	got, err := domain.PeriodsEndingOn(domain.PeriodQuarter, utcDay(2026, time.February, 2), 3)
	if err != nil {
		t.Fatalf("PeriodsEndingOn: %v", err)
	}
	want := []string{"Q3 2025", "Q4 2025", "Q1 2026"}
	for i, label := range want {
		if got[i].Label() != label {
			t.Errorf("period %d = %q, want %q", i, got[i].Label(), label)
		}
	}

	halves, err := domain.PeriodsEndingOn(domain.PeriodHalf, utcDay(2026, time.February, 2), 3)
	if err != nil {
		t.Fatalf("PeriodsEndingOn(half): %v", err)
	}
	wantHalves := []string{"H1 2025", "H2 2025", "H1 2026"}
	for i, label := range wantHalves {
		if halves[i].Label() != label {
			t.Errorf("half %d = %q, want %q", i, halves[i].Label(), label)
		}
	}

	years, err := domain.PeriodsEndingOn(domain.PeriodYear, utcDay(2026, time.February, 2), 3)
	if err != nil {
		t.Fatalf("PeriodsEndingOn(year): %v", err)
	}
	wantYears := []string{"2024", "2025", "2026"}
	for i, label := range wantYears {
		if years[i].Label() != label {
			t.Errorf("year %d = %q, want %q", i, years[i].Label(), label)
		}
	}
}

func TestPeriodsEndingOnRefusesACountThatMakesNoSeries(t *testing.T) {
	for _, count := range []int{0, -1} {
		if _, err := domain.PeriodsEndingOn(domain.PeriodQuarter, utcDay(2026, time.August, 9), count); !errors.Is(err, domain.ErrPeriodCountOutOfRange) {
			t.Errorf("count %d error = %v, want ErrPeriodCountOutOfRange", count, err)
		}
	}
}

// IsCurrent is what the screen reads to say "to date" rather than presenting a
// half-finished quarter as a closed one.
func TestIsCurrentIsTrueOnlyForThePeriodTodayFallsIn(t *testing.T) {
	today := utcDay(2026, time.August, 9)
	current := mustPeriod(t, domain.PeriodQuarter, 2026, 3)
	if !current.IsCurrent(today) {
		t.Error("Q3 2026 contains 9 August 2026 and is the current quarter")
	}
	previous := mustPeriod(t, domain.PeriodQuarter, 2026, 2)
	if previous.IsCurrent(today) {
		t.Error("Q2 2026 has closed")
	}
	next := mustPeriod(t, domain.PeriodQuarter, 2026, 4)
	if next.IsCurrent(today) {
		t.Error("Q4 2026 has not started")
	}
}
