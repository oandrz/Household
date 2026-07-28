package postgres

import (
	"context"
	"fmt"

	"github.com/andreasoentoro/hearth/api/internal/adapter/postgres/sqlcgen"
	"github.com/andreasoentoro/hearth/api/internal/domain"
)

// seedSize is how many rows SeedCategories inserts. The query is thirteen
// literal VALUES rows rather than an unnest of arrays -- sqlc's query
// analyzer rejects the multi-array form of unnest outright, see the query's
// own comment -- so the number of starter categories is fixed here, not
// looped over.
const seedSize = 13

type CategoryRepo struct{ q *sqlcgen.Queries }

func NewCategoryRepo(db *DB) *CategoryRepo {
	return &CategoryRepo{q: sqlcgen.New(db.Pool())}
}

func (r *CategoryRepo) List(ctx context.Context, householdID string, includeArchived bool) ([]domain.Category, error) {
	if includeArchived {
		rows, err := r.q.ListCategoriesIncludingArchived(ctx, uuid(householdID))
		if err != nil {
			return nil, translate(err, "list categories including archived")
		}
		out := make([]domain.Category, 0, len(rows))
		for _, row := range rows {
			out = append(out, toCategory(row))
		}
		return out, nil
	}

	rows, err := r.q.ListCategories(ctx, uuid(householdID))
	if err != nil {
		return nil, translate(err, "list categories")
	}
	out := make([]domain.Category, 0, len(rows))
	for _, row := range rows {
		out = append(out, toCategory(row))
	}
	return out, nil
}

// EnsureSeeded inserts the starter set for a household that has none.
//
// The count check is an optimisation, not the correctness argument: without it
// every categories request would issue a thirteen-row insert that the unique
// index throws away. The correctness comes from SeedCategories' ON CONFLICT DO
// NOTHING, which is what makes two simultaneous first requests produce one set
// rather than a duplicate-key error. Removing the count would still be
// correct; removing the ON CONFLICT would not.
//
// starter must carry exactly seedSize entries -- in practice always
// domain.StarterCategories() -- because SeedCategories has no room to insert
// any other number.
func (r *CategoryRepo) EnsureSeeded(ctx context.Context, householdID string, starter []domain.Category) error {
	count, err := r.q.CountCategories(ctx, uuid(householdID))
	if err != nil {
		return translate(err, "count categories")
	}
	if count > 0 {
		return nil
	}
	if len(starter) != seedSize {
		return fmt.Errorf("postgres: EnsureSeeded got %d starter categories, want exactly %d", len(starter), seedSize)
	}

	err = r.q.SeedCategories(ctx, sqlcgen.SeedCategoriesParams{
		HouseholdID: uuid(householdID),
		Name1:       starter[0].Name, Kind1: string(starter[0].Kind), SortOrder1: int32(starter[0].SortOrder),
		Name2: starter[1].Name, Kind2: string(starter[1].Kind), SortOrder2: int32(starter[1].SortOrder),
		Name3: starter[2].Name, Kind3: string(starter[2].Kind), SortOrder3: int32(starter[2].SortOrder),
		Name4: starter[3].Name, Kind4: string(starter[3].Kind), SortOrder4: int32(starter[3].SortOrder),
		Name5: starter[4].Name, Kind5: string(starter[4].Kind), SortOrder5: int32(starter[4].SortOrder),
		Name6: starter[5].Name, Kind6: string(starter[5].Kind), SortOrder6: int32(starter[5].SortOrder),
		Name7: starter[6].Name, Kind7: string(starter[6].Kind), SortOrder7: int32(starter[6].SortOrder),
		Name8: starter[7].Name, Kind8: string(starter[7].Kind), SortOrder8: int32(starter[7].SortOrder),
		Name9: starter[8].Name, Kind9: string(starter[8].Kind), SortOrder9: int32(starter[8].SortOrder),
		Name10: starter[9].Name, Kind10: string(starter[9].Kind), SortOrder10: int32(starter[9].SortOrder),
		Name11: starter[10].Name, Kind11: string(starter[10].Kind), SortOrder11: int32(starter[10].SortOrder),
		Name12: starter[11].Name, Kind12: string(starter[11].Kind), SortOrder12: int32(starter[11].SortOrder),
		Name13: starter[12].Name, Kind13: string(starter[12].Kind), SortOrder13: int32(starter[12].SortOrder),
	})
	if err != nil {
		return translate(err, "seed categories")
	}
	return nil
}

func (r *CategoryRepo) BelongsToHousehold(ctx context.Context, householdID, categoryID string) (bool, error) {
	ok, err := r.q.CategoryBelongsToHousehold(ctx, sqlcgen.CategoryBelongsToHouseholdParams{
		ID:          uuid(categoryID),
		HouseholdID: uuid(householdID),
	})
	if err != nil {
		return false, translate(err, "check category household")
	}
	return ok, nil
}

func toCategory(c sqlcgen.Category) domain.Category {
	return domain.Category{
		ID:          uuidToString(c.ID),
		HouseholdID: uuidToString(c.HouseholdID),
		Name:        c.Name,
		Kind:        domain.CategoryKind(c.Kind),
		SortOrder:   int(c.SortOrder),
		ArchivedAt:  timePtrOf(c.ArchivedAt),
	}
}
