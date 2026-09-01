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
