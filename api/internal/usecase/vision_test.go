package usecase_test

import (
	"context"
	"testing"
	"time"

	"github.com/andreasoentoro/hearth/api/internal/domain"
	"github.com/andreasoentoro/hearth/api/internal/usecase"
)

func TestVisionGetReturnsAnEmptyVisionForAYearNeverSet(t *testing.T) {
	svc := usecase.NewVisionService(newVisionRepoDouble(), newGoalProgressDouble(),
		&fixedClock{now: time.Date(2026, 8, 28, 0, 0, 0, 0, time.UTC)})

	got, err := svc.Get(context.Background(), "h1", 2026)
	if err != nil {
		t.Fatalf("a year never set must not be an error: %v", err)
	}
	if got.Version != 0 {
		t.Fatalf("an unset year must carry version 0 -- it is what tells a save it is a create -- got %d", got.Version)
	}
	if got.Theme != "" || len(got.Pillars) != 0 || len(got.Milestones) != 0 {
		t.Fatalf("want a blank vision, got %+v", got)
	}
	if got.Year != 2026 {
		t.Fatalf("want the requested year echoed back, got %d", got.Year)
	}
}

func TestVisionGetResolvesALinkedMeasure(t *testing.T) {
	repo := newVisionRepoDouble()
	repo.seed(domain.Vision{
		HouseholdID: "h1", Year: 2026, Theme: "T", Version: 3,
		Pillars: []domain.Pillar{{Name: "Money without fear", Measures: []domain.Measure{
			{Label: "Emergency fund", Kind: domain.MeasureLinked, GoalID: "g1"},
			// A second linked measure at 100%, so Met's true branch is
			// actually exercised -- not just its negation via the first
			// measure's 62%.
			{Label: "Vacation fund", Kind: domain.MeasureLinked, GoalID: "g2"},
		}}},
	})
	goals := newGoalProgressDouble()
	goals.progress["g1"] = usecase.GoalProgress{GoalID: "g1", Name: "Emergency fund", Percent: 62}
	goals.progress["g2"] = usecase.GoalProgress{GoalID: "g2", Name: "Vacation fund", Percent: 100}
	svc := usecase.NewVisionService(repo, goals, &fixedClock{now: time.Date(2026, 8, 28, 0, 0, 0, 0, time.UTC)})

	got, err := svc.Get(context.Background(), "h1", 2026)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	m := got.Pillars[0].Measures[0]
	if !m.HasFigure || m.Percent != 62 {
		t.Fatalf("want a resolved 62%% figure, got %+v", m)
	}
	if m.Met {
		t.Fatalf("62%% must not be met, got %+v", m)
	}

	m2 := got.Pillars[0].Measures[1]
	if !m2.HasFigure || m2.Percent != 100 || !m2.Met {
		t.Fatalf("want a met 100%% figure, got %+v", m2)
	}
}

func TestVisionGetRendersNoFigureWhenTheLinkedGoalIsGone(t *testing.T) {
	repo := newVisionRepoDouble()
	repo.seed(domain.Vision{
		HouseholdID: "h1", Year: 2026, Theme: "T", Version: 3,
		Pillars: []domain.Pillar{{Name: "P", Measures: []domain.Measure{
			// A link the repository still has, whose goal the reader cannot find.
			{Label: "Emergency fund", Kind: domain.MeasureLinked, GoalID: "gone"},
			// And the broken shape ON DELETE SET NULL leaves behind.
			{Label: "Old target", Kind: domain.MeasureBroken},
		}}},
	})
	svc := usecase.NewVisionService(repo, newGoalProgressDouble(),
		&fixedClock{now: time.Date(2026, 8, 28, 0, 0, 0, 0, time.UTC)})

	got, err := svc.Get(context.Background(), "h1", 2026)
	if err != nil {
		t.Fatalf("a missing goal must not fail the page: %v", err)
	}
	for i, m := range got.Pillars[0].Measures {
		if m.HasFigure {
			t.Fatalf("measure %d must render no figure, got %+v", i, m)
		}
		if m.Percent != 0 || m.Current != 0 || m.Target != 0 {
			t.Fatalf("measure %d must carry no numbers at all -- never a zero standing in for a figure: %+v", i, m)
		}
		if m.Label == "" {
			t.Fatalf("measure %d must keep its label", i)
		}
	}
}

func TestVisionGetMarksATypedMeasureMetAtTarget(t *testing.T) {
	repo := newVisionRepoDouble()
	repo.seed(domain.Vision{
		HouseholdID: "h1", Year: 2026, Theme: "T", Version: 1,
		Pillars: []domain.Pillar{{Name: "P", Measures: []domain.Measure{
			{Label: "Date nights / month", Kind: domain.MeasureTyped, Current: 2, Target: 2},
			{Label: "Phone-free dinners / week", Kind: domain.MeasureTyped, Current: 3, Target: 5},
		}}},
	})
	svc := usecase.NewVisionService(repo, newGoalProgressDouble(),
		&fixedClock{now: time.Date(2026, 8, 28, 0, 0, 0, 0, time.UTC)})

	got, _ := svc.Get(context.Background(), "h1", 2026)
	if !got.Pillars[0].Measures[0].Met {
		t.Fatal("2 of 2 must be met")
	}
	if got.Pillars[0].Measures[1].Met {
		t.Fatal("3 of 5 must not be met")
	}
}

// CurrentYear is a real deliverable of this task (the handler's default-year
// source), not just plumbing -- an untested delegation to the clock is
// exactly the kind of change a later refactor could silently break.
func TestVisionServiceCurrentYearUsesTheClock(t *testing.T) {
	svc := usecase.NewVisionService(newVisionRepoDouble(), newGoalProgressDouble(),
		&fixedClock{now: time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC)})

	if got := svc.CurrentYear(); got != 2027 {
		t.Fatalf("want the clock's own year, got %d", got)
	}
}
