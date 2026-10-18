package usecase

import (
	"context"

	"github.com/andreasoentoro/hearth/api/internal/domain"
)

// AdminReauthService re-verifies a signed-in person's password: before the
// admin surface opens, so a stolen 30-day session cookie alone isn't the key
// to every household's data, and before AccessResetService.ChangePassword
// accepts a new password. Failures are counted in their own per-person
// ledger, never login_attempts -- that table's lockout is household-scoped,
// and would lock a whole household out of the ordinary product over
// mistypes on a screen nobody else can see. The two callers share the
// ledger: it is the same person's same password.
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
// ErrHasherBusy means nothing was checked and nothing was counted.
//
// The attempt is written down as a failure before anything is decided; see
// FailureReservation for why that order matters.
func (s *AdminReauthService) Verify(ctx context.Context, userID, password string) error {
	if s.d.Hasher.Full() {
		return ErrHasherBusy
	}
	now := s.d.Clock.Now()

	reserved, err := s.d.Attempts.ReserveFailure(ctx, userID, now, now.Add(-s.d.Policy.Window))
	if err != nil {
		return err
	}
	if s.d.Policy.Evaluate(reserved.Earlier, now).Locked {
		// The reserved attempt stays on record and so extends the lock,
		// matching AuthService.SignIn: an indefinitely extending lock beats
		// one that expires on a schedule an attacker can wait out. The way
		// out is to stop guessing for the policy window, or
		// `adminctl unlock-admin --email=` on the box. Signing in is not
		// affected, and Sign out everywhere needs no password, so a person
		// whose check is held locked by someone else's guesses can still
		// end every session that is guessing.
		//
		// No hash runs here, which is what bounds this route: however many
		// requests arrive at once, only the few that find room get one.
		return domain.ErrAdminLocked
	}

	user, err := s.d.Users.ByID(ctx, userID)
	if err != nil {
		withdrawReservation(ctx, s.d.Attempts, reserved.ID)
		return err
	}

	// A user with no password at all (a member created without credentials)
	// can never satisfy this. Verify is not asked; an empty stored hash is a
	// refusal, not something to compare against.
	if user.PasswordHash == "" {
		return domain.ErrInvalidCredentials
	}
	matched, err := s.d.Hasher.Verify(ctx, password, user.PasswordHash)
	if err != nil {
		// No check ran, so this attempt must not count as a wrong password.
		withdrawReservation(ctx, s.d.Attempts, reserved.ID)
		return err
	}
	if !matched {
		return domain.ErrInvalidCredentials
	}

	// Clear before recording success, mirroring AuthService.SignIn's
	// ClearFailures-then-Record order -- otherwise two mistypes, then a
	// success, then one more mistype would count as three cumulative
	// failures, not one fresh strike, which isn't what
	// domain.DefaultLockoutPolicy describes. Clearing also removes the
	// attempt reserved above.
	if err := s.d.Attempts.ClearFailures(ctx, userID); err != nil {
		return err
	}
	return s.d.Attempts.Record(ctx, userID, true, now)
}
