package postgres

import (
	"context"
	"time"

	"github.com/andreasoentoro/hearth/api/internal/adapter/postgres/sqlcgen"
	"github.com/andreasoentoro/hearth/api/internal/usecase"
)

type SessionRepo struct{ q *sqlcgen.Queries }

func NewSessionRepo(db *DB) *SessionRepo { return &SessionRepo{q: sqlcgen.New(db.Pool())} }

func (r *SessionRepo) Create(ctx context.Context, tokenHash []byte, userID, householdID string, expiresAt time.Time) error {
	_, err := r.q.CreateSession(ctx, sqlcgen.CreateSessionParams{
		TokenHash:   tokenHash,
		UserID:      uuid(userID),
		HouseholdID: uuid(householdID),
		ExpiresAt:   timestamptz(expiresAt),
	})
	return translate(err, "create session")
}

// ByTokenHash relies on GetLiveSession's own WHERE clause (revoked_at IS
// NULL AND expires_at > now()) to make a revoked or expired session
// unfindable; there is no separate check here.
func (r *SessionRepo) ByTokenHash(ctx context.Context, tokenHash []byte) (usecase.SessionRecord, error) {
	row, err := r.q.GetLiveSession(ctx, tokenHash)
	if err != nil {
		return usecase.SessionRecord{}, translate(err, "get live session")
	}
	return usecase.SessionRecord{
		UserID:      uuidToString(row.UserID),
		HouseholdID: uuidToString(row.HouseholdID),
		ExpiresAt:   timeOf(row.ExpiresAt),
	}, nil
}

func (r *SessionRepo) Extend(ctx context.Context, tokenHash []byte, expiresAt time.Time) error {
	return translate(r.q.ExtendSession(ctx, sqlcgen.ExtendSessionParams{
		TokenHash: tokenHash,
		ExpiresAt: timestamptz(expiresAt),
	}), "extend session")
}

func (r *SessionRepo) RevokeByToken(ctx context.Context, tokenHash []byte) error {
	return translate(r.q.RevokeSessionByToken(ctx, tokenHash), "revoke session by token")
}

func (r *SessionRepo) RevokeAllForUser(ctx context.Context, userID string) error {
	return translate(r.q.RevokeSessionsForUser(ctx, uuid(userID)), "revoke sessions for user")
}
