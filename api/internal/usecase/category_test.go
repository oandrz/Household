package usecase_test

import (
	"context"
	"errors"
	"testing"

	"github.com/andreasoentoro/hearth/api/internal/domain"
	"github.com/andreasoentoro/hearth/api/internal/usecase"
)

// The modal needs a category list before the household's first transaction
// exists, which is why the read is what seeds. A first-run household must
// never be shown an empty dropdown.
func TestListSeedsTheStarterSetForAHouseholdWithNone(t *testing.T) {
	repo := &fakeCategoryRepo{}
	svc := usecase.NewCategoryService(repo)

	got, err := svc.List(context.Background(), "house-1")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(got) != len(domain.StarterCategories()) {
		t.Fatalf("got %d categories on first read, want the starter set of %d",
			len(got), len(domain.StarterCategories()))
	}
	if repo.seeds != 1 {
		t.Fatalf("seeded %d times on one read, want 1", repo.seeds)
	}
}

func TestListDoesNotReseedAHouseholdThatAlreadyHasCategories(t *testing.T) {
	repo := &fakeCategoryRepo{}
	svc := usecase.NewCategoryService(repo)
	ctx := context.Background()

	if _, err := svc.List(ctx, "house-1"); err != nil {
		t.Fatalf("first list: %v", err)
	}
	if _, err := svc.List(ctx, "house-1"); err != nil {
		t.Fatalf("second list: %v", err)
	}
	if repo.seeds != 1 {
		t.Fatalf("seeded %d times across two reads, want 1", repo.seeds)
	}
}

// TestCreateTrimsWhitespaceAndRefusesEmptyName pins the same whitespace
// convention AccountService.validate uses for a nickname: leading and
// trailing space is stripped before the empty check, so "  " and "" are both
// refused rather than one slipping through as a category named "  ".
func TestCreateTrimsWhitespaceAndRefusesEmptyName(t *testing.T) {
	repo := &fakeCategoryRepo{}
	svc := usecase.NewCategoryService(repo)
	ctx := context.Background()

	got, err := svc.Create(ctx, "house-1", "  Streaming  ")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if got.Name != "Streaming" {
		t.Fatalf("name = %q, want trimmed %q", got.Name, "Streaming")
	}

	for _, name := range []string{"", "   "} {
		if _, err := svc.Create(ctx, "house-1", name); !errors.Is(err, domain.ErrCategoryNameRequired) {
			t.Fatalf("create(%q): err = %v, want ErrCategoryNameRequired", name, err)
		}
	}
}

// TestCreateCategoryIsAlwaysExpenseKind pins that a category made through
// this write path can never cap an income line -- Budget's caps are an
// expense-only concept, so Create never takes a kind argument at all.
func TestCreateCategoryIsAlwaysExpenseKind(t *testing.T) {
	repo := &fakeCategoryRepo{}
	svc := usecase.NewCategoryService(repo)

	got, err := svc.Create(context.Background(), "house-1", "Streaming")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if got.Kind != domain.CategoryExpense {
		t.Fatalf("kind = %q, want %q", got.Kind, domain.CategoryExpense)
	}
}

// TestCreateCategoryCollisionPassesThroughUntranslated asserts the service
// does not wrap or swallow the repository's collision sentinel -- Task 10's
// handler maps domain.ErrCategoryNameTaken to a 409 by identity, so
// errors.Is must still hold after the service call.
func TestCreateCategoryCollisionPassesThroughUntranslated(t *testing.T) {
	repo := &fakeCategoryRepo{}
	svc := usecase.NewCategoryService(repo)
	ctx := context.Background()

	if _, err := svc.Create(ctx, "house-1", "Groceries"); err != nil {
		t.Fatalf("first create: %v", err)
	}
	if _, err := svc.Create(ctx, "house-1", "Groceries"); !errors.Is(err, domain.ErrCategoryNameTaken) {
		t.Fatalf("second create: err = %v, want ErrCategoryNameTaken", err)
	}
}

// TestRenameTrimsWhitespaceAndRefusesEmptyName mirrors Create's validation --
// the two must not drift, the same reasoning AccountService.validate's own
// comment gives for sharing one check between Create and Update.
func TestRenameTrimsWhitespaceAndRefusesEmptyName(t *testing.T) {
	repo := &fakeCategoryRepo{}
	svc := usecase.NewCategoryService(repo)
	ctx := context.Background()

	created, err := svc.Create(ctx, "house-1", "Streaming")
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	got, err := svc.Rename(ctx, "house-1", created.ID, "  Subscriptions  ")
	if err != nil {
		t.Fatalf("rename: %v", err)
	}
	if got.Name != "Subscriptions" {
		t.Fatalf("name = %q, want trimmed %q", got.Name, "Subscriptions")
	}

	if _, err := svc.Rename(ctx, "house-1", created.ID, "   "); !errors.Is(err, domain.ErrCategoryNameRequired) {
		t.Fatalf("rename to blank: err = %v, want ErrCategoryNameRequired", err)
	}
}

// TestRenameNotFoundPassesThroughUntranslated asserts a category id from
// another household -- or one that never existed -- surfaces as
// domain.ErrNotFound with errors.Is still true, not a generic failure.
func TestRenameNotFoundPassesThroughUntranslated(t *testing.T) {
	repo := &fakeCategoryRepo{}
	svc := usecase.NewCategoryService(repo)

	_, err := svc.Rename(context.Background(), "house-1", "does-not-exist", "New name")
	if !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("rename unknown id: err = %v, want ErrNotFound", err)
	}
}

// TestArchiveThenRestoreRoundTrips pins that Archive and Restore are thin
// orchestration over SetArchived -- nothing here re-validates the name or
// touches sort order, only the archived_at flip the repository already owns.
func TestArchiveThenRestoreRoundTrips(t *testing.T) {
	repo := &fakeCategoryRepo{}
	svc := usecase.NewCategoryService(repo)
	ctx := context.Background()

	created, err := svc.Create(ctx, "house-1", "Streaming")
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	archived, err := svc.Archive(ctx, "house-1", created.ID)
	if err != nil {
		t.Fatalf("archive: %v", err)
	}
	if !archived.IsArchived() {
		t.Fatalf("archived.IsArchived() = false, want true")
	}

	restored, err := svc.Restore(ctx, "house-1", created.ID)
	if err != nil {
		t.Fatalf("restore: %v", err)
	}
	if restored.IsArchived() {
		t.Fatalf("restored.IsArchived() = true, want false")
	}
}

// TestArchiveNotFoundPassesThroughUntranslated covers Archive and Restore's
// shared not-found path the same way Rename's does.
func TestArchiveNotFoundPassesThroughUntranslated(t *testing.T) {
	repo := &fakeCategoryRepo{}
	ctx := context.Background()

	if _, err := repo.Create(ctx, domain.Category{HouseholdID: "house-2", Name: "Other household", Kind: domain.CategoryExpense}); err != nil {
		t.Fatalf("seed other household: %v", err)
	}
	other, err := repo.List(ctx, "house-2", false)
	if err != nil || len(other) != 1 {
		t.Fatalf("list other household: %v, %d rows", err, len(other))
	}

	svc := usecase.NewCategoryService(repo)
	if _, err := svc.Archive(ctx, "house-1", other[0].ID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("archive across households: err = %v, want ErrNotFound", err)
	}
	if _, err := svc.Restore(ctx, "house-1", other[0].ID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("restore across households: err = %v, want ErrNotFound", err)
	}
}
