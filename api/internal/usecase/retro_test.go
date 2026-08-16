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
func TestRetroSummaryQuoteIsDerivedFromNotes(t *testing.T) {
	retros := newRetroRepoDouble()
	retros.seed(jul2026(), 4, "Best month this year. Agreed to keep the budget review.", true)
	retros.seed(jun2026(), 2, "", true)

	svc := usecase.NewRetroService(retros, newRetroActionRepoDouble())
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
}

// The carry-over offer reads the IMMEDIATELY previous month only.
func TestRetroMonthOffersOnlyLastMonthsOpenActions(t *testing.T) {
	retros := newRetroRepoDouble()
	aug := retros.seed(aug2026(), 0, "", false)
	actions := newRetroActionRepoDouble()
	actions.seedOpen(jul2026(), "phone-free dinners")
	actions.seedOpen(time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC), "should not appear")

	svc := usecase.NewRetroService(retros, actions)
	view, err := svc.Month(context.Background(), "hh", aug.Month)
	if err != nil {
		t.Fatalf("Month: %v", err)
	}

	if len(view.CarryOver) != 1 || view.CarryOver[0].Body != "phone-free dinners" {
		t.Fatalf("CarryOver = %+v, want only July's open action", view.CarryOver)
	}
}
