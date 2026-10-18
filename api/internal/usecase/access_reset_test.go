package usecase_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/andreasoentoro/hearth/api/internal/domain"
	"github.com/andreasoentoro/hearth/api/internal/usecase"
)

type resetFixture struct {
	svc      *usecase.AccessResetService
	users    *userDouble
	sessions *sessionDouble
	access   *accessRevokerDouble
	attempts *fakeReauthAttemptRepo
	userID   string
}

func newResetFixture(t *testing.T, hasher usecase.PasswordHasher) resetFixture {
	t.Helper()
	clock := &fixedClock{now: time.Date(2026, 10, 18, 9, 0, 0, 0, time.UTC)}
	users := newUserDouble()
	u := users.mustCreate(t, "andreas@hearth.family", "hashed:old-password-123", "Andreas")
	sessions := newSessionDouble(clock)
	access := newAccessRevokerDouble(sessions, newAPITokenDouble())
	attempts := newFakeReauthAttemptRepo()
	reauth := usecase.NewAdminReauthService(usecase.AdminReauthDeps{
		Users: users, Attempts: attempts, Hasher: hasher, Clock: clock, Policy: domain.DefaultLockoutPolicy(),
	})
	svc := usecase.NewAccessResetService(usecase.AccessResetDeps{
		Users: users, Reauth: reauth, Hasher: hasher, Access: access,
		Sessions: sessions, Tokens: &seqTokens{}, Clock: clock, SessionTTL: 30 * 24 * time.Hour,
	})
	return resetFixture{svc: svc, users: users, sessions: sessions, access: access, attempts: attempts, userID: u.ID}
}

func (f resetFixture) storedHash(t *testing.T) string {
	t.Helper()
	u, err := f.users.ByID(context.Background(), f.userID)
	if err != nil {
		t.Fatalf("ByID: %v", err)
	}
	return u.PasswordHash
}

func TestChangePasswordSavesTheNewPasswordCutsOffEverythingAndIssuesAFreshSession(t *testing.T) {
	f := newResetFixture(t, &fakeHasher{})
	if err := f.sessions.Create(context.Background(), []byte("old-session"), f.userID, "household-1",
		time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)); err != nil {
		t.Fatal(err)
	}

	got, err := f.svc.ChangePassword(context.Background(), f.userID, "household-1", "old-password-123", "new-password-456")
	if err != nil {
		t.Fatalf("ChangePassword: %v", err)
	}
	if f.storedHash(t) != "hashed:new-password-456" {
		t.Fatalf("stored hash = %q, want the new password's", f.storedHash(t))
	}
	if len(f.access.calls) != 1 || f.access.calls[0].Chat != usecase.RevokeChat {
		t.Fatalf("revoke calls = %+v, want one RevokeChat", f.access.calls)
	}
	if got.SessionToken == "" || got.UserID != f.userID || got.HouseholdID != "household-1" {
		t.Fatalf("result = %+v, want a fresh session for this person and household", got)
	}
	if _, err := f.sessions.ByTokenHash(context.Background(), []byte("old-session")); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("the old session still works: %v", err)
	}
}

// orderRecorder notes the order the password is saved and access revoked: the
// save must come first, so a session minted with the old password before the
// save is caught by the revoke after it.
type orderRecorder struct {
	*userDouble
	log *[]string
}

func (o orderRecorder) SetPasswordHash(ctx context.Context, userID, hash string) error {
	*o.log = append(*o.log, "save")
	return o.userDouble.SetPasswordHash(ctx, userID, hash)
}

type orderedRevoker struct {
	*accessRevokerDouble
	log *[]string
}

func (o orderedRevoker) RevokeAccess(ctx context.Context, userID string, chat usecase.ChatRule) error {
	*o.log = append(*o.log, "revoke")
	return o.accessRevokerDouble.RevokeAccess(ctx, userID, chat)
}

func TestChangePasswordSavesBeforeItRevokes(t *testing.T) {
	f := newResetFixture(t, &fakeHasher{})
	var log []string
	clock := &fixedClock{now: time.Now()}
	users := orderRecorder{userDouble: f.users, log: &log}
	svc := usecase.NewAccessResetService(usecase.AccessResetDeps{
		Users: users,
		Reauth: usecase.NewAdminReauthService(usecase.AdminReauthDeps{
			Users: f.users, Attempts: newFakeReauthAttemptRepo(), Hasher: &fakeHasher{}, Clock: clock,
		}),
		Hasher: &fakeHasher{}, Access: orderedRevoker{accessRevokerDouble: f.access, log: &log},
		Sessions: f.sessions, Tokens: &seqTokens{}, Clock: clock, SessionTTL: time.Hour,
	})
	if _, err := svc.ChangePassword(context.Background(), f.userID, "household-1", "old-password-123", "new-password-456"); err != nil {
		t.Fatalf("ChangePassword: %v", err)
	}
	if len(log) != 2 || log[0] != "save" || log[1] != "revoke" {
		t.Fatalf("order = %v, want [save revoke]", log)
	}
}

func TestChangePasswordRefusesAShortNewPasswordBeforeCheckingAnything(t *testing.T) {
	f := newResetFixture(t, &fakeHasher{})
	_, err := f.svc.ChangePassword(context.Background(), f.userID, "household-1", "wrong", "short")
	if !errors.Is(err, usecase.ErrPasswordTooShort) {
		t.Fatalf("err = %v, want ErrPasswordTooShort", err)
	}
	if got := f.attempts.failureCount(f.userID); got != 0 {
		t.Fatalf("a refused new password counted %d guesses, want 0", got)
	}
}

func TestChangePasswordWithTheWrongCurrentPasswordChangesNothing(t *testing.T) {
	f := newResetFixture(t, &fakeHasher{})
	_, err := f.svc.ChangePassword(context.Background(), f.userID, "household-1", "not-my-password", "new-password-456")
	if !errors.Is(err, usecase.ErrCurrentPasswordWrong) {
		t.Fatalf("err = %v, want ErrCurrentPasswordWrong", err)
	}
	if f.storedHash(t) != "hashed:old-password-123" || len(f.access.calls) != 0 {
		t.Fatalf("a wrong current password changed something: hash %q, revokes %+v", f.storedHash(t), f.access.calls)
	}
}

func TestChangePasswordLocksAfterThreeWrongGuessesEvenForTheRightOne(t *testing.T) {
	f := newResetFixture(t, &fakeHasher{})
	for i := 0; i < 3; i++ {
		_, _ = f.svc.ChangePassword(context.Background(), f.userID, "household-1", "guess", "new-password-456")
	}
	_, err := f.svc.ChangePassword(context.Background(), f.userID, "household-1", "old-password-123", "new-password-456")
	if !errors.Is(err, usecase.ErrPasswordChecksLocked) {
		t.Fatalf("err = %v, want ErrPasswordChecksLocked", err)
	}
	if f.storedHash(t) != "hashed:old-password-123" {
		t.Fatal("the password changed while locked")
	}
}

func TestChangePasswordOnABusyHasherChangesNothingAndCountsNothing(t *testing.T) {
	f := newResetFixture(t, busyHasher{})
	_, err := f.svc.ChangePassword(context.Background(), f.userID, "household-1", "old-password-123", "new-password-456")
	if !errors.Is(err, usecase.ErrHasherBusy) {
		t.Fatalf("err = %v, want ErrHasherBusy", err)
	}
	if got := f.attempts.failureCount(f.userID); got != 0 {
		t.Fatalf("a busy hasher counted %d guesses, want 0", got)
	}
	if len(f.access.calls) != 0 {
		t.Fatal("a busy hasher revoked access")
	}
}

func TestChangePasswordWhoseRevokeFailsSaysSoAndIssuesNoSession(t *testing.T) {
	f := newResetFixture(t, &fakeHasher{})
	f.access.failNext(errors.New("simulated transaction failure"))
	got, err := f.svc.ChangePassword(context.Background(), f.userID, "household-1", "old-password-123", "new-password-456")
	if !errors.Is(err, usecase.ErrPasswordChangedAccessKept) {
		t.Fatalf("err = %v, want ErrPasswordChangedAccessKept", err)
	}
	if got.SessionToken != "" {
		t.Fatal("a session was issued although nothing was revoked")
	}
}

func TestChangePasswordKeepsTheChatOfAnAccountWithNoEmail(t *testing.T) {
	f := newResetFixture(t, &fakeHasher{})
	f.users.put(usecase.StoredUser{User: domain.User{ID: "user-tg"}, PasswordHash: "hashed:old-password-123"})
	if _, err := f.svc.ChangePassword(context.Background(), "user-tg", "household-1", "old-password-123", "new-password-456"); err != nil {
		t.Fatalf("ChangePassword: %v", err)
	}
	if len(f.access.calls) != 1 || f.access.calls[0].Chat != usecase.KeepChat {
		t.Fatalf("revoke calls = %+v, want one KeepChat", f.access.calls)
	}
}

func TestSignOutEverywhereCutsOffEverythingIncludingTheChat(t *testing.T) {
	f := newResetFixture(t, &fakeHasher{})
	if err := f.svc.SignOutEverywhere(context.Background(), f.userID); err != nil {
		t.Fatalf("SignOutEverywhere: %v", err)
	}
	if len(f.access.calls) != 1 || f.access.calls[0] != (revokeCall{UserID: f.userID, Chat: usecase.RevokeChat}) {
		t.Fatalf("revoke calls = %+v, want one RevokeChat", f.access.calls)
	}
}

func TestSignOutEverywhereKeepsTheChatOfAnAccountWithNoEmail(t *testing.T) {
	f := newResetFixture(t, &fakeHasher{})
	f.users.addTelegramOnly("user-tg")
	if err := f.svc.SignOutEverywhere(context.Background(), "user-tg"); err != nil {
		t.Fatalf("SignOutEverywhere: %v", err)
	}
	if len(f.access.calls) != 1 || f.access.calls[0].Chat != usecase.KeepChat {
		t.Fatalf("revoke calls = %+v, want one KeepChat", f.access.calls)
	}
}

func TestSignOutEverywhereThatFailsSaysNothingWasSignedOut(t *testing.T) {
	f := newResetFixture(t, &fakeHasher{})
	f.access.failNext(errors.New("simulated transaction failure"))
	if err := f.svc.SignOutEverywhere(context.Background(), f.userID); !errors.Is(err, usecase.ErrNothingRevoked) {
		t.Fatalf("err = %v, want ErrNothingRevoked", err)
	}
}

// cancelOnSave cancels the request the moment the new password is saved, as
// a closed tab or a dropped connection would.
type cancelOnSave struct {
	*userDouble
	cancel context.CancelFunc
}

func (c cancelOnSave) SetPasswordHash(ctx context.Context, userID, hash string) error {
	err := c.userDouble.SetPasswordHash(ctx, userID, hash)
	c.cancel()
	return err
}

// ctxCheckingRevoker fails the way a real transaction does when its context
// is already cancelled.
type ctxCheckingRevoker struct{ *accessRevokerDouble }

func (r ctxCheckingRevoker) RevokeAccess(ctx context.Context, userID string, chat usecase.ChatRule) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return r.accessRevokerDouble.RevokeAccess(ctx, userID, chat)
}

// TestChangePasswordStillRevokesWhenTheCallerLeavesAfterTheSave: once the new
// password is saved, the revoke must run even if the caller has gone. A saved
// password with nothing revoked is the one state nobody would be told about.
func TestChangePasswordStillRevokesWhenTheCallerLeavesAfterTheSave(t *testing.T) {
	f := newResetFixture(t, &fakeHasher{})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	clock := &fixedClock{now: time.Now()}
	svc := usecase.NewAccessResetService(usecase.AccessResetDeps{
		Users: cancelOnSave{userDouble: f.users, cancel: cancel},
		Reauth: usecase.NewAdminReauthService(usecase.AdminReauthDeps{
			Users: f.users, Attempts: newFakeReauthAttemptRepo(), Hasher: &fakeHasher{}, Clock: clock,
		}),
		Hasher: &fakeHasher{}, Access: ctxCheckingRevoker{f.access},
		Sessions: f.sessions, Tokens: &seqTokens{}, Clock: clock, SessionTTL: time.Hour,
	})

	_, _ = svc.ChangePassword(ctx, f.userID, "household-1", "old-password-123", "new-password-456")

	if f.storedHash(t) != "hashed:new-password-456" {
		t.Fatalf("stored hash = %q, want the new password saved", f.storedHash(t))
	}
	if len(f.access.calls) != 1 {
		t.Fatalf("revoke calls = %+v, want the cut-off to run although the caller left", f.access.calls)
	}
}
