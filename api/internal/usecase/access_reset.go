package usecase

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/andreasoentoro/hearth/api/internal/domain"
)

// The four ways a self-service reset can end other than success. They live
// here, not in domain, because domain has no notion of a password.
var (
	// ErrCurrentPasswordWrong must never be answered with 401: the frontend
	// signs the browser out on any 401, so one typo would sign the person out.
	ErrCurrentPasswordWrong = errors.New("the current password is wrong")
	// ErrPasswordChecksLocked follows three wrong current passwords; even the
	// right one is refused until the window passes.
	ErrPasswordChecksLocked = errors.New("too many wrong current passwords")
	// ErrPasswordChangedAccessKept: the new password is saved, nothing was
	// revoked, and the person is still signed in on the old session.
	ErrPasswordChangedAccessKept = errors.New("the password was changed but revoking access failed")
	// ErrNothingRevoked: the cut-off rolled back, so every session, token and
	// the chat are exactly as they were.
	ErrNothingRevoked = errors.New("revoking access failed; nothing was revoked")
)

// saveAndRevokeTimeout bounds ChangePassword's save and revoke once they no
// longer follow the caller's context: two short writes, given room to finish.
const saveAndRevokeTimeout = 10 * time.Second

type AccessResetDeps struct {
	Users UserRepository
	// Reauth counts wrong current passwords per person. It is the admin
	// re-auth counter, not the sign-in lockout: that lock is household-wide,
	// so a typo here would lock the other member out of signing in.
	Reauth     *AdminReauthService
	Hasher     PasswordHasher
	Access     AccessRevoker
	Sessions   SessionRepository
	Tokens     TokenGenerator
	Clock      Clock
	SessionTTL time.Duration
}

// AccessResetService is what a signed-in person uses to take their own
// account back: change the password, or sign out everywhere. Both end in
// AccessRevoker's one transaction, so either everything is cut off or
// nothing is.
type AccessResetService struct{ d AccessResetDeps }

func NewAccessResetService(d AccessResetDeps) *AccessResetService {
	return &AccessResetService{d: d}
}

// ChangePassword checks current, saves next, cuts every way in off -- the
// session making this request too -- and answers a fresh session for this
// browser. Fresh rather than kept: a stolen cookie is a copy of this
// session, so keeping it would keep the thief in.
//
// The new password is validated before anything else, so a refused one
// costs no guess and no hash. The save comes before the revoke, so a session
// minted with the old password in between is caught.
func (s *AccessResetService) ChangePassword(ctx context.Context, userID, householdID, current, next string) (SignInResult, error) {
	if err := validatePassword(next); err != nil {
		return SignInResult{}, err
	}
	switch err := s.d.Reauth.Verify(ctx, userID, current); {
	case errors.Is(err, domain.ErrInvalidCredentials):
		return SignInResult{}, ErrCurrentPasswordWrong
	case errors.Is(err, domain.ErrAdminLocked):
		return SignInResult{}, ErrPasswordChecksLocked
	case err != nil:
		return SignInResult{}, err
	}
	user, err := s.d.Users.ByID(ctx, userID)
	if err != nil {
		return SignInResult{}, err
	}
	hash, err := s.d.Hasher.Hash(ctx, next)
	if err != nil {
		return SignInResult{}, err
	}
	// The save and the revoke run to the end even if the caller leaves: a
	// saved password with nothing revoked is the one state nobody would be
	// told about.
	both, cancel := context.WithTimeout(context.WithoutCancel(ctx), saveAndRevokeTimeout)
	defer cancel()
	if err := s.d.Users.SetPasswordHash(both, userID, hash); err != nil {
		return SignInResult{}, err
	}
	if err := s.d.Access.RevokeAccess(both, userID, chatRuleFor(user)); err != nil {
		return SignInResult{}, fmt.Errorf("%w: %v", ErrPasswordChangedAccessKept, err)
	}
	return issueSession(ctx, s.d.Sessions, s.d.Tokens, s.d.SessionTTL, userID, householdID, s.d.Clock.Now())
}

// SignOutEverywhere cuts every way in off, this session included. It asks
// for no password: the person who most needs it may have forgotten theirs,
// or locked the password check with wrong guesses.
func (s *AccessResetService) SignOutEverywhere(ctx context.Context, userID string) error {
	user, err := s.d.Users.ByID(ctx, userID)
	if err != nil {
		return err
	}
	if err := s.d.Access.RevokeAccess(ctx, userID, chatRuleFor(user)); err != nil {
		return fmt.Errorf("%w: %v", ErrNothingRevoked, err)
	}
	return nil
}

// chatRuleFor keeps the chat of an account with no email address: the chat
// is that account's only way to sign in. Every other account loses it,
// because a chat can ask the bot for a sign-in link.
func chatRuleFor(user StoredUser) ChatRule {
	if user.Email == "" {
		return KeepChat
	}
	return RevokeChat
}
