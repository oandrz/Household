package postgres_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/andreasoentoro/hearth/api/internal/adapter/postgres"
	"github.com/andreasoentoro/hearth/api/internal/domain"
	"github.com/andreasoentoro/hearth/api/internal/usecase"
)

func month2026(m time.Month) time.Time {
	return time.Date(2026, m, 1, 0, 0, 0, 0, time.UTC)
}

// august is july's (transaction_repo_test.go) counterpart, used to give a
// rollover's OccurredOn a calendar month that disagrees with its Month on
// purpose: a bug that took source_budget_month from OccurredOn instead of
// the normalised Month can't hide behind dates that already agree.
func august(day int) time.Time {
	return time.Date(2026, time.August, day, 0, 0, 0, 0, time.UTC)
}

// firstOfMonth duplicates the repository's own startOfMonth normalisation
// (transaction_repo.go) independently, so a bug in that private function
// can't also hide from the test meant to catch it.
func firstOfMonth(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), 1, 0, 0, 0, 0, time.UTC)
}

// budgetRolloverStamp reads a budget month's rollover stamp directly off the
// table: domain.Budget carries neither rolled_over_at nor rollover_goal_id,
// so this is the only way a test can see what RollOverToGoal writes (or
// ClearBudgetRollover clears).
func budgetRolloverStamp(t *testing.T, db *postgres.DB, householdID string, month time.Time) (rolledOverAt *time.Time, rolloverGoalID *string) {
	t.Helper()
	err := db.Pool().QueryRow(context.Background(),
		`SELECT rolled_over_at, rollover_goal_id FROM budgets WHERE household_id = $1 AND month = $2`,
		householdID, firstOfMonth(month)).Scan(&rolledOverAt, &rolloverGoalID)
	if err != nil {
		t.Fatalf("read budget rollover stamp for %v: %v", month, err)
	}
	return rolledOverAt, rolloverGoalID
}

// countRolloverContributions counts goal_contributions rows for this
// household-month with source = 'budget_rollover' -- the same key
// goal_contributions_one_rollover_per_month uses, so it proves "at most
// one", not just that one read looks right.
func countRolloverContributions(t *testing.T, db *postgres.DB, householdID string, month time.Time) int {
	t.Helper()
	var count int
	err := db.Pool().QueryRow(context.Background(),
		`SELECT count(*) FROM goal_contributions WHERE household_id = $1 AND source_budget_month = $2 AND source = 'budget_rollover'`,
		householdID, firstOfMonth(month)).Scan(&count)
	if err != nil {
		t.Fatalf("count rollover contributions for %v: %v", month, err)
	}
	return count
}

// rolloverFixture is the setup every rollover test below shares: a budgeted
// month and a goal to roll into. It returns the ids the tests then act on.
func rolloverFixture(t *testing.T, db *postgres.DB, budgetRepo *postgres.BudgetRepo, goalRepo *postgres.GoalRepo,
	householdID string, month time.Time, goalName string) (categoryID, goalID string) {
	t.Helper()
	ctx := context.Background()
	categoryID = insertTestCategory(t, db, householdID, "Groceries")
	if _, err := budgetRepo.Upsert(ctx, domain.Budget{
		HouseholdID: householdID,
		Month:       month,
		Lines:       []domain.BudgetLine{{CategoryID: categoryID, Cap: moneyOf(80000)}},
	}); err != nil {
		t.Fatalf("Upsert budget: %v", err)
	}
	g, err := goalRepo.Create(ctx, newTestGoal(householdID, goalName), 0, july(1))
	if err != nil {
		t.Fatalf("Create goal: %v", err)
	}
	return categoryID, g.ID
}

// moneyOf is a shorthand for the SGD test fixtures below -- every household
// insertTestHousehold creates defaults to SGD (migrations/00002_identity.sql),
// which is what budgets.Upsert reads back as the caps' authoritative currency.
func moneyOf(amount int64) domain.Money {
	return domain.Money{Amount: amount, Currency: "SGD"}
}

// TestBudgetUpsertCreatesThenReplaces pins Upsert's full-replace contract
// (usecase.BudgetRepository's doc comment): a second Upsert for the same
// household-month does not merge with the first, it replaces it wholesale.
func TestBudgetUpsertCreatesThenReplaces(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	repo := postgres.NewBudgetRepo(db)
	householdID := insertTestHousehold(t, db)
	groceries := insertTestCategory(t, db, householdID, "Groceries")
	dining := insertTestCategory(t, db, householdID, "Dining out")

	_, err := repo.Upsert(ctx, domain.Budget{
		HouseholdID: householdID,
		Month:       month2026(time.July),
		Lines: []domain.BudgetLine{
			{CategoryID: groceries, Cap: moneyOf(80000)},
			{CategoryID: dining, Cap: moneyOf(45000)},
		},
	})
	if err != nil {
		t.Fatalf("first Upsert: %v", err)
	}

	first, err := repo.Get(ctx, householdID, month2026(time.July))
	if err != nil {
		t.Fatalf("Get after first Upsert: %v", err)
	}
	if len(first.Lines) != 2 {
		t.Fatalf("after first Upsert: %d lines, want 2", len(first.Lines))
	}

	_, err = repo.Upsert(ctx, domain.Budget{
		HouseholdID: householdID,
		Month:       month2026(time.July),
		Lines: []domain.BudgetLine{
			{CategoryID: groceries, Cap: moneyOf(90000)},
		},
	})
	if err != nil {
		t.Fatalf("second Upsert: %v", err)
	}

	second, err := repo.Get(ctx, householdID, month2026(time.July))
	if err != nil {
		t.Fatalf("Get after second Upsert: %v", err)
	}
	if len(second.Lines) != 1 {
		t.Fatalf("after second Upsert: %d lines, want exactly 1 (the dining line must be GONE)", len(second.Lines))
	}
	line := second.Lines[0]
	if line.CategoryID != groceries {
		t.Fatalf("surviving line's category = %q, want groceries %q", line.CategoryID, groceries)
	}
	if line.Cap.Amount != 90000 {
		t.Fatalf("surviving line's cap = %d, want the new 90000, not the old 80000", line.Cap.Amount)
	}
}

// TestBudgetGetUnbudgetedMonthIsErrNotFound pins Get's empty-state contract:
// no budgets row is domain.ErrNotFound, not a zero-valued Budget, so a
// caller can tell "never budgeted" from "budgeted with nothing in it".
func TestBudgetGetUnbudgetedMonthIsErrNotFound(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	repo := postgres.NewBudgetRepo(db)
	householdID := insertTestHousehold(t, db)

	_, err := repo.Get(ctx, householdID, month2026(time.July))
	if !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("Get on a never-budgeted month: err = %v, want domain.ErrNotFound", err)
	}
}

// TestBudgetUpsertIsOneTransaction is the shape guarding-partial-writes
// exists for: a line whose category belongs to ANOTHER household must fail
// the whole Upsert, and Get must show the month unchanged -- not a parent
// row updated with the old lines half-replaced.
func TestBudgetUpsertIsOneTransaction(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	repo := postgres.NewBudgetRepo(db)
	householdID := insertTestHousehold(t, db)
	groceries := insertTestCategory(t, db, householdID, "Groceries")

	otherHouseholdID := insertTestHousehold(t, db)
	foreignCategory := insertTestCategory(t, db, otherHouseholdID, "Someone else's category")

	_, err := repo.Upsert(ctx, domain.Budget{
		HouseholdID: householdID,
		Month:       month2026(time.August),
		Lines: []domain.BudgetLine{
			{CategoryID: groceries, Cap: moneyOf(70000)},
		},
	})
	if err != nil {
		t.Fatalf("initial Upsert: %v", err)
	}

	_, err = repo.Upsert(ctx, domain.Budget{
		HouseholdID: householdID,
		Month:       month2026(time.August),
		Lines: []domain.BudgetLine{
			{CategoryID: groceries, Cap: moneyOf(70000)},
			{CategoryID: foreignCategory, Cap: moneyOf(10000)},
		},
	})
	if err == nil {
		t.Fatal("Upsert with a foreign-household category line succeeded, want an error")
	}

	after, err := repo.Get(ctx, householdID, month2026(time.August))
	if err != nil {
		t.Fatalf("Get after the failed Upsert: %v", err)
	}
	if len(after.Lines) != 1 {
		t.Fatalf("after the failed Upsert: %d lines, want exactly the original 1 (no partial write)", len(after.Lines))
	}
	if after.Lines[0].CategoryID != groceries || after.Lines[0].Cap.Amount != 70000 {
		t.Fatalf("after the failed Upsert: line = %+v, want the original groceries/70000 line unchanged", after.Lines[0])
	}
}

// TestBudgetUpsertDuplicateCategoryLineRollsBackAndStaysAtTheBoundary covers
// what TestBudgetUpsertIsOneTransaction cannot: a rollback that has to undo
// writes already made, not just refuse to start. Two lines share one
// category the household genuinely owns, so dedup lets the transaction reach
// UpsertBudget and DeleteBudgetLines before the second InsertBudgetLine fails
// budget_lines' UNIQUE (budget_id, category_id) -- the rollback must undo
// that DELETE. It also pins the adapter boundary: the 23505 must map to
// domain.ErrAlreadyExists (as for spaces, see
// TestSpaceRepoRejectsADuplicateKeyWithErrAlreadyExists) and never leak the
// raw *pgconn.PgError.
func TestBudgetUpsertDuplicateCategoryLineRollsBackAndStaysAtTheBoundary(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	repo := postgres.NewBudgetRepo(db)
	householdID := insertTestHousehold(t, db)
	groceries := insertTestCategory(t, db, householdID, "Groceries")

	_, err := repo.Upsert(ctx, domain.Budget{
		HouseholdID: householdID,
		Month:       month2026(time.October),
		Lines: []domain.BudgetLine{
			{CategoryID: groceries, Cap: moneyOf(55000)},
		},
	})
	if err != nil {
		t.Fatalf("initial Upsert: %v", err)
	}

	_, err = repo.Upsert(ctx, domain.Budget{
		HouseholdID: householdID,
		Month:       month2026(time.October),
		Lines: []domain.BudgetLine{
			{CategoryID: groceries, Cap: moneyOf(80000)},
			{CategoryID: groceries, Cap: moneyOf(80000)}, // same category twice
		},
	})
	if err == nil {
		t.Fatal("Upsert with a duplicate category line succeeded, want an error")
	}
	if !errors.Is(err, domain.ErrAlreadyExists) {
		t.Fatalf("err = %v, want it to wrap domain.ErrAlreadyExists (translate's 23505 mapping)", err)
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		t.Fatalf("err = %v exposes a raw *pgconn.PgError; no database type may cross the adapter boundary", err)
	}

	after, err := repo.Get(ctx, householdID, month2026(time.October))
	if err != nil {
		t.Fatalf("Get after the failed Upsert: %v", err)
	}
	if len(after.Lines) != 1 {
		t.Fatalf("after the failed Upsert: %d lines, want exactly the original 1 -- "+
			"DeleteBudgetLines had already run before InsertBudgetLine failed, and the "+
			"rollback still has to undo it", len(after.Lines))
	}
	if after.Lines[0].CategoryID != groceries || after.Lines[0].Cap.Amount != 55000 {
		t.Fatalf("after the failed Upsert: line = %+v, want the original groceries/55000 line unchanged", after.Lines[0])
	}
}

// TestBudgetHistorySkipsUnbudgetedMonths pins History's absent-means-absent
// contract (usecase.BudgetRepository's doc comment): a month with no row is
// simply missing from the result, never a zero-filled placeholder.
func TestBudgetHistorySkipsUnbudgetedMonths(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	repo := postgres.NewBudgetRepo(db)
	householdID := insertTestHousehold(t, db)
	groceries := insertTestCategory(t, db, householdID, "Groceries")

	for _, m := range []time.Month{time.May, time.July} {
		_, err := repo.Upsert(ctx, domain.Budget{
			HouseholdID: householdID,
			Month:       month2026(m),
			Lines: []domain.BudgetLine{
				{CategoryID: groceries, Cap: moneyOf(50000)},
			},
		})
		if err != nil {
			t.Fatalf("Upsert %s: %v", m, err)
		}
	}

	history, err := repo.History(ctx, householdID, month2026(time.July), 6)
	if err != nil {
		t.Fatalf("History: %v", err)
	}
	if len(history) != 2 {
		t.Fatalf("History returned %d budgets, want exactly 2 (July and May, no zero-filled June)", len(history))
	}
	// Newest first.
	if !history[0].Month.Equal(month2026(time.July)) {
		t.Fatalf("history[0].Month = %v, want July (newest first)", history[0].Month)
	}
	if !history[1].Month.Equal(month2026(time.May)) {
		t.Fatalf("history[1].Month = %v, want May", history[1].Month)
	}
}

// TestBudgetExpectedIncomeNullRoundTrips pins the nil <-> SQL NULL convention
// for ExpectedIncome: omitting it must come back as nil, never a zero Money,
// because zero is a claim the household never made (migration's own comment).
func TestBudgetExpectedIncomeNullRoundTrips(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	repo := postgres.NewBudgetRepo(db)
	householdID := insertTestHousehold(t, db)
	groceries := insertTestCategory(t, db, householdID, "Groceries")

	_, err := repo.Upsert(ctx, domain.Budget{
		HouseholdID:    householdID,
		Month:          month2026(time.September),
		ExpectedIncome: nil,
		Lines: []domain.BudgetLine{
			{CategoryID: groceries, Cap: moneyOf(50000)},
		},
	})
	if err != nil {
		t.Fatalf("Upsert: %v", err)
	}

	got, err := repo.Get(ctx, householdID, month2026(time.September))
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.ExpectedIncome != nil {
		t.Fatalf("ExpectedIncome = %+v, want nil, not a zero Money", got.ExpectedIncome)
	}

	// The other half of the convention, proven here rather than in a
	// separate test: a real value must round-trip too, or a repository that
	// always returns nil would pass the assertion above for the wrong
	// reason.
	income := moneyOf(650000)
	_, err = repo.Upsert(ctx, domain.Budget{
		HouseholdID:    householdID,
		Month:          month2026(time.September),
		ExpectedIncome: &income,
		Lines: []domain.BudgetLine{
			{CategoryID: groceries, Cap: moneyOf(50000)},
		},
	})
	if err != nil {
		t.Fatalf("Upsert with ExpectedIncome: %v", err)
	}
	got, err = repo.Get(ctx, householdID, month2026(time.September))
	if err != nil {
		t.Fatalf("Get after setting ExpectedIncome: %v", err)
	}
	if got.ExpectedIncome == nil || got.ExpectedIncome.Amount != 650000 {
		t.Fatalf("ExpectedIncome = %+v, want 650000", got.ExpectedIncome)
	}
}

// TestRollOverToGoalWritesContributionAndStampTogether pins
// usecase.BudgetRepository.RollOverToGoal's own doc comment: one transaction
// writes the contribution AND stamps the month.
//
// Month is passed mid-month (july(17)) and OccurredOn a different calendar
// month (august(3)) on purpose: a bug that skipped startOfMonth's
// normalisation, or wrote source_budget_month from OccurredOn instead of
// Month, cannot hide behind dates that already agree.
func TestRollOverToGoalWritesContributionAndStampTogether(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	budgetRepo := postgres.NewBudgetRepo(db)
	goalRepo := postgres.NewGoalRepo(db)
	householdID := insertTestHousehold(t, db)
	_, goalID := rolloverFixture(t, db, budgetRepo, goalRepo, householdID, july(17), "Emergency fund")

	occurredOn := august(3)
	contribution, err := budgetRepo.RollOverToGoal(ctx, usecase.RollOverToGoalInput{
		HouseholdID: householdID,
		Month:       july(17),
		GoalID:      goalID,
		Amount:      moneyOf(20000),
		OccurredOn:  occurredOn,
	})
	if err != nil {
		t.Fatalf("RollOverToGoal: %v", err)
	}

	if contribution.Source != domain.ContributionBudgetRollover {
		t.Fatalf("Source = %q, want budget_rollover", contribution.Source)
	}
	if contribution.Note != "" {
		t.Fatalf("Note = %q, want empty -- user-facing copy is composed in the frontend, not written here", contribution.Note)
	}
	if contribution.Amount.Amount != 20000 {
		t.Fatalf("Amount = %d, want 20000", contribution.Amount.Amount)
	}
	if !contribution.OccurredOn.Equal(occurredOn) {
		t.Fatalf("OccurredOn = %v, want %v (the caller's date, unchanged)", contribution.OccurredOn, occurredOn)
	}
	if contribution.SourceBudgetMonth == nil || !contribution.SourceBudgetMonth.Equal(july(1)) {
		t.Fatalf("SourceBudgetMonth = %v, want July 1 2026 -- the normalised Month, never OccurredOn's August",
			contribution.SourceBudgetMonth)
	}

	record, err := goalRepo.Get(ctx, householdID, goalID)
	if err != nil {
		t.Fatalf("Get goal: %v", err)
	}
	if record.ContributedMinor != 20000 {
		t.Fatalf("ContributedMinor = %d, want 20000", record.ContributedMinor)
	}

	rolledOverAt, rolloverGoalID := budgetRolloverStamp(t, db, householdID, july(1))
	if rolledOverAt == nil {
		t.Fatal("rolled_over_at is NULL, want it stamped")
	}
	if rolloverGoalID == nil || *rolloverGoalID != goalID {
		t.Fatalf("rollover_goal_id = %v, want %s", rolloverGoalID, goalID)
	}
}

// TestBudgetGetSurfacesRolloverAmountFromTheContributionRow pins GetBudget's
// own LEFT JOIN: nil before any rollover, then in lockstep with
// rolled_over_at/rollover_goal_id, and the exact amount written to
// goal_contributions -- read off that row, not off a live Spent/Remaining
// recomputation, since Get never touches transactions.
func TestBudgetGetSurfacesRolloverAmountFromTheContributionRow(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	budgetRepo := postgres.NewBudgetRepo(db)
	goalRepo := postgres.NewGoalRepo(db)
	householdID := insertTestHousehold(t, db)
	_, goalID := rolloverFixture(t, db, budgetRepo, goalRepo, householdID, july(17), "Emergency fund")

	before, err := budgetRepo.Get(ctx, householdID, july(1))
	if err != nil {
		t.Fatalf("Get before rollover: %v", err)
	}
	if before.RolloverAmountMinor != nil {
		t.Fatalf("RolloverAmountMinor before rollover = %v, want nil", before.RolloverAmountMinor)
	}

	if _, err := budgetRepo.RollOverToGoal(ctx, usecase.RollOverToGoalInput{
		HouseholdID: householdID,
		Month:       july(17),
		GoalID:      goalID,
		Amount:      moneyOf(20000),
		OccurredOn:  august(3),
	}); err != nil {
		t.Fatalf("RollOverToGoal: %v", err)
	}

	after, err := budgetRepo.Get(ctx, householdID, july(1))
	if err != nil {
		t.Fatalf("Get after rollover: %v", err)
	}
	if after.RolloverAmountMinor == nil || *after.RolloverAmountMinor != 20000 {
		t.Fatalf("RolloverAmountMinor after rollover = %v, want 20000", after.RolloverAmountMinor)
	}
}

// TestRollOverToGoalTwiceIsErrRolloverAlreadyDone pins the conditional
// UPDATE's own guard: a second rollover for the same household-month must
// fail, and must not write a second contribution.
func TestRollOverToGoalTwiceIsErrRolloverAlreadyDone(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	budgetRepo := postgres.NewBudgetRepo(db)
	goalRepo := postgres.NewGoalRepo(db)
	householdID := insertTestHousehold(t, db)
	_, goalID := rolloverFixture(t, db, budgetRepo, goalRepo, householdID, july(17), "Emergency fund")

	in := usecase.RollOverToGoalInput{
		HouseholdID: householdID,
		Month:       july(17),
		GoalID:      goalID,
		Amount:      moneyOf(20000),
		OccurredOn:  august(3),
	}
	if _, err := budgetRepo.RollOverToGoal(ctx, in); err != nil {
		t.Fatalf("first RollOverToGoal: %v", err)
	}

	// A different day within the same month: the guard must key on the
	// normalised month, not the exact instant passed in.
	in.Month = july(25)
	in.OccurredOn = august(4)
	if _, err := budgetRepo.RollOverToGoal(ctx, in); !errors.Is(err, domain.ErrRolloverAlreadyDone) {
		t.Fatalf("second RollOverToGoal: err = %v, want domain.ErrRolloverAlreadyDone", err)
	}

	if count := countRolloverContributions(t, db, householdID, july(1)); count != 1 {
		t.Fatalf("rollover contribution count = %d, want exactly 1 -- the second call must not have written another", count)
	}
}

// TestRollOverToGoalWithoutABudgetRowIsErrNotFound pins the ambiguous-zero-
// rows case from the other side: a month with genuinely no budgets row (a
// closed month can have spend and no caps) must be domain.ErrNotFound,
// never domain.ErrRolloverAlreadyDone.
func TestRollOverToGoalWithoutABudgetRowIsErrNotFound(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	budgetRepo := postgres.NewBudgetRepo(db)
	goalRepo := postgres.NewGoalRepo(db)
	householdID := insertTestHousehold(t, db)

	g, err := goalRepo.Create(ctx, newTestGoal(householdID, "Emergency fund"), 0, july(1))
	if err != nil {
		t.Fatalf("Create goal: %v", err)
	}

	_, err = budgetRepo.RollOverToGoal(ctx, usecase.RollOverToGoalInput{
		HouseholdID: householdID,
		Month:       july(17),
		GoalID:      g.ID,
		Amount:      moneyOf(20000),
		OccurredOn:  august(3),
	})
	if !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("err = %v, want domain.ErrNotFound", err)
	}

	if count := countRolloverContributions(t, db, householdID, july(1)); count != 0 {
		t.Fatalf("rollover contribution count = %d, want 0 -- nothing should be written on ErrNotFound", count)
	}
}

// TestRollOverThenDeleteThenRollOverAgainSucceeds is THE round trip: roll
// over and confirm the stamp; delete the contribution and confirm the stamp
// is FULLY gone (both columns); roll over again and confirm it succeeds
// with exactly one contribution surviving. The stamp and count checks
// matter: a test that only asserted the second call's success would still
// pass even if the delete had left a stray duplicate contribution behind.
func TestRollOverThenDeleteThenRollOverAgainSucceeds(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	budgetRepo := postgres.NewBudgetRepo(db)
	goalRepo := postgres.NewGoalRepo(db)
	householdID := insertTestHousehold(t, db)
	_, goalID := rolloverFixture(t, db, budgetRepo, goalRepo, householdID, july(17), "Emergency fund")

	first, err := budgetRepo.RollOverToGoal(ctx, usecase.RollOverToGoalInput{
		HouseholdID: householdID,
		Month:       july(17),
		GoalID:      goalID,
		Amount:      moneyOf(20000),
		OccurredOn:  august(3),
	})
	if err != nil {
		t.Fatalf("first RollOverToGoal: %v", err)
	}
	if rolledOverAt, _ := budgetRolloverStamp(t, db, householdID, july(1)); rolledOverAt == nil {
		t.Fatal("stamp missing right after the first rollover")
	}

	if err := goalRepo.DeleteContribution(ctx, householdID, goalID, first.ID); err != nil {
		t.Fatalf("DeleteContribution: %v", err)
	}
	if rolledOverAt, rolloverGoalID := budgetRolloverStamp(t, db, householdID, july(1)); rolledOverAt != nil || rolloverGoalID != nil {
		t.Fatalf("stamp after delete = (rolled_over_at=%v, rollover_goal_id=%v), want BOTH nil", rolledOverAt, rolloverGoalID)
	}

	second, err := budgetRepo.RollOverToGoal(ctx, usecase.RollOverToGoalInput{
		HouseholdID: householdID,
		Month:       july(19),
		GoalID:      goalID,
		Amount:      moneyOf(30000),
		OccurredOn:  august(10),
	})
	if err != nil {
		t.Fatalf("second RollOverToGoal: %v, want it to succeed now that the stamp is clear", err)
	}
	if second.ID == first.ID {
		t.Fatal("second contribution reused the first's id, want a fresh row")
	}

	if count := countRolloverContributions(t, db, householdID, july(1)); count != 1 {
		t.Fatalf("rollover contribution count = %d, want exactly 1 (the second's, not two)", count)
	}
	if rolledOverAt, rolloverGoalID := budgetRolloverStamp(t, db, householdID, july(1)); rolledOverAt == nil || rolloverGoalID == nil || *rolloverGoalID != goalID {
		t.Fatalf("stamp after second rollover = (rolled_over_at=%v, rollover_goal_id=%v), want both set to %s",
			rolledOverAt, rolloverGoalID, goalID)
	}
}

// TestRollOverToGoalPartialIndexMapsToErrRolloverAlreadyDone pins that a
// 23505 on goal_contributions_one_rollover_per_month also maps to
// domain.ErrRolloverAlreadyDone -- checked by constraint name, not just
// SQLSTATE. The conditional UPDATE always wins first when the stamp agrees
// with the contribution row, so nothing else in this file can reach that
// INSERT; this test manufactures the one state where they disagree (stamp
// cleared by hand, contribution left in place) to drive the INSERT into the
// partial unique index without two genuinely concurrent transactions.
func TestRollOverToGoalPartialIndexMapsToErrRolloverAlreadyDone(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	budgetRepo := postgres.NewBudgetRepo(db)
	goalRepo := postgres.NewGoalRepo(db)
	householdID := insertTestHousehold(t, db)
	_, goalID := rolloverFixture(t, db, budgetRepo, goalRepo, householdID, july(17), "Emergency fund")

	if _, err := budgetRepo.RollOverToGoal(ctx, usecase.RollOverToGoalInput{
		HouseholdID: householdID,
		Month:       july(17),
		GoalID:      goalID,
		Amount:      moneyOf(20000),
		OccurredOn:  august(3),
	}); err != nil {
		t.Fatalf("first RollOverToGoal: %v", err)
	}

	// Simulate the stamp cleared without its contribution -- the "strand"
	// state DeleteContribution's transaction exists to prevent -- so the
	// second RollOverToGoal's conditional UPDATE succeeds this time and its
	// INSERT hits the partial index instead.
	if _, err := db.Pool().Exec(ctx,
		`UPDATE budgets SET rolled_over_at = NULL, rollover_goal_id = NULL WHERE household_id = $1 AND month = $2`,
		householdID, july(1)); err != nil {
		t.Fatalf("manually clear stamp: %v", err)
	}

	_, err := budgetRepo.RollOverToGoal(ctx, usecase.RollOverToGoalInput{
		HouseholdID: householdID,
		Month:       july(19),
		GoalID:      goalID,
		Amount:      moneyOf(30000),
		OccurredOn:  august(10),
	})
	if !errors.Is(err, domain.ErrRolloverAlreadyDone) {
		t.Fatalf("err = %v, want domain.ErrRolloverAlreadyDone (the partial unique index this time, not the conditional UPDATE)", err)
	}

	// The whole transaction -- including the UPDATE that briefly re-set the
	// stamp before the INSERT failed -- must have rolled back: the stamp is
	// still clear, not left half-set with no contribution to match it.
	if rolledOverAt, rolloverGoalID := budgetRolloverStamp(t, db, householdID, july(1)); rolledOverAt != nil || rolloverGoalID != nil {
		t.Fatalf("stamp after the failed second rollover = (rolled_over_at=%v, rollover_goal_id=%v), want BOTH nil -- "+
			"the UPDATE must have rolled back along with the failed INSERT", rolledOverAt, rolloverGoalID)
	}
	if count := countRolloverContributions(t, db, householdID, july(1)); count != 1 {
		t.Fatalf("rollover contribution count = %d, want still exactly 1 (the original; no new row from the failed insert)", count)
	}
}
