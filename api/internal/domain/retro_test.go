package domain_test

import (
	"errors"
	"testing"
	"time"

	"github.com/andreasoentoro/hearth/api/internal/domain"
)

func TestParseMoodRefusesAnythingOutsideOneToFive(t *testing.T) {
	for _, n := range []int{-1, 0, 6, 99} {
		if _, err := domain.ParseMood(n); !errors.Is(err, domain.ErrInvalidMood) {
			t.Fatalf("ParseMood(%d) err = %v, want ErrInvalidMood", n, err)
		}
	}
	for _, n := range []int{1, 2, 3, 4, 5} {
		got, err := domain.ParseMood(n)
		if err != nil || int(got) != n {
			t.Fatalf("ParseMood(%d) = %v, %v; want %d, nil", n, got, err, n)
		}
	}
}

// The button starts the EARLIER of {previous month, current month} that has no
// retro row, so a couple doing July's retro on 2 August files it as July and
// August is still available afterwards (spec decision 5).
func TestStartableMonth(t *testing.T) {
	today := time.Date(2026, 8, 2, 21, 0, 0, 0, time.UTC)
	july := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	august := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)

	cases := []struct {
		name                          string
		currentExists, previousExists bool
		want                          time.Time
		wantOK                        bool
	}{
		{"neither exists: offers the missed month", false, false, july, true},
		{"previous exists: offers this month", false, true, august, true},
		{"only this month exists: offers the missed one", true, false, july, true},
		{"both exist: offers nothing", true, true, time.Time{}, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, ok := domain.StartableMonth(today, c.currentExists, c.previousExists)
			if ok != c.wantOK || !got.Equal(c.want) {
				t.Fatalf("= %v, %v; want %v, %v", got, ok, c.want, c.wantOK)
			}
		})
	}
}

// January must walk back to the previous December, not to month zero.
func TestStartableMonthCrossesTheYear(t *testing.T) {
	today := time.Date(2027, 1, 4, 9, 0, 0, 0, time.UTC)
	want := time.Date(2026, 12, 1, 0, 0, 0, 0, time.UTC)

	got, ok := domain.StartableMonth(today, false, false)
	if !ok || !got.Equal(want) {
		t.Fatalf("= %v, %v; want %v, true", got, ok, want)
	}
}

func TestFirstSentence(t *testing.T) {
	cases := []struct{ in, want string }{
		{"Best month this year. Agreed to keep the budget review.", "Best month this year."},
		{"Did we really do that?! Yes.", "Did we really do that?"},
		{"", ""},
		{"no terminator at all here", "no terminator at all here"},
		{"a very long note with no terminator that runs well past the sixty character budget we allow", "a very long note with no terminator that runs well past the s…"},
	}
	for _, c := range cases {
		if got := domain.FirstSentence(c.in); got != c.want {
			t.Fatalf("FirstSentence(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}
