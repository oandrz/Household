package postgres_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/andreasoentoro/hearth/api/internal/adapter/postgres"
	"github.com/andreasoentoro/hearth/api/internal/domain"
)

func month2026(m time.Month) time.Time {
	return time.Date(2026, m, 1, 0, 0, 0, 0, time.UTC)
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
