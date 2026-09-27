// This file holds the identity slice's household ports: the household row,
// memberships, spaces and notification preferences. ports.go lists every
// ports file.

package usecase

import (
	"context"

	"github.com/andreasoentoro/hearth/api/internal/domain"
)

type HouseholdRepository interface {
	Get(ctx context.Context, householdID string) (domain.Household, error)
	Update(ctx context.Context, h domain.Household) (domain.Household, error)
	// Create writes a household from a fully-populated domain.Household. It
	// takes the value rather than (name, familyName). Don't depend on the
	// column defaults: a self-serve household needs different values than
	// Seed's. h.ID is ignored (the database assigns it), and h.FXRateMode
	// is ignored (the column default 'auto' is the only value the CHECK
	// constraint makes safe to assume at creation time).
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
	// UpdateWithCheck changes one membership's role and capabilities under a
	// household-wide rule held ACROSS the write: the implementation locks the
	// household, lists its memberships in the same transaction, and calls decide
	// with them. A nil error writes the role and capabilities decide returned;
	// any error writes nothing and is returned unchanged. A second writer blocks
	// on the lock and decides from the first writer's result, never a stale
	// copy.
	//
	// decide resolves what to write, not only whether: a PATCH that omits a
	// field means "keep what the membership has NOW", read under the lock, or a
	// capabilities-only change could overwrite a role change that committed in
	// between. Listing, checking and writing as three separate calls is NOT
	// equivalent either: two owners demoting each other at the same moment could
	// each see "another owner remains", both commit, and leave the household
	// with no owner -- the rule itself is the caller's
	// (domain.ValidateMembershipChange), this port only owns the transaction and
	// the lock.
	//
	// decide MUST BE PURE: no I/O, no repository calls. It runs inside an open
	// transaction holding the household lock and one pool connection; asking the
	// pool for a second one here can deadlock it under load, the way
	// VisionRepo.Save once did (see adapter/postgres/agreement_write_repo.go).
	// There is deliberately no unguarded Update -- every role change can affect
	// the last-owner rule -- and a membership that is not this household's is
	// domain.ErrNotFound.
	UpdateWithCheck(ctx context.Context, householdID, membershipID string, decide func(current []domain.Membership) (domain.Role, domain.Capabilities, error)) error
	// DeleteWithCheck is the same guarantee for removing a membership. check
	// receives the memberships as they are BEFORE the removal
	// (domain.ValidateMembershipRemoval works out what would remain). The
	// same rules apply: check must be pure (no I/O, no repository calls),
	// there is no unguarded Delete, and a membership that is not this
	// household's is domain.ErrNotFound.
	DeleteWithCheck(ctx context.Context, householdID, membershipID string, check func(current []domain.Membership) error) error
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
