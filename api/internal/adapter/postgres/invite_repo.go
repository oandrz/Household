package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/andreasoentoro/hearth/api/internal/adapter/postgres/sqlcgen"
	"github.com/andreasoentoro/hearth/api/internal/domain"
	"github.com/andreasoentoro/hearth/api/internal/usecase"
)

// InviteRepo keeps the pool alongside the pool-backed *sqlcgen.Queries every
// other repository is content with, because Accept needs to begin its own
// transaction -- something a *sqlcgen.Queries built once at construction time
// cannot do on its own.
type InviteRepo struct {
	q    *sqlcgen.Queries
	pool *pgxpool.Pool
}

func NewInviteRepo(db *DB) *InviteRepo {
	return &InviteRepo{q: sqlcgen.New(db.Pool()), pool: db.Pool()}
}

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

// Accept runs the guarded MarkInviteAccepted update, the user insert, and the
// membership insert in a single transaction, so a household of two is never
// left with an invite that can neither be accepted nor retried.
//
// MarkInviteAccepted runs first, before either insert: it is what makes a
// concurrent second acceptance of the same invite fail cheaply, as
// domain.ErrInviteAlreadyAccepted, before any row is written -- rather than
// failing with a raw unique-constraint error from CreateUser colliding on the
// invite's email address, which the first, successful acceptance already
// claimed.
func (r *InviteRepo) Accept(ctx context.Context, inviteID, email, passwordHash, displayName string,
	householdID string, role domain.Role, caps domain.Capabilities) (usecase.AcceptedInvite, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return usecase.AcceptedInvite{}, fmt.Errorf("begin accept invite transaction: %w", err)
	}
	// A no-op once Commit has succeeded; the error from a post-commit
	// Rollback call is deliberately discarded, matching the standard
	// defer-rollback pattern for pgx transactions.
	defer func() { _ = tx.Rollback(ctx) }()

	q := r.q.WithTx(tx)

	if _, err := q.MarkInviteAccepted(ctx, uuid(inviteID)); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return usecase.AcceptedInvite{}, domain.ErrInviteAlreadyAccepted
		}
		return usecase.AcceptedInvite{}, fmt.Errorf("mark invite accepted: %w", err)
	}

	userRow, err := q.CreateUser(ctx, sqlcgen.CreateUserParams{
		Email:         nullableText(email),
		PasswordHash:  nullableText(passwordHash),
		DisplayName:   displayName,
		AvatarInitial: initialOf(displayName),
	})
	if err != nil {
		return usecase.AcceptedInvite{}, translate(err, "create user for invite acceptance")
	}

	membershipRow, err := q.CreateMembership(ctx, sqlcgen.CreateMembershipParams{
		HouseholdID:  uuid(householdID),
		UserID:       userRow.ID,
		Role:         string(role),
		Capabilities: caps.Strings(),
	})
	if err != nil {
		return usecase.AcceptedInvite{}, translate(err, "create membership for invite acceptance")
	}

	if err := tx.Commit(ctx); err != nil {
		return usecase.AcceptedInvite{}, fmt.Errorf("commit accept invite transaction: %w", err)
	}

	return usecase.AcceptedInvite{
		UserID:       uuidToString(userRow.ID),
		MembershipID: uuidToString(membershipRow.ID),
		HouseholdID:  uuidToString(membershipRow.HouseholdID),
	}, nil
}
