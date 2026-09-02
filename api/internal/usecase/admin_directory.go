package usecase

import (
	"context"
	"strings"
	"time"

	"github.com/andreasoentoro/hearth/api/internal/domain"
)

// AdminDirectoryService is the operator's read-only view of the install:
// how many households exist, which are active, who signed up, and -- for
// one household -- who its members are and whether its sign-in is locked.
//
// It is separate from AdminService on purpose: that service is "who is a
// platform admin, feature flags, the audit log", and this one reads across
// every household, a boundary nothing else in the product crosses. It takes
// no actor parameter; the HTTP layer's /admin guards are the only gate.
type AdminDirectoryService struct{ d AdminDirectoryDeps }

type AdminDirectoryDeps struct {
	Directory     AdminDirectoryRepository
	LoginAttempts LoginAttemptRepository
	Clock         Clock
	// Policy is the household sign-in lockout policy, the same one
	// AuthService.SignIn evaluates. Zero means domain.DefaultLockoutPolicy,
	// filled in by the constructor exactly as NewAuthService does, so the
	// two can never disagree by omission.
	Policy domain.LockoutPolicy
}

const (
	// DirectoryDefaultLimit is how many households Overview returns when
	// the caller names no limit or an unusable one.
	DirectoryDefaultLimit = 50
	// DirectoryMaxLimit is the most Overview will return; past it the
	// screen tells the operator to search instead.
	DirectoryMaxLimit = 200

	directoryActiveWindow = 7 * 24 * time.Hour
	directorySignupWindow = 30 * 24 * time.Hour
)

func NewAdminDirectoryService(d AdminDirectoryDeps) *AdminDirectoryService {
	if d.Policy.MaxAttempts == 0 {
		d.Policy = domain.DefaultLockoutPolicy()
	}
	return &AdminDirectoryService{d: d}
}

type DirectoryOverview struct {
	Metrics    DirectoryMetrics
	Households []HouseholdListing
	// Truncated is true when more households matched than were returned.
	Truncated bool
}

type HouseholdPage struct {
	HouseholdDetail
	// LockedUntil is non-nil while the household's password sign-in is
	// locked, computed by the same policy sign-in itself applies.
	LockedUntil *time.Time
}

// clampDirectoryLimit: anything unusable falls back to the default rather
// than failing -- the operator typed a URL, not a form.
func clampDirectoryLimit(limit int) int {
	switch {
	case limit <= 0:
		return DirectoryDefaultLimit
	case limit > DirectoryMaxLimit:
		return DirectoryMaxLimit
	default:
		return limit
	}
}

// Overview is the households page: the four counters and the matching
// households, in one call so one page view is one request.
func (s *AdminDirectoryService) Overview(ctx context.Context, q string, limit int) (DirectoryOverview, error) {
	now := s.d.Clock.Now()
	limit = clampDirectoryLimit(limit)
	q = strings.TrimSpace(q)

	metrics, err := s.d.Directory.Metrics(ctx, now.Add(-directoryActiveWindow), now.Add(-directorySignupWindow), now)
	if err != nil {
		return DirectoryOverview{}, err
	}
	// limit+1: one row past the limit is how "more exist" is learned
	// without a second COUNT.
	rows, err := s.d.Directory.SearchHouseholds(ctx, q, limit+1, now)
	if err != nil {
		return DirectoryOverview{}, err
	}
	truncated := len(rows) > limit
	if truncated {
		rows = rows[:limit]
	}
	if rows == nil {
		rows = []HouseholdListing{}
	}
	return DirectoryOverview{Metrics: metrics, Households: rows, Truncated: truncated}, nil
}

// Household is the drill-in page. The lockout is evaluated only after the
// household is known to exist, so an unknown id is ErrNotFound before any
// second query runs.
func (s *AdminDirectoryService) Household(ctx context.Context, householdID string) (HouseholdPage, error) {
	now := s.d.Clock.Now()
	detail, err := s.d.Directory.Household(ctx, householdID, now)
	if err != nil {
		return HouseholdPage{}, err
	}
	failures, err := s.d.LoginAttempts.FailuresSince(ctx, householdID, now.Add(-s.d.Policy.Window))
	if err != nil {
		return HouseholdPage{}, err
	}
	page := HouseholdPage{HouseholdDetail: detail}
	if state := s.d.Policy.Evaluate(failures, now); state.Locked {
		until := state.Until
		page.LockedUntil = &until
	}
	return page, nil
}
