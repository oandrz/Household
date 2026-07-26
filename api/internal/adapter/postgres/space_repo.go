package postgres

import (
	"context"

	"github.com/andreasoentoro/hearth/api/internal/adapter/postgres/sqlcgen"
	"github.com/andreasoentoro/hearth/api/internal/domain"
)

type SpaceRepo struct{ q *sqlcgen.Queries }

func NewSpaceRepo(db *DB) *SpaceRepo { return &SpaceRepo{q: sqlcgen.New(db.Pool())} }

// List orders by position, per ListSpaces' own ORDER BY -- domain.VisibleSpaces
// relies on that ordering and does not sort itself.
func (r *SpaceRepo) List(ctx context.Context, householdID string) ([]domain.Space, error) {
	rows, err := r.q.ListSpaces(ctx, uuid(householdID))
	if err != nil {
		return nil, translate(err, "list spaces")
	}
	spaces := make([]domain.Space, len(rows))
	for i, row := range rows {
		spaces[i] = toDomainSpace(row)
	}
	return spaces, nil
}

func (r *SpaceRepo) Create(ctx context.Context, s domain.Space) (domain.Space, error) {
	row, err := r.q.CreateSpace(ctx, sqlcgen.CreateSpaceParams{
		HouseholdID:        uuid(s.HouseholdID),
		Key:                s.Key,
		Name:               s.Name,
		Visibility:         string(s.Visibility),
		Position:           int32(s.Position),
		IsBuiltin:          s.IsBuiltin,
		RequiredCapability: string(s.RequiredCapability),
	})
	if err != nil {
		return domain.Space{}, translate(err, "create space")
	}
	return toDomainSpace(row), nil
}

func (r *SpaceRepo) NextPosition(ctx context.Context, householdID string) (int, error) {
	next, err := r.q.NextSpacePosition(ctx, uuid(householdID))
	if err != nil {
		return 0, translate(err, "next space position")
	}
	return int(next), nil
}
