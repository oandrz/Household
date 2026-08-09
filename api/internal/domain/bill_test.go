package domain_test

import (
	"errors"
	"testing"
	"time"

	"github.com/andreasoentoro/hearth/api/internal/domain"
)

func day(s string) time.Time {
	t, err := time.Parse("2006-01-02", s)
	if err != nil {
		panic(err)
	}
	return t
}

func TestParseCadenceRefusesAnythingElse(t *testing.T) {
	for _, ok := range []string{"one_off", "monthly", "quarterly", "yearly"} {
		if _, err := domain.ParseCadence(ok); err != nil {
			t.Fatalf("ParseCadence(%q): %v", ok, err)
		}
	}
	// The default arm is the point: this value arrives from a database column
	// and from a request body.
	if _, err := domain.ParseCadence("weekly"); !errors.Is(err, domain.ErrUnknownCadence) {
		t.Fatalf("ParseCadence(\"weekly\") = %v, want ErrUnknownCadence", err)
	}
}

// The clamp is the whole reason NextDue exists. Go's own AddDate(0,1,0) on
// 31 January returns 3 March.
func TestNextDueClampsToTheLastDayOfAShortMonth(t *testing.T) {
	cases := []struct {
		name      string
		cadence   domain.Cadence
		from      string
		anchorDay int
		want      string
	}{
		{"31 Jan monthly lands on 28 Feb", domain.CadenceMonthly, "2026-01-31", 31, "2026-02-28"},
		{"29 Feb exists in a leap year", domain.CadenceMonthly, "2028-01-31", 31, "2028-02-29"},
		// The anchor is what makes this one right. Advancing from the clamped
		// 28 Feb would give 28 March and the bill would have moved off the
		// 31st permanently.
		{"28 Feb advances back to 31 Mar, not 28 Mar", domain.CadenceMonthly, "2026-02-28", 31, "2026-03-31"},
		{"an ordinary month is untouched", domain.CadenceMonthly, "2026-08-08", 8, "2026-09-08"},
		{"quarterly crosses three months", domain.CadenceQuarterly, "2026-11-30", 30, "2027-02-28"},
		{"yearly crosses a year", domain.CadenceYearly, "2026-03-15", 15, "2027-03-15"},
		{"29 Feb yearly clamps to 28 Feb", domain.CadenceYearly, "2028-02-29", 29, "2029-02-28"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, ok := domain.NextDue(c.cadence, day(c.from), c.anchorDay)
			if !ok {
				t.Fatal("NextDue reported no next date for a recurring cadence")
			}
			if !got.Equal(day(c.want)) {
				t.Fatalf("NextDue = %s, want %s", got.Format("2006-01-02"), c.want)
			}
		})
	}
}

func TestNextDueHasNoNextDateForAOneOff(t *testing.T) {
	if _, ok := domain.NextDue(domain.CadenceOneOff, day("2026-08-08"), 8); ok {
		t.Fatal("a one-off has no next due date")
	}
}

func TestIsOverdue(t *testing.T) {
	today := day("2026-08-09")
	if !domain.IsOverdue(day("2026-08-08"), today) {
		t.Fatal("yesterday is overdue")
	}
	// Due today is not overdue: the household has the whole day to pay it.
	if domain.IsOverdue(today, today) {
		t.Fatal("a bill due today is not overdue")
	}
	if domain.IsOverdue(day("2026-08-10"), today) {
		t.Fatal("tomorrow is not overdue")
	}
}

// Time.Year/Month/Day report components in the receiver's own Location, not
// UTC, so a from or today built in a non-UTC zone must be converted before
// NextDue or IsOverdue read its calendar date -- the same family of bug
// docs/HANDOVER.md §6 records shipping at two other call sites in this
// project. Every other test in this file builds its times through day(),
// which time.Parse defaults to UTC, so none of them can exercise this path;
// this one builds its times directly with a non-UTC time.FixedZone.
func TestNextDueAndIsOverdueNormaliseNonUTCInputToUTC(t *testing.T) {
	sevenHoursEast := time.FixedZone("+07:00", 7*60*60)

	// 2026-08-09T02:00+07:00 is 2026-08-08T19:00Z -- the same UTC calendar
	// day as the due date, so the bill is due today, not overdue. Reading
	// the +07:00 components directly would see 9 August and call it overdue.
	today := time.Date(2026, time.August, 9, 2, 0, 0, 0, sevenHoursEast)
	if domain.IsOverdue(day("2026-08-08"), today) {
		t.Fatal("IsOverdue read today in its own Location instead of UTC and called a bill due today overdue")
	}

	// 2026-02-01T00:30+07:00 is 2026-01-31T17:30Z -- NextDue must advance
	// from January. Reading the +07:00 components directly would see
	// 1 February and land a month late.
	from := time.Date(2026, time.February, 1, 0, 30, 0, 0, sevenHoursEast)
	got, ok := domain.NextDue(domain.CadenceMonthly, from, 31)
	if !ok {
		t.Fatal("NextDue reported no next date for a recurring cadence")
	}
	if !got.Equal(day("2026-02-28")) {
		t.Fatalf("NextDue = %s, want 2026-02-28 (read from in its own Location instead of UTC)", got.Format("2006-01-02"))
	}
}

// Integer-first: multiply up to a year, divide exactly once at the end. The
// 50/30/20 budget template shipped a float multiply that drifted a minor unit
// on a real figure (docs/LEARNING.md, Domain and money catalogue).
func TestAnnualEquivalentMultipliesAndNeverDivides(t *testing.T) {
	cases := []struct {
		cadence domain.Cadence
		minor   int64
		want    int64
		ok      bool
	}{
		{domain.CadenceMonthly, 1998, 23976, true},
		{domain.CadenceQuarterly, 5000, 20000, true},
		{domain.CadenceYearly, 12000, 12000, true},
		{domain.CadenceOneOff, 9999, 0, false},
	}
	for _, c := range cases {
		got, ok := domain.AnnualEquivalentMinor(c.cadence, c.minor)
		if ok != c.ok || got != c.want {
			t.Fatalf("AnnualEquivalentMinor(%s, %d) = (%d, %v), want (%d, %v)",
				c.cadence, c.minor, got, ok, c.want, c.ok)
		}
	}
}
