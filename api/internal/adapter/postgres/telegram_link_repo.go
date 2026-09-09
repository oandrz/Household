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

// Create returns the new row's id: the browser that just minted the nonce
// has to poll with it, and this insert is the only moment that id exists to
// hand back -- a lookup by nonce_hash afterwards would be a second way to
// address a row by its secret.
func (r *TelegramLinkRepo) Create(ctx context.Context, userID string, nonceHash []byte, expiresAt time.Time) (string, error) {
	id, err := r.q.CreateTelegramLinkRequest(ctx, sqlcgen.CreateTelegramLinkRequestParams{
		NonceHash: nonceHash,
		ExpiresAt: timestamptz(expiresAt),
		UserID:    nullableUUID(optionalID(userID)),
	})
	if err != nil {
		return "", translate(err, "create telegram link request")
	}
	return uuidToString(id), nil
}

// Consume goes through translate, so an unknown, expired or already-consumed
// nonce all surface as domain.ErrNotFound. Keeping the three indistinguishable
// is deliberate: the bot answers all of them with one message, so none of them
// can be told apart by probing.
func (r *TelegramLinkRepo) Consume(ctx context.Context, nonceHash []byte, chatID int64, chatUsername string) (usecase.TelegramLinkRedemption, error) {
	row, err := r.q.ConsumeTelegramLinkRequest(ctx, sqlcgen.ConsumeTelegramLinkRequestParams{
		NonceHash:    nonceHash,
		ChatID:       &chatID,
		ChatUsername: nullableText(chatUsername),
	})
	if err != nil {
		return usecase.TelegramLinkRedemption{}, translate(err, "consume telegram link request")
	}
	return usecase.TelegramLinkRedemption{
		ID:     uuidToString(row.ID),
		UserID: uuidOrEmpty(row.UserID),
	}, nil
}

func (r *TelegramLinkRepo) ByID(ctx context.Context, id string) (usecase.TelegramLinkRequest, error) {
	row, err := r.q.GetTelegramLinkRequest(ctx, uuid(id))
	if err != nil {
		return usecase.TelegramLinkRequest{}, translate(err, "get telegram link request")
	}
	return usecase.TelegramLinkRequest{
		ID:           uuidToString(row.ID),
		UserID:       uuidOrEmpty(row.UserID),
		ChatID:       int64Or(row.ChatID),
		ChatUsername: stringOrEmpty(row.ChatUsername),
		Consumed:     row.ConsumedAt.Valid,
		ExpiresAt:    timeOf(row.ExpiresAt),
	}, nil
}

func (r *TelegramLinkRepo) CountMintsSince(ctx context.Context, userID string, since time.Time) (int, error) {
	count, err := r.q.CountTelegramLinkMintsSince(ctx, sqlcgen.CountTelegramLinkMintsSinceParams{
		UserID:    uuid(userID),
		CreatedAt: timestamptz(since),
	})
	if err != nil {
		return 0, translate(err, "count telegram link mints")
	}
	return int(count), nil
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
