package usecase_test

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/andreasoentoro/hearth/api/internal/domain"
)

// usageDouble answers HouseholdUsage from counts a test sets. Every kind
// starts at zero, so a service under test is far below every cap unless the
// test says otherwise. Like the real adapter, it refuses a kind outside
// domain.CountedLimitKinds.
type usageDouble struct {
	mu     sync.Mutex
	counts map[domain.LimitKind]int
	months map[string]int
}

func newUsageDouble() *usageDouble {
	return &usageDouble{counts: map[domain.LimitKind]int{}, months: map[string]int{}}
}

func (u *usageDouble) set(kind domain.LimitKind, n int) {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.counts[kind] = n
}

func (u *usageDouble) setMonth(month time.Time, n int) {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.months[month.Format("2006-01")] = n
}

func (u *usageDouble) Count(_ context.Context, _ string, kind domain.LimitKind) (int, error) {
	u.mu.Lock()
	defer u.mu.Unlock()
	for _, k := range domain.CountedLimitKinds {
		if k == kind {
			return u.counts[kind], nil
		}
	}
	return 0, fmt.Errorf("usageDouble: no count for %q", kind)
}

func (u *usageDouble) TransactionsInMonth(_ context.Context, _ string, month time.Time) (int, error) {
	u.mu.Lock()
	defer u.mu.Unlock()
	return u.months[month.Format("2006-01")], nil
}

// failingUsage is a HouseholdUsage whose counts cannot be read.
type failingUsage struct{}

func (failingUsage) Count(context.Context, string, domain.LimitKind) (int, error) {
	return 0, fmt.Errorf("usage unavailable")
}
func (failingUsage) TransactionsInMonth(context.Context, string, time.Time) (int, error) {
	return 0, fmt.Errorf("usage unavailable")
}
