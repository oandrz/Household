package usecase_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/andreasoentoro/hearth/api/internal/domain"
	"github.com/andreasoentoro/hearth/api/internal/usecase"
)

var directoryNow = time.Date(2026, 9, 2, 12, 0, 0, 0, time.UTC)

// fakeDirectoryRepo records every argument it is given and answers what the
// test configured, so a test can assert both what the service asked for and
// what it did with the answer.
type fakeDirectoryRepo struct {
	metrics        usecase.DirectoryMetrics
	rows           []usecase.HouseholdListing
	detail         usecase.HouseholdDetail
	detailErr      error
	gotQ           string
	gotLimit       int
	gotActive      time.Time
	gotSignups     time.Time
	householdCalls int
}

func (f *fakeDirectoryRepo) Metrics(_ context.Context, activeSince, signupsSince, _ time.Time) (usecase.DirectoryMetrics, error) {
	f.gotActive, f.gotSignups = activeSince, signupsSince
	return f.metrics, nil
}

func (f *fakeDirectoryRepo) SearchHouseholds(_ context.Context, q string, limit int, _ time.Time) ([]usecase.HouseholdListing, error) {
	f.gotQ, f.gotLimit = q, limit
	if len(f.rows) > limit {
		return f.rows[:limit], nil
	}
	return f.rows, nil
}

func (f *fakeDirectoryRepo) Household(_ context.Context, _ string, _ time.Time) (usecase.HouseholdDetail, error) {
	f.householdCalls++
	return f.detail, f.detailErr
}

// directoryAttempts is a LoginAttemptRepository that answers FailuresSince
// with a fixed list and counts how often it was asked. Every other method
// fails loudly so a test cannot lean on one by accident.
type directoryAttempts struct {
	failures []time.Time
	calls    int
}

var errAttemptsUnexpected = errors.New("directoryAttempts: unexpected call")

func (d *directoryAttempts) Record(context.Context, *string, *string, string, bool, time.Time) error {
	return errAttemptsUnexpected
}
func (d *directoryAttempts) FailuresSince(_ context.Context, _ string, _ time.Time) ([]time.Time, error) {
	d.calls++
	return d.failures, nil
}
func (d *directoryAttempts) FailuresSinceForEmail(context.Context, string, time.Time) ([]time.Time, error) {
	return nil, errAttemptsUnexpected
}
func (d *directoryAttempts) ClearFailures(context.Context, string) error {
	return errAttemptsUnexpected
}
func (d *directoryAttempts) Prune(context.Context, time.Time) (int64, error) {
	return 0, errAttemptsUnexpected
}

func listings(n int) []usecase.HouseholdListing {
	out := make([]usecase.HouseholdListing, n)
	for i := range out {
		out[i] = usecase.HouseholdListing{ID: "h" + string(rune('a'+i)), Name: "House"}
	}
	return out
}

func newDirectoryService(repo *fakeDirectoryRepo, attempts *directoryAttempts) *usecase.AdminDirectoryService {
	return usecase.NewAdminDirectoryService(usecase.AdminDirectoryDeps{
		Directory:     repo,
		LoginAttempts: attempts,
		Clock:         &fixedClock{now: directoryNow},
	})
}

func TestOverviewDefaultsAndClampsTheLimit(t *testing.T) {
	cases := []struct{ in, want int }{
		{0, usecase.DirectoryDefaultLimit},
		{-1, usecase.DirectoryDefaultLimit},
		{7, 7},
		{usecase.DirectoryMaxLimit, usecase.DirectoryMaxLimit},
		{500, usecase.DirectoryMaxLimit},
	}
	for _, tc := range cases {
		repo := &fakeDirectoryRepo{}
		svc := newDirectoryService(repo, &directoryAttempts{})
		if _, err := svc.Overview(context.Background(), "", tc.in); err != nil {
			t.Fatalf("Overview(limit=%d): %v", tc.in, err)
		}
		// limit+1: that extra row is how Truncated is known.
		if repo.gotLimit != tc.want+1 {
			t.Fatalf("limit %d reached the repository as %d, want %d", tc.in, repo.gotLimit, tc.want+1)
		}
	}
}

func TestOverviewTrimsTheQuery(t *testing.T) {
	repo := &fakeDirectoryRepo{}
	svc := newDirectoryService(repo, &directoryAttempts{})
	if _, err := svc.Overview(context.Background(), "  christine@ ", 10); err != nil {
		t.Fatalf("Overview: %v", err)
	}
	if repo.gotQ != "christine@" {
		t.Fatalf("q reached the repository as %q", repo.gotQ)
	}
}

func TestOverviewIsTruncatedOnlyWhenAnExtraRowCameBack(t *testing.T) {
	repo := &fakeDirectoryRepo{rows: listings(4)}
	svc := newDirectoryService(repo, &directoryAttempts{})

	got, err := svc.Overview(context.Background(), "", 3)
	if err != nil {
		t.Fatalf("Overview: %v", err)
	}
	if !got.Truncated || len(got.Households) != 3 {
		t.Fatalf("4 rows for limit 3: Truncated=%v len=%d, want true and 3", got.Truncated, len(got.Households))
	}

	got, err = svc.Overview(context.Background(), "", 4)
	if err != nil {
		t.Fatalf("Overview: %v", err)
	}
	if got.Truncated || len(got.Households) != 4 {
		t.Fatalf("4 rows for limit 4: Truncated=%v len=%d, want false and 4", got.Truncated, len(got.Households))
	}
}

func TestOverviewAsksForTheSpecsWindows(t *testing.T) {
	repo := &fakeDirectoryRepo{}
	svc := newDirectoryService(repo, &directoryAttempts{})
	if _, err := svc.Overview(context.Background(), "", 0); err != nil {
		t.Fatalf("Overview: %v", err)
	}
	if want := directoryNow.Add(-7 * 24 * time.Hour); !repo.gotActive.Equal(want) {
		t.Fatalf("active window cutoff = %v, want %v", repo.gotActive, want)
	}
	if want := directoryNow.Add(-30 * 24 * time.Hour); !repo.gotSignups.Equal(want) {
		t.Fatalf("sign-up window cutoff = %v, want %v", repo.gotSignups, want)
	}
}

func TestHouseholdReportsLockedUntilWhenThePolicySaysLocked(t *testing.T) {
	policy := domain.DefaultLockoutPolicy()
	latest := directoryNow.Add(-time.Minute)
	attempts := &directoryAttempts{failures: []time.Time{
		latest.Add(-2 * time.Minute), latest.Add(-time.Minute), latest,
	}}
	svc := newDirectoryService(&fakeDirectoryRepo{detail: usecase.HouseholdDetail{ID: "h1"}}, attempts)

	page, err := svc.Household(context.Background(), "h1")
	if err != nil {
		t.Fatalf("Household: %v", err)
	}
	if page.LockedUntil == nil {
		t.Fatal("three failures inside the window did not report a lock")
	}
	if want := latest.Add(policy.LockFor); !page.LockedUntil.Equal(want) {
		t.Fatalf("LockedUntil = %v, want %v (latest failure + LockFor)", page.LockedUntil, want)
	}
}

func TestHouseholdReportsNoLockWithTwoFailures(t *testing.T) {
	attempts := &directoryAttempts{failures: []time.Time{directoryNow.Add(-2 * time.Minute), directoryNow.Add(-time.Minute)}}
	svc := newDirectoryService(&fakeDirectoryRepo{detail: usecase.HouseholdDetail{ID: "h1"}}, attempts)

	page, err := svc.Household(context.Background(), "h1")
	if err != nil {
		t.Fatalf("Household: %v", err)
	}
	if page.LockedUntil != nil {
		t.Fatalf("two failures reported a lock until %v", page.LockedUntil)
	}
}

func TestHouseholdNotFoundNeverConsultsLoginAttempts(t *testing.T) {
	attempts := &directoryAttempts{}
	svc := newDirectoryService(&fakeDirectoryRepo{detailErr: domain.ErrNotFound}, attempts)

	_, err := svc.Household(context.Background(), "missing")
	if !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound unchanged", err)
	}
	if attempts.calls != 0 {
		t.Fatalf("FailuresSince was called %d times for a household that does not exist", attempts.calls)
	}
}
