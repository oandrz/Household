package usecase_test

import (
	"context"
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
