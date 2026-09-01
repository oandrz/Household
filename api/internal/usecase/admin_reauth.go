package usecase

import (
	"context"

	"github.com/andreasoentoro/hearth/api/internal/domain"
)

// AdminReauthService verifies the password again before the admin surface
// opens. The session cookie lives 30 days; this exists so a stolen cookie
// alone is not the key to every household's data.
//
// Its failures are counted in their own ledger, never in login_attempts. That
// table's lockout is household-scoped, so an operator's mistypes there would
// lock their whole household out of the ordinary product -- a bad outcome
// caused by a screen nobody else can even see.
type AdminReauthService struct{ d AdminReauthDeps }

type AdminReauthDeps struct {
	Users    UserRepository
	Attempts AdminReauthAttemptRepository
	Hasher   PasswordHasher
	Clock    Clock
	Policy   domain.LockoutPolicy
}

// NewAdminReauthService fills in a zero-valued Policy, which would otherwise
// never lock -- the same guard NewAuthService applies for the same reason.
func NewAdminReauthService(d AdminReauthDeps) *AdminReauthService {
	if d.Policy.MaxAttempts == 0 {
		d.Policy = domain.DefaultLockoutPolicy()
	}
	return &AdminReauthService{d: d}
}

// Verify answers nil when password is this user's, domain.ErrInvalidCredentials
// when it is not, and domain.ErrAdminLocked while the lockout is in force --
// including for the correct password, since guessing right is exactly what the
// lock exists to stop.
func (s *AdminReauthService) Verify(ctx context.Context, userID, password string) error {
	now := s.d.Clock.Now()

	failures, err := s.d.Attempts.FailuresSince(ctx, userID, now.Add(-s.d.Policy.Window))
	if err != nil {
		return err
	}
	if state := s.d.Policy.Evaluate(failures, now); state.Locked {
		// Record this attempt too, rather than returning before it ever
		// reaches the ledger: continued guessing against an already-locked
		// account must keep extending the lock and keep showing up in the
		// audit trail, the same choice AuthService.SignIn makes for the
		// household lock (see its own doc comment on that branch). No
		// password is checked here -- while locked, none can succeed, so
		// there is nothing to verify -- the attempt is simply logged as
		// failed.
		if recErr := s.d.Attempts.Record(ctx, userID, false, now); recErr != nil {
			return recErr
		}
		return domain.ErrAdminLocked
	}

	user, err := s.d.Users.ByID(ctx, userID)
	if err != nil {
		return err
	}

	// A user with no password at all (a member created without credentials)
	// can never satisfy this. Verify is not asked; an empty stored hash is a
	// refusal, not something to compare against.
	if user.PasswordHash == "" || !s.d.Hasher.Verify(password, user.PasswordHash) {
		if recErr := s.d.Attempts.Record(ctx, userID, false, now); recErr != nil {
			return recErr
		}
		return domain.ErrInvalidCredentials
	}

	if err := s.d.Attempts.Record(ctx, userID, true, now); err != nil {
		return err
	}
	return nil
}
