package domain_test

import (
	"errors"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

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

// longBefore is a creation day far enough back that the creation-month floor
// plays no part in a test about something else.
var longBefore = time.Date(2020, 1, 15, 0, 0, 0, 0, time.UTC)

// The button starts the EARLIER of {previous month, current month} that has no
// retro row, so a couple doing July's retro on 2 August files it as July and
// August is still available afterwards.
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
			got, ok := domain.StartableMonth(today, longBefore, c.currentExists, c.previousExists)
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

	got, ok := domain.StartableMonth(today, longBefore, false, false)
	if !ok || !got.Equal(want) {
		t.Fatalf("= %v, %v; want %v, true", got, ok, want)
	}
}

// A household did not exist in the month before it was created, so that month
// is never offered: the household starts with the month it was created in.
func TestStartableMonthNeverReachesBeforeTheCreationMonth(t *testing.T) {
	september := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	october := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)

	cases := []struct {
		name                          string
		today, createdOn              time.Time
		currentExists, previousExists bool
		want                          time.Time
		wantOK                        bool
	}{
		{
			name:  "created today, on the 1st: offers this month, not the one before",
			today: october, createdOn: october,
			want: october, wantOK: true,
		},
		{
			name:  "created earlier this month: still offers this month",
			today: time.Date(2026, 10, 20, 0, 0, 0, 0, time.UTC), createdOn: time.Date(2026, 10, 3, 0, 0, 0, 0, time.UTC),
			want: october, wantOK: true,
		},
		{
			name:  "created this month, this month already started: offers nothing",
			today: october, createdOn: october,
			currentExists: true,
			wantOK:        false,
		},
		{
			name:  "created on the last day of last month: last month is its own and is offered",
			today: october, createdOn: time.Date(2026, 9, 30, 0, 0, 0, 0, time.UTC),
			want: september, wantOK: true,
		},
		{
			name:  "created last month, last month done: offers this month",
			today: october, createdOn: time.Date(2026, 9, 30, 0, 0, 0, 0, time.UTC),
			previousExists: true,
			want:           october, wantOK: true,
		},
		{
			// Only a clock that went backwards gets here. Nothing is offered
			// rather than a month the household was not there for.
			name:  "created after today: offers nothing",
			today: time.Date(2026, 9, 30, 0, 0, 0, 0, time.UTC), createdOn: october,
			wantOK: false,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, ok := domain.StartableMonth(c.today, c.createdOn, c.currentExists, c.previousExists)
			if ok != c.wantOK || !got.Equal(c.want) {
				t.Fatalf("= %v, %v; want %v, %v", got, ok, c.want, c.wantOK)
			}
		})
	}
}

func TestFirstSentence(t *testing.T) {
	cases := []struct{ in, want string }{
		{"Best month this year. Agreed to keep the budget review.", "Best month this year."},
		{"Did we really do that?! Yes.", "Did we really do that?"},
		{"", ""},
		{"no terminator at all here", "no terminator at all here"},
		{"a very long note with no terminator that runs well past the sixty character budget we allow", "a very long note with no terminator that runs well past the…"},
	}
	for _, c := range cases {
		if got := domain.FirstSentence(c.in); got != c.want {
			t.Fatalf("FirstSentence(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// The long-note case is the only one with a length rule, and the string
// above was hand-counted wrong once already. Assert the rule itself.
func TestFirstSentenceTruncatesToSixtyRunes(t *testing.T) {
	got := domain.FirstSentence("a very long note with no terminator that runs well past the sixty character budget we allow")
	if n := utf8.RuneCountInString(got); n != 60 {
		t.Fatalf("truncated length = %d runes, want 60 including the ellipsis", n)
	}
}

// Every fixture above is pure ASCII, where byte- and rune-slicing agree, so a
// regression to byte-slicing would still pass all of them. CJK characters are
// three bytes each in UTF-8 -- the smallest input that actually exercises the
// boundary FirstSentence claims to protect.
func TestFirstSentenceCutsOnARuneBoundary(t *testing.T) {
	// 110 runes, no '.', '!' or '?' -- long enough that truncation fires.
	note := strings.Repeat("春の話し合いは長かった", 10)

	got := domain.FirstSentence(note)

	if !utf8.ValidString(got) {
		t.Fatalf("FirstSentence(%q) produced invalid UTF-8: %q", note, got)
	}
	if n := utf8.RuneCountInString(got); n != 60 {
		t.Fatalf("truncated length = %d runes, want 60 including the ellipsis", n)
	}
	// The boundary itself: a byte-sliced version would end in a replacement
	// character or a truncated sequence, not the clean 59-rune prefix.
	runes := []rune(note)
	want := string(runes[:59]) + "…"
	if got != want {
		t.Fatalf("FirstSentence(%q) = %q, want %q", note, got, want)
	}
}
