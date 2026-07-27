// Package usecase holds the application services. It depends on domain and on
// the port interfaces declared here — never on an adapter.
package usecase

import (
	"context"
	"math"
	"time"

	"github.com/andreasoentoro/hearth/api/internal/domain"
)

type Clock interface {
	Now() time.Time
}

type PasswordHasher interface {
	Hash(plain string) (string, error)
	Verify(plain, encoded string) bool
}

type TokenGenerator interface {
	NewToken() (raw string, hash []byte, err error)
	HashToken(raw string) []byte
}

type Mailer interface {
	SendMagicLink(ctx context.Context, to, name, url string) error
	SendInvite(ctx context.Context, to, name, inviterName, url string) error
	// SendSignupLink mails the create-household link. There is no name
	// parameter: at sign-up-request time nobody has told us one, and inventing
	// a greeting from the local part of the address would read worse than
	// having none.
	SendSignupLink(ctx context.Context, to, url string) error
	// SendSignupForExistingAccount mails "you already have an account" with no
	// token. It is as load-bearing as SendSignupLink, not a courtesy: if only
	// the fresh-address branch sent mail, the *absence* of an email would tell
	// anyone who can observe the mailbox that the address is registered, which
	// is the oracle the identical 202 exists to prevent.
	SendSignupForExistingAccount(ctx context.Context, to, signInURL string) error
}

// StoredUser carries the password hash, which never leaves the usecase layer.
//
// users.password_hash is nullable in the database, and sqlc generates *string
// for it — but PasswordHash here is a plain string by design, not an
// unwritten gap the Postgres implementation has to paper over. The
// convention, both directions: SQL NULL maps to "", and "" maps to SQL NULL.
// A user created without credentials (e.g. an invited member with no
// password yet) has PasswordHash == "", and Task 12 already treats that
// empty string as "cannot sign in" — the repository must not turn a NULL
// into any other sentinel.
//
// The embedded domain.User.Email follows the identical convention, for the
// identical reason: users.email is also nullable (and citext UNIQUE, so
// storing "" rather than NULL for two credential-less members would collide
// on the unique index where two NULLs do not). A member created without an
// email of their own — the same invited-member-with-no-login case — has
// Email == "", and the repository must round-trip SQL NULL to "" and "" to
// SQL NULL there exactly as it does for PasswordHash.
type StoredUser struct {
	domain.User
	PasswordHash string
}

type UserRepository interface {
	ByEmail(ctx context.Context, email string) (StoredUser, error)
	ByID(ctx context.Context, id string) (StoredUser, error)
	// Create writes email and passwordHash following the same "" <-> NULL
	// convention as StoredUser.PasswordHash (and, by the same reasoning,
	// StoredUser's embedded domain.User.Email): passing "" for either stores
	// SQL NULL, not an empty string in the column. Children (members with no
	// login of their own) are created this way, with email == "" and
	// passwordHash == "".
	Create(ctx context.Context, email, passwordHash, displayName string) (domain.User, error)
	SetPasswordHash(ctx context.Context, userID, hash string) error
	// CreateWithMembership creates the user and their membership in one
	// transaction. Either both happen or neither does: a partial failure leaves an
	// orphaned user with no membership, and because a child's email is NULL there
	// is no unique constraint to make the retry fail loudly — it silently creates
	// another orphan each time.
	CreateWithMembership(ctx context.Context, email, passwordHash, displayName string,
		m domain.Membership) (domain.User, domain.Membership, error)
	// FindOrphanedChild returns the credential-less user (no email, no
	// password) with this exact display name that currently holds no
	// membership anywhere, if one exists. It reports domain.ErrNotFound when
	// there is none. This is the state removing a membership leaves behind
	// without deleting the user row underneath it -- a credential-less
	// member has no email for a unique constraint to protect the way a real
	// address does, so nothing else stops a second create under the same
	// name from silently duplicating one.
	FindOrphanedChild(ctx context.Context, displayName string) (domain.User, error)
}

type HouseholdRepository interface {
	Get(ctx context.Context, householdID string) (domain.Household, error)
	Update(ctx context.Context, h domain.Household) (domain.Household, error)
	// Create writes a household from a fully-populated domain.Household.
	// It takes the value rather than (name, familyName) so no caller depends on
	// the table's currency column defaults -- Seed used to, silently, and a
	// self-serve household needs different values.
	//
	// h.ID is ignored (the database assigns it) and h.FXRateMode is ignored
	// (the column default 'auto' is the only value the CHECK constraint makes
	// safe to assume at creation time).
	Create(ctx context.Context, h domain.Household) (domain.Household, error)
}

// MemberView is a membership joined to its user, which is what every consumer
// of the members list actually wants.
type MemberView struct {
	Membership domain.Membership
	User       domain.User
}

type MembershipRepository interface {
	List(ctx context.Context, householdID string) ([]MemberView, error)
	// ByUser is the one method that cannot take a household scope, because
	// sign-in resolves the household from it. It is therefore the seam where
	// multi-tenancy will need attention: today it returns the single membership
	// a user has, and the query's LIMIT 1 would pick arbitrarily if a user ever
	// belonged to two households.
	ByUser(ctx context.Context, userID string) (domain.Membership, error)
	Create(ctx context.Context, m domain.Membership) (domain.Membership, error)
	Update(ctx context.Context, householdID, membershipID string, role domain.Role, caps domain.Capabilities) error
	Delete(ctx context.Context, householdID, membershipID string) error
}

type SessionRecord struct {
	UserID      string
	HouseholdID string
	ExpiresAt   time.Time
}

type SessionRepository interface {
	Create(ctx context.Context, tokenHash []byte, userID, householdID string, expiresAt time.Time) error
	ByTokenHash(ctx context.Context, tokenHash []byte) (SessionRecord, error)
	Extend(ctx context.Context, tokenHash []byte, expiresAt time.Time) error
	RevokeByToken(ctx context.Context, tokenHash []byte) error
	RevokeAllForUser(ctx context.Context, userID string) error
}

type MagicLinkRepository interface {
	Create(ctx context.Context, userID string, tokenHash []byte, expiresAt time.Time) error
	Consume(ctx context.Context, tokenHash []byte) (userID string, err error)
	CountSince(ctx context.Context, email string, since time.Time) (int, error)
}

type LoginAttemptRepository interface {
	Record(ctx context.Context, householdID, userID *string, email string, succeeded bool, at time.Time) error
	FailuresSince(ctx context.Context, householdID string, since time.Time) ([]time.Time, error)
	// FailuresSinceForEmail counts attempts by address rather than by household.
	// Sign-in uses it for addresses that match no user, so a stranger sees the
	// same countdown a member does and cannot tell the two apart.
	FailuresSinceForEmail(ctx context.Context, email string, since time.Time) ([]time.Time, error)
	ClearFailures(ctx context.Context, householdID string) error
	// Prune deletes attempts older than before, including the
	// NULL-household_id rows an unknown-address attempt records -- which
	// ClearFailures cannot reach, because it is scoped WHERE household_id = $1
	// and that never matches NULL.
	//
	// The caller is responsible for a cutoff well outside
	// domain.LockoutPolicy.Window. Deleting a row still inside that window
	// would clear a live lockout: a security regression dressed as a cleanup.
	Prune(ctx context.Context, before time.Time) (int64, error)
}

type InviteDetails struct {
	ID           string
	HouseholdID  string
	Email        string
	Name         string
	Role         domain.Role
	Capabilities domain.Capabilities
	FamilyName   string
	InviterName  string
	ExpiresAt    time.Time
	AcceptedAt   *time.Time
}

// AcceptedInvite is what a successful acceptance produces.
type AcceptedInvite struct {
	UserID       string
	MembershipID string
	HouseholdID  string
}

type InviteRepository interface {
	Create(ctx context.Context, householdID, email, name string, role domain.Role,
		caps domain.Capabilities, tokenHash []byte, invitedBy string, expiresAt time.Time) (string, error)
	ByTokenHash(ctx context.Context, tokenHash []byte) (InviteDetails, error)
	// LiveInviteForEmail answers "is there already something usable in
	// flight for this address in this household" -- neither accepted nor
	// expired -- without requiring the raw token that produced it, which is
	// never persisted anywhere. It reports domain.ErrNotFound when there is
	// none, exactly as ByTokenHash does for an unknown token: "no live
	// invite" and "no invite at all" are the same absence from a caller's
	// point of view.
	LiveInviteForEmail(ctx context.Context, householdID, email string) (InviteDetails, error)
	// Returns domain.ErrInviteAlreadyAccepted when the invite was already
	// accepted or has expired — the guard lives in the SQL, not in the caller.
	MarkAccepted(ctx context.Context, inviteID string) error
	// Accept creates the user, creates the membership, and marks the invite
	// accepted in one transaction. Either all three happen or none do -- a
	// partial acceptance would leave an orphaned user occupying the unique
	// email index, which makes the invite permanently unusable: a retry could
	// never create a second user with that address, so there would be no path
	// forward short of manual SQL. Returns domain.ErrInviteAlreadyAccepted,
	// with nothing written, when the invite was already accepted or has
	// expired.
	Accept(ctx context.Context, inviteID, email, passwordHash, displayName string,
		householdID string, role domain.Role, caps domain.Capabilities) (AcceptedInvite, error)
}

// SignupDetails is a pending sign-up, read back by token.
type SignupDetails struct {
	ID         string
	Email      string
	ExpiresAt  time.Time
	ConsumedAt *time.Time
}

// ProvisionedHousehold is what a successful provision produces.
type ProvisionedHousehold struct {
	UserID       string
	HouseholdID  string
	MembershipID string
}

type SignupRepository interface {
	Create(ctx context.Context, email string, tokenHash []byte, expiresAt time.Time) error
	// CreateConsumed writes a signup row that is already consumed at insert
	// time -- consumed_at is set to now in the same statement, not stamped
	// afterward. SignupService.Request calls this on the already-registered
	// branch instead of Create, so that branch also advances
	// CountForEmailSince/CountSince: those counters read every row in this
	// table regardless of consumed_at, but before this method existed,
	// nothing was ever written here for a registered address, so its
	// counters stayed at zero forever and signupPerHourLimit/
	// SignupGlobalDailyLimit never fired for that branch no matter how many
	// requests arrived (see the fix-round note in signup.go's Request doc
	// comment). A row created this way can never provision anything --
	// Provision's guarded UPDATE (ConsumeSignup) requires consumed_at IS
	// NULL -- so it exists solely to be counted, and its token is never
	// mailed to anyone.
	CreateConsumed(ctx context.Context, email string, tokenHash []byte, expiresAt time.Time) error
	ByTokenHash(ctx context.Context, tokenHash []byte) (SignupDetails, error)
	// CountForEmailSince counts sign-up requests for one address since a
	// cutoff, over rows written by both Create and CreateConsumed. Unlike
	// MagicLinkRepository.CountSince it does not join through users -- there
	// is no user to join to -- so it can report a non-zero count for an
	// address with no account, and must: a limit that could only be hit by
	// a registered address would itself distinguish the two.
	CountForEmailSince(ctx context.Context, email string, since time.Time) (int, error)
	// CountSince counts every sign-up request since a cutoff, for the global
	// daily mail ceiling. It reads the table rather than an in-memory counter
	// so restarting the API cannot reset the ceiling.
	CountSince(ctx context.Context, since time.Time) (int, error)
	// Provision creates the household, the owner user, the owner membership,
	// every builtin space and the notification preferences, and stamps the
	// signup consumed -- all in one transaction. Either all of it happens or
	// none of it does.
	//
	// The owner's email address is read from the signup row this transaction is
	// already touching; it is deliberately NOT a parameter. The address that
	// gets an account must be the one the mailed token actually proved, and
	// passing it in would let a caller substitute a different one between
	// SignupService.Complete's read and this write.
	//
	// A partial provision leaves a users row occupying users.email's unique
	// index with no membership under it, which makes that address permanently
	// unable to sign up again: a retry could never create a second user with
	// it. That is the same failure InviteRepository.Accept's doc comment
	// describes, and this method exists for the same reason.
	//
	// Returns domain.ErrTokenExpired, with nothing written, when the signup is
	// no longer usable -- consumed or expired. Like InviteRepository.Accept's
	// guarded UPDATE this collapses the two cases into one zero-rows result and
	// cannot tell them apart; SignupService.Complete's own TokenLifecycle read
	// is what distinguishes them for a caller, and this answer is authoritative
	// only for the race window between that read and this write.
	Provision(ctx context.Context, signupID, passwordHash string,
		b HouseholdBlueprint) (ProvisionedHousehold, error)
	// Prune deletes consumed and expired rows older than before.
	Prune(ctx context.Context, before time.Time) (int64, error)
}

type SpaceRepository interface {
	List(ctx context.Context, householdID string) ([]domain.Space, error)
	Create(ctx context.Context, s domain.Space) (domain.Space, error)
	NextPosition(ctx context.Context, householdID string) (int, error)
}

type NotificationPreferences struct {
	BillReminders   bool
	OverspendAlerts bool
	RetroReminder   bool
	WeeklyDigest    bool
}

type NotificationRepository interface {
	Get(ctx context.Context, householdID string) (NotificationPreferences, error)
	Upsert(ctx context.Context, householdID string, p NotificationPreferences) (NotificationPreferences, error)
}

// Rate is a ratio, held as a fraction rather than a scaled decimal. SGD to IDR
// is {12410, 1}; IDR to SGD is {1, 12410}. A scaled decimal cannot represent
// the second direction — 0.0000806 truncates to zero at any sane scale — and
// IDR to SGD is precisely the direction the design's Finances screen uses.
type Rate struct {
	Numerator   int64
	Denominator int64
}

// Apply converts an amount of minor units, rounding half away from zero. It
// reports domain.ErrAmountOverflow rather than silently wrapping when
// minorUnits * r.Numerator would not fit in an int64 — the same failure mode
// domain.Money.Add already refuses to allow on this codebase's monetary
// path, and multiplication overflows far sooner than addition does.
func (r Rate) Apply(minorUnits int64) (int64, error) {
	if mulOverflows(minorUnits, r.Numerator) {
		return 0, domain.ErrAmountOverflow
	}
	num := minorUnits * r.Numerator
	half := r.Denominator / 2
	if num < 0 {
		return (num - half) / r.Denominator, nil
	}
	return (num + half) / r.Denominator, nil
}

// mulOverflows reports whether a*b would overflow an int64. It computes the
// product (which may wrap, but wrapping a signed integer is well-defined in
// Go, never a panic) and divides back by b: for any b != 0, (a*b)/b == a
// unless the multiplication actually wrapped. The one case that check misses
// is a==-1,b==math.MinInt64 (or the reverse): math.MinInt64 has no positive
// counterpart, so negating it wraps right back to math.MinInt64 and the
// divide-back reproduces a even though the multiplication did overflow —
// that pair is therefore checked explicitly, before the general rule runs.
func mulOverflows(a, b int64) bool {
	if a == 0 || b == 0 {
		return false
	}
	if (a == -1 && b == math.MinInt64) || (b == -1 && a == math.MinInt64) {
		return true
	}
	return (a*b)/b != a
}

// FXRateProvider converts between the household's primary and secondary
// currencies. The design labels the rate "auto"; a live provider replaces the
// static one without any caller changing.
type FXRateProvider interface {
	Rate(ctx context.Context, from, to string) (Rate, error)
}
