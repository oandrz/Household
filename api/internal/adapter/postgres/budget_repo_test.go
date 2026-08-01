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

// august is july's (transaction_repo_test.go) own counterpart for the same
// year, used below to give a rollover's OccurredOn a calendar month that
// disagrees with its Month -- deliberately, so a bug that wrote
// source_budget_month from OccurredOn instead of the normalised Month cannot
// hide behind two dates that happen to agree.
func august(day int) time.Time {
	return time.Date(2026, time.August, day, 0, 0, 0, 0, time.UTC)
}

// firstOfMonth is a test-only duplicate of the repository's own startOfMonth
// normalisation (transaction_repo.go), kept deliberately independent: the
// assertions below compute their own expectation instead of borrowing the
// private function under test, so a bug in that function's own normalisation
// cannot also hide from the test that is supposed to catch it.
func firstOfMonth(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), 1, 0, 0, 0, 0, time.UTC)
}

// budgetRolloverStamp reads a budget month's rollover stamp directly off the
// table. domain.Budget itself carries neither rolled_over_at nor
// rollover_goal_id -- BudgetRepository.Get has no reason to return them --
// so this raw read is the only way a test can see the stamp RollOverToGoal
// writes (or ClearBudgetRollover clears).
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
// household-month with source = 'budget_rollover' -- the same (household_id,
// source_budget_month) pair goal_contributions_one_rollover_per_month is
// built on, so this is what proves "at most one", not just "the one read got
// back looks right".
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
// a month with no budgets row is domain.ErrNotFound, not a zero-valued
// Budget, so the caller can translate it into the empty state rather than
// misreading an unbudgeted month as a budget of nothing.
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
// the whole Upsert, and Get must show the month exactly as it was before the
// call -- not a parent row updated with the old lines half-replaced.
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
// the case TestBudgetUpsertIsOneTransaction above cannot: there, the foreign
// category fails validateLineCategories before any write runs, so nothing
// pins pgx.BeginFunc's rollback once writes have already started. Here, two
// lines share one category this household genuinely owns -- dedup means
// validateLineCategories's count check passes, so the transaction proceeds
// to UpsertBudget and DeleteBudgetLines, and only the *second*
// InsertBudgetLine fails, against budget_lines' own UNIQUE (budget_id,
// category_id). That failure has to unwind a transaction whose DELETE has
// already executed, not merely refuse to start one.
//
// It also pins the adapter boundary: the 23505 this hits must translate into
// domain.ErrAlreadyExists, the same as TestSpaceRepoRejectsADuplicateKeyWithErrAlreadyExists
// pins for spaces, and must never expose the raw *pgconn.PgError -- "no
// database type crosses out of the adapter layer" (CLAUDE.md).
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

	// The other half of the convention, proven in the same test rather than a
	// separate one: a real value must round-trip too, or a repository that
	// always returns nil regardless of input would pass the assertion above
	// for the wrong reason.
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
// month entirely (august(3)) on purpose: a bug that skipped startOfMonth's
// normalisation, or that wrote source_budget_month from OccurredOn instead
// of Month -- exactly the failure Task 4's implementer flagged as the thing
// most likely to silently break this -- cannot hide behind dates that
// happen to already agree.
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
// rows case from the other side: a month with genuinely no budgets row at
// all (a state Budget decision 4 makes reachable -- a closed month can have
// spend and no caps) must be domain.ErrNotFound, never
// domain.ErrRolloverAlreadyDone.
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
// over, confirm the stamp; delete the rollover contribution, confirm the
// stamp is FULLY gone (both columns, not just one); roll over again, confirm
// it succeeds with exactly one contribution surviving. A test that only
// asserted the second RollOverToGoal's success would pass even if the first
// delete had left a stray duplicate contribution behind -- this is why the
// test also checks the stamp is gone in between, and that there is exactly
// one contribution at the end, not two.
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

// TestRollOverToGoalPartialIndexMapsToErrRolloverAlreadyDone is not in the
// task brief's own enumerated list, but the brief's own Step 3 is explicit:
// "a 23505 on goal_contributions_one_rollover_per_month also maps to
// domain.ErrRolloverAlreadyDone ... Check the constraint name, not just the
// SQLSTATE". Nothing else in this file can ever reach that INSERT, because
// the conditional UPDATE always wins first when the stamp agrees with the
// contribution row -- so this test manufactures the one state where they
// disagree (the stamp cleared by hand, the rollover contribution left in
// place) to drive the second RollOverToGoal's INSERT into the partial unique
// index deliberately, without needing two genuinely concurrent transactions.
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

	// Simulate the stamp having been cleared without its contribution going
	// with it -- exactly the "strand" state DeleteContribution's own
	// transaction exists to prevent, manufactured here by hand so the second
	// RollOverToGoal's conditional UPDATE succeeds this time, and its INSERT
	// is the one that has to hit the partial index instead.
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
