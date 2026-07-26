package usecase_test

import (
	"context"
	"errors"
	"strings"
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
	// Pin the guard's actual purpose: the hasher must never be asked to
	// verify a password against Ethan's own (empty) stored hash. Without
	// this assertion the test would pass even with the guard deleted,
	// because fakeHasher happens to reject an empty encoded hash for every
	// input. SignIn does run a decoy verification for this branch now (see
	// TestACredentialLessMemberRunsADecoyVerification below), against a
	// different encoded hash entirely — that call is expected and doesn't
	// count here.
	if n := f.hasher.verifyCallsWithEncoded(""); n != 0 {
		t.Fatalf("Verify was called %d times against the member's own empty hash; "+
			"a credential-less member must be rejected before any real password comparison", n)
	}
}

// TestUnknownAddressRunsADecoyVerification, TestALockedHouseholdRunsADecoyVerification
// and TestACredentialLessMemberRunsADecoyVerification pin the timing-parity
// fix: every branch of SignIn that returns without a real password
// comparison must still call Hasher.Verify exactly once, against a decoy
// hash, so that branch costs the same (against a real hasher) as one that
// does compare a real password. Without the decoy call, these branches would
// return near-instantly while a wrong-password guess pays argon2id's real
// cost — a timing side channel that defeats the same indistinguishability
// the error type and the attempts countdown exist to protect.
func TestUnknownAddressRunsADecoyVerification(t *testing.T) {
	f := newFixture(t)

	f.auth.SignIn(context.Background(), "stranger@example.com", "whatever")

	if got := f.hasher.verifyCallCount(); got != 1 {
		t.Fatalf("Verify calls = %d, want 1 (a decoy verification for timing parity)", got)
	}
}

func TestALockedHouseholdRunsADecoyVerification(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	for i := 0; i < 3; i++ {
		f.auth.SignIn(ctx, "andreas@hearth.family", "wrong")
		f.clock.Advance(time.Second)
	}
	before := f.hasher.verifyCallCount()

	f.auth.SignIn(ctx, "andreas@hearth.family", "hunter2")

	if got := f.hasher.verifyCallCount() - before; got != 1 {
		t.Fatalf("Verify calls while locked = %d, want 1 (a decoy verification for timing parity)", got)
	}
}

func TestACredentialLessMemberRunsADecoyVerification(t *testing.T) {
	f := newFixture(t)

	f.auth.SignIn(context.Background(), "ethan@hearth.family", "")

	if got := f.hasher.verifyCallCount(); got != 1 {
		t.Fatalf("Verify calls = %d, want 1 (a decoy verification for timing parity)", got)
	}
}

// TestLockedUntilAdvancesWhileALockedHouseholdIsGuessed and its companion
// below pin the fix for a timing oracle: a caller hammering an
// already-locked household must see LockedUntil advance exactly like a
// caller hammering an unknown address does. If the locked branch ever stops
// recording the attempt, LockedUntil freezes here while the unknown-address
// companion keeps moving, and the two paths become distinguishable again.
func TestLockedUntilAdvancesWhileALockedHouseholdIsGuessed(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	for i := 0; i < 3; i++ {
		f.auth.SignIn(ctx, "andreas@hearth.family", "wrong")
		f.clock.Advance(time.Second)
	}

	_, err := f.auth.SignIn(ctx, "andreas@hearth.family", "hunter2")
	var first *usecase.SignInFailedError
	if !errors.As(err, &first) || !first.Locked {
		t.Fatalf("err = %v, want a locked *SignInFailedError", err)
	}

	f.clock.Advance(time.Minute)

	_, err = f.auth.SignIn(ctx, "andreas@hearth.family", "hunter2")
	var second *usecase.SignInFailedError
	if !errors.As(err, &second) || !second.Locked {
		t.Fatalf("err = %v, want a locked *SignInFailedError", err)
	}

	if !second.LockedUntil.After(first.LockedUntil) {
		t.Fatalf("LockedUntil did not advance: first = %v, second = %v — "+
			"a locked household must not report a frozen deadline while an unknown "+
			"address's deadline keeps advancing, or the two become distinguishable",
			first.LockedUntil, second.LockedUntil)
	}
}

func TestLockedUntilAdvancesWhileAnUnknownAddressIsGuessed(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	for i := 0; i < 3; i++ {
		f.auth.SignIn(ctx, "stranger@example.com", "whatever")
		f.clock.Advance(time.Second)
	}

	_, err := f.auth.SignIn(ctx, "stranger@example.com", "whatever")
	var first *usecase.SignInFailedError
	if !errors.As(err, &first) || !first.Locked {
		t.Fatalf("err = %v, want a locked *SignInFailedError", err)
	}

	f.clock.Advance(time.Minute)

	_, err = f.auth.SignIn(ctx, "stranger@example.com", "whatever")
	var second *usecase.SignInFailedError
	if !errors.As(err, &second) || !second.Locked {
		t.Fatalf("err = %v, want a locked *SignInFailedError", err)
	}

	if !second.LockedUntil.After(first.LockedUntil) {
		t.Fatalf("LockedUntil did not advance: first = %v, second = %v",
			first.LockedUntil, second.LockedUntil)
	}
}

var _ = domain.DefaultLockoutPolicy

func TestRequestMagicLinkSendsAnEmail(t *testing.T) {
	f := newFixture(t)

	if err := f.auth.RequestMagicLink(context.Background(), "andreas@hearth.family"); err != nil {
		t.Fatalf("RequestMagicLink: %v", err)
	}
	if len(f.mailer.magicLinks) != 1 {
		t.Fatalf("sent = %d, want 1", len(f.mailer.magicLinks))
	}
	if !strings.HasPrefix(f.mailer.magicLinks[0].URL, "http://localhost:5173/sign-in/magic?token=") {
		t.Fatalf("url = %q", f.mailer.magicLinks[0].URL)
	}
}

func TestRequestMagicLinkStaysSilentForAnUnknownAddress(t *testing.T) {
	f := newFixture(t)

	if err := f.auth.RequestMagicLink(context.Background(), "stranger@example.com"); err != nil {
		t.Fatalf("an unknown address must not produce an error: %v", err)
	}
	if len(f.mailer.magicLinks) != 0 {
		t.Fatal("no email should have been sent")
	}
}

func TestMagicLinkIsRateLimitedSilentlyToThreePerHour(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	for i := 0; i < 3; i++ {
		if err := f.auth.RequestMagicLink(ctx, "andreas@hearth.family"); err != nil {
			t.Fatalf("request %d: %v", i, err)
		}
		f.clock.Advance(time.Minute)
	}

	// The fourth request must look exactly like the first three from the
	// outside. Returning an error here would make four requests an oracle for
	// whether the address belongs to a member, which is the property this
	// endpoint exists to avoid.
	if err := f.auth.RequestMagicLink(ctx, "andreas@hearth.family"); err != nil {
		t.Fatalf("the rate limit must be silent, got err = %v", err)
	}
	if len(f.mailer.magicLinks) != 3 {
		t.Fatalf("sent = %d, want 3 — the fourth request sends nothing", len(f.mailer.magicLinks))
	}
}

func TestMagicLinkRequestsAreIndistinguishableAtEveryCount(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	for i := 0; i < 5; i++ {
		knownErr := f.auth.RequestMagicLink(ctx, "andreas@hearth.family")
		unknownErr := f.auth.RequestMagicLink(ctx, "stranger@example.com")
		if knownErr != nil || unknownErr != nil {
			t.Fatalf("request %d: known = %v, unknown = %v — both must be nil", i, knownErr, unknownErr)
		}
		f.clock.Advance(time.Minute)
	}
}

func TestConsumingAMagicLinkSignsIn(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	if err := f.auth.RequestMagicLink(ctx, "andreas@hearth.family"); err != nil {
		t.Fatalf("RequestMagicLink: %v", err)
	}
	token := f.mailer.lastMagicToken(t)

	result, err := f.auth.ConsumeMagicLink(ctx, token)
	if err != nil {
		t.Fatalf("ConsumeMagicLink: %v", err)
	}
	if result.SessionToken == "" {
		t.Fatal("expected a session")
	}
}

func TestAMagicLinkCannotBeUsedTwice(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	f.auth.RequestMagicLink(ctx, "andreas@hearth.family")
	token := f.mailer.lastMagicToken(t)
	f.auth.ConsumeMagicLink(ctx, token)

	if _, err := f.auth.ConsumeMagicLink(ctx, token); !errors.Is(err, domain.ErrTokenExpired) {
		t.Fatalf("err = %v, want ErrTokenExpired", err)
	}
}

func TestAMagicLinkExpiresAfterFifteenMinutes(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	f.auth.RequestMagicLink(ctx, "andreas@hearth.family")
	token := f.mailer.lastMagicToken(t)
	f.clock.Advance(16 * time.Minute)

	if _, err := f.auth.ConsumeMagicLink(ctx, token); !errors.Is(err, domain.ErrTokenExpired) {
		t.Fatalf("err = %v, want ErrTokenExpired", err)
	}
}

func TestAMagicLinkWorksWhileTheHouseholdIsLocked(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	for i := 0; i < 3; i++ {
		f.auth.SignIn(ctx, "andreas@hearth.family", "wrong")
		f.clock.Advance(time.Second)
	}
	if _, err := f.auth.SignIn(ctx, "andreas@hearth.family", "hunter2"); err == nil {
		t.Fatal("expected the household to be locked")
	}

	if err := f.auth.RequestMagicLink(ctx, "andreas@hearth.family"); err != nil {
		t.Fatalf("magic link must remain available while locked: %v", err)
	}
	if _, err := f.auth.ConsumeMagicLink(ctx, f.mailer.lastMagicToken(t)); err != nil {
		t.Fatalf("consuming a magic link while locked must succeed: %v", err)
	}
}
