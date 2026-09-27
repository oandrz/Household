package usecase

import (
	"context"

	"github.com/andreasoentoro/hearth/api/internal/domain"
)

// AdminReauthService re-verifies the password before the admin surface
// opens, so a stolen 30-day session cookie alone isn't the key to every
// household's data. Failures are counted in their own ledger, never
// login_attempts -- that table's lockout is household-scoped, and would
// lock a whole household out of the ordinary product over mistypes on a
// screen nobody else can see.
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

// Verify answers nil for a correct password, domain.ErrInvalidCredentials
// for a wrong one, and domain.ErrAdminLocked while locked -- even for the
// correct password, since guessing right is what the lock exists to stop.
func (s *AdminReauthService) Verify(ctx context.Context, userID, password string) error {
	now := s.d.Clock.Now()

	failures, err := s.d.Attempts.FailuresSince(ctx, userID, now.Add(-s.d.Policy.Window))
	if err != nil {
		return err
	}
	if state := s.d.Policy.Evaluate(failures, now); state.Locked {
		// Recording here is deliberate, matching AuthService.SignIn: an
		// indefinitely extending lock beats one that expires on a schedule
		// an attacker can wait out. Unlike the household lock, there's no
		// in-product escape hatch -- the way back in is `adminctl
		// unlock-admin --email=`, run on the box, an acceptable trade since
		// only shell access could have made anyone an admin to begin with.
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

	// Clear before recording success, mirroring AuthService.SignIn's
	// ClearFailures-then-Record order -- otherwise two mistypes, then a
	// success, then one more mistype would count as three cumulative
	// failures, not one fresh strike, which isn't what
	// domain.DefaultLockoutPolicy describes.
	if err := s.d.Attempts.ClearFailures(ctx, userID); err != nil {
		return err
	}
	if err := s.d.Attempts.Record(ctx, userID, true, now); err != nil {
		return err
	}
	return nil
}
