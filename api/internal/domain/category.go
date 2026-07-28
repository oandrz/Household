package domain

import (
	"fmt"
	"time"
)

// CategoryKind splits what a household spends from what it receives, so the
// "Log a transaction" modal can offer Groceries for an expense and never for
// an income.
type CategoryKind string

const (
	CategoryExpense CategoryKind = "expense"
	CategoryIncome  CategoryKind = "income"
)

// ParseCategoryKind refuses anything it does not recognise. The default is the
// point: a kind arrives from a database column, so it is a value this code did
// not construct, and guessing at an unknown one would offer a spending
// category for income.
func ParseCategoryKind(s string) (CategoryKind, error) {
	switch CategoryKind(s) {
	case CategoryExpense:
		return CategoryExpense, nil
	case CategoryIncome:
		return CategoryIncome, nil
	default:
		return "", fmt.Errorf("%w: %q", ErrUnknownCategoryKind, s)
	}
}

// Category is one line of a household's spending taxonomy. Budget's envelopes
// are sums over the transactions that reference these rows.
type Category struct {
	ID          string
	HouseholdID string
	Name        string
	Kind        CategoryKind
	SortOrder   int
	ArchivedAt  *time.Time
}

// IsArchived reports whether Budget's "Edit categories" has retired this one.
// An archived category keeps its row so transactions that reference it keep
// their name, and keeps its unique key so the starter set is not re-seeded
// over it.
func (c Category) IsArchived() bool { return c.ArchivedAt != nil }

// StarterCategories is what a household gets the first time anything reads its
// categories. It is the design's own Budget screen list, in the design's own
// order -- which is why SortOrder is explicit rather than alphabetical.
//
// It lives in the domain rather than in CategoryService so that the seeding
// path and any future template share exactly one definition. Adding to it is
// safe for existing households: EnsureSeeded only fires for a household that
// has none, so a new entry reaches new households and leaves everyone else's
// list as they have arranged it.
func StarterCategories() []Category {
	names := []struct {
		name string
		kind CategoryKind
	}{
		{"Groceries", CategoryExpense},
		{"Dining out", CategoryExpense},
		{"Transport", CategoryExpense},
		{"Petrol", CategoryExpense},
		{"Household", CategoryExpense},
		{"Kids & school", CategoryExpense},
		{"Health", CategoryExpense},
		{"Utilities", CategoryExpense},
		{"Insurance", CategoryExpense},
		{"Subscriptions", CategoryExpense},
		{"Fun & hobbies", CategoryExpense},
		{"Giving", CategoryExpense},
		{"Income", CategoryIncome},
	}
	out := make([]Category, 0, len(names))
	for i, n := range names {
		out = append(out, Category{Name: n.name, Kind: n.kind, SortOrder: i + 1})
	}
	return out
}
