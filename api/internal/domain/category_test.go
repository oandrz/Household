package domain_test

import (
	"errors"
	"testing"

	"github.com/andreasoentoro/hearth/api/internal/domain"
)

func TestParseCategoryKindRefusesAnythingElse(t *testing.T) {
	for _, in := range []string{"", "Expense", "spending", "transfer"} {
		if _, err := domain.ParseCategoryKind(in); !errors.Is(err, domain.ErrUnknownCategoryKind) {
			t.Fatalf("ParseCategoryKind(%q) = %v, want ErrUnknownCategoryKind", in, err)
		}
	}
	for _, in := range []domain.CategoryKind{domain.CategoryExpense, domain.CategoryIncome} {
		got, err := domain.ParseCategoryKind(string(in))
		if err != nil || got != in {
			t.Fatalf("ParseCategoryKind(%q) = %q, %v", in, got, err)
		}
	}
}

// The starter set is the design's own Budget screen. It is asserted here
// rather than in the service so that changing it is a deliberate edit to a
// test, not a silent edit to a slice of strings.
func TestStarterCategoriesAreTheDesignsOwnList(t *testing.T) {
	starter := domain.StarterCategories()

	wantNames := []string{
		"Groceries", "Dining out", "Transport", "Petrol", "Household",
		"Kids & school", "Health", "Utilities", "Insurance", "Subscriptions",
		"Fun & hobbies", "Giving", "Income",
	}
	if len(starter) != len(wantNames) {
		t.Fatalf("starter set has %d categories, want %d", len(starter), len(wantNames))
	}
	for i, want := range wantNames {
		if starter[i].Name != want {
			t.Fatalf("starter[%d].Name = %q, want %q", i, starter[i].Name, want)
		}
		if starter[i].SortOrder != i+1 {
			t.Fatalf("starter[%d].SortOrder = %d, want %d", i, starter[i].SortOrder, i+1)
		}
	}

	// Exactly one income category. An income transaction with nothing to pick
	// would be a dead end in the modal.
	income := 0
	for _, c := range starter {
		if c.Kind == domain.CategoryIncome {
			income++
		}
	}
	if income != 1 {
		t.Fatalf("starter set has %d income categories, want exactly 1", income)
	}
}
