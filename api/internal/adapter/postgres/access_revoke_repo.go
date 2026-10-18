package postgres

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/andreasoentoro/hearth/api/internal/adapter/postgres/sqlcgen"
	"github.com/andreasoentoro/hearth/api/internal/usecase"
)

var _ usecase.AccessRevoker = (*AccessRevokeRepo)(nil)

// AccessRevokeRepo writes across five tables in one transaction, which no
// single-table repository can do; see usecase.AccessRevoker for the contract.
type AccessRevokeRepo struct {
	q    *sqlcgen.Queries
	pool *pgxpool.Pool
}

func NewAccessRevokeRepo(db *DB) *AccessRevokeRepo {
	return &AccessRevokeRepo{q: sqlcgen.New(db.Pool()), pool: db.Pool()}
}

func (r *AccessRevokeRepo) RevokeAccess(ctx context.Context, userID string, chat usecase.ChatRule) error {
	switch chat {
	case usecase.RevokeChat, usecase.KeepChat:
	default:
		return fmt.Errorf("revoke access: unknown chat rule %d", chat)
	}
	id := uuid(userID)
	err := pgx.BeginFunc(ctx, r.pool, func(tx pgx.Tx) error {
		q := r.q.WithTx(tx)
		if err := q.RevokeSessionsForUser(ctx, id); err != nil {
			return fmt.Errorf("sessions: %w", err)
		}
		if err := q.RevokeAPITokensForUser(ctx, id); err != nil {
			return fmt.Errorf("api tokens: %w", err)
		}
		if err := q.ConsumeUnusedMagicLinksForUser(ctx, id); err != nil {
			return fmt.Errorf("magic links: %w", err)
		}
		if err := q.ExpireTelegramLinkRequestsForUser(ctx, id); err != nil {
			return fmt.Errorf("telegram link requests: %w", err)
		}
		if chat == usecase.RevokeChat {
			if err := q.DeleteTelegramAccount(ctx, id); err != nil {
				return fmt.Errorf("telegram account: %w", err)
			}
		}
		return nil
	})
	return translate(err, "revoke access")
}
