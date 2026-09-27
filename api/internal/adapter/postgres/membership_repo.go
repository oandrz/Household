package postgres

import (
	"context"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/andreasoentoro/hearth/api/internal/adapter/postgres/sqlcgen"
	"github.com/andreasoentoro/hearth/api/internal/domain"
	"github.com/andreasoentoro/hearth/api/internal/usecase"
)

// MembershipRepo keeps the pool alongside the pool-backed *sqlcgen.Queries,
// like HoldingEventRepo, because UpdateWithCheck and DeleteWithCheck each
// begin their own transaction -- something a *sqlcgen.Queries built once at
// construction time cannot do on its own.
type MembershipRepo struct {
	q    *sqlcgen.Queries
	pool *pgxpool.Pool
}

func NewMembershipRepo(db *DB) *MembershipRepo {
	return &MembershipRepo{q: sqlcgen.New(db.Pool()), pool: db.Pool()}
}

func (r *MembershipRepo) List(ctx context.Context, householdID string) ([]usecase.MemberView, error) {
	rows, err := r.q.ListMemberships(ctx, uuid(householdID))
	if err != nil {
		return nil, translate(err, "list memberships")
	}
	views := make([]usecase.MemberView, len(rows))
	for i, row := range rows {
		membership, err := toMembership(row.ID, row.HouseholdID, row.UserID, row.Role, row.Capabilities)
		if err != nil {
			return nil, err
		}
		views[i] = usecase.MemberView{
			Membership: membership,
			User: domain.User{
				ID:            uuidToString(row.UserID),
				Email:         stringOrEmpty(row.Email),
				DisplayName:   row.DisplayName,
				AvatarInitial: row.AvatarInitial,
			},
		}
	}
	return views, nil
}

func (r *MembershipRepo) ByUser(ctx context.Context, userID string) (domain.Membership, error) {
	row, err := r.q.GetMembershipByUser(ctx, uuid(userID))
	if err != nil {
		return domain.Membership{}, translate(err, "get membership by user")
	}
	return toMembership(row.ID, row.HouseholdID, row.UserID, row.Role, row.Capabilities)
}

// Create passes m straight through to Postgres without re-checking the
// capability rules domain.NewMembership already enforces: the database's
// CHECK constraints (owners_hold_all_capabilities,
// limited_members_have_no_marriage, capabilities_are_known) are the second
// gate for exactly this reason, and a caller that bypassed the first --
// building a Membership struct literal directly, which the domain doc
// comment on NewMembership explicitly allows -- must see that second gate's
// error rather than have this repository silently re-validate and swallow it.
func (r *MembershipRepo) Create(ctx context.Context, m domain.Membership) (domain.Membership, error) {
	row, err := r.q.CreateMembership(ctx, sqlcgen.CreateMembershipParams{
		HouseholdID:  uuid(m.HouseholdID),
		UserID:       uuid(m.UserID),
		Role:         string(m.Role),
		Capabilities: m.Capabilities.Strings(),
	})
	if err != nil {
		return domain.Membership{}, translate(err, "create membership")
	}
	return toMembership(row.ID, row.HouseholdID, row.UserID, row.Role, row.Capabilities)
}

// UpdateWithCheck changes one membership with the household's rules held
// across the write.
//
// It locks the household row, lists the household's memberships inside the
// same transaction, and hands them to check -- the caller's own rule, which is
// domain.ValidateMembershipChange. Only if check accepts does the update
// happen, and the lock is not released until the transaction commits. A
// second writer blocks on the lock and therefore checks the FIRST one's
// result, not a stale copy of it.
//
// Without this, two owners demoting each other at the same moment each see
// "another owner remains", both commit, and the household is left with no
// owner at all -- nobody who can manage members, invites or settings. The
// rule stays in the domain; this method owns the transaction and the lock.
func (r *MembershipRepo) UpdateWithCheck(
	ctx context.Context,
	householdID, membershipID string,
	role domain.Role, caps domain.Capabilities,
	check func([]domain.Membership) error,
) error {
	return pgx.BeginFunc(ctx, r.pool, func(tx pgx.Tx) error {
		q := r.q.WithTx(tx)
		current, err := lockAndListMembershipsTx(ctx, q, householdID)
		if err != nil {
			return err
		}
		if err := check(current); err != nil {
			return err
		}
		n, err := q.UpdateMembership(ctx, sqlcgen.UpdateMembershipParams{
			HouseholdID:  uuid(householdID),
			ID:           uuid(membershipID),
			Role:         string(role),
			Capabilities: caps.Strings(),
		})
		if err != nil {
			return translate(err, "update membership")
		}
		// An UPDATE that matched nothing is not success: the membership is
		// not this household's, and the caller must be told so.
		if n == 0 {
			return domain.ErrNotFound
		}
		return nil
	})
}

// DeleteWithCheck is UpdateWithCheck's guarantee for removing a membership.
// check receives the memberships as they are before the removal.
func (r *MembershipRepo) DeleteWithCheck(
	ctx context.Context,
	householdID, membershipID string,
	check func([]domain.Membership) error,
) error {
	return pgx.BeginFunc(ctx, r.pool, func(tx pgx.Tx) error {
		q := r.q.WithTx(tx)
		current, err := lockAndListMembershipsTx(ctx, q, householdID)
		if err != nil {
			return err
		}
		if err := check(current); err != nil {
			return err
		}
		n, err := q.DeleteMembership(ctx, sqlcgen.DeleteMembershipParams{
			HouseholdID: uuid(householdID),
			ID:          uuid(membershipID),
		})
		if err != nil {
			return translate(err, "delete membership")
		}
		if n == 0 {
			return domain.ErrNotFound
		}
		return nil
	})
}

// lockAndListMembershipsTx is the shared first half of the two guarded
// writes: take the household's membership lock, then read its memberships.
// The ORDER matters. The list is a new statement run after the lock is held,
// so it sees whatever an earlier writer committed while this one waited.
func lockAndListMembershipsTx(ctx context.Context, q *sqlcgen.Queries, householdID string) ([]domain.Membership, error) {
	// No row means no such household: translate turns that into
	// domain.ErrNotFound, which is also the honest answer for its members.
	if _, err := q.LockHouseholdMemberships(ctx, uuid(householdID)); err != nil {
		return nil, translate(err, "lock household memberships")
	}
	rows, err := q.ListMemberships(ctx, uuid(householdID))
	if err != nil {
		return nil, translate(err, "list memberships")
	}
	out := make([]domain.Membership, len(rows))
	for i, row := range rows {
		m, err := toMembership(row.ID, row.HouseholdID, row.UserID, row.Role, row.Capabilities)
		if err != nil {
			return nil, err
		}
		out[i] = m
	}
	return out, nil
}
