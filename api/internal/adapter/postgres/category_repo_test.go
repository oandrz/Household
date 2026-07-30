package postgres_test

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/andreasoentoro/hearth/api/internal/adapter/postgres"
	"github.com/andreasoentoro/hearth/api/internal/adapter/postgres/sqlcgen"
	"github.com/andreasoentoro/hearth/api/internal/domain"
)

// The seed is the one place in this product where a read writes, so its
// idempotence is not a nicety -- every categories request runs it.
func TestEnsureSeededIsIdempotent(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	repo := postgres.NewCategoryRepo(db)
	householdID := insertTestHousehold(t, db)

	for i := 0; i < 3; i++ {
		if err := repo.EnsureSeeded(ctx, householdID, domain.StarterCategories()); err != nil {
			t.Fatalf("seed %d: %v", i, err)
		}
	}

	got, err := repo.List(ctx, householdID, false)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(got) != len(domain.StarterCategories()) {
		t.Fatalf("got %d categories after three seeds, want %d",
			len(got), len(domain.StarterCategories()))
	}
	// Sort order, not insertion order or alphabetical.
	if got[0].Name != "Groceries" {
		t.Fatalf("first category is %q, want Groceries", got[0].Name)
	}
}

// Two simultaneous first requests are the case ON CONFLICT exists for. A
// read-then-write seed passes the test above and fails this one.
//
// The pool is warmed and every goroutine is released through a closed
// channel rather than started with a bare `go`. Without this, each
// goroutine's first query pays its own connection-dial latency, which
// serialises the count-then-insert window enough that this test stayed
// green for five straight runs against a seed with no ON CONFLICT at all --
// verified while diagnosing this test. Warming the pool first and releasing
// every goroutine from the same instant closes that gap and reproduces the
// race the seed is meant to survive.
func TestEnsureSeededSurvivesConcurrentFirstRequests(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	repo := postgres.NewCategoryRepo(db)
	householdID := insertTestHousehold(t, db)

	const n = 16
	var warm sync.WaitGroup
	for i := 0; i < n; i++ {
		warm.Add(1)
		go func() {
			defer warm.Done()
			_, _ = repo.List(ctx, householdID, false)
		}()
	}
	warm.Wait()

	start := make(chan struct{})
	var wg sync.WaitGroup
	errs := make([]error, n)
	for i := range errs {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			errs[i] = repo.EnsureSeeded(ctx, householdID, domain.StarterCategories())
		}(i)
	}
	close(start)
	wg.Wait()
	for i, err := range errs {
		if err != nil {
			t.Fatalf("concurrent seed %d: %v", i, err)
		}
	}

	got, err := repo.List(ctx, householdID, false)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(got) != len(domain.StarterCategories()) {
		t.Fatalf("got %d categories after %d concurrent seeds, want %d",
			len(got), n, len(domain.StarterCategories()))
	}
}

// A household that cleared its list has arranged it deliberately, and must
// not have it rebuilt out from under it.
//
// On this path -- EnsureSeeded called in sequence, not concurrently -- what
// actually stops the rebuild is CountCategories counting archived rows the
// same as live ones: the household already has thirteen rows, so the count
// check reports it as already seeded and EnsureSeeded returns before ever
// reaching the INSERT. That makes this test blind to ON CONFLICT and the
// unique key it targets -- it would pass identically if ON CONFLICT were
// deleted from SeedCategories entirely, because SeedCategories is never
// called here a second time.
// TestSeedCategoriesRespectsTheUniqueKeyEvenWhenEveryRowIsArchived exercises
// the unique key directly, by calling SeedCategories without EnsureSeeded's
// count check in the way.
func TestEnsureSeededDoesNotRebuildOverArchivedCategories(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	repo := postgres.NewCategoryRepo(db)
	householdID := insertTestHousehold(t, db)

	if err := repo.EnsureSeeded(ctx, householdID, domain.StarterCategories()); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if _, err := db.Pool().Exec(ctx,
		`UPDATE categories SET archived_at = now() WHERE household_id = $1`, householdID); err != nil {
		t.Fatalf("archive all: %v", err)
	}
	if err := repo.EnsureSeeded(ctx, householdID, domain.StarterCategories()); err != nil {
		t.Fatalf("second seed: %v", err)
	}

	live, err := repo.List(ctx, householdID, false)
	if err != nil {
		t.Fatalf("list live: %v", err)
	}
	if len(live) != 0 {
		t.Fatalf("the seed rebuilt %d categories over an archived list", len(live))
	}
	all, err := repo.List(ctx, householdID, true)
	if err != nil {
		t.Fatalf("list including archived: %v", err)
	}
	if len(all) != len(domain.StarterCategories()) {
		t.Fatalf("got %d rows including archived, want %d",
			len(all), len(domain.StarterCategories()))
	}
}

// TestSeedCategoriesRespectsTheUniqueKeyEvenWhenEveryRowIsArchived pins
// SeedCategories' own ON CONFLICT DO NOTHING, which
// TestEnsureSeededDoesNotRebuildOverArchivedCategories cannot: that test's
// second EnsureSeeded call never reaches the INSERT, because
// CountCategories already reports the household as seeded. This test calls
// the generated query directly -- bypassing EnsureSeeded's count check
// entirely -- against a household whose starter set exists but is entirely
// archived, which is the one place only the unique key protects.
func TestSeedCategoriesRespectsTheUniqueKeyEvenWhenEveryRowIsArchived(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	repo := postgres.NewCategoryRepo(db)
	householdID := insertTestHousehold(t, db)
	starter := domain.StarterCategories()

	if err := repo.EnsureSeeded(ctx, householdID, starter); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if _, err := db.Pool().Exec(ctx,
		`UPDATE categories SET archived_at = now() WHERE household_id = $1`, householdID); err != nil {
		t.Fatalf("archive all: %v", err)
	}

	var hid pgtype.UUID
	if err := hid.Scan(householdID); err != nil {
		t.Fatalf("scan household id: %v", err)
	}
	err := sqlcgen.New(db.Pool()).SeedCategories(ctx, sqlcgen.SeedCategoriesParams{
		HouseholdID: hid,
		Name1:       starter[0].Name, Kind1: string(starter[0].Kind), SortOrder1: int32(starter[0].SortOrder),
		Name2: starter[1].Name, Kind2: string(starter[1].Kind), SortOrder2: int32(starter[1].SortOrder),
		Name3: starter[2].Name, Kind3: string(starter[2].Kind), SortOrder3: int32(starter[2].SortOrder),
		Name4: starter[3].Name, Kind4: string(starter[3].Kind), SortOrder4: int32(starter[3].SortOrder),
		Name5: starter[4].Name, Kind5: string(starter[4].Kind), SortOrder5: int32(starter[4].SortOrder),
		Name6: starter[5].Name, Kind6: string(starter[5].Kind), SortOrder6: int32(starter[5].SortOrder),
		Name7: starter[6].Name, Kind7: string(starter[6].Kind), SortOrder7: int32(starter[6].SortOrder),
		Name8: starter[7].Name, Kind8: string(starter[7].Kind), SortOrder8: int32(starter[7].SortOrder),
		Name9: starter[8].Name, Kind9: string(starter[8].Kind), SortOrder9: int32(starter[8].SortOrder),
		Name10: starter[9].Name, Kind10: string(starter[9].Kind), SortOrder10: int32(starter[9].SortOrder),
		Name11: starter[10].Name, Kind11: string(starter[10].Kind), SortOrder11: int32(starter[10].SortOrder),
		Name12: starter[11].Name, Kind12: string(starter[11].Kind), SortOrder12: int32(starter[11].SortOrder),
		Name13: starter[12].Name, Kind13: string(starter[12].Kind), SortOrder13: int32(starter[12].SortOrder),
	})
	if err != nil {
		t.Fatalf("SeedCategories against an already-archived household: %v", err)
	}

	all, err := repo.List(ctx, householdID, true)
	if err != nil {
		t.Fatalf("list including archived: %v", err)
	}
	if len(all) != len(starter) {
		t.Fatalf("got %d rows, want %d -- ON CONFLICT should have discarded the repeat insert instead of duplicating past the unique key",
			len(all), len(starter))
	}
}

// TestCategoryCreateAppendsToSortOrder pins Create's own contract
// (usecase.CategoryRepository's doc comment): a new category lands at the
// end of the household's sort order, in the same statement as the insert.
func TestCategoryCreateAppendsToSortOrder(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	repo := postgres.NewCategoryRepo(db)
	householdID := insertTestHousehold(t, db)

	if err := repo.EnsureSeeded(ctx, householdID, domain.StarterCategories()); err != nil {
		t.Fatalf("seed: %v", err)
	}

	created, err := repo.Create(ctx, domain.Category{
		HouseholdID: householdID,
		Name:        "Helper's salary",
		Kind:        domain.CategoryExpense,
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if created.ID == "" {
		t.Fatal("create returned no id")
	}
	if created.Name != "Helper's salary" {
		t.Fatalf("name = %q, want %q", created.Name, "Helper's salary")
	}
	if created.Kind != domain.CategoryExpense {
		t.Fatalf("kind = %q, want expense", created.Kind)
	}
	if created.SortOrder != len(domain.StarterCategories())+1 {
		t.Fatalf("sort_order = %d, want %d (max of the seeded set, plus one)",
			created.SortOrder, len(domain.StarterCategories())+1)
	}

	got, err := repo.List(ctx, householdID, false)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(got) == 0 {
		t.Fatal("List returned no categories after seeding and creating one")
	}
	if last := got[len(got)-1]; last.ID != created.ID {
		t.Fatalf("List's last row is %+v, want the newly created category last", last)
	}
}

// TestCategoryCreateDuplicateNameIsErrCategoryNameTaken pins the collision
// contract: a name already in use -- whether the existing row is live or
// archived -- surfaces as domain.ErrCategoryNameTaken, not a generic
// ErrAlreadyExists or a raw driver error.
func TestCategoryCreateDuplicateNameIsErrCategoryNameTaken(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	repo := postgres.NewCategoryRepo(db)
	householdID := insertTestHousehold(t, db)

	if err := repo.EnsureSeeded(ctx, householdID, domain.StarterCategories()); err != nil {
		t.Fatalf("seed: %v", err)
	}

	_, err := repo.Create(ctx, domain.Category{
		HouseholdID: householdID,
		Name:        "Groceries",
		Kind:        domain.CategoryExpense,
	})
	if !errors.Is(err, domain.ErrCategoryNameTaken) {
		t.Fatalf("create against a live name: err = %v, want ErrCategoryNameTaken", err)
	}

	archivedID := insertTestCategory(t, db, householdID, "Retired category")
	if _, err := db.Pool().Exec(ctx, `UPDATE categories SET archived_at = now() WHERE id = $1`, archivedID); err != nil {
		t.Fatalf("archive: %v", err)
	}

	_, err = repo.Create(ctx, domain.Category{
		HouseholdID: householdID,
		Name:        "Retired category",
		Kind:        domain.CategoryExpense,
	})
	if !errors.Is(err, domain.ErrCategoryNameTaken) {
		t.Fatalf("create against an archived name: err = %v, want ErrCategoryNameTaken", err)
	}
}

// TestCategoryRenameKeepsEverythingElse pins Rename's contract: only the
// name changes, the same collision rule as Create applies, and a category id
// from another household is domain.ErrNotFound rather than a silent no-op.
func TestCategoryRenameKeepsEverythingElse(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	repo := postgres.NewCategoryRepo(db)
	householdID := insertTestHousehold(t, db)

	if err := repo.EnsureSeeded(ctx, householdID, domain.StarterCategories()); err != nil {
		t.Fatalf("seed: %v", err)
	}
	all, err := repo.List(ctx, householdID, false)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	var groceries domain.Category
	for _, c := range all {
		if c.Name == "Groceries" {
			groceries = c
		}
	}
	if groceries.ID == "" {
		t.Fatal("seeded set has no Groceries")
	}

	renamed, err := repo.Rename(ctx, householdID, groceries.ID, "Food")
	if err != nil {
		t.Fatalf("rename: %v", err)
	}
	if renamed.Name != "Food" {
		t.Fatalf("name = %q, want Food", renamed.Name)
	}
	if renamed.Kind != groceries.Kind {
		t.Fatalf("kind changed: got %q, want %q", renamed.Kind, groceries.Kind)
	}
	if renamed.SortOrder != groceries.SortOrder {
		t.Fatalf("sort_order changed: got %d, want %d", renamed.SortOrder, groceries.SortOrder)
	}
	if renamed.ArchivedAt != nil {
		t.Fatalf("archived_at = %v, want nil", renamed.ArchivedAt)
	}

	// Renaming a row to the name it already has must not collide with
	// itself -- the common case of an edit form submitted unchanged (or with
	// only whitespace fixed). The UPDATE only ever sees this row's own
	// current name occupying that unique key, so it is not a collision.
	same, err := repo.Rename(ctx, householdID, renamed.ID, "Food")
	if err != nil {
		t.Fatalf("rename to its own current name: %v", err)
	}
	if same.Name != "Food" {
		t.Fatalf("name = %q, want Food", same.Name)
	}

	// Renaming to a sibling's existing name collides, the same as Create.
	_, err = repo.Rename(ctx, householdID, renamed.ID, "Dining out")
	if !errors.Is(err, domain.ErrCategoryNameTaken) {
		t.Fatalf("rename onto a sibling's name: err = %v, want ErrCategoryNameTaken", err)
	}

	// A category id belonging to another household must not be reachable.
	otherHouseholdID := insertTestHousehold(t, db)
	_, err = repo.Rename(ctx, otherHouseholdID, renamed.ID, "Whatever")
	if !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("rename across households: err = %v, want ErrNotFound", err)
	}
}

// TestCategoryArchiveAndRestore pins SetArchived's contract: it filters
// List, never touches a referencing transaction's category_id, is idempotent
// on repeat calls, and clears cleanly when un-archived.
func TestCategoryArchiveAndRestore(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	repo := postgres.NewCategoryRepo(db)
	txRepo := postgres.NewTransactionRepo(db)
	householdID := insertTestHousehold(t, db)
	groceries := insertTestCategory(t, db, householdID, "Groceries")
	account := insertTestAccount(t, db, householdID, "DBS Everyday", "SGD")

	tx, err := txRepo.Create(ctx, domain.Transaction{
		HouseholdID:   householdID,
		Kind:          domain.TransactionExpense,
		OccurredOn:    july(18),
		Description:   "Cold Storage",
		FromAccountID: account,
		CategoryID:    groceries,
		Amount:        domain.Money{Amount: 5230, Currency: "SGD"},
	})
	if err != nil {
		t.Fatalf("insert transaction fixture: %v", err)
	}

	archived, err := repo.SetArchived(ctx, householdID, groceries, true)
	if err != nil {
		t.Fatalf("archive: %v", err)
	}
	if archived.ArchivedAt == nil {
		t.Fatal("archived_at not stamped")
	}
	firstStamp := *archived.ArchivedAt

	live, err := repo.List(ctx, householdID, false)
	if err != nil {
		t.Fatalf("list live: %v", err)
	}
	for _, c := range live {
		if c.ID == groceries {
			t.Fatal("List(includeArchived=false) still returned the archived category")
		}
	}
	withArchived, err := repo.List(ctx, householdID, true)
	if err != nil {
		t.Fatalf("list including archived: %v", err)
	}
	found := false
	for _, c := range withArchived {
		if c.ID == groceries {
			found = true
		}
	}
	if !found {
		t.Fatal("List(includeArchived=true) omitted the archived category")
	}

	gotTx, err := txRepo.Get(ctx, householdID, tx.ID)
	if err != nil {
		t.Fatalf("get transaction: %v", err)
	}
	if gotTx.Transaction.CategoryID != groceries {
		t.Fatalf("archiving the category cleared the transaction's category_id: got %q, want %q",
			gotTx.Transaction.CategoryID, groceries)
	}

	// Archiving twice must not move the stamp forward.
	archivedAgain, err := repo.SetArchived(ctx, householdID, groceries, true)
	if err != nil {
		t.Fatalf("archive again: %v", err)
	}
	if archivedAgain.ArchivedAt == nil || !archivedAgain.ArchivedAt.Equal(firstStamp) {
		t.Fatalf("second archive moved the stamp: got %v, want %v", archivedAgain.ArchivedAt, firstStamp)
	}

	restored, err := repo.SetArchived(ctx, householdID, groceries, false)
	if err != nil {
		t.Fatalf("restore: %v", err)
	}
	if restored.ArchivedAt != nil {
		t.Fatalf("archived_at = %v, want nil after restore", restored.ArchivedAt)
	}
}
