package postgres_test

import (
	"context"
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
