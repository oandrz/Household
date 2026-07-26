package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/andreasoentoro/hearth/api/internal/adapter/postgres/sqlcgen"
	"github.com/andreasoentoro/hearth/api/internal/domain"
	"github.com/andreasoentoro/hearth/api/internal/usecase"
)

type InviteRepo struct{ q *sqlcgen.Queries }

func NewInviteRepo(db *DB) *InviteRepo { return &InviteRepo{q: sqlcgen.New(db.Pool())} }

func (r *InviteRepo) Create(ctx context.Context, householdID, email, name string, role domain.Role,
	caps domain.Capabilities, tokenHash []byte, invitedBy string, expiresAt time.Time) (string, error) {
	id, err := r.q.CreateInvite(ctx, sqlcgen.CreateInviteParams{
		HouseholdID:  uuid(householdID),
		Email:        email,
		Name:         name,
		Role:         string(role),
		Capabilities: caps.Strings(),
		TokenHash:    tokenHash,
		InvitedBy:    uuid(invitedBy),
		ExpiresAt:    timestamptz(expiresAt),
	})
	if err != nil {
		return "", translate(err, "create invite")
	}
	return uuidToString(id), nil
}

func (r *InviteRepo) ByTokenHash(ctx context.Context, tokenHash []byte) (usecase.InviteDetails, error) {
	row, err := r.q.GetInviteByTokenHash(ctx, tokenHash)
	if err != nil {
		return usecase.InviteDetails{}, translate(err, "get invite by token hash")
	}
	return usecase.InviteDetails{
		ID:           uuidToString(row.ID),
		HouseholdID:  uuidToString(row.HouseholdID),
		Email:        row.Email,
		Name:         row.Name,
		Role:         toRole(row.Role),
		Capabilities: toCapabilities(row.Capabilities),
		FamilyName:   row.FamilyName,
		InviterName:  row.InviterName,
		ExpiresAt:    timeOf(row.ExpiresAt),
		AcceptedAt:   timePtrOf(row.AcceptedAt),
	}, nil
}

// MarkAccepted deliberately does not go through translate. MarkInviteAccepted
// is a guarded atomic update (accepted_at IS NULL AND expires_at > now()), so
// zero rows means the invite was already accepted or has expired -- not that
// no invite with this id ever existed. translate's generic
// pgx.ErrNoRows -> domain.ErrNotFound mapping would misreport that as "no
// such invite"; the task's constraints call for domain.ErrInviteAlreadyAccepted
// instead, so that mapping is applied directly here.
func (r *InviteRepo) MarkAccepted(ctx context.Context, inviteID string) error {
	_, err := r.q.MarkInviteAccepted(ctx, uuid(inviteID))
	if err == nil {
		return nil
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.ErrInviteAlreadyAccepted
	}
	return fmt.Errorf("mark invite accepted: %w", err)
}
