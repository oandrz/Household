package usecase_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/andreasoentoro/hearth/api/internal/domain"
	"github.com/andreasoentoro/hearth/api/internal/usecase"
)

func TestAdminReauthVerifiesTheRightPassword(t *testing.T) {
	users := newUserDouble()
	user := users.mustCreate(t, "operator@example.test", "hashed:correct-horse", "Operator")
	attempts := newFakeReauthAttemptRepo()

	svc := usecase.NewAdminReauthService(usecase.AdminReauthDeps{
		Users: users, Attempts: attempts, Hasher: &fakeHasher{},
		Clock: &fixedClock{now: time.Now()}, Policy: domain.DefaultLockoutPolicy(),
	})

	if err := svc.Verify(context.Background(), user.ID, "correct-horse"); err != nil {
		t.Fatalf("Verify with the right password = %v, want nil", err)
	}
	if err := svc.Verify(context.Background(), user.ID, "wrong"); !errors.Is(err, domain.ErrInvalidCredentials) {
		t.Fatalf("Verify with a wrong password = %v, want ErrInvalidCredentials", err)
	}
}

// TestAdminReauthLocksAfterThreeFailures uses the same policy as household
// sign-in, over its own ledger.
func TestAdminReauthLocksAfterThreeFailures(t *testing.T) {
	users := newUserDouble()
	user := users.mustCreate(t, "operator@example.test", "hashed:correct-horse", "Operator")
	attempts := newFakeReauthAttemptRepo()

	svc := usecase.NewAdminReauthService(usecase.AdminReauthDeps{
		Users: users, Attempts: attempts, Hasher: &fakeHasher{},
		Clock: &fixedClock{now: time.Now()}, Policy: domain.DefaultLockoutPolicy(),
	})

	for i := 0; i < 3; i++ {
		if err := svc.Verify(context.Background(), user.ID, "wrong"); !errors.Is(err, domain.ErrInvalidCredentials) {
			t.Fatalf("attempt %d = %v, want ErrInvalidCredentials", i+1, err)
		}
	}

	// Even the correct password is refused while locked -- otherwise the lock
	// bounds nothing, since guessing right is exactly what it must prevent.
	if err := svc.Verify(context.Background(), user.ID, "correct-horse"); !errors.Is(err, domain.ErrAdminLocked) {
		t.Fatalf("Verify while locked = %v, want ErrAdminLocked", err)
	}
}

// TestAdminReauthFailuresStayOutOfTheHouseholdLedger is the whole reason the
// second table exists. The fake login-attempt repo must never be touched.
func TestAdminReauthFailuresStayOutOfTheHouseholdLedger(t *testing.T) {
	users := newUserDouble()
	user := users.mustCreate(t, "operator@example.test", "hashed:correct-horse", "Operator")
	attempts := newFakeReauthAttemptRepo()
	household := newLoginAttemptDouble()

	svc := usecase.NewAdminReauthService(usecase.AdminReauthDeps{
		Users: users, Attempts: attempts, Hasher: &fakeHasher{},
		Clock: &fixedClock{now: time.Now()}, Policy: domain.DefaultLockoutPolicy(),
	})

	for i := 0; i < 5; i++ {
		_ = svc.Verify(context.Background(), user.ID, "wrong")
	}

	if len(household.records) != 0 {
		t.Fatalf("admin re-auth wrote %d rows to login_attempts; it must write none", len(household.records))
	}
	failures, err := attempts.FailuresSince(context.Background(), user.ID, time.Now().Add(-time.Hour))
	if err != nil {
		t.Fatalf("FailuresSince: %v", err)
	}
	if len(failures) != 5 {
		t.Fatalf("admin ledger holds %d failures, want 5", len(failures))
	}
}
