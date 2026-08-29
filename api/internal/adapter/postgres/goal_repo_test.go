package postgres_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/andreasoentoro/hearth/api/internal/adapter/postgres"
	"github.com/andreasoentoro/hearth/api/internal/domain"
	"github.com/andreasoentoro/hearth/api/internal/usecase"
)

// newTestGoal is the shared fixture every test below builds on: a goal with
// no target month and a zero starting balance, which every test overrides
// only the fields it actually cares about.
func newTestGoal(householdID, name string) domain.Goal {
	return domain.Goal{
		HouseholdID:    householdID,
		Name:           name,
		Target:         moneyOf(1000000),
		PlannedMonthly: moneyOf(50000),
	}
}

func TestGoalCreateWritesTheOpeningContribution(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	repo := postgres.NewGoalRepo(db)
	householdID := insertTestHousehold(t, db)

	createdOn := july(5)
	g, err := repo.Create(ctx, newTestGoal(householdID, "Emergency fund"), 250000, createdOn)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	record, err := repo.Get(ctx, householdID, g.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if record.ContributedMinor != 250000 {
		t.Fatalf("ContributedMinor = %d, want 250000", record.ContributedMinor)
	}

	contributions, err := repo.ListContributions(ctx, householdID, g.ID, 0)
	if err != nil {
		t.Fatalf("ListContributions: %v", err)
	}
	if len(contributions) != 1 {
		t.Fatalf("len(contributions) = %d, want exactly 1", len(contributions))
	}
	c := contributions[0]
	if c.Source != domain.ContributionStartingBalance {
		t.Fatalf("Source = %q, want starting_balance", c.Source)
	}
	if !c.OccurredOn.Equal(createdOn) {
		t.Fatalf("OccurredOn = %v, want %v (createdOn)", c.OccurredOn, createdOn)
	}
	if c.Amount.Amount != 250000 {
		t.Fatalf("Amount = %d, want 250000", c.Amount.Amount)
	}
}

// TestGoalCreateThatFailsWritesNothingAtAll is the reachable half of the
// atomicity claim (see this file's own package doc and the brief's own
// comment on this test): a goal insert that fails on a name collision must
// leave no orphaned contribution row anywhere for the household, even though
// the failed Create asked for a non-zero starting balance.
func TestGoalCreateThatFailsWritesNothingAtAll(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	repo := postgres.NewGoalRepo(db)
	householdID := insertTestHousehold(t, db)

	if _, err := repo.Create(ctx, newTestGoal(householdID, "New Car"), 0, july(1)); err != nil {
		t.Fatalf("first Create: %v", err)
	}

	_, err := repo.Create(ctx, newTestGoal(householdID, "New Car"), 500000, july(2))
	if !errors.Is(err, domain.ErrGoalNameTaken) {
		t.Fatalf("err = %v, want domain.ErrGoalNameTaken", err)
	}

	// No orphan: the household has zero contribution rows at all, not just
	// zero on the goal that failed to write -- the first, successful Create
	// above asked for a zero starting balance, which writes nothing either.
	var count int
	if err := db.Pool().QueryRow(ctx, `SELECT count(*) FROM goal_contributions WHERE household_id = $1`, householdID).
		Scan(&count); err != nil {
		t.Fatalf("count goal_contributions: %v", err)
	}
	if count != 0 {
		t.Fatalf("goal_contributions count = %d, want 0 -- no orphan for the goal that never got written", count)
	}
}

func TestGoalCreateWithZeroStartingBalanceWritesNoContribution(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	repo := postgres.NewGoalRepo(db)
	householdID := insertTestHousehold(t, db)

	g, err := repo.Create(ctx, newTestGoal(householdID, "Holiday"), 0, july(1))
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	record, err := repo.Get(ctx, householdID, g.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if record.ContributedMinor != 0 {
		t.Fatalf("ContributedMinor = %d, want 0", record.ContributedMinor)
	}

	contributions, err := repo.ListContributions(ctx, householdID, g.ID, 0)
	if err != nil {
		t.Fatalf("ListContributions: %v", err)
	}
	if len(contributions) != 0 {
		t.Fatalf("len(contributions) = %d, want 0 -- zero is not a contribution", len(contributions))
	}
}

func TestGoalCreateDuplicateNameIsErrGoalNameTaken(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	repo := postgres.NewGoalRepo(db)
	householdID := insertTestHousehold(t, db)

	first, err := repo.Create(ctx, newTestGoal(householdID, "Wedding"), 0, july(1))
	if err != nil {
		t.Fatalf("first Create: %v", err)
	}

	if _, err := repo.Create(ctx, newTestGoal(householdID, "Wedding"), 0, july(1)); !errors.Is(err, domain.ErrGoalNameTaken) {
		t.Fatalf("err = %v, want domain.ErrGoalNameTaken", err)
	}

	if _, err := repo.SetArchived(ctx, householdID, first.ID, true, july(2)); err != nil {
		t.Fatalf("SetArchived: %v", err)
	}

	// An archived goal still occupies its unique key -- the categories
	// gotcha, restated for goals.
	if _, err := repo.Create(ctx, newTestGoal(householdID, "Wedding"), 0, july(1)); !errors.Is(err, domain.ErrGoalNameTaken) {
		t.Fatalf("err after archiving the first = %v, want still domain.ErrGoalNameTaken", err)
	}
}

func TestGoalGetFromAnotherHouseholdIsErrNotFound(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	repo := postgres.NewGoalRepo(db)
	householdID := insertTestHousehold(t, db)
	otherHouseholdID := insertTestHousehold(t, db)

	g, err := repo.Create(ctx, newTestGoal(householdID, "Down payment"), 0, july(1))
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	if _, err := repo.Get(ctx, otherHouseholdID, g.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("err = %v, want domain.ErrNotFound", err)
	}
}

func TestGoalUpdateClearsTargetMonth(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	repo := postgres.NewGoalRepo(db)
	householdID := insertTestHousehold(t, db)

	target := july(1)
	goal := newTestGoal(householdID, "Laptop")
	goal.TargetMonth = &target
	g, err := repo.Create(ctx, goal, 0, july(1))
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if g.TargetMonth == nil {
		t.Fatal("TargetMonth is nil right after Create, want the month just set")
	}

	g.TargetMonth = nil
	updated, err := repo.Update(ctx, g)
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if updated.TargetMonth != nil {
		t.Fatalf("TargetMonth = %v, want nil after clearing, not the zero time", updated.TargetMonth)
	}

	fetched, err := repo.Get(ctx, householdID, g.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if fetched.Goal.TargetMonth != nil {
		t.Fatalf("Get after Update: TargetMonth = %v, want nil", fetched.Goal.TargetMonth)
	}
}

func TestGoalArchiveAndRestoreKeepContributions(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	repo := postgres.NewGoalRepo(db)
	householdID := insertTestHousehold(t, db)

	g, err := repo.Create(ctx, newTestGoal(householdID, "Sabbatical"), 300000, july(1))
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	firstStamp := july(10)
	archived, err := repo.SetArchived(ctx, householdID, g.ID, true, firstStamp)
	if err != nil {
		t.Fatalf("SetArchived(true): %v", err)
	}
	if archived.ArchivedAt == nil || !archived.ArchivedAt.Equal(firstStamp) {
		t.Fatalf("ArchivedAt = %v, want %v", archived.ArchivedAt, firstStamp)
	}

	live, err := repo.List(ctx, householdID, false)
	if err != nil {
		t.Fatalf("List(false): %v", err)
	}
	for _, r := range live {
		if r.Goal.ID == g.ID {
			t.Fatal("List(false) includes the archived goal, want it omitted")
		}
	}

	all, err := repo.List(ctx, householdID, true)
	if err != nil {
		t.Fatalf("List(true): %v", err)
	}
	var found bool
	for _, r := range all {
		if r.Goal.ID == g.ID {
			found = true
			if r.ContributedMinor != 300000 {
				t.Fatalf("ContributedMinor = %d, want 300000 -- archiving must not touch contributions", r.ContributedMinor)
			}
		}
	}
	if !found {
		t.Fatal("List(true) does not include the archived goal")
	}

	// Archiving twice does not move the original stamp forward.
	secondStamp := july(20)
	archivedAgain, err := repo.SetArchived(ctx, householdID, g.ID, true, secondStamp)
	if err != nil {
		t.Fatalf("second SetArchived(true): %v", err)
	}
	if archivedAgain.ArchivedAt == nil || !archivedAgain.ArchivedAt.Equal(firstStamp) {
		t.Fatalf("ArchivedAt after second archive = %v, want it to keep the FIRST stamp %v", archivedAgain.ArchivedAt, firstStamp)
	}

	restored, err := repo.SetArchived(ctx, householdID, g.ID, false, july(25))
	if err != nil {
		t.Fatalf("SetArchived(false): %v", err)
	}
	if restored.ArchivedAt != nil {
		t.Fatalf("ArchivedAt after restore = %v, want nil", restored.ArchivedAt)
	}
}

func TestGoalMonthContributionTotalsExcludesStartingBalance(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	repo := postgres.NewGoalRepo(db)
	householdID := insertTestHousehold(t, db)

	g, err := repo.Create(ctx, newTestGoal(householdID, "Rainy day"), 400000, july(5))
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if _, err := repo.AddContribution(ctx, domain.GoalContribution{
		GoalID: g.ID, HouseholdID: householdID,
		Amount: moneyOf(20000), OccurredOn: july(10), Source: domain.ContributionManual,
	}); err != nil {
		t.Fatalf("AddContribution this month: %v", err)
	}
	lastMonth := time.Date(2026, time.June, 15, 0, 0, 0, 0, time.UTC)
	if _, err := repo.AddContribution(ctx, domain.GoalContribution{
		GoalID: g.ID, HouseholdID: householdID,
		Amount: moneyOf(15000), OccurredOn: lastMonth, Source: domain.ContributionManual,
	}); err != nil {
		t.Fatalf("AddContribution last month: %v", err)
	}

	totals, err := repo.MonthContributionTotals(ctx, householdID, july(1))
	if err != nil {
		t.Fatalf("MonthContributionTotals: %v", err)
	}
	if len(totals) != 1 {
		t.Fatalf("totals = %+v, want exactly one goal", totals)
	}
	if totals[0].GoalID != g.ID || totals[0].AmountMinor != 20000 {
		t.Fatalf("totals[0] = %+v, want goal %s at 20000 (the manual contribution alone, "+
			"never the 400000 starting balance or last month's 15000)", totals[0], g.ID)
	}
}

func TestGoalMonthContributionTotalsExcludesArchivedGoals(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	repo := postgres.NewGoalRepo(db)
	householdID := insertTestHousehold(t, db)

	g, err := repo.Create(ctx, newTestGoal(householdID, "Archived goal"), 0, july(1))
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if _, err := repo.AddContribution(ctx, domain.GoalContribution{
		GoalID: g.ID, HouseholdID: householdID,
		Amount: moneyOf(10000), OccurredOn: july(5), Source: domain.ContributionManual,
	}); err != nil {
		t.Fatalf("AddContribution: %v", err)
	}
	if _, err := repo.SetArchived(ctx, householdID, g.ID, true, july(6)); err != nil {
		t.Fatalf("SetArchived: %v", err)
	}

	totals, err := repo.MonthContributionTotals(ctx, householdID, july(1))
	if err != nil {
		t.Fatalf("MonthContributionTotals: %v", err)
	}
	if len(totals) != 0 {
		t.Fatalf("totals = %+v, want none -- the only goal with a contribution this month is archived", totals)
	}
}

func TestGoalDeleteContributionRemovesExactlyThatRow(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	repo := postgres.NewGoalRepo(db)
	householdID := insertTestHousehold(t, db)

	g, err := repo.Create(ctx, newTestGoal(householdID, "Two contributions"), 0, july(1))
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	first, err := repo.AddContribution(ctx, domain.GoalContribution{
		GoalID: g.ID, HouseholdID: householdID,
		Amount: moneyOf(30000), OccurredOn: july(2), Source: domain.ContributionManual,
	})
	if err != nil {
		t.Fatalf("AddContribution first: %v", err)
	}
	if _, err := repo.AddContribution(ctx, domain.GoalContribution{
		GoalID: g.ID, HouseholdID: householdID,
		Amount: moneyOf(45000), OccurredOn: july(3), Source: domain.ContributionManual,
	}); err != nil {
		t.Fatalf("AddContribution second: %v", err)
	}

	before, err := repo.Get(ctx, householdID, g.ID)
	if err != nil {
		t.Fatalf("Get before delete: %v", err)
	}

	if err := repo.DeleteContribution(ctx, householdID, g.ID, first.ID); err != nil {
		t.Fatalf("DeleteContribution: %v", err)
	}

	after, err := repo.Get(ctx, householdID, g.ID)
	if err != nil {
		t.Fatalf("Get after delete: %v", err)
	}
	if after.ContributedMinor != before.ContributedMinor-30000 {
		t.Fatalf("ContributedMinor = %d, want %d (dropped by exactly the deleted amount)",
			after.ContributedMinor, before.ContributedMinor-30000)
	}

	remaining, err := repo.ListContributions(ctx, householdID, g.ID, 0)
	if err != nil {
		t.Fatalf("ListContributions: %v", err)
	}
	if len(remaining) != 1 || remaining[0].Amount.Amount != 45000 {
		t.Fatalf("remaining = %+v, want exactly the surviving 45000 contribution", remaining)
	}

	if err := repo.DeleteContribution(ctx, householdID, g.ID, first.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("second delete of the same id: err = %v, want domain.ErrNotFound", err)
	}
}

func TestGoalDeleteContributionOfAnotherGoalIsErrNotFound(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	repo := postgres.NewGoalRepo(db)
	householdID := insertTestHousehold(t, db)

	goalA, err := repo.Create(ctx, newTestGoal(householdID, "Goal A"), 0, july(1))
	if err != nil {
		t.Fatalf("Create goal A: %v", err)
	}
	goalB, err := repo.Create(ctx, newTestGoal(householdID, "Goal B"), 0, july(1))
	if err != nil {
		t.Fatalf("Create goal B: %v", err)
	}
	contribution, err := repo.AddContribution(ctx, domain.GoalContribution{
		GoalID: goalA.ID, HouseholdID: householdID,
		Amount: moneyOf(10000), OccurredOn: july(2), Source: domain.ContributionManual,
	})
	if err != nil {
		t.Fatalf("AddContribution: %v", err)
	}

	// The pair is checked, not just the contribution id: goalB is real and in
	// this household, but the contribution belongs to goalA.
	if err := repo.DeleteContribution(ctx, householdID, goalB.ID, contribution.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("err = %v, want domain.ErrNotFound", err)
	}

	remaining, err := repo.ListContributions(ctx, householdID, goalA.ID, 0)
	if err != nil {
		t.Fatalf("ListContributions: %v", err)
	}
	if len(remaining) != 1 {
		t.Fatalf("len(remaining) = %d, want 1 -- the row must still be there", len(remaining))
	}
}

func TestGoalDeleteContributionFromAnotherHouseholdIsErrNotFound(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	repo := postgres.NewGoalRepo(db)
	householdID := insertTestHousehold(t, db)
	otherHouseholdID := insertTestHousehold(t, db)

	g, err := repo.Create(ctx, newTestGoal(householdID, "Mine"), 0, july(1))
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	contribution, err := repo.AddContribution(ctx, domain.GoalContribution{
		GoalID: g.ID, HouseholdID: householdID,
		Amount: moneyOf(10000), OccurredOn: july(2), Source: domain.ContributionManual,
	})
	if err != nil {
		t.Fatalf("AddContribution: %v", err)
	}

	if err := repo.DeleteContribution(ctx, otherHouseholdID, g.ID, contribution.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("err = %v, want domain.ErrNotFound", err)
	}

	remaining, err := repo.ListContributions(ctx, householdID, g.ID, 0)
	if err != nil {
		t.Fatalf("ListContributions: %v", err)
	}
	if len(remaining) != 1 {
		t.Fatalf("len(remaining) = %d, want 1 -- the row must still be there", len(remaining))
	}
}

// TestGoalListOrdersDatedGoalsFirstThenDateless is not in the brief's own
// enumerated test list, but GoalRepository.List's doc comment pins this
// ordering by name specifically so an ORDER BY cannot silently choose the
// wrong NULL placement -- "DESC NULLS LAST" is two words away from "DESC",
// Postgres's own default null-ordering for DESC being NULLS FIRST (the
// opposite of what the port requires). Left unprotected, that mutation would
// pass every other test in this file, since none of them lists more than one
// goal at a time.
func TestGoalListOrdersDatedGoalsFirstThenDateless(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	repo := postgres.NewGoalRepo(db)
	householdID := insertTestHousehold(t, db)

	sept := time.Date(2026, time.September, 1, 0, 0, 0, 0, time.UTC)
	dec := time.Date(2026, time.December, 1, 0, 0, 0, 0, time.UTC)

	makeGoal := func(name string, targetMonth *time.Time) {
		t.Helper()
		g := newTestGoal(householdID, name)
		g.TargetMonth = targetMonth
		if _, err := repo.Create(ctx, g, 0, july(1)); err != nil {
			t.Fatalf("Create %s: %v", name, err)
		}
	}
	// Deliberately not created in the order List should return them, so a
	// query with no ORDER BY at all could not accidentally pass.
	makeGoal("Zebra dateless", nil)
	makeGoal("December goal", &dec)
	makeGoal("Ant dateless", nil)
	makeGoal("September goal", &sept)

	goals, err := repo.List(ctx, householdID, false)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(goals) != 4 {
		t.Fatalf("len(goals) = %d, want 4", len(goals))
	}
	var names []string
	for _, g := range goals {
		names = append(names, g.Goal.Name)
	}
	want := []string{"December goal", "September goal", "Ant dateless", "Zebra dateless"}
	for i, name := range want {
		if names[i] != name {
			t.Fatalf("List order = %v, want %v (dated newest-first, then dateless by name)", names, want)
		}
	}
}

// TestGoalListContributionsRespectsALowLimit proves the limit parameter is
// actually threaded into the query, not merely defaulted -- the other tests
// in this file all call ListContributions with limit 0 (which the port
// treats as "50", see the doc comment) and would not notice a limit that was
// silently ignored.
func TestGoalListContributionsRespectsALowLimit(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	repo := postgres.NewGoalRepo(db)
	householdID := insertTestHousehold(t, db)

	g, err := repo.Create(ctx, newTestGoal(householdID, "Limit test"), 0, july(1))
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	for _, day := range []int{2, 3, 4} {
		if _, err := repo.AddContribution(ctx, domain.GoalContribution{
			GoalID: g.ID, HouseholdID: householdID,
			Amount: moneyOf(int64(day) * 1000), OccurredOn: july(day), Source: domain.ContributionManual,
		}); err != nil {
			t.Fatalf("AddContribution day %d: %v", day, err)
		}
	}

	limited, err := repo.ListContributions(ctx, householdID, g.ID, 2)
	if err != nil {
		t.Fatalf("ListContributions with limit 2: %v", err)
	}
	if len(limited) != 2 {
		t.Fatalf("len(limited) = %d, want exactly 2", len(limited))
	}
	// Newest first: the two most recent of the three (July 4 and July 3).
	if limited[0].Amount.Amount != 4000 || limited[1].Amount.Amount != 3000 {
		t.Fatalf("limited = %+v, want [4000, 3000] newest first", limited)
	}
}

// TestDeleteManualContributionLeavesEveryStampAlone pins
// GoalRepository.DeleteContribution's own doc comment from the other
// direction: deleting a MANUAL contribution on a goal that also holds a
// budget_rollover contribution must leave that rollover's stamp completely
// untouched -- ClearBudgetRollover only ever runs for a deleted row whose OWN
// source is budget_rollover, never merely because the same goal has one
// somewhere.
func TestDeleteManualContributionLeavesEveryStampAlone(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	budgetRepo := postgres.NewBudgetRepo(db)
	goalRepo := postgres.NewGoalRepo(db)
	householdID := insertTestHousehold(t, db)
	_, goalID := rolloverFixture(t, db, budgetRepo, goalRepo, householdID, july(17), "Goal A")

	if _, err := budgetRepo.RollOverToGoal(ctx, usecase.RollOverToGoalInput{
		HouseholdID: householdID,
		Month:       july(17),
		GoalID:      goalID,
		Amount:      moneyOf(20000),
		OccurredOn:  august(3),
	}); err != nil {
		t.Fatalf("RollOverToGoal: %v", err)
	}

	manual, err := goalRepo.AddContribution(ctx, domain.GoalContribution{
		GoalID: goalID, HouseholdID: householdID,
		Amount: moneyOf(5000), OccurredOn: august(4), Source: domain.ContributionManual,
	})
	if err != nil {
		t.Fatalf("AddContribution manual: %v", err)
	}

	if err := goalRepo.DeleteContribution(ctx, householdID, goalID, manual.ID); err != nil {
		t.Fatalf("DeleteContribution: %v", err)
	}

	if rolledOverAt, rolloverGoalID := budgetRolloverStamp(t, db, householdID, july(1)); rolledOverAt == nil || rolloverGoalID == nil {
		t.Fatalf("stamp after deleting the MANUAL contribution = (rolled_over_at=%v, rollover_goal_id=%v), want both still set",
			rolledOverAt, rolloverGoalID)
	}

	remaining, err := goalRepo.ListContributions(ctx, householdID, goalID, 0)
	if err != nil {
		t.Fatalf("ListContributions: %v", err)
	}
	if len(remaining) != 1 || remaining[0].Source != domain.ContributionBudgetRollover {
		t.Fatalf("remaining = %+v, want exactly the surviving rollover contribution", remaining)
	}
}

// TestDeleteRolloverClearsOnlyItsOwnMonth pins why the contribution carries
// source_budget_month at all rather than ClearBudgetRollover keying off
// rollover_goal_id alone: two different months rolled into the SAME goal must
// carry independent stamps, and deleting one month's rollover must never
// touch the other's -- clearing by goal id alone would unstamp both (the
// task brief's own reasoning for this test).
func TestDeleteRolloverClearsOnlyItsOwnMonth(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	budgetRepo := postgres.NewBudgetRepo(db)
	goalRepo := postgres.NewGoalRepo(db)
	householdID := insertTestHousehold(t, db)
	june := time.Date(2026, time.June, 17, 0, 0, 0, 0, time.UTC)

	_, goalID := rolloverFixture(t, db, budgetRepo, goalRepo, householdID, june, "Shared goal")
	if _, err := budgetRepo.Upsert(ctx, domain.Budget{
		HouseholdID: householdID,
		Month:       july(17),
		Lines:       []domain.BudgetLine{{CategoryID: insertTestCategory(t, db, householdID, "Dining out"), Cap: moneyOf(50000)}},
	}); err != nil {
		t.Fatalf("Upsert July budget: %v", err)
	}

	juneRollover, err := budgetRepo.RollOverToGoal(ctx, usecase.RollOverToGoalInput{
		HouseholdID: householdID, Month: june, GoalID: goalID,
		Amount: moneyOf(10000), OccurredOn: july(2),
	})
	if err != nil {
		t.Fatalf("RollOverToGoal June: %v", err)
	}
	if _, err := budgetRepo.RollOverToGoal(ctx, usecase.RollOverToGoalInput{
		HouseholdID: householdID, Month: july(17), GoalID: goalID,
		Amount: moneyOf(15000), OccurredOn: august(2),
	}); err != nil {
		t.Fatalf("RollOverToGoal July: %v", err)
	}

	if err := goalRepo.DeleteContribution(ctx, householdID, goalID, juneRollover.ID); err != nil {
		t.Fatalf("DeleteContribution June's rollover: %v", err)
	}

	if rolledOverAt, rolloverGoalID := budgetRolloverStamp(t, db, householdID, june); rolledOverAt != nil || rolloverGoalID != nil {
		t.Fatalf("June's stamp = (rolled_over_at=%v, rollover_goal_id=%v), want BOTH nil after deleting its rollover",
			rolledOverAt, rolloverGoalID)
	}
	if rolledOverAt, rolloverGoalID := budgetRolloverStamp(t, db, householdID, july(1)); rolledOverAt == nil || rolloverGoalID == nil || *rolloverGoalID != goalID {
		t.Fatalf("July's stamp = (rolled_over_at=%v, rollover_goal_id=%v), want BOTH still set -- "+
			"deleting June's rollover must not touch July's", rolledOverAt, rolloverGoalID)
	}
}

// TestGoalProgressByIDsReturnsOnlyThisHouseholdsGoals pins
// usecase.GoalProgressReader's household scoping: a goal id from another
// household must be indistinguishable from one that does not exist at all,
// not merely absent from some higher-level filter.
func TestGoalProgressByIDsReturnsOnlyThisHouseholdsGoals(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	repo := postgres.NewGoalRepo(db)
	mine := insertTestHousehold(t, db)
	theirs := insertTestHousehold(t, db)
	myGoal := insertTestGoal(t, db, mine)
	theirGoal := insertTestGoal(t, db, theirs)

	got, err := repo.ProgressByIDs(ctx, mine, []string{myGoal, theirGoal, myGoal})
	if err != nil {
		t.Fatalf("progress by ids: %v", err)
	}
	if _, ok := got[theirGoal]; ok {
		t.Fatal("another household's goal must not appear")
	}
	progress, ok := got[myGoal]
	if !ok {
		t.Fatalf("want this household's goal, got %v", got)
	}
	if progress.Name != "Emergency fund" {
		t.Fatalf("want the goal's name, got %q", progress.Name)
	}
	// insertTestGoal writes no contributions, so nothing has been saved yet.
	if progress.Percent != 0 {
		t.Fatalf("want 0%%, got %d", progress.Percent)
	}
}

// TestGoalProgressByIDsIsAMissNotAnErrorForAnUnknownID pins the port's
// central ruling: an id ProgressByIDs cannot find is a miss, not an error --
// a measure whose linked goal was deleted must render as a label with no
// figure, never fail the whole vision page.
func TestGoalProgressByIDsIsAMissNotAnErrorForAnUnknownID(t *testing.T) {
	db := openTestDB(t)
	repo := postgres.NewGoalRepo(db)
	householdID := insertTestHousehold(t, db)

	got, err := repo.ProgressByIDs(context.Background(), householdID,
		[]string{"00000000-0000-0000-0000-000000000000"})
	if err != nil {
		t.Fatalf("an unknown id must be a miss, not an error: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("want an empty map, got %v", got)
	}
}

// TestGoalProgressByIDsReportsAnArchivedGoalsProgress pins the other half of
// the port's contract (usecase.GoalProgressReader's doc comment, spec
// decision 8): archiving is not deletion anywhere else in this product, so
// an archived goal must still count as found and keep its real figure.
// Only a real DELETE fires goals.id's ON DELETE SET NULL into
// vision_measures.goal_id -- without this test, nothing stops a future
// editor "helpfully" adding an archived_at IS NULL filter to the query.
func TestGoalProgressByIDsReportsAnArchivedGoalsProgress(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	repo := postgres.NewGoalRepo(db)
	householdID := insertTestHousehold(t, db)
	goalID := insertTestGoal(t, db, householdID) // target_amount_minor 1000000

	if _, err := repo.AddContribution(ctx, domain.GoalContribution{
		GoalID: goalID, HouseholdID: householdID,
		Amount: moneyOf(500000), OccurredOn: july(5), Source: domain.ContributionManual,
	}); err != nil {
		t.Fatalf("AddContribution: %v", err)
	}
	if _, err := repo.SetArchived(ctx, householdID, goalID, true, july(6)); err != nil {
		t.Fatalf("SetArchived: %v", err)
	}

	got, err := repo.ProgressByIDs(ctx, householdID, []string{goalID})
	if err != nil {
		t.Fatalf("progress by ids: %v", err)
	}
	progress, ok := got[goalID]
	if !ok {
		t.Fatalf("an archived goal must still be found -- archiving is not deletion, got %v", got)
	}
	if progress.Percent != 50 {
		t.Fatalf("want 50%% (500000/1000000), got %d", progress.Percent)
	}
}
