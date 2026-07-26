package domain_test

import (
	"testing"
	"time"

	"github.com/andreasoentoro/hearth/api/internal/domain"
)

var lockoutNow = time.Date(2026, 7, 18, 9, 41, 0, 0, time.UTC)

func TestNoFailuresMeansFullAllowance(t *testing.T) {
	state := domain.DefaultLockoutPolicy().Evaluate(nil, lockoutNow)

	if state.Locked {
		t.Fatal("did not expect a lock")
	}
	if state.AttemptsRemaining != 3 {
		t.Fatalf("AttemptsRemaining = %d, want 3", state.AttemptsRemaining)
	}
}

func TestOneFailureLeavesTwoTries(t *testing.T) {
	failures := []time.Time{lockoutNow.Add(-time.Minute)}

	state := domain.DefaultLockoutPolicy().Evaluate(failures, lockoutNow)

	if state.Locked {
		t.Fatal("did not expect a lock")
	}
	if state.AttemptsRemaining != 2 {
		t.Fatalf("AttemptsRemaining = %d, want 2 — the design's copy says \"Two tries left\"", state.AttemptsRemaining)
	}
}

func TestThreeFailuresInsideTheWindowLock(t *testing.T) {
	failures := []time.Time{
		lockoutNow.Add(-10 * time.Minute),
		lockoutNow.Add(-5 * time.Minute),
		lockoutNow.Add(-1 * time.Minute),
	}

	state := domain.DefaultLockoutPolicy().Evaluate(failures, lockoutNow)

	if !state.Locked {
		t.Fatal("expected a lock")
	}
	if state.AttemptsRemaining != 0 {
		t.Fatalf("AttemptsRemaining = %d, want 0", state.AttemptsRemaining)
	}
	want := lockoutNow.Add(-1 * time.Minute).Add(15 * time.Minute)
	if !state.Until.Equal(want) {
		t.Fatalf("Until = %v, want %v — the lock runs from the most recent failure", state.Until, want)
	}
}

func TestFailuresOlderThanTheWindowAreIgnored(t *testing.T) {
	failures := []time.Time{
		lockoutNow.Add(-40 * time.Minute),
		lockoutNow.Add(-30 * time.Minute),
		lockoutNow.Add(-20 * time.Minute),
	}

	state := domain.DefaultLockoutPolicy().Evaluate(failures, lockoutNow)

	if state.Locked {
		t.Fatal("did not expect a lock from failures outside the window")
	}
	if state.AttemptsRemaining != 3 {
		t.Fatalf("AttemptsRemaining = %d, want 3", state.AttemptsRemaining)
	}
}

func TestTheLockExpiresAfterFifteenMinutes(t *testing.T) {
	failures := []time.Time{
		lockoutNow.Add(-20 * time.Minute),
		lockoutNow.Add(-19 * time.Minute),
		lockoutNow.Add(-18 * time.Minute),
	}

	state := domain.DefaultLockoutPolicy().Evaluate(failures, lockoutNow)

	if state.Locked {
		t.Fatal("the lock should have expired 3 minutes ago")
	}
}

// TestALockAtTheExactBoundaryOfExpiryGrantsAFreshAllowance pins the design
// decision documented on Evaluate's expired-lock branch: once a served lock
// reaches its LockFor deadline, the household gets a full fresh allowance,
// even though the failures that triggered it are still (at this exact
// instant) counted as inside the window. Under DefaultLockoutPolicy,
// Window == LockFor, so the only moment this branch is reachable is the
// nanosecond the lock's deadline lands exactly on `now` — this test
// constructs that boundary deliberately so the invariant stays visible to
// whoever next changes the policy's Window/LockFor relationship.
func TestALockAtTheExactBoundaryOfExpiryGrantsAFreshAllowance(t *testing.T) {
	policy := domain.DefaultLockoutPolicy()
	cutoff := lockoutNow.Add(-policy.Window)
	failures := []time.Time{cutoff, cutoff, cutoff}

	state := policy.Evaluate(failures, lockoutNow)

	if state.Locked {
		t.Fatal("expected the lock to have just expired, not to still be active")
	}
	if state.AttemptsRemaining != policy.MaxAttempts {
		t.Fatalf("AttemptsRemaining = %d, want %d (a fresh allowance) — see the comment on Evaluate's expired-lock branch", state.AttemptsRemaining, policy.MaxAttempts)
	}
}

func TestFailuresNeedNotBeSorted(t *testing.T) {
	failures := []time.Time{
		lockoutNow.Add(-1 * time.Minute),
		lockoutNow.Add(-10 * time.Minute),
		lockoutNow.Add(-5 * time.Minute),
	}

	state := domain.DefaultLockoutPolicy().Evaluate(failures, lockoutNow)

	if !state.Locked {
		t.Fatal("expected a lock regardless of input order")
	}
}
