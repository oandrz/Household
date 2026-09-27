// This file holds the identity slice's ports: users, sessions, magic links,
// personal API tokens and the sign-in lockout ledger. ports.go lists every
// ports file.

package usecase

import (
	"context"
	"time"

	"github.com/andreasoentoro/hearth/api/internal/domain"
)

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

type SessionRecord struct {
	UserID      string
	HouseholdID string
	ExpiresAt   time.Time
	// AdminGrantExpiresAt is nil for every ordinary session. It is non-nil
	// only between a successful POST /admin/session and that grant's expiry.
	AdminGrantExpiresAt *time.Time
	// LastSeenAt is nil until the first Touch after migration 00013.
	// Readers treat nil as "use CreatedAt" -- see the migration's comment.
	LastSeenAt *time.Time
}

type SessionRepository interface {
	Create(ctx context.Context, tokenHash []byte, userID, householdID string, expiresAt time.Time) error
	ByTokenHash(ctx context.Context, tokenHash []byte) (SessionRecord, error)
	Extend(ctx context.Context, tokenHash []byte, expiresAt time.Time) error
	RevokeByToken(ctx context.Context, tokenHash []byte) error
	RevokeAllForUser(ctx context.Context, userID string) error
	// GrantAdmin stamps this session's admin re-auth grant. A nil expiry
	// clears it. It writes one column: session extension and this must not
	// overwrite each other.
	GrantAdmin(ctx context.Context, tokenHash []byte, expiresAt *time.Time) error
	// Touch records that the session was used at `at`. It writes one
	// column, last_seen_at, and must not be folded into Extend or
	// GrantAdmin: each of the three owns its column so none can overwrite
	// another's. Callers throttle it (middleware_session.go); the
	// repository does not.
	Touch(ctx context.Context, tokenHash []byte, at time.Time) error
}

type MagicLinkRepository interface {
	Create(ctx context.Context, userID string, tokenHash []byte, expiresAt time.Time) error
	Consume(ctx context.Context, tokenHash []byte) (userID string, err error)
	CountSince(ctx context.Context, email string, since time.Time) (int, error)
}

// APITokenRepository stores a member's long-lived credentials. Only the
// hash of a token is ever persisted; the raw value is shown once and never
// written anywhere.
type APITokenRepository interface {
	// Create stores one token and returns the row. tokenHash is
	// TokenGenerator.HashToken of the raw secret; prefix is the raw
	// secret's opening characters, for listing.
	Create(ctx context.Context, tokenHash []byte, prefix string, t domain.APIToken) (domain.APIToken, error)
	// ByTokenHash resolves a live token: revoked or expired is
	// domain.ErrNotFound, indistinguishable from unknown. The middleware
	// depends on that -- it never checks expiry itself.
	ByTokenHash(ctx context.Context, tokenHash []byte) (domain.APIToken, error)
	// ListForUser returns one person's own tokens, newest first, with
	// revoked ones excluded but expired ones kept in -- unlike
	// HouseholdTokenLister.ListForHousehold (access_list.go), which answers
	// "what can get in right now" and drops expired rows, this answers
	// "what has this member ever minted".
	ListForUser(ctx context.Context, userID string) ([]domain.APIToken, error)
	// Revoke stamps one token, scoped to its owner: another user's id, an
	// unknown id and an already-revoked token are all domain.ErrNotFound.
	Revoke(ctx context.Context, userID, tokenID string) error
	// RevokeAllForUser is the "this person is gone" call MemberService
	// makes beside SessionRepository.RevokeAllForUser.
	RevokeAllForUser(ctx context.Context, userID string) error
	// Touch records the last use. Callers throttle it; the repository
	// does not.
	Touch(ctx context.Context, tokenID string, at time.Time) error
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
