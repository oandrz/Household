package postgres

import (
	"context"
	"time"

	"github.com/andreasoentoro/hearth/api/internal/adapter/postgres/sqlcgen"
	"github.com/andreasoentoro/hearth/api/internal/domain"
)

type APITokenRepo struct{ q *sqlcgen.Queries }

func NewAPITokenRepo(db *DB) *APITokenRepo { return &APITokenRepo{q: sqlcgen.New(db.Pool())} }

func (r *APITokenRepo) Create(ctx context.Context, tokenHash []byte, prefix string, t domain.APIToken) (domain.APIToken, error) {
	row, err := r.q.CreateAPIToken(ctx, sqlcgen.CreateAPITokenParams{
		UserID:      uuid(t.UserID),
		HouseholdID: uuid(t.HouseholdID),
		Name:        t.Name,
		TokenHash:   tokenHash,
		Prefix:      prefix,
		ExpiresAt:   timestamptz(t.ExpiresAt),
	})
	if err != nil {
		return domain.APIToken{}, translate(err, "create api token")
	}
	return toAPIToken(row), nil
}

// ByTokenHash relies on GetLiveAPIToken's own WHERE clause (revoked_at IS
// NULL AND expires_at > now()) exactly as SessionRepo.ByTokenHash does.
func (r *APITokenRepo) ByTokenHash(ctx context.Context, tokenHash []byte) (domain.APIToken, error) {
	row, err := r.q.GetLiveAPIToken(ctx, tokenHash)
	if err != nil {
		return domain.APIToken{}, translate(err, "get live api token")
	}
	return toAPIToken(row), nil
}

func (r *APITokenRepo) ListForUser(ctx context.Context, userID string) ([]domain.APIToken, error) {
	rows, err := r.q.ListAPITokensForUser(ctx, uuid(userID))
	if err != nil {
		return nil, translate(err, "list api tokens")
	}
	out := make([]domain.APIToken, 0, len(rows))
	for _, row := range rows {
		out = append(out, toAPIToken(row))
	}
	return out, nil
}

func (r *APITokenRepo) Revoke(ctx context.Context, userID, tokenID string) error {
	n, err := r.q.RevokeAPIToken(ctx, sqlcgen.RevokeAPITokenParams{ID: uuid(tokenID), UserID: uuid(userID)})
	if err != nil {
		return translate(err, "revoke api token")
	}
	if n == 0 {
		return domain.ErrNotFound
	}
	return nil
}

func (r *APITokenRepo) RevokeAllForUser(ctx context.Context, userID string) error {
	return translate(r.q.RevokeAPITokensForUser(ctx, uuid(userID)), "revoke api tokens for user")
}

func (r *APITokenRepo) Touch(ctx context.Context, tokenID string, at time.Time) error {
	return translate(r.q.TouchAPIToken(ctx, sqlcgen.TouchAPITokenParams{ID: uuid(tokenID), LastUsedAt: timestamptz(at)}), "touch api token")
}

func toAPIToken(row sqlcgen.ApiToken) domain.APIToken {
	return domain.APIToken{
		ID:          uuidToString(row.ID),
		UserID:      uuidToString(row.UserID),
		HouseholdID: uuidToString(row.HouseholdID),
		Name:        row.Name,
		Prefix:      row.Prefix,
		CreatedAt:   timeOf(row.CreatedAt),
		ExpiresAt:   timeOf(row.ExpiresAt),
		LastUsedAt:  timePtrOf(row.LastUsedAt),
		RevokedAt:   timePtrOf(row.RevokedAt),
	}
}
