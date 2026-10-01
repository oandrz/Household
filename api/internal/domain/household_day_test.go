package domain_test

import (
	"errors"
	"testing"
	"time"

	"github.com/andreasoentoro/hearth/api/internal/domain"
)

func TestTodayInIsTheHouseholdsCalendarDayNotTheServers(t *testing.T) {
	// 23:00 UTC on 30 September: still September for the server, already
	// 07:00 on 1 October in Singapore, and 16:00 on 30 September in Los
	// Angeles. The second row is the instant the QA run found five defects at.
	cases := []struct {
		name string
		now  time.Time
		zone string
		want time.Time
	}{
		{"UTC keeps the server's day", time.Date(2026, 9, 30, 23, 0, 0, 0, time.UTC), "UTC", utcDay(2026, 9, 30)},
		{"east of UTC is already tomorrow", time.Date(2026, 9, 30, 23, 0, 0, 0, time.UTC), "Asia/Singapore", utcDay(2026, 10, 1)},
		{"west of UTC is still yesterday", time.Date(2026, 10, 1, 3, 0, 0, 0, time.UTC), "America/Los_Angeles", utcDay(2026, 9, 30)},
		{"the far east edge of the map", time.Date(2026, 12, 31, 10, 0, 0, 0, time.UTC), "Pacific/Kiritimati", utcDay(2027, 1, 1)},
		{"the far west edge of the map", time.Date(2027, 1, 1, 10, 59, 0, 0, time.UTC), "Pacific/Pago_Pago", utcDay(2026, 12, 31)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := domain.TodayIn(tc.now, tc.zone)
			if err != nil {
				t.Fatalf("TodayIn(%s, %q): %v", tc.now, tc.zone, err)
			}
			if !got.Equal(tc.want) {
				t.Errorf("TodayIn(%s, %q) = %s, want %s", tc.now, tc.zone, got, tc.want)
			}
		})
	}
}

// The answer is a DATE: midnight, located in UTC. A zoned instant handed to
// Bills would be converted to UTC again by domain.NextDue and land on the day
// before for a household east of Greenwich.
func TestTodayInAnswersMidnightStampedUTC(t *testing.T) {
	got, err := domain.TodayIn(time.Date(2026, 9, 30, 23, 17, 42, 9, time.UTC), "Asia/Singapore")
	if err != nil {
		t.Fatalf("TodayIn: %v", err)
	}
	if got.Location() != time.UTC {
		t.Errorf("location = %s, want UTC", got.Location())
	}
	if h, m, s := got.Clock(); h != 0 || m != 0 || s != 0 || got.Nanosecond() != 0 {
		t.Errorf("time of day = %s, want midnight", got.Format(time.RFC3339Nano))
	}
}

// The same instant written in two locations is one instant, so it is one
// household day. Reading the caller's wall clock instead would make the
// answer depend on how the clock adapter happens to label its value.
func TestTodayInReadsTheInstantNotTheCallersWallClock(t *testing.T) {
	instant := time.Date(2026, 9, 30, 23, 0, 0, 0, time.UTC)
	elsewhere := instant.In(time.FixedZone("far west", -11*60*60))

	fromUTC, err := domain.TodayIn(instant, "Asia/Singapore")
	if err != nil {
		t.Fatalf("TodayIn(utc): %v", err)
	}
	fromElsewhere, err := domain.TodayIn(elsewhere, "Asia/Singapore")
	if err != nil {
		t.Fatalf("TodayIn(elsewhere): %v", err)
	}
	if !fromUTC.Equal(fromElsewhere) {
		t.Errorf("one instant gave two days: %s and %s", fromUTC, fromElsewhere)
	}
}

// "" and "Local" are the two names Go accepts without complaint and should
// not: "" loads as UTC and "Local" as whatever zone the server sits in. A
// household would silently keep the server's calendar, which is the defect
// the stored zone exists to remove.
func TestParseTimezoneRefusesWhatItCannotName(t *testing.T) {
	for _, zone := range []string{"", "Local", "Mars/Olympus_Mons", " Asia/Singapore", "../etc/passwd", "+08:00"} {
		t.Run(zone, func(t *testing.T) {
			if _, err := domain.ParseTimezone(zone); !errors.Is(err, domain.ErrInvalidTimezone) {
				t.Errorf("ParseTimezone(%q) error = %v, want ErrInvalidTimezone", zone, err)
			}
			if _, err := domain.TodayIn(time.Date(2026, 9, 30, 23, 0, 0, 0, time.UTC), zone); !errors.Is(err, domain.ErrInvalidTimezone) {
				t.Errorf("TodayIn(_, %q) error = %v, want ErrInvalidTimezone", zone, err)
			}
		})
	}
}

func TestParseTimezoneAcceptsIANANames(t *testing.T) {
	for _, zone := range []string{"UTC", "Asia/Singapore", "America/Los_Angeles", "Pacific/Kiritimati", "Asia/Kolkata"} {
		loc, err := domain.ParseTimezone(zone)
		if err != nil {
			t.Fatalf("ParseTimezone(%q): %v", zone, err)
		}
		if loc.String() != zone {
			t.Errorf("ParseTimezone(%q) loaded %q", zone, loc)
		}
	}
}

func TestIsAfterDayComparesCalendarDaysNotInstants(t *testing.T) {
	today := utcDay(2026, 10, 1)
	cases := []struct {
		name string
		date time.Time
		want bool
	}{
		{"yesterday", utcDay(2026, 9, 30), false},
		{"today", utcDay(2026, 10, 1), false},
		{"today, late in the evening", time.Date(2026, 10, 1, 23, 59, 59, 0, time.UTC), false},
		{"tomorrow", utcDay(2026, 10, 2), true},
		{"next year", utcDay(2027, 1, 1), true},
		// The day is read in the value's own location: 00:30 on the 2nd in
		// Singapore is the 2nd, though the same instant is still the 1st in UTC.
		{"tomorrow, written in another zone", time.Date(2026, 10, 2, 0, 30, 0, 0, time.FixedZone("SGT", 8*60*60)), true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := domain.IsAfterDay(tc.date, today); got != tc.want {
				t.Errorf("IsAfterDay(%s, %s) = %v, want %v", tc.date, today, got, tc.want)
			}
		})
	}
}

// today's own time of day must not matter either: a caller holding a raw
// clock reading late in the day would otherwise refuse a date that is today.
func TestIsAfterDayIgnoresTodaysTimeOfDay(t *testing.T) {
	earlyToday := time.Date(2026, 10, 1, 0, 0, 1, 0, time.UTC)
	if domain.IsAfterDay(time.Date(2026, 10, 1, 18, 0, 0, 0, time.UTC), earlyToday) {
		t.Error("a date later the same day was treated as a later day")
	}
}

// The shape guard. Hearth's date rules normalise a day in two ways: Bills
// convert to UTC first (IsOverdue, NextDue) and periods and retros read the
// value's own location (PeriodContaining, StartableMonth). TodayIn's answer
// has to mean the same day to both families, or one screen is a day behind
// another.
func TestTodayInMeansTheSameDayToBothFamiliesOfDateRule(t *testing.T) {
	now := time.Date(2026, 9, 30, 23, 0, 0, 0, time.UTC) // 07:00 on 1 October in Singapore
	today, err := domain.TodayIn(now, "Asia/Singapore")
	if err != nil {
		t.Fatalf("TodayIn: %v", err)
	}

	// The family that converts to UTC.
	if !domain.IsOverdue(utcDay(2026, 9, 30), today) {
		t.Error("a bill due on 30 September is not overdue on 1 October")
	}
	if domain.IsOverdue(utcDay(2026, 10, 1), today) {
		t.Error("a bill due on 1 October is already overdue on 1 October")
	}

	// The family that reads the value's own location.
	period, err := domain.PeriodContaining(domain.PeriodQuarter, today)
	if err != nil {
		t.Fatalf("PeriodContaining: %v", err)
	}
	if period.Label() != "Q4 2026" {
		t.Errorf("current quarter = %s, want Q4 2026", period.Label())
	}
	// Created long ago, so the creation-month floor plays no part here.
	month, ok := domain.StartableMonth(today, utcDay(2020, 1, 1), false, false)
	if !ok || !month.Equal(utcDay(2026, 9, 1)) {
		t.Errorf("startable month = %s (%v), want September 2026", month, ok)
	}
}

// Why the day is not simply now.In(zone): the two families disagree about a
// zoned instant. This is the trap TodayIn's comment warns about, kept as a
// test so the warning cannot quietly stop being true.
func TestAZonedInstantIsNotAHouseholdDay(t *testing.T) {
	singapore, err := domain.ParseTimezone("Asia/Singapore")
	if err != nil {
		t.Fatalf("ParseTimezone: %v", err)
	}
	zoned := time.Date(2026, 9, 30, 23, 0, 0, 0, time.UTC).In(singapore)

	period, err := domain.PeriodContaining(domain.PeriodQuarter, zoned)
	if err != nil {
		t.Fatalf("PeriodContaining: %v", err)
	}
	// Own-location family: 1 October, Q4.
	if period.Label() != "Q4 2026" {
		t.Fatalf("current quarter = %s, want Q4 2026", period.Label())
	}
	// UTC-converting family: still 30 September, so a bill due that day is
	// not yet overdue -- a different day from the one the quarter was read on.
	if domain.IsOverdue(utcDay(2026, 9, 30), zoned) {
		t.Error("IsOverdue read a zoned instant as its local day; if both families now agree, TodayIn's warning is out of date")
	}
}
