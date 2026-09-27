package usecase

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/andreasoentoro/hearth/api/internal/domain"
)

const (
	// SignupTTL is how long a create-household token lives. Exported because
	// the frontend and the mail template both state it ("expires in 24
	// hours"), so the value has one source. 24 hours, not magicLinkTTL's 15
	// minutes or inviteTTL's 7 days: a person creating a household may
	// finish that evening, but an unverified address shouldn't hold a
	// provisioning token for a week.
	SignupTTL = 24 * time.Hour

	// signupPerHourLimit mirrors magicLinkPerHourLimit. Being over it is
	// silent, like every other branch.
	signupPerHourLimit = 3

	// SignupGlobalDailyLimit backstops both the per-address and per-IP
	// limits combined. Counted from the signups table, not memory, so a
	// restart can't reset it, and evaluated against the calendar day
	// (startOfDay), not a rolling 24 hours.
	//
	// Sign-up is open to anyone, making this the last defense between the
	// SMTP relay and a determined loop. Too high costs the relay extra mail
	// on a bad day (recoverable, visible in metrics); too low silently
	// stops onboarding (every sign-up still answers 202, mailing nothing).
	// A busy real day is dozens of sign-ups; 1000 stays well above that
	// while still bounding a loop spread across many addresses and IPs.
	//
	// Exported (not signupGlobalDailyLimit) solely so
	// TestSignUpRateLimitsCompose in httpadapter can assert
	// signUpRequestsPerIPPerHour * 24 < SignupGlobalDailyLimit without an
	// import cycle -- usecase has no import on httpadapter, which already
	// imports usecase, so this adds no new dependency edge.
	SignupGlobalDailyLimit = 1000

	// signupSendTimeout bounds the background send so a wedged relay cannot
	// leak goroutines forever. Generous because nothing waits on it.
	signupSendTimeout = 30 * time.Second
)

// ErrSignupAlreadyUsed is Preview's and Complete's answer for a token that
// already provisioned a household. Deliberately not domain.ErrAlreadyExists:
// that sentinel's copy ("That already exists.") tells the holder of a spent
// link nothing useful, and its doc comment scopes it to a unique-constraint
// race between concurrent writers.
var ErrSignupAlreadyUsed = errors.New("this sign-up link has already been used")

type SignupDeps struct {
	Signups    SignupRepository
	Users      UserRepository
	Sessions   SessionRepository
	Mailer     Mailer
	Hasher     PasswordHasher
	Tokens     TokenGenerator
	Clock      Clock
	SessionTTL time.Duration
	BaseURL    string
}

type SignupService struct {
	d SignupDeps
}

func NewSignupService(d SignupDeps) *SignupService {
	return &SignupService{d: d}
}

// SignupPreview is what the create-household screen needs before creating
// anything. Channel tells the screen which identity the token proved, so it
// shows a read-only address for email or "Telegram" for Telegram, rather
// than an empty box that looks like a field the person forgot to fill in.
type SignupPreview struct {
	Email   string
	Channel string
}

// signupChannel refuses a row that names neither channel rather than guessing.
// The database constraint should make that unreachable; this is the second
// gate, for rows written by anything that bypasses it.
func signupChannel(d SignupDetails) (string, error) {
	switch {
	case d.Email != "":
		return "email", nil
	case d.TelegramChatID != nil:
		return "telegram", nil
	default:
		return "", fmt.Errorf("signup %s names no channel", d.ID)
	}
}

// Request is deliberately quiet: it returns nil for a fresh address, one
// that already has an account, one over its hourly limit, a day over the
// global mail ceiling, an implausibly-formed address, and every internal
// failure below the branch point. Any observable difference between those
// would let a caller discover which addresses are registered.
//
// Before any of that, isPlausibleEmail is checked, and failure returns nil
// immediately, before any read or write below runs. This is not one of the
// four properties below: it isn't making two branches indistinguishable,
// it's refusing to spend a counted read or signups row on input that can't
// be a real address ("", "not-an-email"). That's safe because the check is
// a pure function of the string, independent of whether any address is
// registered, so it can't become a registration oracle. See
// isPlausibleEmail's own doc comment for its exact scope.
//
// Four properties make the rest of it true, and all four are load-bearing:
//
//  1. All three reads below run unconditionally, in this fixed order, on
//     every call that passes the plausibility gate. Don't return early once
//     a rate-limit check decides the outcome: a skipped read makes the
//     *number of repository reads* distinguish that branch as surely as an
//     error would. signup_test.go's ordered read log defends against it.
//
//  2. Mail is sent off the request path (see sendAsync), so a slow or wedged
//     relay cannot make the fresh-address branch measurably slower than the
//     others.
//
//  3. Both branches write a signups row (Create or CreateConsumed) using the
//     same generated token -- not just the same reads, the same writes.
//     Don't let only the fresh branch write a countable row:
//     CountForEmailSince/CountSince count this table, so an uncounted
//     registered branch would let the rate limit gate only fresh addresses,
//     letting a registered inbox absorb unlimited existing-account mail --
//     the exact oracle SendSignupForExistingAccount's doc comment exists to
//     close. CreateConsumed's row can never provision anything (Provision's
//     guarded UPDATE requires consumed_at IS NULL) and its token is never
//     mailed; it exists solely to be counted.
//
//  4. Everything after the branch point -- token generation, the INSERT, the
//     send -- is reachable by both a fresh and a registered under-limit
//     address, so a propagated error from any of them would be a discrete
//     yes/no oracle for "is this address registered", cheaper to exploit than
//     any timing measurement. Each is logged at error level with a hashed
//     address and returns nil instead. ANYONE ADDING A STEP BELOW THE BRANCH
//     POINT OWES IT THE SAME TREATMENT.
func (s *SignupService) Request(ctx context.Context, email string) error {
	// A budget guard, not a correctness check (see isPlausibleEmail). Checked
	// before the Clock and before any read or write: {"email":""} must not
	// advance a counter or write a countable row for free.
	if !isPlausibleEmail(email) {
		return nil
	}

	now := s.d.Clock.Now()

	// startOfDay, not now.Add(-24*time.Hour): the global ceiling resets at
	// midnight, not on a rolling 24-hour trailing window. See startOfDay's own
	// doc comment for which zone "midnight" means here and why.
	globalCount, err := s.d.Signups.CountSince(ctx, startOfDay(now))
	if err != nil {
		return err
	}
	addressCount, err := s.d.Signups.CountForEmailSince(ctx, email, now.Add(-time.Hour))
	if err != nil {
		return err
	}
	_, err = s.d.Users.ByEmail(ctx, email)
	if err != nil && !errors.Is(err, domain.ErrNotFound) {
		return err
	}
	alreadyRegistered := err == nil

	// Both limits gate both branches: if only the fresh branch were gated,
	// someone could flood a registered address's inbox with existing-account
	// notices, and the differing behaviour would itself distinguish the two
	// cases. This check is only as real as the counters it reads -- every
	// branch below writes a row for it to count.
	//
	// The two limits are checked separately and logged at different levels,
	// though both still return nil identically to the caller -- the log
	// level is for the operator, and a differing *response* would be the
	// enumeration oracle this design prevents. Global is checked first so a
	// breach is always logged loudly, even when the address is also over
	// its hourly limit; checking address-first would downgrade those
	// requests to routine noise and hide the outage on the traffic most
	// likely causing it. Per-address tripping is an ordinary no-op; global
	// tripping means attack or overwhelming success, and needs a human to
	// notice some way other than a customer complaint.
	if globalCount >= SignupGlobalDailyLimit {
		slog.Error("sign-up request declined by the global daily mail ceiling",
			"email_hash", hashPrefix(s.d.Tokens.HashToken(email), 12),
			"address_count", addressCount,
			"global_count", globalCount,
			"global_daily_limit", SignupGlobalDailyLimit,
		)
		return nil
	}
	if addressCount >= signupPerHourLimit {
		slog.Info("sign-up request declined by the per-address rate limit",
			"email_hash", hashPrefix(s.d.Tokens.HashToken(email), 12),
			"address_count", addressCount,
			"global_count", globalCount,
		)
		return nil
	}

	// One token is generated here, before the branch, and used by whichever
	// runs -- not because the registered branch's token matters (it's never
	// mailed; its row is pre-consumed), but so a generation failure is
	// handled identically for both branches by construction, not by two
	// copies of the same code.
	raw, hash, err := s.d.Tokens.NewToken()
	if err != nil {
		slog.Error("sign-up token generation failed",
			"error", err, "email_hash", hashPrefix(s.d.Tokens.HashToken(email), 12))
		return nil
	}

	if alreadyRegistered {
		// This row can never provision anything (see CreateConsumed's doc
		// comment) and its token is never mailed -- it exists solely so this
		// branch's own CountForEmailSince/CountSince advance, the same way
		// Create does for the fresh branch.
		if err := s.d.Signups.CreateConsumed(ctx, email, hash, now.Add(SignupTTL)); err != nil {
			slog.Error("sign-up persistence failed (existing-account counter row)",
				"error", err, "email_hash", hashPrefix(s.d.Tokens.HashToken(email), 12))
			return nil
		}
		s.sendAsync(func(ctx context.Context) error {
			return s.d.Mailer.SendSignupForExistingAccount(ctx, email, s.d.BaseURL+"/sign-in")
		}, email, "existing account notice")
		return nil
	}

	if err := s.d.Signups.Create(ctx, email, hash, now.Add(SignupTTL)); err != nil {
		slog.Error("sign-up persistence failed",
			"error", err, "email_hash", hashPrefix(s.d.Tokens.HashToken(email), 12))
		return nil
	}

	url := fmt.Sprintf("%s/sign-up/%s", s.d.BaseURL, raw)
	s.sendAsync(func(ctx context.Context) error {
		return s.d.Mailer.SendSignupLink(ctx, email, url)
	}, email, "sign-up link")
	return nil
}

// startOfDay returns midnight for t in t's own location, not a hardcoded
// time.UTC -- it asks the clock what "today" is, the same rule this file
// follows for every use of "now": never reach for wall-clock time directly,
// only the injected Clock. In production the Clock already normalizes to
// UTC, so this computes UTC midnight in practice. This is deliberate, not
// an oversight: "midnight" is ambiguous across zones, so "today starting
// over" means whichever zone the Clock reports, not one fixed here.
func startOfDay(t time.Time) time.Time {
	y, m, d := t.Date()
	return time.Date(y, m, d, 0, 0, 0, 0, t.Location())
}

// sendAsync fires a send off the request path and returns immediately, for
// the same reasons sendMagicLinkAsync does: timing parity between branches,
// and Request's contract of "always nil, always silent" -- a down relay
// must not become a caller-visible error on only one branch.
//
// The context is derived from context.Background(), not the request's, because
// the request context is cancelled the moment the handler returns -- which
// happens before this goroutine would otherwise run.
func (s *SignupService) sendAsync(send func(context.Context) error, email, what string) {
	// Computed on the caller's goroutine, which the HTTP recoverer still
	// covers, so the recover below can reuse it without hashing again inside a
	// panic handler.
	emailHash := hashPrefix(s.d.Tokens.HashToken(email), 12)

	go func() {
		// Nothing supervises this goroutine: the HTTP recoverer guards only the
		// request goroutine. An unrecovered panic here would take down every
		// unrelated in-flight request, not just this send.
		defer func() {
			if r := recover(); r != nil {
				slog.Error("sign-up mail panicked", "panic", r, "kind", what, "email_hash", emailHash)
			}
		}()

		ctx, cancel := context.WithTimeout(context.Background(), signupSendTimeout)
		defer cancel()
		if err := send(ctx); err != nil {
			slog.Error("sign-up mail failed to send", "error", err, "kind", what, "email_hash", emailHash)
		}
	}()
}

// Preview lets the create-household screen show which address it is about to
// create an account for. It shares its liveness check with Complete through
// checkSignupLive.
func (s *SignupService) Preview(ctx context.Context, token string) (SignupPreview, error) {
	details, err := s.d.Signups.ByTokenHash(ctx, s.d.Tokens.HashToken(token))
	if err != nil {
		return SignupPreview{}, err
	}
	if err := checkSignupLive(details, s.d.Clock.Now()); err != nil {
		return SignupPreview{}, err
	}
	channel, err := signupChannel(details)
	if err != nil {
		return SignupPreview{}, err
	}
	return SignupPreview{Email: details.Email, Channel: channel}, nil
}

// checkSignupLive reports why a sign-up token can no longer be used, keeping
// consumed and expired apart because the next action differs: consumed
// means sign in, expired means start again. The ordering rule lives in
// domain.TokenLifecycle, shared with invites.
func checkSignupLive(details SignupDetails, now time.Time) error {
	switch domain.TokenLifecycle(now, details.ExpiresAt, details.ConsumedAt) {
	case domain.TokenLive:
		return nil
	case domain.TokenConsumed:
		return ErrSignupAlreadyUsed
	case domain.TokenExpired:
		return domain.ErrTokenExpired
	default:
		// An unrecognised state refuses rather than treating the token as
		// usable. Adding a TokenState without a case here rejects the sign-up;
		// it does not silently accept it.
		return domain.ErrTokenExpired
	}
}

// Complete turns a verified address into a household and signs its owner
// in. Every validation happens before the hash and before Provision, so a
// rejected form never consumes the token -- a mistyped password can simply
// be resubmitted. The session is minted by the same issueSession that
// SignIn and InviteService.Accept use, so it's indistinguishable from
// theirs.
func (s *SignupService) Complete(ctx context.Context, token, householdName, displayName,
	currency, password string) (SignInResult, error) {
	now := s.d.Clock.Now()

	if err := validatePassword(password); err != nil {
		return SignInResult{}, err
	}
	blueprint, err := NewSignupBlueprint(householdName, displayName, currency)
	if err != nil {
		return SignInResult{}, err
	}

	details, err := s.d.Signups.ByTokenHash(ctx, s.d.Tokens.HashToken(token))
	if err != nil {
		return SignInResult{}, err
	}
	if err := checkSignupLive(details, now); err != nil {
		return SignInResult{}, err
	}

	passwordHash, err := s.d.Hasher.Hash(password)
	if err != nil {
		return SignInResult{}, fmt.Errorf("hash sign-up password: %w", err)
	}

	// Provision's guarded UPDATE is the concurrency gate: its answer is
	// authoritative for the race between the read above and this write, so its
	// error is returned as-is rather than re-derived from the stale read.
	provisioned, err := s.d.Signups.Provision(ctx, details.ID, passwordHash, blueprint)
	if err != nil {
		return SignInResult{}, err
	}

	return issueSession(ctx, s.d.Sessions, s.d.Tokens, s.d.SessionTTL,
		provisioned.UserID, provisioned.HouseholdID, now)
}
