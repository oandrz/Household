package usecase_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/andreasoentoro/hearth/api/internal/domain"
	"github.com/andreasoentoro/hearth/api/internal/usecase"
)

func TestSignInWithTheCorrectPasswordCreatesASession(t *testing.T) {
	f := newFixture(t)

	result, err := f.auth.SignIn(context.Background(), "andreas@hearth.family", "hunter2")
	if err != nil {
		t.Fatalf("SignIn: %v", err)
	}
	if result.SessionToken == "" {
		t.Fatal("expected a session token")
	}
	if result.HouseholdID != f.householdID {
		t.Fatalf("HouseholdID = %q, want %q", result.HouseholdID, f.householdID)
	}
	if got := f.sessions.count(); got != 1 {
		t.Fatalf("sessions = %d, want 1", got)
	}
}

func TestSignInWithAWrongPasswordReportsTwoTriesLeft(t *testing.T) {
	f := newFixture(t)

	_, err := f.auth.SignIn(context.Background(), "andreas@hearth.family", "wrong")

	var failure *usecase.SignInFailedError
	if !errors.As(err, &failure) {
		t.Fatalf("err = %v, want *SignInFailedError", err)
	}
	if failure.AttemptsRemaining != 2 {
		t.Fatalf("AttemptsRemaining = %d, want 2", failure.AttemptsRemaining)
	}
	if failure.Locked {
		t.Fatal("one failure must not lock")
	}
}

func TestThreeWrongPasswordsLockTheHousehold(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	for i := 0; i < 3; i++ {
		f.auth.SignIn(ctx, "andreas@hearth.family", "wrong")
		f.clock.Advance(time.Second)
	}

	_, err := f.auth.SignIn(ctx, "andreas@hearth.family", "hunter2")

	var failure *usecase.SignInFailedError
	if !errors.As(err, &failure) {
		t.Fatalf("err = %v, want *SignInFailedError", err)
	}
	if !failure.Locked {
		t.Fatal("expected the household to be locked")
	}
	if failure.LockedUntil.IsZero() {
		t.Fatal("expected LockedUntil to be set")
	}
}

func TestTheLockLiftsAfterFifteenMinutes(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	for i := 0; i < 3; i++ {
		f.auth.SignIn(ctx, "andreas@hearth.family", "wrong")
		f.clock.Advance(time.Second)
	}
	f.clock.Advance(16 * time.Minute)

	if _, err := f.auth.SignIn(ctx, "andreas@hearth.family", "hunter2"); err != nil {
		t.Fatalf("SignIn after the lock expired: %v", err)
	}
}

func TestASuccessfulSignInClearsTheFailureCount(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	f.auth.SignIn(ctx, "andreas@hearth.family", "wrong")
	f.clock.Advance(time.Second)
	f.auth.SignIn(ctx, "andreas@hearth.family", "wrong")
	f.clock.Advance(time.Second)

	if _, err := f.auth.SignIn(ctx, "andreas@hearth.family", "hunter2"); err != nil {
		t.Fatalf("SignIn: %v", err)
	}

	_, err := f.auth.SignIn(ctx, "andreas@hearth.family", "wrong")
	var failure *usecase.SignInFailedError
	errors.As(err, &failure)
	if failure.AttemptsRemaining != 2 {
		t.Fatalf("AttemptsRemaining = %d, want 2 — success resets the counter", failure.AttemptsRemaining)
	}
}

func TestAnUnknownEmailFailsIdenticallyToAWrongPassword(t *testing.T) {
	f := newFixture(t)

	_, unknownErr := f.auth.SignIn(context.Background(), "stranger@example.com", "whatever")
	_, wrongErr := f.auth.SignIn(context.Background(), "andreas@hearth.family", "wrong")

	var a, b *usecase.SignInFailedError
	if !errors.As(unknownErr, &a) || !errors.As(wrongErr, &b) {
		t.Fatalf("both must be *SignInFailedError: %v / %v", unknownErr, wrongErr)
	}
	if a.Locked != b.Locked {
		t.Fatal("the two failures must be indistinguishable to a caller")
	}
	if a.AttemptsRemaining != b.AttemptsRemaining {
		t.Fatalf("AttemptsRemaining differs: unknown = %d, wrong password = %d — "+
			"the countdown itself must not reveal whether the address exists",
			a.AttemptsRemaining, b.AttemptsRemaining)
	}
}

func TestAnUnknownEmailNeverLocksARealHousehold(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	for i := 0; i < 5; i++ {
		f.auth.SignIn(ctx, "stranger@example.com", "whatever")
		f.clock.Advance(time.Second)
	}

	if _, err := f.auth.SignIn(ctx, "andreas@hearth.family", "hunter2"); err != nil {
		t.Fatalf("attempts against an unknown address must not lock the household: %v", err)
	}
}

func TestSignOutRevokesTheSession(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	result, err := f.auth.SignIn(ctx, "andreas@hearth.family", "hunter2")
	if err != nil {
		t.Fatalf("SignIn: %v", err)
	}

	if err := f.auth.SignOut(ctx, result.SessionToken); err != nil {
		t.Fatalf("SignOut: %v", err)
	}
	if f.sessions.live() != 0 {
		t.Fatal("expected the session to be revoked")
	}
}

func TestUsersWithoutAPasswordCannotSignIn(t *testing.T) {
	f := newFixture(t)

	_, err := f.auth.SignIn(context.Background(), "ethan@hearth.family", "")

	var failure *usecase.SignInFailedError
	if !errors.As(err, &failure) {
		t.Fatalf("err = %v, want *SignInFailedError — a credential-less member must not sign in", err)
	}
}

var _ = domain.DefaultLockoutPolicy
