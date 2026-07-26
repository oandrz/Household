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

// TestSignInRejectsAPasswordOverTheLengthCeilingWithoutHashing proves the
// fix for the uncapped-password DoS: argon2id's cost scales with the size
// of the string it hashes, so a caller who submits a multi-megabyte
// password must never reach the hasher at all, decoy or real. The
// resulting failure must still look exactly like a wrong password (see
// TestSignInWithATooLongPasswordFailsIdenticallyToAWrongPassword below for
// the side-by-side comparison); this test's extra job is confirming no
// Verify call happened.
//
// It checks this for both a known address (Andreas) and an unknown one
// (stranger@example.com), not just the former. The unknown-address branch's
// entire reason to call Verify at all is timing parity with the known
// branches (see the decoy doc comment in auth.go) — if the length guard
// were ever narrowed to cover only the real-verify path and not every decoy
// call site, the known-address half of this test would still pass while
// unknown-vs-known timing quietly diverged again for oversized passwords.
// Asserting both branches skip the hasher is what actually pins that down.
func TestSignInRejectsAPasswordOverTheLengthCeilingWithoutHashing(t *testing.T) {
	f := newFixture(t)
	tooLong := strings.Repeat("a", 257)

	beforeKnown := f.hasher.verifyCallCount()
	_, err := f.auth.SignIn(context.Background(), "andreas@hearth.family", tooLong)

	var failure *usecase.SignInFailedError
	if !errors.As(err, &failure) {
		t.Fatalf("err = %v, want *SignInFailedError", err)
	}
	if failure.AttemptsRemaining != 2 {
		t.Fatalf("AttemptsRemaining = %d, want 2 — must look exactly like a wrong password", failure.AttemptsRemaining)
	}
	if failure.Locked {
		t.Fatal("one over-length attempt must not lock")
	}
	if got := f.hasher.verifyCallCount(); got != beforeKnown {
		t.Fatalf("hasher Verify calls after a known-address attempt = %d, want %d unchanged — "+
			"an over-length password must never reach the hasher, decoy or real", got, beforeKnown)
	}

	beforeUnknown := f.hasher.verifyCallCount()
	if _, err := f.auth.SignIn(context.Background(), "stranger@example.com", tooLong); err == nil {
		t.Fatal("expected a *SignInFailedError for an unknown address")
	}
	if got := f.hasher.verifyCallCount(); got != beforeUnknown {
		t.Fatalf("hasher Verify calls after an unknown-address attempt = %d, want %d unchanged — "+
			"the decoy call that exists purely for timing parity must also be skipped for an over-length password", got, beforeUnknown)
	}
}

// TestSignInWithATooLongPasswordFailsIdenticallyToAWrongPassword is the
// indistinguishability check the coordinator asked for: a too-long password
// against a known user must produce the identical SignInFailedError shape a
// wrong password of ordinary length would, using two independent fixtures
// so neither call's recorded attempt affects the other's countdown.
func TestSignInWithATooLongPasswordFailsIdenticallyToAWrongPassword(t *testing.T) {
	fTooLong := newFixture(t)
	fWrong := newFixture(t)

	tooLong := strings.Repeat("a", 257)
	_, tooLongErr := fTooLong.auth.SignIn(context.Background(), "andreas@hearth.family", tooLong)
	_, wrongErr := fWrong.auth.SignIn(context.Background(), "andreas@hearth.family", "wrong")

	var a, b *usecase.SignInFailedError
	if !errors.As(tooLongErr, &a) || !errors.As(wrongErr, &b) {
		t.Fatalf("both must be *SignInFailedError: %v / %v", tooLongErr, wrongErr)
	}
	if a.Locked != b.Locked {
		t.Fatal("the two failures must be indistinguishable to a caller")
	}
	if a.AttemptsRemaining != b.AttemptsRemaining {
		t.Fatalf("AttemptsRemaining differs: too-long = %d, wrong password = %d — "+
			"a too-long password must not be a distinguishable failure mode", a.AttemptsRemaining, b.AttemptsRemaining)
	}
}

// TestSignInAcceptsAPasswordAtTheLengthCeiling proves the ceiling is
// inclusive: exactly 256 characters is still a legitimate password, not one
// character over the line into rejection.
func TestSignInAcceptsAPasswordAtTheLengthCeiling(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	atLimit := strings.Repeat("b", 256)
	f.users.put(usecase.StoredUser{
		User:         domain.User{ID: "user-atlimit", Email: "atlimit@hearth.family", DisplayName: "At Limit"},
		PasswordHash: mustHash(t, f.hasher, atLimit),
	})
	f.members.put(domain.Membership{
		ID: "membership-atlimit", HouseholdID: f.householdID, UserID: "user-atlimit",
		Role: domain.RoleLimited, Capabilities: domain.Capabilities{domain.CapChores},
	})

	result, err := f.auth.SignIn(ctx, "atlimit@hearth.family", atLimit)
	if err != nil {
		t.Fatalf("SignIn: %v", err)
	}
	if result.SessionToken == "" {
		t.Fatal("expected a session token")
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

// TestSignInForARemovedMemberFailsIdenticallyToAnUnknownAddress pins the fix
// for the 404 an ex-member used to get at sign-in: removing a member deletes
// only its memberships row, not the users row underneath it (see
// MemberService.Remove and the fix report), so Members.ByUser(ctx, user.ID)
// returns domain.ErrNotFound for a real, still-existing user. SignIn used to
// propagate that bare, and MapDomainError turned it into 404 -- a status no
// other sign-in failure ever produces, and one a stranger's guess never
// gets, which is itself a tell that the address once belonged to someone.
// This must fail exactly like a stranger's guess instead: the same
// *SignInFailedError shape, the same countdown.
func TestSignInForARemovedMemberFailsIdenticallyToAnUnknownAddress(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	if err := f.members.Delete(ctx, f.householdID, "membership-andreas"); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	_, removedErr := f.auth.SignIn(ctx, "andreas@hearth.family", "hunter2")
	_, unknownErr := f.auth.SignIn(ctx, "stranger@example.com", "whatever")

	var a, b *usecase.SignInFailedError
	if !errors.As(removedErr, &a) {
		t.Fatalf("removed member: err = %v, want *SignInFailedError (not a bare domain.ErrNotFound / 404)", removedErr)
	}
	if !errors.As(unknownErr, &b) {
		t.Fatalf("unknown address: err = %v, want *SignInFailedError", unknownErr)
	}
	if a.Locked != b.Locked || a.AttemptsRemaining != b.AttemptsRemaining {
		t.Fatalf("a removed member's failure must be indistinguishable from an unknown address's: %+v vs %+v", a, b)
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
	// The send happens off the request path (see sendMagicLinkAsync), so the
	// double's state isn't settled the instant RequestMagicLink returns —
	// wait for it before reading magicLinks, or this races the background
	// goroutine as well as flaking under load.
	f.mailer.waitForSend(t)
	if got := f.mailer.sentCount(); got != 1 {
		t.Fatalf("sent = %d, want 1", got)
	}
	if url := f.mailer.magicLinkURLAt(0); !strings.HasPrefix(url, "http://localhost:5173/sign-in/magic?token=") {
		t.Fatalf("url = %q", url)
	}
}

func TestRequestMagicLinkStaysSilentForAnUnknownAddress(t *testing.T) {
	f := newFixture(t)

	if err := f.auth.RequestMagicLink(context.Background(), "stranger@example.com"); err != nil {
		t.Fatalf("an unknown address must not produce an error: %v", err)
	}
	// An unknown address never reaches sendMagicLinkAsync, so no background
	// goroutine is spawned and there is nothing to wait for here.
	if got := f.mailer.sentCount(); got != 0 {
		t.Fatalf("sent = %d, want 0 — no email should have been sent", got)
	}
}

// TestRequestMagicLinkStaysSilentForARemovedMember pins the fix for the
// unusable link an ex-member used to be mailed: removing a member deletes
// only the memberships row, not the users row underneath it (see
// MemberService.Remove and the fix report), so RequestMagicLink's
// known-address branch used to run for them too -- minting a token,
// persisting it, and mailing a link that ConsumeMagicLink could never turn
// into a session (it calls Members.ByUser itself and would fail
// identically). This must be treated exactly like an unknown address: no
// email sent, no error.
func TestRequestMagicLinkStaysSilentForARemovedMember(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	if err := f.members.Delete(ctx, f.householdID, "membership-andreas"); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	if err := f.auth.RequestMagicLink(ctx, "andreas@hearth.family"); err != nil {
		t.Fatalf("a removed member's address must not produce an error: %v", err)
	}
	if got := f.mailer.sentCount(); got != 0 {
		t.Fatalf("sent = %d, want 0 — no email should have been sent to a removed member", got)
	}
}

func TestMagicLinkIsRateLimitedSilentlyToThreePerHour(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	for i := 0; i < 3; i++ {
		if err := f.auth.RequestMagicLink(ctx, "andreas@hearth.family"); err != nil {
			t.Fatalf("request %d: %v", i, err)
		}
		f.mailer.waitForSend(t)
		f.clock.Advance(time.Minute)
	}

	// The fourth request must look exactly like the first three from the
	// outside. Returning an error here would make four requests an oracle for
	// whether the address belongs to a member, which is the property this
	// endpoint exists to avoid.
	if err := f.auth.RequestMagicLink(ctx, "andreas@hearth.family"); err != nil {
		t.Fatalf("the rate limit must be silent, got err = %v", err)
	}
	// The fourth request is rate-limited, so it never spawns a send
	// goroutine — nothing more to wait for before reading the count.
	if got := f.mailer.sentCount(); got != 3 {
		t.Fatalf("sent = %d, want 3 — the fourth request sends nothing", got)
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

// TestRequestMagicLinkSwallowsAMailerFailure pins the fix for the oracle a
// propagated mailer error would create: RequestMagicLink's contract is
// "always nil," and a relay outage must not carve out an exception for
// known addresses only, since unknown and rate-limited addresses can never
// fail this way. A caller who can make the relay misbehave (or who simply
// catches it misbehaving) must not learn anything from the response.
func TestRequestMagicLinkSwallowsAMailerFailure(t *testing.T) {
	f := newFixture(t)
	f.mailer.failNextMagicLink(errors.New("smtp: connection refused"))

	knownErr := f.auth.RequestMagicLink(context.Background(), "andreas@hearth.family")
	f.mailer.waitForSend(t) // the failing send still happens; wait for it to land.

	unknownErr := f.auth.RequestMagicLink(context.Background(), "stranger@example.com")

	if knownErr != nil {
		t.Fatalf("a mailer failure must not surface to the caller: %v", knownErr)
	}
	if knownErr != unknownErr {
		t.Fatalf("known = %v, unknown = %v — a mailer failure must be indistinguishable "+
			"from an unknown address", knownErr, unknownErr)
	}
	if got := f.mailer.sentCount(); got != 0 {
		t.Fatalf("sent = %d, want 0 — the send failed and must not be recorded as delivered", got)
	}
	if got := f.magicLinks.count(); got != 1 {
		t.Fatalf("magic link rows = %d, want 1 — the token must persist even though "+
			"delivery failed; a retry within the rate limit can still use it or ask again", got)
	}
}

// TestRequestMagicLinkSwallowsAPersistenceFailure is
// TestRequestMagicLinkSwallowsAMailerFailure's sibling for the other two
// steps that are reachable only from the known-address branch: token
// generation and MagicLinks.Create. An INSERT that fails on a statement
// timeout or a connection blip is exactly as reachable-only-by-a-member as
// a mailer failure is, so it must be swallowed identically.
func TestRequestMagicLinkSwallowsAPersistenceFailure(t *testing.T) {
	f := newFixture(t)
	f.magicLinks.failNextMagicLinkCreate(errors.New("pq: statement timeout"))

	knownErr := f.auth.RequestMagicLink(context.Background(), "andreas@hearth.family")
	unknownErr := f.auth.RequestMagicLink(context.Background(), "stranger@example.com")

	if knownErr != nil {
		t.Fatalf("a persistence failure must not surface to the caller: %v", knownErr)
	}
	if knownErr != unknownErr {
		t.Fatalf("known = %v, unknown = %v — a persistence failure must be indistinguishable "+
			"from an unknown address", knownErr, unknownErr)
	}
	// Create itself failed, so no row and nothing to send.
	if got := f.magicLinks.count(); got != 0 {
		t.Fatalf("magic link rows = %d, want 0 — Create failed and nothing should have persisted", got)
	}
	if got := f.mailer.sentCount(); got != 0 {
		t.Fatalf("sent = %d, want 0 — there is no token to send a link for", got)
	}
}

// TestSendMagicLinkAsyncRecoversFromAPanicInTheSend pins the fix for the
// second finding: chi's middleware.Recoverer guards only the request
// goroutine, and sendMagicLinkAsync's send runs on a goroutine of its own
// after the request has already returned. Without a recover() there, this
// test's panicking mailer would crash the entire test binary right here,
// rather than merely failing an assertion — that is the whole point of the
// fix, and it's why this test's strongest assertion is simply "execution
// reached this line at all."
func TestSendMagicLinkAsyncRecoversFromAPanicInTheSend(t *testing.T) {
	f := newFixture(t)
	f.mailer.panicNextMagicLink("simulated panic in the mail client")

	if err := f.auth.RequestMagicLink(context.Background(), "andreas@hearth.family"); err != nil {
		t.Fatalf("RequestMagicLink: %v", err)
	}
	// If the goroutine's recover() didn't catch the panic, the process
	// would already be dead before this line runs.
	f.mailer.waitForSend(t)

	// A recovered panic must not wedge the double or the service: the next,
	// healthy request should still succeed and still send.
	if err := f.auth.RequestMagicLink(context.Background(), "andreas@hearth.family"); err != nil {
		t.Fatalf("RequestMagicLink after a recovered panic: %v", err)
	}
	f.mailer.waitForSend(t)
	if got := f.mailer.sentCount(); got != 1 {
		t.Fatalf("sent = %d, want 1 — the panicking send must not count, "+
			"but the following healthy one must", got)
	}
}

// TestRequestMagicLinkPerformsTheSameReadsForEveryOutcome pins the other
// half of the fix: a known address under the limit, an unknown address, and
// a known address that has exhausted the limit must all perform the exact
// same number of repository reads, in the same order. Before this fix, a
// rate-limited request returned before ever calling Users.ByEmail, which
// made the read count itself distinguish "rate limited" from the other two
// cases — and since CountRecentMagicLinks joins through users, an unknown
// address can never reach the rate-limited branch in the first place, so
// that asymmetry was really a membership oracle wearing a read-count
// disguise.
func TestRequestMagicLinkPerformsTheSameReadsForEveryOutcome(t *testing.T) {
	countReads := func(t *testing.T, do func(f *fixture)) (byEmail, countSince int) {
		t.Helper()
		f := newFixture(t)
		do(f)
		return f.users.byEmailCalls, f.magicLinks.countSinceCalls
	}

	knownByEmail, knownCountSince := countReads(t, func(f *fixture) {
		if err := f.auth.RequestMagicLink(context.Background(), "andreas@hearth.family"); err != nil {
			t.Fatalf("RequestMagicLink: %v", err)
		}
		f.mailer.waitForSend(t)
	})

	unknownByEmail, unknownCountSince := countReads(t, func(f *fixture) {
		if err := f.auth.RequestMagicLink(context.Background(), "stranger@example.com"); err != nil {
			t.Fatalf("RequestMagicLink: %v", err)
		}
	})

	rateLimitedByEmail, rateLimitedCountSince := countReads(t, func(f *fixture) {
		ctx := context.Background()
		for i := 0; i < magicLinkRateLimitForTest; i++ {
			if err := f.auth.RequestMagicLink(ctx, "andreas@hearth.family"); err != nil {
				t.Fatalf("priming request %d: %v", i, err)
			}
			f.mailer.waitForSend(t)
			f.clock.Advance(time.Minute)
		}
		primed := f.mailer.sentCount()
		// Reset the counters right before the request under test: we only
		// care about the fourth (rate-limited) request's own reads, not the
		// three that filled the bucket.
		f.users.byEmailCalls = 0
		f.magicLinks.countSinceCalls = 0
		if err := f.auth.RequestMagicLink(ctx, "andreas@hearth.family"); err != nil {
			t.Fatalf("RequestMagicLink: %v", err)
		}
		// Self-check: if magicLinkPerHourLimit ever changes in auth.go
		// without this test's local copy following it, the priming loop
		// above would stop actually exhausting the limit, and this whole
		// test would silently compare "known, under the limit" against
		// itself twice instead of against the rate-limited case. Confirm
		// the fourth request really was rate-limited — no additional send —
		// before trusting the read counts below.
		if got := f.mailer.sentCount(); got != primed {
			t.Fatalf("sent = %d after the priming loop's %d, want no change — "+
				"the request under test was not actually rate-limited; "+
				"magicLinkRateLimitForTest may be out of sync with auth.go's magicLinkPerHourLimit",
				got, primed)
		}
	})

	if knownByEmail != unknownByEmail || knownByEmail != rateLimitedByEmail {
		t.Fatalf("Users.ByEmail calls differ: known = %d, unknown = %d, rate-limited = %d — "+
			"every outcome must perform the same reads", knownByEmail, unknownByEmail, rateLimitedByEmail)
	}
	if knownCountSince != unknownCountSince || knownCountSince != rateLimitedCountSince {
		t.Fatalf("MagicLinks.CountSince calls differ: known = %d, unknown = %d, rate-limited = %d",
			knownCountSince, unknownCountSince, rateLimitedCountSince)
	}
}

// magicLinkRateLimitForTest mirrors auth.go's unexported
// magicLinkPerHourLimit (3). It's redeclared here rather than exported from
// the service, since the architecture rule keeps this test file from adding
// any new exported surface to usecase just to read a constant.
const magicLinkRateLimitForTest = 3
