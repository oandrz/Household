package postgres

import (
	"context"
	"time"

	"github.com/andreasoentoro/hearth/api/internal/adapter/postgres/sqlcgen"
)

type MagicLinkRepo struct{ q *sqlcgen.Queries }

func NewMagicLinkRepo(db *DB) *MagicLinkRepo { return &MagicLinkRepo{q: sqlcgen.New(db.Pool())} }

func (r *MagicLinkRepo) Create(ctx context.Context, userID string, tokenHash []byte, expiresAt time.Time) error {
	return translate(r.q.CreateMagicLink(ctx, sqlcgen.CreateMagicLinkParams{
		UserID:    uuid(userID),
		TokenHash: tokenHash,
		ExpiresAt: timestamptz(expiresAt),
	}), "create magic link")
}

// Consume goes through translate like every other lookup here, so a used or
// unknown token surfaces as domain.ErrNotFound. ConsumeMagicLink's guard
// (consumed_at IS NULL AND expires_at > now()) is structurally the same
// "zero rows is ambiguous" shape as MarkInviteAccepted's, and domain.ErrTokenExpired
// reads like it was meant for exactly this path -- but the task's global
// constraints call out only the invite guard for the ErrNotFound carve-out,
// so this repository does not add a second one on its own authority. Flagged
// in the task report as a mapping Task 12 may want to reconsider.
func (r *MagicLinkRepo) Consume(ctx context.Context, tokenHash []byte) (string, error) {
	id, err := r.q.ConsumeMagicLink(ctx, tokenHash)
	if err != nil {
		return "", translate(err, "consume magic link")
	}
	return uuidToString(id), nil
}

func (r *MagicLinkRepo) CountSince(ctx context.Context, email string, since time.Time) (int, error) {
	count, err := r.q.CountRecentMagicLinks(ctx, sqlcgen.CountRecentMagicLinksParams{
		Email:     text(email),
		CreatedAt: timestamptz(since),
	})
	if err != nil {
		return 0, translate(err, "count recent magic links")
	}
	return int(count), nil
}
