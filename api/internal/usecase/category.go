package usecase

import (
	"context"
	"strings"

	"github.com/andreasoentoro/hearth/api/internal/domain"
)

// CategoryService is the household's spending taxonomy: reading it, and the
// writes Budget's "Edit categories" screen makes -- add, rename, archive,
// restore. Transactions reads the same list this service seeds and writes.
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
//
// Create deliberately does not seed -- it is a pure write, not a second
// seeding moment. The invariant that keeps that safe: every UI path reaches
// Create only after populating a category list first (the "Log a
// transaction" modal's dropdown, Budget's "Edit categories" screen), and
// that list is always this method's return value. A household whose first-
// ever category action was a direct Create, bypassing List entirely, would
// get one category and never the starter set.
func (s *CategoryService) List(ctx context.Context, householdID string) ([]domain.Category, error) {
	if err := s.repo.EnsureSeeded(ctx, householdID, domain.StarterCategories()); err != nil {
		return nil, err
	}
	return s.repo.List(ctx, householdID, false)
}

// Create adds one category to a household's list. It is always
// domain.CategoryExpense -- Budget's caps envelope spending only, an income
// category has no cap to set, so the write path that makes one takes no kind
// argument at all rather than trusting a caller to send the right one.
//
// A name colliding with an existing (live or archived) row surfaces as
// domain.ErrCategoryNameTaken straight from the repository; this method does
// not translate it.
func (s *CategoryService) Create(ctx context.Context, householdID, name string) (domain.Category, error) {
	name, err := validateCategoryName(name)
	if err != nil {
		return domain.Category{}, err
	}
	return s.repo.Create(ctx, domain.Category{
		HouseholdID: householdID,
		Name:        name,
		Kind:        domain.CategoryExpense,
	})
}

// Rename changes a category's name only, with the same trim-then-refuse-empty
// rule as Create -- deliberately, so the two validations cannot drift apart.
// A collision or an id outside this household passes through untranslated as
// domain.ErrCategoryNameTaken or domain.ErrNotFound.
func (s *CategoryService) Rename(ctx context.Context, householdID, categoryID, name string) (domain.Category, error) {
	name, err := validateCategoryName(name)
	if err != nil {
		return domain.Category{}, err
	}
	return s.repo.Rename(ctx, householdID, categoryID, name)
}

// Archive retires a category without deleting it, so every transaction and
// budget line that references it keeps its name. domain.ErrNotFound passes
// through untranslated for an id outside this household.
func (s *CategoryService) Archive(ctx context.Context, householdID, categoryID string) (domain.Category, error) {
	return s.repo.SetArchived(ctx, householdID, categoryID, true)
}

// Restore reverses Archive. Same not-found contract.
func (s *CategoryService) Restore(ctx context.Context, householdID, categoryID string) (domain.Category, error) {
	return s.repo.SetArchived(ctx, householdID, categoryID, false)
}

// validateCategoryName trims surrounding whitespace and refuses an empty
// result, the shared rule Create and Rename both apply.
func validateCategoryName(name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", domain.ErrCategoryNameRequired
	}
	return name, nil
}
