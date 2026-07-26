package postgres

import (
	"context"

	"github.com/andreasoentoro/hearth/api/internal/adapter/postgres/sqlcgen"
	"github.com/andreasoentoro/hearth/api/internal/domain"
	"github.com/andreasoentoro/hearth/api/internal/usecase"
)

type MembershipRepo struct{ q *sqlcgen.Queries }

func NewMembershipRepo(db *DB) *MembershipRepo { return &MembershipRepo{q: sqlcgen.New(db.Pool())} }

func (r *MembershipRepo) List(ctx context.Context, householdID string) ([]usecase.MemberView, error) {
	rows, err := r.q.ListMemberships(ctx, uuid(householdID))
	if err != nil {
		return nil, translate(err, "list memberships")
	}
	views := make([]usecase.MemberView, len(rows))
	for i, row := range rows {
		views[i] = usecase.MemberView{
			Membership: domain.Membership{
				ID:           uuidToString(row.ID),
				HouseholdID:  uuidToString(row.HouseholdID),
				UserID:       uuidToString(row.UserID),
				Role:         toRole(row.Role),
				Capabilities: toCapabilities(row.Capabilities),
			},
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
	return domain.Membership{
		ID:           uuidToString(row.ID),
		HouseholdID:  uuidToString(row.HouseholdID),
		UserID:       uuidToString(row.UserID),
		Role:         toRole(row.Role),
		Capabilities: toCapabilities(row.Capabilities),
	}, nil
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
	return domain.Membership{
		ID:           uuidToString(row.ID),
		HouseholdID:  uuidToString(row.HouseholdID),
		UserID:       uuidToString(row.UserID),
		Role:         toRole(row.Role),
		Capabilities: toCapabilities(row.Capabilities),
	}, nil
}

func (r *MembershipRepo) Update(ctx context.Context, householdID, membershipID string, role domain.Role, caps domain.Capabilities) error {
	return translate(r.q.UpdateMembership(ctx, sqlcgen.UpdateMembershipParams{
		HouseholdID:  uuid(householdID),
		ID:           uuid(membershipID),
		Role:         string(role),
		Capabilities: caps.Strings(),
	}), "update membership")
}

func (r *MembershipRepo) Delete(ctx context.Context, householdID, membershipID string) error {
	return translate(r.q.DeleteMembership(ctx, sqlcgen.DeleteMembershipParams{
		HouseholdID: uuid(householdID),
		ID:          uuid(membershipID),
	}), "delete membership")
}
