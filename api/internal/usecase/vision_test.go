package usecase_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/andreasoentoro/hearth/api/internal/domain"
	"github.com/andreasoentoro/hearth/api/internal/usecase"
)

func TestVisionGetReturnsAnEmptyVisionForAYearNeverSet(t *testing.T) {
	svc := usecase.NewVisionService(newVisionRepoDouble(), newGoalProgressDouble())

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
	svc := usecase.NewVisionService(repo, goals)

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
	svc := usecase.NewVisionService(repo, newGoalProgressDouble())

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
	svc := usecase.NewVisionService(repo, newGoalProgressDouble())

	got, _ := svc.Get(context.Background(), "h1", 2026)
	if !got.Pillars[0].Measures[0].Met {
		t.Fatal("2 of 2 must be met")
	}
	if got.Pillars[0].Measures[1].Met {
		t.Fatal("3 of 5 must not be met")
	}
}

// CurrentYear is the year the Vision page opens on when a request names
// none. It is the year of the household's calendar day, which the caller
// passes in: on New Year's morning in Singapore that is already next year,
// while the server's own clock still reads the old one.
func TestVisionServiceCurrentYearIsTheYearOfTheHouseholdsDay(t *testing.T) {
	svc := usecase.NewVisionService(newVisionRepoDouble(), newGoalProgressDouble())

	householdToday := time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC)
	if got := svc.CurrentYear(householdToday); got != 2027 {
		t.Fatalf("CurrentYear = %d, want 2027, the year of the day it was given", got)
	}
}

// --- Save -------------------------------------------------------------

func TestVisionSaveValidatesBeforeTouchingTheRepository(t *testing.T) {
	repo := newVisionRepoDouble()
	svc := usecase.NewVisionService(repo, newGoalProgressDouble())

	_, err := svc.Save(context.Background(), "h1", 2026, domain.Vision{Theme: "   "})
	if !errors.Is(err, domain.ErrVisionThemeRequired) {
		t.Fatalf("want ErrVisionThemeRequired, got %v", err)
	}
	// The double has no opinion on the theme, so if Save had called it, a row
	// would land at h1/2026 (Version 0 against an empty repo is a create).
	// ErrNotFound here is proof no such call happened.
	if _, err := repo.Get(context.Background(), "h1", 2026); !errors.Is(err, domain.ErrNotFound) {
		t.Fatal("an invalid draft must never reach the repository")
	}
}

func TestVisionSaveOverwritesHouseholdAndYearFromTheRoute(t *testing.T) {
	repo := newVisionRepoDouble()
	repo.seed(domain.Vision{HouseholdID: "h1", Year: 2026, Theme: "Old theme", Version: 2})
	// The attacker's claimed household/year already has its own row, so a
	// mutation that lets the body's values through has somewhere real to
	// wrongly land, not just an ErrNotFound that fails for the wrong reason.
	repo.seed(domain.Vision{HouseholdID: "someone-else", Year: 1999, Theme: "Their theme", Version: 2})
	svc := usecase.NewVisionService(repo, newGoalProgressDouble())

	// A body claiming a different household and year. The route's values win:
	// a request body must never be able to write into someone else's
	// household by naming it.
	_, err := svc.Save(context.Background(), "h1", 2026, domain.Vision{
		HouseholdID: "someone-else", Year: 1999, Theme: "Slow down together", Version: 2,
	})
	if err != nil {
		t.Fatalf("save: %v", err)
	}
	got, err := repo.Get(context.Background(), "h1", 2026)
	if err != nil || got.Theme != "Slow down together" {
		t.Fatalf("want the route's household/year to hold the new theme, got %+v, err %v", got, err)
	}
	other, err := repo.Get(context.Background(), "someone-else", 1999)
	if err != nil || other.Theme != "Their theme" || other.Version != 2 {
		t.Fatalf("the body's claimed household/year must be untouched, got %+v, err %v", other, err)
	}
}

func TestVisionSaveReturnsTheComposedViewWithTheNewVersion(t *testing.T) {
	repo := newVisionRepoDouble()
	repo.seed(domain.Vision{HouseholdID: "h1", Year: 2026, Theme: "Old theme", Version: 4})
	goals := newGoalProgressDouble()
	goals.progress["g1"] = usecase.GoalProgress{GoalID: "g1", Name: "Emergency fund", Percent: 62}
	svc := usecase.NewVisionService(repo, goals)

	got, err := svc.Save(context.Background(), "h1", 2026, domain.Vision{
		Theme:   "Slow down together",
		Version: 4,
		Pillars: []domain.Pillar{{Name: "Money without fear", Measures: []domain.Measure{
			{Label: "Emergency fund", Kind: domain.MeasureLinked, GoalID: "g1"},
		}}},
	})
	if err != nil {
		t.Fatalf("save: %v", err)
	}
	if got.Version != 5 {
		t.Fatalf("the response must carry the version the next save will send, got %d", got.Version)
	}
	if got.Theme != "Slow down together" {
		t.Fatalf("want the saved theme back, got %q", got.Theme)
	}
	// A hand-rolled response echoing the draft's own fields would pass the
	// two checks above without resolving the link -- this is what proves
	// Save routes through compose exactly as Get does.
	m := got.Pillars[0].Measures[0]
	if !m.HasFigure || m.Percent != 62 {
		t.Fatalf("want the linked measure's figure resolved, got %+v", m)
	}
}

// TestVisionSaveCreatesTheFirstVisionForAYear is the flow a household hits
// first: TestVisionGetReturnsAnEmptyVisionForAYearNeverSet hands back
// Version 0 deliberately, so this save must be treated as a create, not an
// update against a row that does not exist.
func TestVisionSaveCreatesTheFirstVisionForAYear(t *testing.T) {
	repo := newVisionRepoDouble()
	svc := usecase.NewVisionService(repo, newGoalProgressDouble())

	got, err := svc.Save(context.Background(), "h1", 2026, domain.Vision{Theme: "Slow down together", Version: 0})
	if err != nil {
		t.Fatalf("save: %v", err)
	}
	if got.Version != 1 {
		t.Fatalf("the first save of a year must land at version 1, got %d", got.Version)
	}
}

func TestVisionSavePassesAConflictThrough(t *testing.T) {
	repo := newVisionRepoDouble()
	repo.seed(domain.Vision{HouseholdID: "h1", Year: 2026, Theme: "Old theme", Version: 3})
	svc := usecase.NewVisionService(repo, newGoalProgressDouble())

	_, err := svc.Save(context.Background(), "h1", 2026, domain.Vision{Theme: "T", Version: 1})
	if !errors.Is(err, domain.ErrVisionChanged) {
		t.Fatalf("want ErrVisionChanged, got %v", err)
	}
}

// --- visionRepoDouble.Save's own contract ------------------------------
//
// The double's create/update logic follows the port's doc comment and the
// real Postgres adapter. These tests exercise it directly, and every Save
// test above leans on it being right. They pin the two-case contract so a
// future edit to the double cannot silently drift from what
// VisionRepository.Save promises.

func TestVisionRepoDoubleSaveCreatesAtVersionZero(t *testing.T) {
	repo := newVisionRepoDouble()

	got, err := repo.Save(context.Background(), domain.Vision{HouseholdID: "h1", Year: 2026, Theme: "T", Version: 0})
	if err != nil {
		t.Fatalf("save: %v", err)
	}
	if got.Version != 1 {
		t.Fatalf("a create must land at version 1, got %d", got.Version)
	}
}

func TestVisionRepoDoubleSaveRefusesASecondCreateAtVersionZero(t *testing.T) {
	repo := newVisionRepoDouble()
	repo.seed(domain.Vision{HouseholdID: "h1", Year: 2026, Theme: "Existing", Version: 1})

	_, err := repo.Save(context.Background(), domain.Vision{HouseholdID: "h1", Year: 2026, Theme: "New", Version: 0})
	if !errors.Is(err, domain.ErrVisionChanged) {
		t.Fatalf("want ErrVisionChanged for a create racing an existing row, got %v", err)
	}
}

func TestVisionRepoDoubleSaveRefusesAStaleVersion(t *testing.T) {
	repo := newVisionRepoDouble()
	repo.seed(domain.Vision{HouseholdID: "h1", Year: 2026, Theme: "Existing", Version: 3})

	_, err := repo.Save(context.Background(), domain.Vision{HouseholdID: "h1", Year: 2026, Theme: "New", Version: 2})
	if !errors.Is(err, domain.ErrVisionChanged) {
		t.Fatalf("want ErrVisionChanged for a stale version, got %v", err)
	}
}

func TestVisionRepoDoubleSaveReportsNotFoundForAYearNeverSeen(t *testing.T) {
	repo := newVisionRepoDouble()

	_, err := repo.Save(context.Background(), domain.Vision{HouseholdID: "h1", Year: 2026, Theme: "T", Version: 5})
	if !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("want ErrNotFound for an update against a row that was never created, got %v", err)
	}
}
