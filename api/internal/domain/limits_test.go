package domain_test

import (
	"errors"
	"testing"
	"time"

	"github.com/andreasoentoro/hearth/api/internal/domain"
)

func TestEveryCountedKindHasAPositiveLimit(t *testing.T) {
	for _, k := range domain.CountedLimitKinds {
		if k.Max() <= 0 {
			t.Errorf("%s: Max() = %d, want a positive limit", k, k.Max())
		}
	}
}

// An unknown kind must refuse everything rather than allow everything:
// fail closed on values this code did not construct.
func TestAnUnknownLimitKindRefusesEverything(t *testing.T) {
	if got := domain.LimitKind("made_up").Max(); got != 0 {
		t.Fatalf("Max() = %d, want 0", got)
	}
}

// The monthly cap only means something below the total: a month can never
// hold more than the whole household.
func TestTheMonthlyTransactionCapIsBelowTheTotal(t *testing.T) {
	if domain.MaxTransactionsPerMonth >= domain.MaxTransactions {
		t.Fatalf("per month %d, total %d", domain.MaxTransactionsPerMonth, domain.MaxTransactions)
	}
}

func TestTheApprovedNumbers(t *testing.T) {
	cases := map[domain.LimitKind]int{
		domain.LimitTransactions:        100_000,
		domain.LimitTransactionsInMonth: 3_000,
		domain.LimitAccounts:            200,
		domain.LimitCategories:          200,
		domain.LimitGoals:               200,
		domain.LimitBills:               200,
		domain.LimitHoldings:            200,
		domain.LimitSpaces:              200,
		domain.LimitAgreementSections:   200,
		domain.LimitGoalContributions:   20_000,
		domain.LimitBillPayments:        20_000,
		domain.LimitHoldingEvents:       20_000,
		domain.LimitHoldingValuations:   20_000,
		domain.LimitHoldingIncome:       20_000,
		domain.LimitRetroActions:        20_000,
		domain.LimitAgreementProposals:  20_000,
		domain.LimitMembers:             30,
		domain.LimitPendingInvites:      20,
		domain.LimitActiveAPITokens:     20,
	}
	for k, want := range cases {
		if got := k.Max(); got != want {
			t.Errorf("%s: Max() = %d, want %d", k, got, want)
		}
	}
	if len(domain.CountedLimitKinds) != len(cases)-1 {
		t.Errorf("CountedLimitKinds has %d kinds, want %d (every kind but the monthly one)",
			len(domain.CountedLimitKinds), len(cases)-1)
	}
}

func TestLimitReachedErrorUnwrapsToItsSentinel(t *testing.T) {
	var err error = &domain.LimitReachedError{Kind: domain.LimitAccounts, Limit: 200}
	if !errors.Is(err, domain.ErrLimitReached) {
		t.Fatal("errors.Is(err, ErrLimitReached) = false")
	}
}

func TestBudgetMonthWindow(t *testing.T) {
	today := time.Date(2026, 10, 16, 0, 0, 0, 0, time.UTC)
	month := func(y int, m time.Month) time.Time { return time.Date(y, m, 1, 0, 0, 0, 0, time.UTC) }
	cases := []struct {
		name  string
		month time.Time
		want  bool
	}{
		{"January 2000 is the first allowed month", month(2000, time.January), true},
		{"December 1999 is refused", month(1999, time.December), false},
		{"this month", month(2026, time.October), true},
		{"24 months ahead", month(2028, time.October), true},
		{"25 months ahead is refused", month(2028, time.November), false},
		{"a mid-month date counts as its month", time.Date(2028, 10, 31, 0, 0, 0, 0, time.UTC), true},
	}
	for _, c := range cases {
		if got := domain.BudgetMonthAllowed(c.month, today); got != c.want {
			t.Errorf("%s: got %v, want %v", c.name, got, c.want)
		}
	}
}
