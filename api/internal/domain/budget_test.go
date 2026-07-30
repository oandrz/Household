package domain

import (
	"testing"
	"time"
)

func date(y int, m time.Month, d int) time.Time {
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
}

// The spec's table, row by row. Each case name quotes the rule it pins.
func TestDaysLeftInMonth(t *testing.T) {
	cases := []struct {
		name  string
		month time.Time
		today time.Time
		want  int
	}{
		{"first of a 31-day month: the whole month", date(2026, time.July, 1), date(2026, time.July, 1), 31},
		{"mid-month: today still counts", date(2026, time.July, 1), date(2026, time.July, 19), 13},
		{"last day: one day left", date(2026, time.July, 1), date(2026, time.July, 31), 1},
		{"past month: zero", date(2026, time.June, 1), date(2026, time.July, 19), 0},
		{"future month: its whole length", date(2026, time.September, 1), date(2026, time.July, 19), 30},
		{"February in a non-leap year", date(2026, time.February, 1), date(2026, time.February, 1), 28},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := DaysLeftInMonth(tc.month, tc.today); got != tc.want {
				t.Fatalf("DaysLeftInMonth = %d, want %d", got, tc.want)
			}
		})
	}
}

func TestPercentUsed(t *testing.T) {
	cases := []struct {
		name             string
		spent, budgeted  int64
		wantPct          int
		wantOK           bool
	}{
		{"the design's own figures round to 66", 342000, 520000, 66, true},
		{"zero budgeted hides the figure, never NaN", 342000, 0, 0, false},
		{"over 100 stays literal", 600000, 520000, 115, true},
		{"rounds to nearest, not down", 335000, 520000, 64, true}, // 64.42 -> 64
		{"half rounds up", 130000, 520000, 25, true},
		{"negative net spend rounds correctly", -338000, 520000, -65, true},
		{"half away from zero", 127400, 520000, 25, true}, // 24.5 -> 25
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			pct, ok := PercentUsed(tc.spent, tc.budgeted)
			if ok != tc.wantOK || (ok && pct != tc.wantPct) {
				t.Fatalf("PercentUsed = %d,%v want %d,%v", pct, ok, tc.wantPct, tc.wantOK)
			}
		})
	}
}

func TestDailyPace(t *testing.T) {
	cases := []struct {
		name      string
		remaining int64
		daysLeft  int
		wantPace  int64
		wantOK    bool
	}{
		{"the design's own figures floor to 136 whole units", 178000, 13, 13692, true}, // 178000/13 = 13692.3 minor
		{"exact division", 130000, 13, 10000, true},
		{"nothing remaining hides the card", 0, 13, 0, false},
		{"overspent hides the card", -5000, 13, 0, false},
		{"past month (zero days) hides the card", 178000, 0, 0, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			pace, ok := DailyPace(tc.remaining, tc.daysLeft)
			if ok != tc.wantOK || (ok && pace != tc.wantPace) {
				t.Fatalf("DailyPace = %d,%v want %d,%v", pace, ok, tc.wantPace, tc.wantOK)
			}
		})
	}
}
