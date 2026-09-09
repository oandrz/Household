package postgres

import (
	"context"

	"github.com/andreasoentoro/hearth/api/internal/adapter/postgres/sqlcgen"
	"github.com/andreasoentoro/hearth/api/internal/usecase"
)

var _ usecase.TelegramAccountRepository = (*TelegramAccountRepo)(nil)

type TelegramAccountRepo struct{ q *sqlcgen.Queries }

func NewTelegramAccountRepo(db *DB) *TelegramAccountRepo {
	return &TelegramAccountRepo{q: sqlcgen.New(db.Pool())}
}

func (r *TelegramAccountRepo) ByChatID(ctx context.Context, chatID int64) (string, error) {
	id, err := r.q.GetTelegramAccountByChatID(ctx, chatID)
	if err != nil {
		return "", translate(err, "get telegram account by chat id")
	}
	return uuidToString(id), nil
}

func (r *TelegramAccountRepo) ByUserID(ctx context.Context, userID string) (usecase.TelegramBinding, error) {
	row, err := r.q.GetTelegramAccountByUserID(ctx, uuid(userID))
	if err != nil {
		return usecase.TelegramBinding{}, translate(err, "get telegram account by user id")
	}
	return usecase.TelegramBinding{
		UserID:       userID,
		ChatID:       row.ChatID,
		ChatUsername: stringOrEmpty(row.ChatUsername),
		LinkedAt:     timeOf(row.LinkedAt),
	}, nil
}

// Create's contract for which UNIQUE wins and why b.LinkedAt is ignored is
// on the port (usecase.TelegramAccountRepository.Create); linked_at is left
// to the column's own DEFAULT now().
func (r *TelegramAccountRepo) Create(ctx context.Context, b usecase.TelegramBinding) error {
	return translate(r.q.CreateTelegramAccount(ctx, sqlcgen.CreateTelegramAccountParams{
		UserID:       uuid(b.UserID),
		ChatID:       b.ChatID,
		ChatUsername: nullableText(b.ChatUsername),
	}), "create telegram account")
}

// Delete is idempotent: removing a binding that is not there is not an
// error, because the caller's goal -- this user has no chat -- is already
// true.
func (r *TelegramAccountRepo) Delete(ctx context.Context, userID string) error {
	return translate(r.q.DeleteTelegramAccount(ctx, uuid(userID)), "delete telegram account")
}
