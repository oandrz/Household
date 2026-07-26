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
type StoredUser struct {
	domain.User
	PasswordHash string
}

type UserRepository interface {
	ByEmail(ctx context.Context, email string) (StoredUser, error)
	ByID(ctx context.Context, id string) (StoredUser, error)
	// Create writes passwordHash following the same "" <-> NULL convention
	// as StoredUser.PasswordHash: passing "" stores SQL NULL, not an empty
	// string in the column. Children (members with no login of their own)
	// are created this way, with passwordHash == "".
	Create(ctx context.Context, email, passwordHash, displayName string) (domain.User, error)
	SetPasswordHash(ctx context.Context, userID, hash string) error
}

type HouseholdRepository interface {
	Get(ctx context.Context, householdID string) (domain.Household, error)
	Update(ctx context.Context, h domain.Household) (domain.Household, error)
	Create(ctx context.Context, name, familyName string) (domain.Household, error)
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

type InviteRepository interface {
	Create(ctx context.Context, householdID, email, name string, role domain.Role,
		caps domain.Capabilities, tokenHash []byte, invitedBy string, expiresAt time.Time) (string, error)
	ByTokenHash(ctx context.Context, tokenHash []byte) (InviteDetails, error)
	// Returns domain.ErrInviteAlreadyAccepted when the invite was already
	// accepted or has expired — the guard lives in the SQL, not in the caller.
	MarkAccepted(ctx context.Context, inviteID string) error
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
