package usecase_test

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/andreasoentoro/hearth/api/internal/domain"
	"github.com/andreasoentoro/hearth/api/internal/usecase"
)

// goalFixture wires a GoalService against in-memory doubles, one household
// ("house-1", SGD) -- the same shape budgetFixture uses.
type goalFixture struct {
	svc        *usecase.GoalService
	goals      *goalDouble
	households *householdDouble
}

func newGoalFixture(t *testing.T) *goalFixture {
	t.Helper()

	households := newHouseholdDouble()
	households.put(domain.Household{ID: "house-1", PrimaryCurrency: "SGD"})

	goals := newGoalDouble()

	svc := usecase.NewGoalService(usecase.GoalDeps{
		Goals:      goals,
		Households: households,
		FX:         staticTestRates{},
	})

	return &goalFixture{svc: svc, goals: goals, households: households}
}

// seedGoal writes a goal directly into the double, bypassing Create, the
// same way budgetFixture.addExpense seeds fakeTransactionRepo directly: a
// List test needs exact, hand-picked figures and dates, not also to depend
// on Create's own validation and normalisation succeeding first.
func (f *goalFixture) seedGoal(g domain.Goal) domain.Goal {
	f.goals.n++
	g.ID = fmt.Sprintf("seed-goal-%d", f.goals.n)
	if g.HouseholdID == "" {
		g.HouseholdID = "house-1"
	}
	f.goals.goals[g.ID] = g
	return g
}

// seedContribution appends a contribution row straight into the double for
// the same reason seedGoal does.
func (f *goalFixture) seedContribution(goalID string, amountMinor int64, currency string, occurredOn time.Time, source domain.ContributionSource) {
	f.goals.contribN++
	f.goals.contributions[goalID] = append(f.goals.contributions[goalID], domain.GoalContribution{
		ID:          fmt.Sprintf("seed-contribution-%d", f.goals.contribN),
		GoalID:      goalID,
		HouseholdID: "house-1",
		Amount:      domain.Money{Amount: amountMinor, Currency: currency},
		OccurredOn:  occurredOn,
		Source:      source,
	})
}

func findGoalView(t *testing.T, views []usecase.GoalView, goalID string) usecase.GoalView {
	t.Helper()
	for _, v := range views {
		if v.Goal.ID == goalID {
			return v
		}
	}
	t.Fatalf("no card for goal %q in %+v", goalID, views)
	return usecase.GoalView{}
}

// TestGoalListComposesTheDesignsCards pins the spec's own worked example --
// four goals with the design's own figures, mid-August 2026 -- and asserts
// each card's Percent and Status is what domain.GoalProgressPercent and
// domain.GoalStatusFor actually compute for them, not a re-derivation.
//
// Worked arithmetic, since none of these divide as cleanly as Budget's own
// worked example:
//
//	Bali:       65% (260000*100+200000)/400000 = 26200000/400000 = 65.
//	            monthsLeft Aug->Dec 2026 inclusive = 5. remaining = 140000.
//	            required = 140000/5 = 28000 exactly <= planned 35000 -> OnTrack.
//	Emergency:  62% (1850000*100+1500000)/3000000 = 186500000/3000000 = 62.
//	            dateless -> GoalStatusNone.
//	Education:  34% (4120000*100+6000000)/12000000 = 418000000/12000000 = 34.
//	            monthsLeft Aug 2026->Dec 2032 inclusive = 77. remaining = 7880000.
//	            required = ceil(7880000/77) = 102338 > planned 80000 -> Behind.
//	Car:        12% (360000*100+1500000)/3000000 = 37500000/3000000 = 12.
//	            monthsLeft Aug 2026->Dec 2029 inclusive = 41. remaining = 2640000.
//	            required = ceil(2640000/41) = 64391 > planned 40000 -> Behind.
func TestGoalListComposesTheDesignsCards(t *testing.T) {
	f := newGoalFixture(t)
	today := time.Date(2026, 8, 15, 0, 0, 0, 0, time.UTC)
	dec2026 := time.Date(2026, 12, 1, 0, 0, 0, 0, time.UTC)
	dec2032 := time.Date(2032, 12, 1, 0, 0, 0, 0, time.UTC)
	dec2029 := time.Date(2029, 12, 1, 0, 0, 0, 0, time.UTC)

	bali := f.seedGoal(domain.Goal{
		Name:           "Bali family trip",
		Target:         domain.Money{Amount: 400000, Currency: "SGD"}, // S$4,000.00
		TargetMonth:    &dec2026,
		PlannedMonthly: domain.Money{Amount: 35000, Currency: "SGD"}, // S$350.00/mo
	})
	f.seedContribution(bali.ID, 260000, "SGD", today, domain.ContributionManual) // S$2,600.00

	emergency := f.seedGoal(domain.Goal{
		Name:           "Emergency fund",
		Target:         domain.Money{Amount: 3000000, Currency: "SGD"}, // S$30,000.00
		PlannedMonthly: domain.Money{Amount: 50000, Currency: "SGD"},   // S$500.00/mo
	})
	f.seedContribution(emergency.ID, 1850000, "SGD", today, domain.ContributionManual) // S$18,500.00

	education := f.seedGoal(domain.Goal{
		Name:           "Education fund",
		Target:         domain.Money{Amount: 12000000, Currency: "SGD"}, // S$120,000.00
		TargetMonth:    &dec2032,
		PlannedMonthly: domain.Money{Amount: 80000, Currency: "SGD"}, // S$800.00/mo
	})
	f.seedContribution(education.ID, 4120000, "SGD", today, domain.ContributionManual) // S$41,200.00

	car := f.seedGoal(domain.Goal{
		Name:           "New family car",
		Target:         domain.Money{Amount: 3000000, Currency: "SGD"}, // S$30,000.00
		TargetMonth:    &dec2029,
		PlannedMonthly: domain.Money{Amount: 40000, Currency: "SGD"}, // S$400.00/mo
	})
	f.seedContribution(car.ID, 360000, "SGD", today, domain.ContributionManual) // S$3,600.00

	got, err := f.svc.List(context.Background(), "house-1", false, today)
	if err != nil {
		t.Fatalf("List: %v", err)
	}

	baliView := findGoalView(t, got.Goals, bali.ID)
	if baliView.Percent != 65 {
		t.Errorf("Bali Percent = %d, want 65", baliView.Percent)
	}
	if baliView.Status != domain.GoalOnTrack {
		t.Errorf("Bali Status = %v, want OnTrack", baliView.Status)
	}

	emergencyView := findGoalView(t, got.Goals, emergency.ID)
	if emergencyView.Percent != 62 {
		t.Errorf("Emergency Percent = %d, want 62", emergencyView.Percent)
	}
	if emergencyView.Status != domain.GoalStatusNone {
		t.Errorf("Emergency Status = %v, want None -- a dateless goal has nothing to be on track against", emergencyView.Status)
	}

	educationView := findGoalView(t, got.Goals, education.ID)
	if educationView.Percent != 34 {
		t.Errorf("Education Percent = %d, want 34", educationView.Percent)
	}
	if educationView.Status != domain.GoalBehind {
		t.Errorf("Education Status = %v, want Behind", educationView.Status)
	}

	carView := findGoalView(t, got.Goals, car.ID)
	if carView.Percent != 12 {
		t.Errorf("Car Percent = %d, want 12", carView.Percent)
	}
	if carView.Status != domain.GoalBehind {
		t.Errorf("Car Status = %v, want Behind", carView.Status)
	}
}

// TestGoalListCountsOnlyDatedUnachievedGoals is the spec's "X of Y on track"
// rule: Y (DatedCount) and the on-track count X both exclude an achieved
// goal entirely, and NoDateCount is its own, separate figure.
func TestGoalListCountsOnlyDatedUnachievedGoals(t *testing.T) {
	f := newGoalFixture(t)
	today := time.Date(2026, 8, 15, 0, 0, 0, 0, time.UTC)
	dec2026 := time.Date(2026, 12, 1, 0, 0, 0, 0, time.UTC)

	dateless := f.seedGoal(domain.Goal{
		Name: "Dateless", Target: domain.Money{Amount: 1000000, Currency: "SGD"},
		PlannedMonthly: domain.Money{Amount: 10000, Currency: "SGD"},
	})
	f.seedContribution(dateless.ID, 500000, "SGD", today, domain.ContributionManual)

	achieved := f.seedGoal(domain.Goal{
		Name: "Achieved", Target: domain.Money{Amount: 1000000, Currency: "SGD"}, TargetMonth: &dec2026,
		PlannedMonthly: domain.Money{Amount: 10000, Currency: "SGD"},
	})
	f.seedContribution(achieved.ID, 1000000, "SGD", today, domain.ContributionManual) // contributed == target

	onTrack := f.seedGoal(domain.Goal{
		Name: "On track", Target: domain.Money{Amount: 1000000, Currency: "SGD"}, TargetMonth: &dec2026,
		PlannedMonthly: domain.Money{Amount: 200000, Currency: "SGD"},
	})
	f.seedContribution(onTrack.ID, 500000, "SGD", today, domain.ContributionManual) // remaining 500000/5 = 100000 <= 200000

	behind := f.seedGoal(domain.Goal{
		Name: "Behind", Target: domain.Money{Amount: 1000000, Currency: "SGD"}, TargetMonth: &dec2026,
		PlannedMonthly: domain.Money{Amount: 100000, Currency: "SGD"},
	})
	f.seedContribution(behind.ID, 100000, "SGD", today, domain.ContributionManual) // remaining 900000/5 = 180000 > 100000

	got, err := f.svc.List(context.Background(), "house-1", false, today)
	if err != nil {
		t.Fatalf("List: %v", err)
	}

	if got.Summary.DatedCount != 2 {
		t.Errorf("DatedCount = %d, want 2 (on-track + behind; the achieved goal is excluded)", got.Summary.DatedCount)
	}
	if got.Summary.NoDateCount != 1 {
		t.Errorf("NoDateCount = %d, want 1", got.Summary.NoDateCount)
	}
	if got.Summary.OnTrackCount != 1 {
		t.Errorf("OnTrackCount = %d, want 1 (counted only among the 2 dated-unachieved goals)", got.Summary.OnTrackCount)
	}
}

// TestGoalListPlannedTotalConvertsThenAdds is LEARNING pattern 12, pinned for
// goals: PlannedMonthlyTotal is each goal's own figure converted to primary
// FIRST, then added -- never summed in minor units and converted once. A
// goal whose currency has no rate to primary (EUR, which staticTestRates
// does not know) is excluded from the total and counted in ExcludedNoRate,
// while its own card keeps rendering in its own currency untouched.
func TestGoalListPlannedTotalConvertsThenAdds(t *testing.T) {
	f := newGoalFixture(t)
	today := time.Date(2026, 8, 15, 0, 0, 0, 0, time.UTC)

	f.seedGoal(domain.Goal{
		Name: "SGD goal", Target: domain.Money{Amount: 10000000, Currency: "SGD"},
		PlannedMonthly: domain.Money{Amount: 40000, Currency: "SGD"}, // S$400.00/mo
	})
	// Rp124,100.00/mo converts to exactly S$10.00/mo: staticTestRates' IDR->SGD
	// is {1, 12410}, so Apply(12,410,000) = (12,410,000 + 6,205) / 12,410 = 1,000
	// with no remainder ambiguity.
	f.seedGoal(domain.Goal{
		Name: "IDR goal", Target: domain.Money{Amount: 500000000, Currency: "IDR"},
		PlannedMonthly: domain.Money{Amount: 12410000, Currency: "IDR"},
	})
	noRateGoal := f.seedGoal(domain.Goal{
		Name: "No rate goal", Target: domain.Money{Amount: 1000000, Currency: "EUR"},
		PlannedMonthly: domain.Money{Amount: 20000, Currency: "EUR"},
	})

	got, err := f.svc.List(context.Background(), "house-1", false, today)
	if err != nil {
		t.Fatalf("List: %v", err)
	}

	if got.Summary.PlannedMonthlyTotal.Currency != "SGD" {
		t.Errorf("PlannedMonthlyTotal.Currency = %q, want SGD", got.Summary.PlannedMonthlyTotal.Currency)
	}
	if got.Summary.PlannedMonthlyTotal.Amount != 41000 {
		t.Errorf("PlannedMonthlyTotal.Amount = %d, want 41000 (40000 SGD + 1000 converted from IDR)", got.Summary.PlannedMonthlyTotal.Amount)
	}
	if got.Summary.ExcludedNoRate != 1 {
		t.Errorf("ExcludedNoRate = %d, want 1 (the EUR goal)", got.Summary.ExcludedNoRate)
	}

	noRateView := findGoalView(t, got.Goals, noRateGoal.ID)
	if noRateView.Goal.PlannedMonthly.Currency != "EUR" {
		t.Errorf("excluded goal's own card currency = %q, want EUR -- only the summary totals exclude it", noRateView.Goal.PlannedMonthly.Currency)
	}
}

// TestGoalListActualThisMonthExcludesStartingBalances pins that
// GoalsSummary.ActualThisMonth is built ONLY from
// GoalRepository.MonthContributionTotals -- which already excludes
// source=starting_balance (Task 3) -- and that the service does not re-add
// a starting balance from anywhere else, such as ContributedMinor.
func TestGoalListActualThisMonthExcludesStartingBalances(t *testing.T) {
	f := newGoalFixture(t)
	today := time.Date(2026, 8, 15, 0, 0, 0, 0, time.UTC)

	goal := f.seedGoal(domain.Goal{
		Name: "Car", Target: domain.Money{Amount: 1000000, Currency: "SGD"},
		PlannedMonthly: domain.Money{Amount: 100000, Currency: "SGD"},
	})
	f.seedContribution(goal.ID, 500000, "SGD", today, domain.ContributionStartingBalance)
	f.seedContribution(goal.ID, 50000, "SGD", today, domain.ContributionManual)

	got, err := f.svc.List(context.Background(), "house-1", false, today)
	if err != nil {
		t.Fatalf("List: %v", err)
	}

	if got.Summary.ActualThisMonth.Amount != 50000 {
		t.Errorf("ActualThisMonth = %d, want 50000 -- the starting balance must not be re-added", got.Summary.ActualThisMonth.Amount)
	}

	// The card figure (ContributedMinor) is a different question -- "how
	// much has actually accumulated" -- and DOES include the starting
	// balance.
	view := findGoalView(t, got.Goals, goal.ID)
	if view.Contributed.Amount != 550000 {
		t.Errorf("Contributed = %d, want 550000 (starting balance + manual)", view.Contributed.Amount)
	}
}

// TestGoalListNextGoalIsTheEarliestDatedUnachievedOne pins the tie-break (by
// name) and the two exclusions: an achieved goal is skipped even when its
// date is earliest, and an all-dateless household reports no next goal.
func TestGoalListNextGoalIsTheEarliestDatedUnachievedOne(t *testing.T) {
	f := newGoalFixture(t)
	today := time.Date(2026, 8, 15, 0, 0, 0, 0, time.UTC)
	jan2027 := time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC)
	mar2027 := time.Date(2027, 3, 1, 0, 0, 0, 0, time.UTC)
	jun2027 := time.Date(2027, 6, 1, 0, 0, 0, 0, time.UTC)

	achievedEarly := f.seedGoal(domain.Goal{
		Name: "Achieved early", Target: domain.Money{Amount: 100000, Currency: "SGD"}, TargetMonth: &jan2027,
		PlannedMonthly: domain.Money{Amount: 10000, Currency: "SGD"},
	})
	f.seedContribution(achievedEarly.ID, 100000, "SGD", today, domain.ContributionManual) // achieved: skipped despite the earliest date

	f.seedGoal(domain.Goal{
		Name: "Zed goal", Target: domain.Money{Amount: 1000000, Currency: "SGD"}, TargetMonth: &mar2027,
		PlannedMonthly: domain.Money{Amount: 10000, Currency: "SGD"},
	})
	alpha := f.seedGoal(domain.Goal{
		Name: "Alpha goal", Target: domain.Money{Amount: 1000000, Currency: "SGD"}, TargetMonth: &mar2027,
		PlannedMonthly: domain.Money{Amount: 10000, Currency: "SGD"},
	})
	f.seedGoal(domain.Goal{
		Name: "Later goal", Target: domain.Money{Amount: 1000000, Currency: "SGD"}, TargetMonth: &jun2027,
		PlannedMonthly: domain.Money{Amount: 10000, Currency: "SGD"},
	})

	got, err := f.svc.List(context.Background(), "house-1", false, today)
	if err != nil {
		t.Fatalf("List: %v", err)
	}

	if got.Summary.NextGoalID != alpha.ID {
		t.Errorf("NextGoalID = %q, want %q (Alpha goal ties Zed goal on month, wins on name)", got.Summary.NextGoalID, alpha.ID)
	}
	if got.Summary.NextGoalName != "Alpha goal" {
		t.Errorf("NextGoalName = %q, want %q", got.Summary.NextGoalName, "Alpha goal")
	}
	if got.Summary.NextGoalMonth == nil || !got.Summary.NextGoalMonth.Equal(mar2027) {
		t.Errorf("NextGoalMonth = %v, want %v", got.Summary.NextGoalMonth, mar2027)
	}

	// All dateless: no candidate, so NextGoalID is "".
	f2 := newGoalFixture(t)
	f2.seedGoal(domain.Goal{Name: "Dateless A", Target: domain.Money{Amount: 100000, Currency: "SGD"}, PlannedMonthly: domain.Money{Amount: 10000, Currency: "SGD"}})
	f2.seedGoal(domain.Goal{Name: "Dateless B", Target: domain.Money{Amount: 100000, Currency: "SGD"}, PlannedMonthly: domain.Money{Amount: 10000, Currency: "SGD"}})

	got2, err := f2.svc.List(context.Background(), "house-1", false, today)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if got2.Summary.NextGoalID != "" {
		t.Errorf("NextGoalID = %q, want \"\" -- every goal is dateless", got2.Summary.NextGoalID)
	}
}

// TestGoalCreateValidates walks every guard Create runs before its
// repository call, plus the two normalisations (target month, starting
// balance) that are not guards at all.
func TestGoalCreateValidates(t *testing.T) {
	f := newGoalFixture(t)
	ctx := context.Background()
	createdOn := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)

	base := func() usecase.NewGoal {
		return usecase.NewGoal{
			HouseholdID:         "house-1",
			Name:                "Test goal",
			TargetMinor:         100000,
			Currency:            "SGD",
			PlannedMonthlyMinor: 10000,
		}
	}

	t.Run("empty name refused", func(t *testing.T) {
		in := base()
		in.Name = "   "
		if _, err := f.svc.Create(ctx, in, createdOn); !errors.Is(err, domain.ErrGoalNameRequired) {
			t.Fatalf("err = %v, want ErrGoalNameRequired", err)
		}
	})

	t.Run("zero target refused", func(t *testing.T) {
		in := base()
		in.Name = "Zero target"
		in.TargetMinor = 0
		if _, err := f.svc.Create(ctx, in, createdOn); !errors.Is(err, domain.ErrGoalTargetNotPositive) {
			t.Fatalf("err = %v, want ErrGoalTargetNotPositive", err)
		}
	})

	t.Run("negative target refused", func(t *testing.T) {
		in := base()
		in.Name = "Negative target"
		in.TargetMinor = -1
		if _, err := f.svc.Create(ctx, in, createdOn); !errors.Is(err, domain.ErrGoalTargetNotPositive) {
			t.Fatalf("err = %v, want ErrGoalTargetNotPositive", err)
		}
	})

	t.Run("negative planned monthly refused", func(t *testing.T) {
		in := base()
		in.Name = "Negative planned"
		in.PlannedMonthlyMinor = -1
		if _, err := f.svc.Create(ctx, in, createdOn); !errors.Is(err, domain.ErrGoalPlannedMonthlyNegative) {
			t.Fatalf("err = %v, want ErrGoalPlannedMonthlyNegative", err)
		}
	})

	t.Run("unknown currency refused", func(t *testing.T) {
		in := base()
		in.Name = "Bad currency"
		in.Currency = "ZZZ"
		_, err := f.svc.Create(ctx, in, createdOn)
		if !errors.Is(err, domain.ErrInvalidMoney) {
			t.Fatalf("err = %v, want domain.ErrInvalidMoney (ParseCurrency's own wrap)", err)
		}
	})

	t.Run("target month normalised to first of month", func(t *testing.T) {
		in := base()
		in.Name = "Mid month date"
		mid := time.Date(2026, 12, 15, 9, 30, 0, 0, time.UTC)
		in.TargetMonth = &mid
		got, err := f.svc.Create(ctx, in, createdOn)
		if err != nil {
			t.Fatalf("Create: %v", err)
		}
		want := time.Date(2026, 12, 1, 0, 0, 0, 0, time.UTC)
		if got.TargetMonth == nil || !got.TargetMonth.Equal(want) {
			t.Fatalf("TargetMonth = %v, want %v", got.TargetMonth, want)
		}
	})

	t.Run("negative starting balance allowed and round-trips", func(t *testing.T) {
		in := base()
		in.Name = "Deficit goal"
		in.StartingBalanceMinor = -20000
		created, err := f.svc.Create(ctx, in, createdOn)
		if err != nil {
			t.Fatalf("Create: %v", err)
		}
		list, err := f.svc.List(ctx, "house-1", false, createdOn)
		if err != nil {
			t.Fatalf("List: %v", err)
		}
		view := findGoalView(t, list.Goals, created.ID)
		if view.Contributed.Amount != -20000 {
			t.Fatalf("Contributed = %d, want -20000", view.Contributed.Amount)
		}
	})

	t.Run("zero starting balance writes no contribution", func(t *testing.T) {
		in := base()
		in.Name = "No starting balance"
		in.StartingBalanceMinor = 0
		created, err := f.svc.Create(ctx, in, createdOn)
		if err != nil {
			t.Fatalf("Create: %v", err)
		}
		contributions, err := f.svc.Contributions(ctx, "house-1", created.ID)
		if err != nil {
			t.Fatalf("Contributions: %v", err)
		}
		if len(contributions) != 0 {
			t.Fatalf("contributions = %+v, want none", contributions)
		}
	})
}

// TestGoalUpdateRefusesACurrencyChangeAndClearsADate exercises the PATCH
// convention. GoalUpdate carries no currency field at all -- see its own
// doc comment -- so there is no way for a caller to even attempt a currency
// change through Update's public signature; ErrGoalCurrencyImmutable
// belongs to Task 8's PATCH decoder (the design spec's own Error handling
// section requires refusing an unexpected "currency" key on the request
// body), not to this method, and no black-box test here can reach it. What
// this test pins instead is that the round trip actually holds -- an
// unrelated patch never disturbs Target.Currency -- plus the ClearTargetMonth
// convention.
func TestGoalUpdateRefusesACurrencyChangeAndClearsADate(t *testing.T) {
	f := newGoalFixture(t)
	ctx := context.Background()
	createdOn := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	dec2029 := time.Date(2029, 12, 1, 0, 0, 0, 0, time.UTC)

	created, err := f.svc.Create(ctx, usecase.NewGoal{
		HouseholdID: "house-1", Name: "Car", TargetMinor: 1000000, Currency: "SGD",
		TargetMonth: &dec2029, PlannedMonthlyMinor: 40000,
	}, createdOn)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	newName := "New car"
	updated, err := f.svc.Update(ctx, "house-1", created.ID, usecase.GoalUpdate{Name: &newName})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if updated.Target.Currency != "SGD" {
		t.Fatalf("Target.Currency = %q, want SGD unchanged", updated.Target.Currency)
	}
	if updated.Name != "New car" {
		t.Fatalf("Name = %q, want %q", updated.Name, "New car")
	}

	// ClearTargetMonth true clears the date even though TargetMonth itself
	// is nil -- a nil TargetMonth alone must not be read as "clear," or a
	// caller updating an unrelated field would silently blank the date.
	cleared, err := f.svc.Update(ctx, "house-1", created.ID, usecase.GoalUpdate{ClearTargetMonth: true})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if cleared.TargetMonth != nil {
		t.Fatalf("TargetMonth = %v, want nil after ClearTargetMonth", cleared.TargetMonth)
	}

	// Both nil (ClearTargetMonth false, TargetMonth nil) leaves whatever
	// date is already stored untouched.
	newDate := time.Date(2030, 6, 1, 0, 0, 0, 0, time.UTC)
	withDate, err := f.svc.Update(ctx, "house-1", created.ID, usecase.GoalUpdate{TargetMonth: &newDate})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if withDate.TargetMonth == nil || !withDate.TargetMonth.Equal(newDate) {
		t.Fatalf("TargetMonth = %v, want %v", withDate.TargetMonth, newDate)
	}

	untouched, err := f.svc.Update(ctx, "house-1", created.ID, usecase.GoalUpdate{})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if untouched.TargetMonth == nil || !untouched.TargetMonth.Equal(newDate) {
		t.Fatalf("TargetMonth = %v, want unchanged %v after an all-nil patch", untouched.TargetMonth, newDate)
	}
}

// TestGoalAddContributionRefusesZeroAndArchivedGoals pins the two guards
// AddContribution runs before any write, plus that the write itself lands in
// the goal's own currency rather than the household's primary.
func TestGoalAddContributionRefusesZeroAndArchivedGoals(t *testing.T) {
	f := newGoalFixture(t)
	ctx := context.Background()
	createdOn := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)

	idrGoal, err := f.svc.Create(ctx, usecase.NewGoal{
		HouseholdID: "house-1", Name: "IDR goal", TargetMinor: 500000000, Currency: "IDR",
		PlannedMonthlyMinor: 12410000,
	}, createdOn)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	t.Run("zero amount refused", func(t *testing.T) {
		_, err := f.svc.AddContribution(ctx, usecase.NewContribution{
			HouseholdID: "house-1", GoalID: idrGoal.ID, AmountMinor: 0, OccurredOn: createdOn,
		})
		if !errors.Is(err, domain.ErrContributionAmountZero) {
			t.Fatalf("err = %v, want ErrContributionAmountZero", err)
		}
	})

	t.Run("archived goal refused", func(t *testing.T) {
		archivable, err := f.svc.Create(ctx, usecase.NewGoal{
			HouseholdID: "house-1", Name: "Archived goal", TargetMinor: 100000, Currency: "SGD",
			PlannedMonthlyMinor: 10000,
		}, createdOn)
		if err != nil {
			t.Fatalf("Create: %v", err)
		}
		if _, err := f.svc.SetArchived(ctx, "house-1", archivable.ID, true, createdOn); err != nil {
			t.Fatalf("SetArchived: %v", err)
		}
		_, err = f.svc.AddContribution(ctx, usecase.NewContribution{
			HouseholdID: "house-1", GoalID: archivable.ID, AmountMinor: 1000, OccurredOn: createdOn,
		})
		if !errors.Is(err, domain.ErrGoalArchived) {
			t.Fatalf("err = %v, want ErrGoalArchived", err)
		}
	})

	t.Run("valid contribution written in the goal's own currency", func(t *testing.T) {
		got, err := f.svc.AddContribution(ctx, usecase.NewContribution{
			HouseholdID: "house-1", GoalID: idrGoal.ID, AmountMinor: 500000, OccurredOn: createdOn, Note: "Bonus",
		})
		if err != nil {
			t.Fatalf("AddContribution: %v", err)
		}
		if got.Amount.Currency != "IDR" {
			t.Fatalf("Amount.Currency = %q, want IDR -- the goal's own currency, not the household's primary SGD", got.Amount.Currency)
		}
		if got.Amount.Amount != 500000 {
			t.Fatalf("Amount.Amount = %d, want 500000", got.Amount.Amount)
		}
	})
}

// TestGoalAddContributionRefusesCrossHouseholdGoal pins the forward
// dependency the task brief calls out: InsertGoalContribution's SQL has no
// constraint tying goal_contributions.goal_id to its own household_id, so
// nothing at the database layer refuses a caller naming another household's
// goal id as its own. AddContribution's Goals.Get(householdID, goalID) call
// is the only thing that can.
//
// The assertion deliberately does NOT use Contributions() to prove nothing
// was written: ListContributions filters by the row's own household_id, so
// a forged row (written with the attacker's household id, had the guard not
// existed) would be invisible there even with the bug present -- exactly
// the "invisible to the victim's ListContributions" half of the brief's own
// warning. ContributedMinor is where a leak would actually show, because
// GetGoalWithTotal and ListGoalsWithTotals both join goal_contributions to
// goals by goal_id alone, with no household_id check on the contribution
// side -- so this test reads the victim's card through List instead.
func TestGoalAddContributionRefusesCrossHouseholdGoal(t *testing.T) {
	f := newGoalFixture(t)
	ctx := context.Background()
	createdOn := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)

	victimGoal, err := f.svc.Create(ctx, usecase.NewGoal{
		HouseholdID: "house-1", Name: "Victim's goal", TargetMinor: 1000000, Currency: "SGD",
		PlannedMonthlyMinor: 10000, StartingBalanceMinor: 50000,
	}, createdOn)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	_, err = f.svc.AddContribution(ctx, usecase.NewContribution{
		HouseholdID: "house-2", GoalID: victimGoal.ID, AmountMinor: 999999, OccurredOn: createdOn, Note: "stolen",
	})
	if !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("err = %v, want domain.ErrNotFound -- a goal from another household must be indistinguishable from one that doesn't exist", err)
	}

	got, err := f.svc.List(ctx, "house-1", false, createdOn)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	view := findGoalView(t, got.Goals, victimGoal.ID)
	if view.Contributed.Amount != 50000 {
		t.Fatalf("Contributed = %d, want 50000 (unchanged) -- the attacker's contribution must not have been written at all", view.Contributed.Amount)
	}
}
