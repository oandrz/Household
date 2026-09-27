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

// Unwrap tells ErrHouseholdLocked from ErrInvalidCredentials on purpose: the
// sign-in screen shows each its own message, so the handler answers 401 and
// 423. Don't collapse them into one status. What must not leak is whether a
// guessed address exists or is locked, and that is guarded by computing
// AttemptsRemaining, Locked and LockedUntil identically on every branch.
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

// NewAuthService fills in a zero-valued Policy -- LockoutPolicy{} never
// locks while reporting "0 tries left", a silent, inconsistent disable that
// a forgotten field in a struct literal reaches easily.
func NewAuthService(d AuthDeps) *AuthService {
	if d.Policy.MaxAttempts == 0 {
		d.Policy = domain.DefaultLockoutPolicy()
	}
	return &AuthService{d: d}
}

// fallbackDecoyHash covers the rare case Hasher.Hash fails (entropy
// exhausted): NewAuthService can't fail and mustn't silently skip the
// timing mitigation, so this constant guarantees decoy() always has
// something to Verify against, and being a literal, can't itself error.
const fallbackDecoyHash = "decoy-hash-used-only-if-generating-a-real-one-failed"

// decoy returns a hash to run Hasher.Verify against on every SignIn path
// that would otherwise skip Verify entirely -- a real hasher costs tens to
// hundreds of milliseconds, and skipping it is a timing tell that defeats
// the same indistinguishability the error type and attempts countdown
// protect. Generated lazily against the service's own Hasher (so it costs
// what the real paths cost) and cached; SignIn always discards the result,
// since the call exists for its cost, not its answer.
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

// verifyPassword is the one path SignIn hands a caller-supplied password
// to the hasher, decoy or real. It rejects a password over
// maxPasswordLength before calling Verify at all -- argon2id's cost scales
// with input size, so an unbounded password would be uncapped CPU
// amplification. This breaks timing parity for that one case, but safely:
// the length is something the caller already knows about their own input,
// and indistinguishability only protects against a caller *guessing an
// address* learning something new about the account.
func (s *AuthService) verifyPassword(password, encoded string) bool {
	if len(password) > maxPasswordLength {
		return false
	}
	return s.d.Hasher.Verify(password, encoded)
}

type SignInResult struct {
	SessionToken string
	ExpiresAt    time.Time
	UserID       string
	HouseholdID  string
}

// signInFailedForUnknownAddress is the shared failure branch for an
// address with no user row, and a user row with no membership (an
// ex-member -- see SignIn's second call site). Both run the same decoy
// Hasher.Verify, in the position a real one would occupy, and the same
// address-scoped record/countdown, so neither is distinguishable from the
// other, or from a real wrong password, by timing or which counter
// advances.
func (s *AuthService) signInFailedForUnknownAddress(ctx context.Context, password, email string, now time.Time) (SignInResult, error) {
	s.verifyPassword(password, s.decoy())
	// Record with no household, so guessing at unknown addresses can't lock
	// a real one; evaluate the same policy over that address's own failures
	// so a stranger's countdown matches a member's.
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

func (s *AuthService) SignIn(ctx context.Context, email, password string) (SignInResult, error) {
	now := s.d.Clock.Now()

	user, err := s.d.Users.ByEmail(ctx, email)
	if err != nil {
		if !errors.Is(err, domain.ErrNotFound) {
			return SignInResult{}, err
		}
		return s.signInFailedForUnknownAddress(ctx, password, email, now)
	}

	membership, err := s.d.Members.ByUser(ctx, user.ID)
	if err != nil {
		if !errors.Is(err, domain.ErrNotFound) {
			return SignInResult{}, err
		}
		// A users row can outlive its membership: removing a member deletes
		// its row from memberships, not from users (see
		// MemberService.Remove). An ex-member's address must fail exactly
		// like a stranger's -- same decoy verify, same address-scoped
		// countdown. Don't let domain.ErrNotFound propagate from here
		// instead: MapDomainError turns that into a bare 404 the sign-in
		// screen has no copy for, and it tells a caller "this address once
		// existed" where every other failure here tells them nothing.
		return s.signInFailedForUnknownAddress(ctx, password, email, now)
	}
	householdID := membership.HouseholdID

	// This counter is household-scoped, not address-scoped, because the
	// lockout itself is household-wide by design: the screen says "we've
	// locked the household," not "this address." That's an accepted
	// disclosure, not an oversight -- guessing one member's address visibly
	// decrements a countdown the other member's address also reports,
	// letting someone who knows one address confirm a candidate second one
	// belongs to the same household. The product owner weighed this against
	// a two-adult household where both addresses are already known to each
	// other, and chose household-wide over per-address scope, which would
	// also change the design's copy. Revisit this trade-off before changing
	// that scope.
	failures, err := s.d.Attempts.FailuresSince(ctx, householdID, now.Add(-s.d.Policy.Window))
	if err != nil {
		return SignInResult{}, err
	}
	if state := s.d.Policy.Evaluate(failures, now); state.Locked {
		// Run the decoy verification in the exact position the real one
		// would occupy (see the password check below) -- otherwise this
		// branch never touches the hasher, timing-distinguishable from every
		// branch that does.
		s.verifyPassword(password, s.decoy())

		// Record and re-evaluate here too, matching the wrong-password and
		// unknown-address branches -- otherwise a locked household's
		// LockedUntil would freeze at the third failure while an unknown
		// address's keeps advancing, an oracle telling a real household from
		// a stranger's address even though the error type is identical.
		// This extends the lock on continued guessing, deliberately matching
		// unknown-address behavior. Accepted trade-off:
		// with no cap, someone who knows the email can keep the household
		// locked forever, chosen over a fixed expiry an active attacker
		// could wait out. Magic-link sign-in isn't gated by this lock (see
		// domain.LockoutPolicy), so a real member always has a way back in.
		updated, err := s.recordHouseholdFailure(ctx, householdID, user.ID, email, now)
		if err != nil {
			return SignInResult{}, err
		}
		return SignInResult{}, &SignInFailedError{Locked: true, LockedUntil: updated.Until}
	}

	passwordFailed := true
	if user.PasswordHash == "" {
		// Empty string is the sentinel for "no password set" (StoredUser's
		// doc comment); never hand it to Verify as a real hash. Decoy
		// verification runs instead, in the real one's position, so a
		// credential-less member costs exactly what a wrong password costs.
		s.verifyPassword(password, s.decoy())
	} else {
		passwordFailed = !s.verifyPassword(password, user.PasswordHash)
	}

	if passwordFailed {
		state, err := s.recordHouseholdFailure(ctx, householdID, user.ID, email, now)
		if err != nil {
			return SignInResult{}, err
		}
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

// recordHouseholdFailure records one failure and evaluates the lock over
// the updated set -- SignIn's already-locked and wrong-password branches
// both end this way. It skips the password/decoy check on purpose: that
// call's position relative to each branch is what keeps them
// timing-indistinguishable, so it stays at each call site, before this.
func (s *AuthService) recordHouseholdFailure(ctx context.Context, householdID, userID, email string, now time.Time) (domain.LockState, error) {
	if err := s.d.Attempts.Record(ctx, &householdID, &userID, email, false, now); err != nil {
		return domain.LockState{}, err
	}
	failures, err := s.d.Attempts.FailuresSince(ctx, householdID, now.Add(-s.d.Policy.Window))
	if err != nil {
		return domain.LockState{}, err
	}
	return s.d.Policy.Evaluate(failures, now), nil
}

func (s *AuthService) issueSession(ctx context.Context, userID, householdID string, now time.Time) (SignInResult, error) {
	return issueSession(ctx, s.d.Sessions, s.d.Tokens, s.d.SessionTTL, userID, householdID, now)
}

// issueSession is the one place a live session gets minted -- a
// package-level function, not a method, so InviteService.Accept can call
// it too: the invite session must be issued identically to sign-in's, not
// a look-alike second implementation. AuthService.issueSession stays as a
// thin wrapper so existing call sites don't change.
func issueSession(ctx context.Context, sessions SessionRepository, tokens TokenGenerator, sessionTTL time.Duration, userID, householdID string, now time.Time) (SignInResult, error) {
	raw, hash, err := tokens.NewToken()
	if err != nil {
		return SignInResult{}, fmt.Errorf("generate session token: %w", err)
	}
	expiresAt := now.Add(sessionTTL)
	if err := sessions.Create(ctx, hash, userID, householdID, expiresAt); err != nil {
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

	// magicLinkSendTimeout bounds the background send so a wedged relay
	// cannot leak goroutines forever. It is generous because nothing is
	// waiting on it -- see sendMagicLinkAsync.
	magicLinkSendTimeout = 30 * time.Second
)

// hashPrefix renders the first n hex characters of hash, or the whole
// thing if shorter. It exists because fmt.Sprintf("%x", hash)[:n] panics
// once hash is shorter than n bytes, and TokenGenerator makes no
// minimum-length promise. Every log line here that redacts an email or
// token goes through this, including inside sendMagicLinkAsync's goroutine,
// where no middleware.Recoverer would catch a panic.
func hashPrefix(hash []byte, n int) string {
	encoded := fmt.Sprintf("%x", hash)
	if len(encoded) < n {
		return encoded
	}
	return encoded[:n]
}

// RequestMagicLink is deliberately quiet: neither an unknown address nor
// an exhausted rate limit produces an error, since any observable
// difference would tell a caller whether the address belongs to a member.
// Nor does any later failure (token generation, persistence, the send) --
// each step is reachable only from the known-address branch, so a
// propagated error there would be the same oracle.
func (s *AuthService) RequestMagicLink(ctx context.Context, email string) error {
	now := s.d.Clock.Now()

	// Both reads below run unconditionally, in this fixed order, for every
	// call. Don't skip ByEmail for a rate-limited address to save a query:
	// CountSince can never report a count at or over the limit for an address
	// with no user behind it (it joins through users), so the *number* of
	// reads alone would prove membership just as surely as an error would.
	count, err := s.d.MagicLinks.CountSince(ctx, email, now.Add(-time.Hour))
	if err != nil {
		return err
	}
	user, err := s.d.Users.ByEmail(ctx, email)
	if err != nil && !errors.Is(err, domain.ErrNotFound) {
		return err
	}
	known := err == nil

	// A users row can outlive its membership (see MemberService.Remove and
	// SignIn's identical check). Minting a token here would mail a link
	// that can never become a session -- ConsumeMagicLink calls
	// Members.ByUser too, and would fail identically. Log it and fall
	// through to the same silent return every unknown/rate-limited address
	// gets below.
	if known {
		if _, membErr := s.d.Members.ByUser(ctx, user.ID); membErr != nil {
			if !errors.Is(membErr, domain.ErrNotFound) {
				return membErr
			}
			slog.Info("magic link requested for a user with no membership",
				"email_hash", hashPrefix(s.d.Tokens.HashToken(email), 12))
			known = false
		}
	}

	rateLimited := count >= magicLinkPerHourLimit

	if !known || rateLimited {
		if rateLimited {
			// An address can't be rate-limited without a user behind it
			// (CountSince joins through users) -- but known can be false here
			// for another reason too: an ex-member whose past magic-link
			// requests, made while still a member, are still within the hour.
			// Either way this log line is accurate.
			slog.Info("magic link rate limit reached", "email_hash", hashPrefix(s.d.Tokens.HashToken(email), 12))
		}
		return nil
	}

	// Everything below is reachable only by a known, under-limit address --
	// the same asymmetry that makes a propagated mailer error an oracle
	// (see sendMagicLinkAsync). Token-generation and INSERT failures are
	// just as reachable only here, so they get the same treatment: log at
	// error level with a hashed address, return nil, never propagate --
	// and so must any step added below this comment.
	raw, hash, err := s.d.Tokens.NewToken()
	if err != nil {
		slog.Error("magic link token generation failed",
			"error", err,
			"email_hash", hashPrefix(s.d.Tokens.HashToken(email), 12),
		)
		return nil
	}
	if err := s.d.MagicLinks.Create(ctx, user.ID, hash, now.Add(magicLinkTTL)); err != nil {
		slog.Error("magic link persistence failed",
			"error", err,
			"email_hash", hashPrefix(s.d.Tokens.HashToken(email), 12),
		)
		return nil
	}

	url := fmt.Sprintf("%s/sign-in/magic?token=%s", s.d.BaseURL, raw)
	s.sendMagicLinkAsync(user.Email, user.DisplayName, url)
	return nil
}

// sendMagicLinkAsync fires the email off the request path and returns
// immediately, for one reason seen from two sides:
//
//   - Timing: the synchronous SMTP conversation plus the token's DB write
//     make the known-address branch far slower than the couple-reads-only
//     unknown/rate-limited branches -- more than the SignIn decoy
//     machinery guards against.
//   - Correctness: the contract is "always nil, always silent," so a down
//     or rejecting relay must not surface as an error on the known-address
//     branch alone -- that would be a discrete membership oracle, cheaper
//     to exploit than any timing gap.
//
// Swallowing the error is a smell but correct here: the token row is
// already committed, so a failure only costs a retry, never correctness
// (the member re-requests, or the existing link still works until its
// 15-minute expiry). The context is context.Background(), not the
// request's, which is cancelled the instant the handler returns -- before
// this goroutine would run, so sending on it would silently deliver
// nothing.
func (s *AuthService) sendMagicLinkAsync(to, name, url string) {
	// Computed here, on the caller's goroutine (still covered by chi's
	// middleware.Recoverer), not inside the goroutine below, so recover()
	// can reuse the value without calling HashToken from inside a panic
	// handler.
	emailHash := hashPrefix(s.d.Tokens.HashToken(to), 12)

	go func() {
		// middleware.Recoverer guards only the request goroutine; nothing
		// supervises this one, so an unrecovered panic here would crash the
		// whole process, not just this send. Recovering keeps a bug in the
		// mailer, or a future step added here, contained to this one send.
		defer func() {
			if r := recover(); r != nil {
				slog.Error("magic link send panicked", "panic", r, "email_hash", emailHash)
			}
		}()

		ctx, cancel := context.WithTimeout(context.Background(), magicLinkSendTimeout)
		defer cancel()
		if err := s.d.Mailer.SendMagicLink(ctx, to, name, url); err != nil {
			slog.Error("magic link email failed to send", "error", err, "email_hash", emailHash)
		}
	}()
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
