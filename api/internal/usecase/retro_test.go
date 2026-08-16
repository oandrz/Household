package usecase_test

import (
	"context"
	"testing"
	"time"

	"github.com/andreasoentoro/hearth/api/internal/usecase"
)

func aug2026() time.Time { return time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC) }
func jul2026() time.Time { return time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC) }
func jun2026() time.Time { return time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC) }

// A draft is not a data point. It shows on the page as its own in-progress
// entry, and it is excluded from the finished count and the mood chart --
// a half-typed month must not become a point on a mood trend (decision 2).
func TestRetroListExcludesDraftsFromTheCountAndTheChart(t *testing.T) {
	retros := newRetroRepoDouble()
	finished := retros.seed(jul2026(), 4, "Best month this year. And more.", true)
	retros.seed(aug2026(), 5, "", false) // the draft

	svc := usecase.NewRetroService(retros, newRetroActionRepoDouble())
	view, err := svc.List(context.Background(), "hh", time.Date(2026, 8, 20, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("List: %v", err)
	}

	if view.DoneCount != 1 {
		t.Fatalf("DoneCount = %d, want 1 (the draft must not count)", view.DoneCount)
	}
	if len(view.Summaries) != 2 {
		t.Fatalf("len(Summaries) = %d, want 2 (the draft still shows on the page)", len(view.Summaries))
	}
	for _, p := range view.Mood {
		if p.Month.Equal(aug2026()) && p.HasMood {
			t.Fatal("the draft's mood reached the chart")
		}
	}
	if view.Since == nil || !view.Since.Equal(finished.Month) {
		t.Fatalf("Since = %v, want %v (the earliest FINISHED month)", view.Since, finished.Month)
	}
}

// Twelve points ending at the current month, gaps for months with no finished
// retro. A gap is never a zero -- zero is a claim, the same rule Budget
// applies to transactions it cannot convert.
func TestRetroMoodSeriesIsTwelveMonthsWithGaps(t *testing.T) {
	retros := newRetroRepoDouble()
	retros.seed(jul2026(), 3, "", true)

	svc := usecase.NewRetroService(retros, newRetroActionRepoDouble())
	view, err := svc.List(context.Background(), "hh", time.Date(2026, 8, 20, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("List: %v", err)
	}

	if len(view.Mood) != 12 {
		t.Fatalf("len(Mood) = %d, want 12", len(view.Mood))
	}
	if got := view.Mood[11].Month; !got.Equal(aug2026()) {
		t.Fatalf("last point = %v, want the current month %v", got, aug2026())
	}
	var withMood int
	for _, p := range view.Mood {
		if p.HasMood {
			withMood++
			if !p.Month.Equal(jul2026()) || p.Mood != 3 {
				t.Fatalf("unexpected point %+v", p)
			}
		}
	}
	if withMood != 1 {
		t.Fatalf("%d points carry a mood, want 1", withMood)
	}
}

// The quoted line in a history row is the first sentence of the notes, and a
// retro with no notes renders no quote at all -- not empty quotation marks.
// The same rows also carry the action count the port's own List doc comment
// promises ("each carrying its own action count") -- asserted here, against
// the two retros wired to a real actions double, rather than left as an
// unverified pass-through (code review finding, Task 3 fix round: a double
// that hardcodes ActionCount to 0 contradicts the port it claims to satisfy).
func TestRetroSummaryQuoteIsDerivedFromNotes(t *testing.T) {
	retros := newRetroRepoDouble()
	jul := retros.seed(jul2026(), 4, "Best month this year. Agreed to keep the budget review.", true)
	jun := retros.seed(jun2026(), 2, "", true)

	actions := newRetroActionRepoDouble()
	retros.setActions(actions)
	actions.seedOpen(jul.ID, jul2026(), "keep the budget review")
	actions.seedOpen(jul.ID, jul2026(), "plan the September trip")
	actions.seedOpen(jun.ID, jun2026(), "call the accountant")

	svc := usecase.NewRetroService(retros, actions)
	view, err := svc.List(context.Background(), "hh", time.Date(2026, 8, 20, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("List: %v", err)
	}

	if got := view.Summaries[0].Quote; got != "Best month this year." {
		t.Fatalf("quote = %q", got)
	}
	if got := view.Summaries[1].Quote; got != "" {
		t.Fatalf("empty notes produced quote %q, want none", got)
	}
	if got := view.Summaries[0].ActionCount; got != 2 {
		t.Fatalf("July's ActionCount = %d, want 2", got)
	}
	if got := view.Summaries[1].ActionCount; got != 1 {
		t.Fatalf("June's ActionCount = %d, want 1", got)
	}
}

// The carry-over offer reads the IMMEDIATELY previous month only, and is
// never confused with the retro's own actions (ForRetro), which come back
// through a separate field entirely. Both need a real RetroID on the seeded
// rows: a Month implementation that never called ForRetro at all would
// still pass a version of this test that never checked view.Actions (code
// review finding, Task 3 fix round) -- seeding July's and July-minus-one's
// actions against retro ids that are NOT August's own is what makes that
// failure mode visible here, rather than merely by construction.
func TestRetroMonthOffersOnlyLastMonthsOpenActions(t *testing.T) {
	retros := newRetroRepoDouble()
	aug := retros.seed(aug2026(), 0, "", false)
	jul := retros.seed(jul2026(), 0, "", true) // last month's own (finished) retro
	actions := newRetroActionRepoDouble()
	actions.seedOpen(aug.ID, aug2026(), "buy the anniversary tickets") // August's own action
	actions.seedOpen(jul.ID, jul2026(), "phone-free dinners")          // last month's open action
	actions.seedOpen("some-other-retro", time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC), "should not appear")

	svc := usecase.NewRetroService(retros, actions)
	view, err := svc.Month(context.Background(), "hh", aug.Month)
	if err != nil {
		t.Fatalf("Month: %v", err)
	}

	if len(view.Actions) != 1 || view.Actions[0].Body != "buy the anniversary tickets" {
		t.Fatalf("Actions = %+v, want only August's own action", view.Actions)
	}
	if len(view.CarryOver) != 1 || view.CarryOver[0].Body != "phone-free dinners" {
		t.Fatalf("CarryOver = %+v, want only July's open action", view.CarryOver)
	}
}

// A repository value that is not exactly midnight-on-the-first must not
// silently miss List's currentExists/previousExists comparisons, which
// StartMonth is computed from -- RetroRecord.Month's own doc comment states
// the midnight-UTC convention, but the service normalises defensively
// rather than trusting a stored value blindly (budget.go's startOfMonth is
// the house fix; code review finding, Task 3 fix round).
func TestRetroListNormalisesANonMidnightStoredMonth(t *testing.T) {
	retros := newRetroRepoDouble()
	retros.seed(jul2026(), 0, "", false)                                  // previous month, seeded clean
	retros.seed(aug2026().Add(14*time.Hour+30*time.Minute), 0, "", false) // current month, dirty: not midnight

	svc := usecase.NewRetroService(retros, newRetroActionRepoDouble())
	view, err := svc.List(context.Background(), "hh", time.Date(2026, 8, 20, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("List: %v", err)
	}

	// Both months already have a retro -- July's clean, August's dirty --
	// so StartMonth must be nil. July is seeded deliberately (previousExists
	// true) so domain.StartableMonth's own `case !previousExists` branch
	// cannot mask a wrong currentExists by returning early: with that branch
	// already closed off, only a correctly-detected August can produce nil,
	// and an unnormalised comparison that misses August's dirty timestamp
	// would instead surface August itself as still startable.
	if view.StartMonth != nil {
		t.Fatalf("StartMonth = %v, want nil (August's dirty-timestamp retro should still count as existing)", *view.StartMonth)
	}
}

// A caller-supplied month that is not exactly midnight-on-the-first must
// still find the retro a repository stores at its own normalised value --
// the other half of the same bug class, on Month's own ByMonth lookup
// rather than List's read of a stored value.
func TestRetroMonthNormalisesANonMidnightArgument(t *testing.T) {
	retros := newRetroRepoDouble()
	retros.seed(aug2026(), 0, "", false) // stored clean, per the port's own convention

	svc := usecase.NewRetroService(retros, newRetroActionRepoDouble())
	dirty := aug2026().Add(9 * time.Hour) // 9am, not midnight -- an unnormalised caller
	view, err := svc.Month(context.Background(), "hh", dirty)
	if err != nil {
		t.Fatalf("Month(%v): %v, want the retro stored at %v to be found", dirty, err, aug2026())
	}
	if !view.Retro.Month.Equal(aug2026()) {
		t.Fatalf("Retro.Month = %v, want %v", view.Retro.Month, aug2026())
	}
}

// StartMonth is pinned across all four presence states, not just the one
// the other tests happen to exercise in passing: no retros at all, the
// previous month only, the current month only, and both -- the fourth being
// the only nil case (decision 5).
func TestRetroListStartMonthAcrossAllFourStates(t *testing.T) {
	today := time.Date(2026, 8, 20, 0, 0, 0, 0, time.UTC)

	cases := []struct {
		name                      string
		seedCurrent, seedPrevious bool
		want                      time.Time
		wantSome                  bool
	}{
		{"neither exists: offers the missed month", false, false, jul2026(), true},
		{"previous exists: offers this month", false, true, aug2026(), true},
		{"only this month exists: offers the missed one", true, false, jul2026(), true},
		{"both exist: offers nothing", true, true, time.Time{}, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			retros := newRetroRepoDouble()
			if c.seedCurrent {
				retros.seed(aug2026(), 0, "", false)
			}
			if c.seedPrevious {
				retros.seed(jul2026(), 0, "", false)
			}

			svc := usecase.NewRetroService(retros, newRetroActionRepoDouble())
			view, err := svc.List(context.Background(), "hh", today)
			if err != nil {
				t.Fatalf("List: %v", err)
			}

			if !c.wantSome {
				if view.StartMonth != nil {
					t.Fatalf("StartMonth = %v, want nil (both months already have a retro)", *view.StartMonth)
				}
				return
			}
			if view.StartMonth == nil || !view.StartMonth.Equal(c.want) {
				t.Fatalf("StartMonth = %v, want %v", view.StartMonth, c.want)
			}
		})
	}
}
