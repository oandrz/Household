package domain_test

import (
	"testing"
	"time"

	"github.com/andreasoentoro/hearth/api/internal/domain"
)

var now = time.Date(2026, 7, 18, 9, 41, 0, 0, time.UTC)

func TestNoFailuresMeansFullAllowance(t *testing.T) {
	state := domain.DefaultLockoutPolicy().Evaluate(nil, now)

	if state.Locked {
		t.Fatal("did not expect a lock")
	}
	if state.AttemptsRemaining != 3 {
		t.Fatalf("AttemptsRemaining = %d, want 3", state.AttemptsRemaining)
	}
}

func TestOneFailureLeavesTwoTries(t *testing.T) {
	failures := []time.Time{now.Add(-time.Minute)}

	state := domain.DefaultLockoutPolicy().Evaluate(failures, now)

	if state.Locked {
		t.Fatal("did not expect a lock")
	}
	if state.AttemptsRemaining != 2 {
		t.Fatalf("AttemptsRemaining = %d, want 2 — the design's copy says \"Two tries left\"", state.AttemptsRemaining)
	}
}

func TestThreeFailuresInsideTheWindowLock(t *testing.T) {
	failures := []time.Time{
		now.Add(-10 * time.Minute),
		now.Add(-5 * time.Minute),
		now.Add(-1 * time.Minute),
	}

	state := domain.DefaultLockoutPolicy().Evaluate(failures, now)

	if !state.Locked {
		t.Fatal("expected a lock")
	}
	if state.AttemptsRemaining != 0 {
		t.Fatalf("AttemptsRemaining = %d, want 0", state.AttemptsRemaining)
	}
	want := now.Add(-1 * time.Minute).Add(15 * time.Minute)
	if !state.Until.Equal(want) {
		t.Fatalf("Until = %v, want %v — the lock runs from the most recent failure", state.Until, want)
	}
}

func TestFailuresOlderThanTheWindowAreIgnored(t *testing.T) {
	failures := []time.Time{
		now.Add(-40 * time.Minute),
		now.Add(-30 * time.Minute),
		now.Add(-20 * time.Minute),
	}

	state := domain.DefaultLockoutPolicy().Evaluate(failures, now)

	if state.Locked {
		t.Fatal("did not expect a lock from failures outside the window")
	}
	if state.AttemptsRemaining != 3 {
		t.Fatalf("AttemptsRemaining = %d, want 3", state.AttemptsRemaining)
	}
}

func TestTheLockExpiresAfterFifteenMinutes(t *testing.T) {
	failures := []time.Time{
		now.Add(-20 * time.Minute),
		now.Add(-19 * time.Minute),
		now.Add(-18 * time.Minute),
	}

	state := domain.DefaultLockoutPolicy().Evaluate(failures, now)

	if state.Locked {
		t.Fatal("the lock should have expired 3 minutes ago")
	}
}

func TestFailuresNeedNotBeSorted(t *testing.T) {
	failures := []time.Time{
		now.Add(-1 * time.Minute),
		now.Add(-10 * time.Minute),
		now.Add(-5 * time.Minute),
	}

	state := domain.DefaultLockoutPolicy().Evaluate(failures, now)

	if !state.Locked {
		t.Fatal("expected a lock regardless of input order")
	}
}
