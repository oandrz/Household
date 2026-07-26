package usecase

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/andreasoentoro/hearth/api/internal/domain"
)

// SignInFailedError is the only failure a caller sees, whether the address was
// unknown, the password wrong, or the household locked. The fields drive the
// design's copy; they never reveal whether the address exists.
type SignInFailedError struct {
	AttemptsRemaining int
	Locked            bool
	LockedUntil       time.Time
}

func (e *SignInFailedError) Error() string { return "sign in failed" }
func (e *SignInFailedError) Unwrap() error {
	if e.Locked {
		return domain.ErrHouseholdLocked
	}
	return domain.ErrInvalidCredentials
}

type AuthDeps struct {
	Users      UserRepository
	Members    MembershipRepository
	Sessions   SessionRepository
	Attempts   LoginAttemptRepository
	MagicLinks MagicLinkRepository
	Mailer     Mailer
	Hasher     PasswordHasher
	Tokens     TokenGenerator
	Clock      Clock
	Policy     domain.LockoutPolicy
	SessionTTL time.Duration
	BaseURL    string
}

type AuthService struct{ d AuthDeps }

// NewAuthService fills in a zero-valued Policy. A LockoutPolicy{} never locks
// anyone out while reporting AttemptsRemaining as 0 — an inconsistent state
// that would silently disable the lockout while the UI showed "0 tries left".
// A struct literal that forgets the field is the obvious way to reach it.
func NewAuthService(d AuthDeps) *AuthService {
	if d.Policy.MaxAttempts == 0 {
		d.Policy = domain.DefaultLockoutPolicy()
	}
	return &AuthService{d: d}
}

type SignInResult struct {
	SessionToken string
	ExpiresAt    time.Time
	UserID       string
	HouseholdID  string
}

func (s *AuthService) SignIn(ctx context.Context, email, password string) (SignInResult, error) {
	now := s.d.Clock.Now()

	user, err := s.d.Users.ByEmail(ctx, email)
	if err != nil {
		if !errors.Is(err, domain.ErrNotFound) {
			return SignInResult{}, err
		}
		// Record the attempt with no household, so guessing at unknown addresses
		// cannot lock a real household. Then evaluate the same policy over that
		// address's own failures, so the countdown a stranger sees is
		// indistinguishable from the one a member sees.
		if err := s.d.Attempts.Record(ctx, nil, nil, email, false, now); err != nil {
			return SignInResult{}, err
		}
		failures, err := s.d.Attempts.FailuresSinceForEmail(ctx, email, now.Add(-s.d.Policy.Window))
		if err != nil {
			return SignInResult{}, err
		}
		state := s.d.Policy.Evaluate(failures, now)
		return SignInResult{}, &SignInFailedError{
			AttemptsRemaining: state.AttemptsRemaining,
			Locked:            state.Locked,
			LockedUntil:       state.Until,
		}
	}

	membership, err := s.d.Members.ByUser(ctx, user.ID)
	if err != nil {
		return SignInResult{}, err
	}
	householdID := membership.HouseholdID

	failures, err := s.d.Attempts.FailuresSince(ctx, householdID, now.Add(-s.d.Policy.Window))
	if err != nil {
		return SignInResult{}, err
	}
	if state := s.d.Policy.Evaluate(failures, now); state.Locked {
		return SignInResult{}, &SignInFailedError{Locked: true, LockedUntil: state.Until}
	}

	if user.PasswordHash == "" || !s.d.Hasher.Verify(password, user.PasswordHash) {
		if err := s.d.Attempts.Record(ctx, &householdID, &user.ID, email, false, now); err != nil {
			return SignInResult{}, err
		}
		failures, err := s.d.Attempts.FailuresSince(ctx, householdID, now.Add(-s.d.Policy.Window))
		if err != nil {
			return SignInResult{}, err
		}
		state := s.d.Policy.Evaluate(failures, now)
		return SignInResult{}, &SignInFailedError{
			AttemptsRemaining: state.AttemptsRemaining,
			Locked:            state.Locked,
			LockedUntil:       state.Until,
		}
	}

	if err := s.d.Attempts.ClearFailures(ctx, householdID); err != nil {
		return SignInResult{}, err
	}
	if err := s.d.Attempts.Record(ctx, &householdID, &user.ID, email, true, now); err != nil {
		return SignInResult{}, err
	}
	return s.issueSession(ctx, user.ID, householdID, now)
}

func (s *AuthService) issueSession(ctx context.Context, userID, householdID string, now time.Time) (SignInResult, error) {
	raw, hash, err := s.d.Tokens.NewToken()
	if err != nil {
		return SignInResult{}, fmt.Errorf("generate session token: %w", err)
	}
	expiresAt := now.Add(s.d.SessionTTL)
	if err := s.d.Sessions.Create(ctx, hash, userID, householdID, expiresAt); err != nil {
		return SignInResult{}, err
	}
	return SignInResult{SessionToken: raw, ExpiresAt: expiresAt, UserID: userID, HouseholdID: householdID}, nil
}

func (s *AuthService) SignOut(ctx context.Context, sessionToken string) error {
	return s.d.Sessions.RevokeByToken(ctx, s.d.Tokens.HashToken(sessionToken))
}
