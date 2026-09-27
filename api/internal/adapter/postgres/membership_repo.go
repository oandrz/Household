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
// capability rules domain.NewMembership already enforces. The database's
// CHECK constraints (owners_hold_all_capabilities,
// limited_members_have_no_marriage, capabilities_are_known) are the second
// gate for exactly that case: a caller that built a Membership literal
// directly -- which NewMembership's own doc comment allows -- must see
// that gate's error rather than have this repository silently re-validate
// and swallow it.
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
// same transaction, and hands them to decide -- the caller's own code, which
// resolves the PATCH and runs domain.ValidateMembershipChange. Only if decide
// accepts does the update happen, and the lock is not released until the
// transaction commits. A second writer blocks on the lock and therefore
// decides from the FIRST one's result, not a stale copy of it.
//
// Without this, two owners demoting each other at the same moment each see
// "another owner remains", both commit, and the household is left with no
// owner at all -- nobody who can manage members, invites or settings. The
// rule stays in the domain; this method owns the transaction and the lock.
func (r *MembershipRepo) UpdateWithCheck(
	ctx context.Context,
	householdID, membershipID string,
	decide func([]domain.Membership) (domain.Role, domain.Capabilities, error),
) error {
	return r.writeUnderHouseholdLock(ctx, householdID, "update membership",
		func(q *sqlcgen.Queries, current []domain.Membership) (int64, error) {
			role, caps, err := decide(current)
			if err != nil {
				return 0, err
			}
			n, err := q.UpdateMembership(ctx, sqlcgen.UpdateMembershipParams{
				HouseholdID:  uuid(householdID),
				ID:           uuid(membershipID),
				Role:         string(role),
				Capabilities: caps.Strings(),
			})
			return n, translate(err, "update membership")
		})
}

// DeleteWithCheck is UpdateWithCheck's guarantee for removing a membership.
// check receives the memberships as they are before the removal.
func (r *MembershipRepo) DeleteWithCheck(
	ctx context.Context,
	householdID, membershipID string,
	check func([]domain.Membership) error,
) error {
	return r.writeUnderHouseholdLock(ctx, householdID, "delete membership",
		func(q *sqlcgen.Queries, current []domain.Membership) (int64, error) {
			if err := check(current); err != nil {
				return 0, err
			}
			n, err := q.DeleteMembership(ctx, sqlcgen.DeleteMembershipParams{
				HouseholdID: uuid(householdID),
				ID:          uuid(membershipID),
			})
			return n, translate(err, "delete membership")
		})
}

// writeUnderHouseholdLock is the one shape both guarded writes share: begin
// a transaction, take the household's membership lock, list the
// memberships, then hand them to write, which runs the caller's check and
// the one statement. A write that touches no row means the membership is
// not this household's, so it comes back as domain.ErrNotFound rather than
// a silent success.
//
// Errors are handled in two groups on purpose. One from inside the
// transaction is already a domain error -- the caller's check, or
// translate -- and comes back unchanged, as the port promises. Anything
// else from pgx.BeginFunc is BEGIN or COMMIT itself failing, a raw driver
// error that must be translated here so no pgx type leaves the adapter.
func (r *MembershipRepo) writeUnderHouseholdLock(
	ctx context.Context,
	householdID, op string,
	write func(q *sqlcgen.Queries, current []domain.Membership) (rowsAffected int64, err error),
) error {
	var inner error
	err := pgx.BeginFunc(ctx, r.pool, func(tx pgx.Tx) error {
		q := r.q.WithTx(tx)
		current, err := lockAndListMembershipsTx(ctx, q, householdID)
		if err != nil {
			inner = err
			return err
		}
		n, err := write(q, current)
		if err == nil && n == 0 {
			err = domain.ErrNotFound
		}
		inner = err
		return err
	})
	if inner != nil {
		return inner
	}
	return translate(err, op)
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
