package postgres

import (
	"context"
	"time"

	"github.com/andreasoentoro/hearth/api/internal/adapter/postgres/sqlcgen"
	"github.com/andreasoentoro/hearth/api/internal/usecase"
)

var _ usecase.TelegramLinkRepository = (*TelegramLinkRepo)(nil)

type TelegramLinkRepo struct{ q *sqlcgen.Queries }

func NewTelegramLinkRepo(db *DB) *TelegramLinkRepo {
	return &TelegramLinkRepo{q: sqlcgen.New(db.Pool())}
}

func (r *TelegramLinkRepo) Create(ctx context.Context, nonceHash []byte, expiresAt time.Time) error {
	return translate(r.q.CreateTelegramLinkRequest(ctx, sqlcgen.CreateTelegramLinkRequestParams{
		NonceHash: nonceHash,
		ExpiresAt: timestamptz(expiresAt),
	}), "create telegram link request")
}

// Consume goes through translate, so an unknown, expired or already-consumed
// nonce all surface as domain.ErrNotFound. Keeping the three indistinguishable
// is deliberate: the bot answers all of them with one message, so none of them
// can be told apart by probing.
func (r *TelegramLinkRepo) Consume(ctx context.Context, nonceHash []byte, chatID int64) error {
	_, err := r.q.ConsumeTelegramLinkRequest(ctx, sqlcgen.ConsumeTelegramLinkRequestParams{
		NonceHash: nonceHash,
		ChatID:    &chatID,
	})
	return translate(err, "consume telegram link request")
}

func (r *TelegramLinkRepo) CountLinksSince(ctx context.Context, chatID int64, since time.Time) (int, error) {
	count, err := r.q.CountTelegramLinksSince(ctx, sqlcgen.CountTelegramLinksSinceParams{
		ChatID:     &chatID,
		ConsumedAt: timestamptz(since),
	})
	if err != nil {
		return 0, translate(err, "count telegram links")
	}
	return int(count), nil
}

func (r *TelegramLinkRepo) Prune(ctx context.Context, before time.Time) (int64, error) {
	deleted, err := r.q.PruneTelegramLinkRequests(ctx, timestamptz(before))
	if err != nil {
		return 0, translate(err, "prune telegram link requests")
	}
	return deleted, nil
}
