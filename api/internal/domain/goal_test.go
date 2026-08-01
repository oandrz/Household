package domain

import (
	"errors"
	"testing"
	"time"
)

func goalDate(y int, m time.Month, d int) time.Time {
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
}

func TestMonthsLeftInclusive(t *testing.T) {
	cases := []struct {
		name        string
		target      time.Time
		today       time.Time
		want        int
	}{
		{"four months ahead counts both ends", goalDate(2026, time.December, 1), goalDate(2026, time.August, 1), 5},
		{"the target month itself is one month", goalDate(2026, time.August, 1), goalDate(2026, time.August, 19), 1},
		{"next month is two", goalDate(2026, time.September, 1), goalDate(2026, time.August, 31), 2},
		{"across a year boundary", goalDate(2027, time.January, 1), goalDate(2026, time.November, 1), 3},
		{"a past target month is zero, never negative", goalDate(2026, time.July, 1), goalDate(2026, time.August, 1), 0},
		{"far in the past is still zero", goalDate(2024, time.March, 1), goalDate(2026, time.August, 1), 0},
		{"the day of the month never matters", goalDate(2026, time.December, 1), goalDate(2026, time.August, 31), 5},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := MonthsLeftInclusive(tc.target, tc.today); got != tc.want {
				t.Fatalf("MonthsLeftInclusive = %d, want %d", got, tc.want)
			}
		})
	}
}

func TestRequiredMonthlyMinor(t *testing.T) {
	cases := []struct {
		name      string
		remaining int64
		months    int
		want      int64
		wantOK    bool
	}{
		{"exact division", 500000, 5, 100000, true},
		{"rounds up, because rounding down never reaches the target", 500001, 5, 100001, true},
		{"one month left needs the whole remainder", 140000, 1, 140000, true},
		{"nothing remaining needs nothing", 0, 5, 0, true},
		{"no months left has no honest figure", 500000, 0, 0, false},
		{"negative months are refused the same way", 500000, -3, 0, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := RequiredMonthlyMinor(tc.remaining, tc.months)
			if ok != tc.wantOK || (ok && got != tc.want) {
				t.Fatalf("RequiredMonthlyMinor = %d,%v want %d,%v", got, ok, tc.want, tc.wantOK)
			}
		})
	}
}

func TestGoalProgressPercent(t *testing.T) {
	cases := []struct {
		name                     string
		contributed, target      int64
		want                     int
	}{
		{"the design's Bali trip", 260000, 400000, 65},
		{"rounds to nearest", 129000, 400000, 32}, // 32.25 -> 32
		{"half rounds up", 130000, 400000, 33},    // 32.5 -> 33
		{"over target caps at 100 for the ring", 500000, 400000, 100},
		{"net negative floors at 0, never a reversed ring", -500000, 400000, 0},
		{"zero target cannot happen but must not divide", 1000, 0, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := GoalProgressPercent(tc.contributed, tc.target); got != tc.want {
				t.Fatalf("GoalProgressPercent = %d, want %d", got, tc.want)
			}
		})
	}
}

func TestGoalStatusFor(t *testing.T) {
	sgd := func(a int64) Money { return Money{Amount: a, Currency: "SGD"} }
	dec2026 := goalDate(2026, time.December, 1)
	jul2026 := goalDate(2026, time.July, 1)
	today := goalDate(2026, time.August, 15)
	archived := goalDate(2026, time.August, 1)

	base := Goal{
		Name:           "Bali family trip",
		Target:         sgd(400000),
		TargetMonth:    &dec2026,
		PlannedMonthly: sgd(35000),
	}

	cases := []struct {
		name        string
		goal        Goal
		contributed int64
		want        GoalStatus
	}{
		// remaining 140000 over 5 months = 28000 required <= 35000 planned.
		{"reachable at the planned rate is on track", base, 260000, GoalOnTrack},
		// remaining 300000 over 5 months = 60000 required > 35000 planned.
		{"unreachable at the planned rate is behind", base, 100000, GoalBehind},
		{"required exactly equal to planned is still on track", func() Goal {
			g := base
			g.PlannedMonthly = sgd(28000)
			return g
		}(), 260000, GoalOnTrack},
		{"one minor unit short of the required figure is behind", func() Goal {
			g := base
			g.PlannedMonthly = sgd(27999)
			return g
		}(), 260000, GoalBehind},
		{"contributed past the target is achieved, whatever the date says", base, 400000, GoalAchieved},
		{"a past target date, unachieved, is behind without dividing", func() Goal {
			g := base
			g.TargetMonth = &jul2026
			return g
		}(), 100000, GoalBehind},
		{"a past target date that was met is still achieved", func() Goal {
			g := base
			g.TargetMonth = &jul2026
			return g
		}(), 400000, GoalAchieved},
		{"no target date means no status", func() Goal {
			g := base
			g.TargetMonth = nil
			return g
		}(), 100000, GoalStatusNone},
		{"an archived goal has no status", func() Goal {
			g := base
			g.ArchivedAt = &archived
			return g
		}(), 100000, GoalStatusNone},
		{"a planned monthly of zero cannot be on track while anything remains", func() Goal {
			g := base
			g.PlannedMonthly = sgd(0)
			return g
		}(), 260000, GoalBehind},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := GoalStatusFor(tc.goal, tc.contributed, today); got != tc.want {
				t.Fatalf("GoalStatusFor = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestParseContributionSourceRefusesAnythingElse(t *testing.T) {
	for _, ok := range []string{"manual", "starting_balance", "budget_rollover"} {
		if _, err := ParseContributionSource(ok); err != nil {
			t.Fatalf("ParseContributionSource(%q) = %v, want nil", ok, err)
		}
	}
	if _, err := ParseContributionSource("automatic"); !errors.Is(err, ErrUnknownContributionSource) {
		t.Fatalf("ParseContributionSource(\"automatic\") = %v, want ErrUnknownContributionSource", err)
	}
	if _, err := ParseContributionSource(""); !errors.Is(err, ErrUnknownContributionSource) {
		t.Fatalf("ParseContributionSource(\"\") = %v, want ErrUnknownContributionSource", err)
	}
}
