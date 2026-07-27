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
	// the frontend's copy states it ("expires in 24 hours") and the mail
	// template repeats it, so the value has one source.
	//
	// 24 hours, not magicLinkTTL's 15 minutes and not inviteTTL's 7 days: a
	// person who asks to create a household may well finish the job that
	// evening, but an unverified address should not hold a provisioning token
	// for a week.
	SignupTTL = 24 * time.Hour

	// signupPerHourLimit mirrors magicLinkPerHourLimit. Being over it is
	// silent, like every other branch.
	signupPerHourLimit = 3

	// SignupGlobalDailyLimit is the backstop for the case both the per-address
	// and per-IP limits are being worked at once. It is counted from the
	// signups table rather than an in-memory counter, so restarting the API
	// cannot reset it, and it is evaluated against the calendar day (see
	// startOfDay), not a rolling 24 hours -- see Request's use of it below.
	//
	// Sign-up is open to anyone (a deliberate product decision), which makes
	// this the last thing standing between the SMTP relay and a stranger with a
	// loop. The two ways of getting this number wrong are not symmetric. Too
	// high, and a bad day costs the relay up to this many mails -- recoverable,
	// and visible in relay metrics and cost. Too low, and onboarding silently
	// stops: every sign-up still answers 202, nothing is mailed, and that looks
	// exactly like nobody signing up, not like a limit tripping. For a
	// household product, a busy real day is dozens of sign-ups; 1000 is far
	// enough above that to never bind for legitimate traffic while still being
	// a real ceiling against a determined loop spread across many addresses
	// and IPs.
	//
	// Exported (not signupGlobalDailyLimit) solely so
	// TestSignUpRateLimitsCompose, in the httpadapter package's test suite, can
	// assert signUpRequestsPerIPPerHour * 24 < SignupGlobalDailyLimit without
	// an import cycle: usecase has no import on httpadapter, and httpadapter
	// already imports usecase in production code (router.go), so exporting
	// this adds no new edge to the dependency graph, only visibility for a
	// test that reads both constants.
	SignupGlobalDailyLimit = 1000

	// signupSendTimeout bounds the background send so a wedged relay cannot
	// leak goroutines forever. Generous because nothing waits on it.
	signupSendTimeout = 30 * time.Second
)

// ErrSignupAlreadyUsed is Preview's and Complete's answer for a token that has
// already provisioned a household. It is deliberately NOT domain.ErrAlreadyExists:
// that sentinel's copy is "That already exists.", which tells the holder of a
// spent link nothing useful, and its own doc comment scopes it to a
// unique-constraint race between concurrent writers.
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

// SignupPreview is what the create-household screen needs before anything is
// created: the address the token proved, so the form can show it read-only.
// Nothing else -- there is no household yet to describe.
type SignupPreview struct {
	Email string
}

// Request is deliberately quiet. It returns nil for a fresh address, an address
// that already has an account, an address over its hourly limit, a day over the
// global mail ceiling, an implausibly-formed address, and every internal
// failure below the branch point. Any observable difference between those
// would let a caller discover which addresses are registered.
//
// Before any of that, isPlausibleEmail is checked, and a failure returns nil
// immediately -- before any of the reads or writes below ever run. That is
// deliberately not one of the four properties below: it is not making two
// branches indistinguishable, it is refusing to spend a counted read or a
// signups row on input that cannot possibly be a real address at all
// ("", "not-an-email"). That is safe precisely because the check is a pure
// function of the string itself, independent of whether any particular
// address is registered -- it rejects "" identically no matter what the users
// or signups tables contain, so it cannot become the registration oracle an
// error return from the reads below would be. See isPlausibleEmail's own doc
// comment for what it does and does not guard against.
//
// Four properties make the rest of it true, and all four are load-bearing:
//
//  1. All three reads below run unconditionally, in this fixed order, on every
//     call that reaches them (every call whose address passed the
//     plausibility gate above). RequestMagicLink once returned as soon as its
//     rate-limit check decided the outcome, which made the *number of
//     repository reads* distinguish the rate-limited case just as surely as
//     an error would have. A read that is skipped on one branch of an address
//     that passed the gate is the defect; the ordered read log in
//     signup_test.go is what defends against it.
//
//  2. Mail is sent off the request path (see sendAsync), so a slow or wedged
//     relay cannot make the fresh-address branch measurably slower than the
//     others.
//
//  3. Both branches write a signups row, through Create or CreateConsumed,
//     using the same generated token -- not just the same reads, the same
//     writes. This closed a fix-round finding: CountForEmailSince/CountSince
//     count rows in that table, and the only writer used to be Create on the
//     fresh branch. An already-registered address's counters therefore never
//     advanced no matter how many requests arrived for it, so the shared
//     rate-limit check below gated the fresh branch in practice and the
//     registered branch never -- POST /auth/sign-up four times for a
//     registered address sent four (then forty, then four hundred)
//     "you already have an account" mails with no ceiling, which is the exact
//     mailbox oracle SendSignupForExistingAccount's own doc comment exists to
//     close, expressed as unbounded volume rather than presence-or-absence.
//     CreateConsumed's row can never provision anything (Provision's guarded
//     UPDATE requires consumed_at IS NULL) and its token is never mailed; it
//     exists solely to be counted, so the limit is now real on both branches,
//     which is what CountForEmailSince's doc comment already claimed.
//
//  4. Everything after the branch point -- token generation, the INSERT, the
//     send -- is reachable by both a fresh and a registered under-limit
//     address, so a propagated error from any of them would be a discrete
//     yes/no oracle for "is this address registered", cheaper to exploit than
//     any timing measurement. Each is logged at error level with a hashed
//     address and returns nil instead. ANYONE ADDING A STEP BELOW THE BRANCH
//     POINT OWES IT THE SAME TREATMENT.
func (s *SignupService) Request(ctx context.Context, email string) error {
	// A budget guard, not a correctness check -- see isPlausibleEmail's doc
	// comment. Checked before the Clock, before every read, before any write:
	// {"email":""} must not advance a rate-limit counter or write a countable
	// signups row for free.
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

	// Both limits gate both branches. If only the fresh-address branch were
	// gated, someone could flood a registered address's inbox with
	// existing-account notices, and the differing behaviour would itself
	// distinguish the two cases. This check is only as real as the counters
	// it reads, which is why every branch below writes a row for it to count.
	//
	// The two limits are checked separately, in this order, and logged at
	// different levels, even though both still return nil identically to the
	// caller (see the doc comment above -- the log level is for the operator,
	// not the caller, and a different *response* on either branch would be
	// exactly the enumeration oracle this whole design exists to prevent).
	// Global is checked first so that every request arriving while the global
	// ceiling is breached is logged loudly, regardless of whether that
	// request's own address also happens to be over its hourly limit --
	// checking address-first would silently downgrade those requests to
	// routine Info noise and hide the outage on exactly the traffic most
	// likely to be causing it. Per-address tripping is ordinary and expected
	// (one caller gets a quiet no-op); the global ceiling tripping means the
	// platform is either under attack or succeeding beyond plan, and either
	// way a human needs to find out from something other than a customer
	// complaining that sign-up mail never arrived.
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

	// One token generated here, before the branch, and used by whichever
	// branch runs -- not because the registered branch's token is ever used
	// for anything (it is never mailed and its row is inserted pre-consumed),
	// but so a token-generation failure is handled identically for both
	// branches by construction, rather than by two copies of the same
	// failure-handling code.
	raw, hash, err := s.d.Tokens.NewToken()
	if err != nil {
		slog.Error("sign-up token generation failed",
			"error", err, "email_hash", hashPrefix(s.d.Tokens.HashToken(email), 12))
		return nil
	}

	if alreadyRegistered {
		// This row can never provision anything -- see CreateConsumed's doc
		// comment in ports.go -- and its token is never mailed. It exists so
		// this branch's own CountForEmailSince/CountSince advance, the same
		// way Create makes the fresh branch's advance below.
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

// startOfDay returns midnight for t, in t's own location -- t.Location(), not
// a hardcoded time.UTC. Which zone that resolves to is therefore a property of
// the Clock this service is built with, not of this function: the production
// Clock (adapter/clock.System.Now) already normalizes every reading to UTC
// (see its own doc comment), so in production this computes UTC midnight --
// but it gets there by asking the clock what "today" is, the same rule
// Request already follows for every other use of "now" in this file (never
// reach for wall-clock time directly; only the injected Clock, which is also
// why this codebase's tests can move the clock across this exact boundary
// without sleeping). "Midnight" is ambiguous across zones by nature, so this
// is recorded as a decision, not a default: were this service ever run with a
// Clock that did not normalize to UTC, the daily ceiling would reset at that
// Clock's own midnight, which is the point -- "today starting over" is
// whichever zone the Clock reports, not a fixed zone baked into this
// function.
func startOfDay(t time.Time) time.Time {
	y, m, d := t.Date()
	return time.Date(y, m, d, 0, 0, 0, 0, t.Location())
}

// sendAsync fires a send off the request path and returns immediately, for the
// same two reasons sendMagicLinkAsync does: timing parity between the branches,
// and the fact that Request's contract is "always nil, always silent", so a
// relay that is down must not become a caller-visible error on one branch only.
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
	return SignupPreview{Email: details.Email}, nil
}

// checkSignupLive reports why a sign-up token can no longer be used, keeping
// consumed and expired apart because the next action differs: a consumed token
// means the household exists and the answer is to sign in, an expired one means
// start again. The ordering rule lives in domain.TokenLifecycle, shared with
// invites.
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

// Complete turns a verified address into a household and signs its owner in.
//
// Every validation happens before the hash and before Provision, so a rejected
// form never consumes the token -- someone who mistypes their password can
// simply resubmit. The session is minted by the same package-level issueSession
// that SignIn and InviteService.Accept use, so a session from sign-up is
// indistinguishable from theirs, down to how it is created.
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
