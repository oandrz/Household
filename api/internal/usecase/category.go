package usecase

import (
	"context"

	"github.com/andreasoentoro/hearth/api/internal/domain"
)

// CategoryService is the household's spending taxonomy: one job, reading it.
//
// Nothing here renames, adds or archives a category. Those controls live on
// Budget's "Edit categories" screen, which is the next feature; this service
// exists so Transactions has a list to show and Budget has one to sum over.
type CategoryService struct {
	repo CategoryRepository
}

func NewCategoryService(repo CategoryRepository) *CategoryService {
	return &CategoryService{repo: repo}
}

// List returns the household's live categories, seeding the starter set first
// if it has none.
//
// A read that writes is unusual, and it is deliberate. Seeding at household
// creation would reach into SignupRepository.Provision -- the transaction a
// stranger's sign-up depends on, whose atomicity is documented at length --
// for a feature that does not need to be there. Seeding on first *write* is
// too late: the "Log a transaction" modal needs a category list before the
// household's first transaction exists. First read is the only moment left.
//
// EnsureSeeded is idempotent and concurrency-safe (see its port doc), so
// calling it on every read costs one cheap count and nothing else.
func (s *CategoryService) List(ctx context.Context, householdID string) ([]domain.Category, error) {
	if err := s.repo.EnsureSeeded(ctx, householdID, domain.StarterCategories()); err != nil {
		return nil, err
	}
	return s.repo.List(ctx, householdID, false)
}
