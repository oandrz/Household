package usecase

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
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

// Unwrap distinguishes ErrHouseholdLocked from ErrInvalidCredentials, and
// that distinction is intentional, not a leak this task's indistinguishability
// work missed: the design's sign-in screen deliberately shows the household a
// different message ("we've locked the household for 15 minutes") than it
// shows a simple wrong password, and the spec assigns 401 to one and 423 to
// the other on purpose. Whoever is looking at the failed sign-in screen is
// meant to learn which case they're in — what the indistinguishability work
// protects against is a caller *guessing at an address* learning whether that
// address exists or belongs to a locked household, which lives entirely in
// AttemptsRemaining/Locked/LockedUntil being computed identically across
// branches, not in collapsing the two errors into one HTTP status. Task 16's
// handler should map these to 401 and 423 respectively; do not "fix" this
// into a single status.
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

type AuthService struct {
	d AuthDeps

	decoyOnce sync.Once
	decoyHash string
}

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

// fallbackDecoyHash is used only if generating a real decoy hash through the
// configured Hasher ever fails (practically: Hash's entropy source is
// exhausted). NewAuthService cannot fail and must not silently skip the
// timing mitigation just because the one-time decoy generation had a bad
// day, so this constant guarantees decoy() always has *something* to Verify
// against. It is built once, right here, so it can never itself error.
const fallbackDecoyHash = "decoy-hash-used-only-if-generating-a-real-one-failed"

// decoy returns an encoded hash to run Hasher.Verify against on every SignIn
// path that would otherwise return without ever calling Verify. Against a
// real hasher (argon2id in production) a genuine verification costs tens to
// hundreds of milliseconds; a branch that skips it is trivially
// distinguishable from one that doesn't by timing alone, which defeats the
// same indistinguishability the error type and the attempts countdown exist
// to protect. decoy() is generated lazily against the service's own Hasher
// the first time it's needed (so it costs the same as the paths it's
// standing in for) and cached for the life of the service; SignIn always
// discards the result, since the call exists for its cost, not its answer.
func (s *AuthService) decoy() string {
	s.decoyOnce.Do(func() {
		hash, err := s.d.Hasher.Hash("decoy-password-for-timing-parity")
		if err != nil {
			s.decoyHash = fallbackDecoyHash
			return
		}
		s.decoyHash = hash
	})
	return s.decoyHash
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
		// Run the decoy verification here, in the position a real
		// Hasher.Verify call would occupy for a known user — this branch
		// otherwise returns without ever touching the hasher, which is
		// trivially distinguishable by timing from every branch that does.
		s.d.Hasher.Verify(password, s.decoy())
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

	// This counter is household-scoped, not address-scoped, because the
	// lockout itself is household-wide by design: the sign-in screen tells
	// whoever is typing "we've locked the household," not "we've locked
	// this address." That is a deliberate, accepted disclosure, not an
	// oversight — a wrong-password guess against one member's address
	// visibly decrements the countdown a second member's address reports,
	// so someone who already knows one member's email can use this to
	// confirm a candidate second address belongs to the same household.
	// The human partner weighed this against the product: a four-user,
	// two-adult household where both adults' addresses are already known
	// to each other, and chose to keep the household-wide lock rather than
	// scope the counter per address (which would also change the design's
	// own copy). Anyone changing the lock's scope away from
	// household-wide should revisit this trade-off, since it's the reason
	// the scoping is what it is.
	failures, err := s.d.Attempts.FailuresSince(ctx, householdID, now.Add(-s.d.Policy.Window))
	if err != nil {
		return SignInResult{}, err
	}
	if state := s.d.Policy.Evaluate(failures, now); state.Locked {
		// Run the decoy verification here, in the exact position the real
		// one would have occupied next (see the password check below) had
		// the household not been locked — otherwise this branch returns
		// without ever touching the hasher, timing-distinguishable from
		// every branch that does.
		s.d.Hasher.Verify(password, s.decoy())

		// Record this attempt too, exactly as the wrong-password and
		// unknown-address branches do, and re-evaluate over the updated
		// failure set before responding. Without this, a caller hammering an
		// already-locked household would see a LockedUntil frozen at the
		// third failure while hammering an unknown address sees one that
		// keeps advancing — a timing oracle that tells the two cases apart
		// even though the error type is identical. Recording here means
		// continued guessing against a locked household extends the lock,
		// matching the unknown-address behavior deliberately.
		//
		// This is itself an accepted trade-off, not an oversight: it means
		// someone who already knows a member's email can keep the household
		// locked indefinitely just by continuing to guess, with no cap. The
		// human partner chose to leave this uncapped rather than let the
		// lock expire on a fixed schedule while an attacker is still
		// actively working it — and magic link sign-in is deliberately
		// never gated by this lock (see domain.LockoutPolicy's doc comment),
		// so a real member always has a way back into their own household
		// even while the password lock is being held open this way.
		if err := s.d.Attempts.Record(ctx, &householdID, &user.ID, email, false, now); err != nil {
			return SignInResult{}, err
		}
		updated, err := s.d.Attempts.FailuresSince(ctx, householdID, now.Add(-s.d.Policy.Window))
		if err != nil {
			return SignInResult{}, err
		}
		state = s.d.Policy.Evaluate(updated, now)
		return SignInResult{}, &SignInFailedError{Locked: true, LockedUntil: state.Until}
	}

	passwordFailed := true
	if user.PasswordHash == "" {
		// The empty string is the sentinel for "no password set" (see
		// StoredUser's doc comment); it must never be handed to Verify as
		// if it were a real stored hash. Run the decoy verification instead,
		// in the exact position the real one would have occupied, so a
		// credential-less member costs exactly what a member with the wrong
		// password costs.
		s.d.Hasher.Verify(password, s.decoy())
	} else {
		passwordFailed = !s.d.Hasher.Verify(password, user.PasswordHash)
	}

	if passwordFailed {
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

const (
	magicLinkTTL          = 15 * time.Minute
	magicLinkPerHourLimit = 3
)

// RequestMagicLink is deliberately quiet. Neither an unknown address nor an
// exhausted rate limit produces an error, because any observable difference
// between the two would let a caller discover who is a member.
func (s *AuthService) RequestMagicLink(ctx context.Context, email string) error {
	now := s.d.Clock.Now()

	count, err := s.d.MagicLinks.CountSince(ctx, email, now.Add(-time.Hour))
	if err != nil {
		return err
	}
	if count >= magicLinkPerHourLimit {
		slog.Info("magic link rate limit reached", "email_hash", fmt.Sprintf("%x", s.d.Tokens.HashToken(email))[:12])
		return nil
	}

	user, err := s.d.Users.ByEmail(ctx, email)
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			return nil
		}
		return err
	}

	raw, hash, err := s.d.Tokens.NewToken()
	if err != nil {
		return fmt.Errorf("generate magic link token: %w", err)
	}
	if err := s.d.MagicLinks.Create(ctx, user.ID, hash, now.Add(magicLinkTTL)); err != nil {
		return err
	}

	url := fmt.Sprintf("%s/sign-in/magic?token=%s", s.d.BaseURL, raw)
	return s.d.Mailer.SendMagicLink(ctx, user.Email, user.DisplayName, url)
}

// ConsumeMagicLink signs the holder in. It is not gated by the household lock:
// the lock exists to stop password guessing, and this is the recovery path.
func (s *AuthService) ConsumeMagicLink(ctx context.Context, token string) (SignInResult, error) {
	now := s.d.Clock.Now()

	userID, err := s.d.MagicLinks.Consume(ctx, s.d.Tokens.HashToken(token))
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			return SignInResult{}, domain.ErrTokenExpired
		}
		return SignInResult{}, err
	}

	membership, err := s.d.Members.ByUser(ctx, userID)
	if err != nil {
		return SignInResult{}, err
	}
	return s.issueSession(ctx, userID, membership.HouseholdID, now)
}
