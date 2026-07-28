package postgres_test

import (
	"context"
	"sync"
	"testing"

	"github.com/andreasoentoro/hearth/api/internal/adapter/postgres"
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

// A household that cleared its list has arranged it deliberately. An archived
// row keeps its unique key, which is what stops the seed rebuilding over it.
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
