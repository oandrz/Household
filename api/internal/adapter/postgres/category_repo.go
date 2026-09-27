package postgres

import (
	"context"
	"fmt"

	"github.com/andreasoentoro/hearth/api/internal/adapter/postgres/sqlcgen"
	"github.com/andreasoentoro/hearth/api/internal/domain"
	"github.com/andreasoentoro/hearth/api/internal/usecase"
)

// var _ pins CategoryRepo to usecase.CategoryLookup at compile time, so a
// wrong Kind signature fails here, next to the repository, rather than at
// main.go's wiring.
var _ usecase.CategoryLookup = (*CategoryRepo)(nil)

// seedSize is how many rows SeedCategories inserts: thirteen literal VALUES
// rows, not a loop over an array, because sqlc's query analyzer rejects the
// multi-array form of unnest (see the query's own comment).
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
			c, err := toCategory(row)
			if err != nil {
				return nil, err
			}
			out = append(out, c)
		}
		return out, nil
	}

	rows, err := r.q.ListCategories(ctx, uuid(householdID))
	if err != nil {
		return nil, translate(err, "list categories")
	}
	out := make([]domain.Category, 0, len(rows))
	for _, row := range rows {
		c, err := toCategory(row)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, nil
}

// EnsureSeeded inserts the starter set for a household that has none.
//
// CountCategories counts archived rows too, so a household that archived
// its whole list already counts as seeded and the INSERT is never reached
// -- that, not ON CONFLICT, is what stops this sequential path from
// rebuilding over an archived list (see
// TestEnsureSeededDoesNotRebuildOverArchivedCategories).
//
// ON CONFLICT DO NOTHING on SeedCategories instead guards the concurrent
// case: two first requests can both read count == 0 and both attempt the
// same thirteen rows. It is also the only guard for a caller that reaches
// SeedCategories directly
// (TestSeedCategoriesRespectsTheUniqueKeyEvenWhenEveryRowIsArchived).
// Removing the count would still be correct, only slower; removing ON
// CONFLICT would not.
//
// starter must carry exactly seedSize entries -- SeedCategories has no room
// to insert any other number.
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

// Kind answers what TransactionService's category validation needs: whether
// a category is for spend or for income, so a transaction cannot be filed
// under the wrong one.
func (r *CategoryRepo) Kind(ctx context.Context, householdID, categoryID string) (domain.CategoryKind, error) {
	kind, err := r.q.GetCategoryKind(ctx, sqlcgen.GetCategoryKindParams{
		ID:          uuid(categoryID),
		HouseholdID: uuid(householdID),
	})
	if err != nil {
		return "", translate(err, "get category kind")
	}
	parsed, err := domain.ParseCategoryKind(kind)
	if err != nil {
		return "", fmt.Errorf("postgres: category kind: %w", err)
	}
	return parsed, nil
}

// Create adds one category at the end of the household's sort order.
// Computing that position in the same INSERT closes the round-trip window a
// read-then-write would have, though not the race between two concurrent
// READ COMMITTED transactions landing the same sort_order -- accepted as
// cosmetic (CreateCategory's own comment says why). A name collision, live
// or archived (UNIQUE (household_id, name) holds both), surfaces as a
// 23505 that translate maps to domain.ErrCategoryNameTaken by constraint
// name.
func (r *CategoryRepo) Create(ctx context.Context, c domain.Category) (domain.Category, error) {
	row, err := r.q.CreateCategory(ctx, sqlcgen.CreateCategoryParams{
		HouseholdID: uuid(c.HouseholdID),
		Name:        c.Name,
		Kind:        string(c.Kind),
	})
	if err != nil {
		return domain.Category{}, translate(err, "create category")
	}
	return toCategory(row)
}

// Rename changes the name only. RenameCategory's WHERE clause scopes the
// UPDATE to householdID as well as categoryID, so an id from another
// household matches no row and translate turns that pgx.ErrNoRows into
// domain.ErrNotFound. Same collision contract as Create.
func (r *CategoryRepo) Rename(ctx context.Context, householdID, categoryID, name string) (domain.Category, error) {
	row, err := r.q.RenameCategory(ctx, sqlcgen.RenameCategoryParams{
		ID:          uuid(categoryID),
		HouseholdID: uuid(householdID),
		Name:        name,
	})
	if err != nil {
		return domain.Category{}, translate(err, "rename category")
	}
	return toCategory(row)
}

// SetArchived stamps or clears archived_at. SetCategoryArchived's own
// COALESCE gives "first stamp wins": archiving an already-archived row keeps
// its original archived_at rather than moving it to now(), so two calls --
// including a retry -- never disagree about when it was archived.
func (r *CategoryRepo) SetArchived(ctx context.Context, householdID, categoryID string, archived bool) (domain.Category, error) {
	row, err := r.q.SetCategoryArchived(ctx, sqlcgen.SetCategoryArchivedParams{
		ID:          uuid(categoryID),
		HouseholdID: uuid(householdID),
		Archived:    archived,
	})
	if err != nil {
		return domain.Category{}, translate(err, "set category archived")
	}
	return toCategory(row)
}

// toCategory refuses a kind this code did not write: a CHECK on
// categories.kind is the schema's promise, not this adapter's, so the value
// still goes through domain.ParseCategoryKind -- an impossible row fails
// here, not at TransactionService's kind check as neither expense nor
// income. Same rule as toBill's cadence and toGoalContribution's source.
func toCategory(c sqlcgen.Category) (domain.Category, error) {
	kind, err := domain.ParseCategoryKind(c.Kind)
	if err != nil {
		return domain.Category{}, fmt.Errorf("postgres: category: %w", err)
	}
	return domain.Category{
		ID:          uuidToString(c.ID),
		HouseholdID: uuidToString(c.HouseholdID),
		Name:        c.Name,
		Kind:        kind,
		SortOrder:   int(c.SortOrder),
		ArchivedAt:  timePtrOf(c.ArchivedAt),
	}, nil
}
