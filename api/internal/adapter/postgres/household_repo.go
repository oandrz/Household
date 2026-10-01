package postgres

import (
	"context"

	"github.com/andreasoentoro/hearth/api/internal/adapter/postgres/sqlcgen"
	"github.com/andreasoentoro/hearth/api/internal/domain"
)

type HouseholdRepo struct{ q *sqlcgen.Queries }

func NewHouseholdRepo(db *DB) *HouseholdRepo { return &HouseholdRepo{q: sqlcgen.New(db.Pool())} }

func (r *HouseholdRepo) Get(ctx context.Context, householdID string) (domain.Household, error) {
	row, err := r.q.GetHousehold(ctx, uuid(householdID))
	if err != nil {
		return domain.Household{}, translate(err, "get household")
	}
	return toDomainHousehold(row.ID, row.Name, row.FamilyName, row.PrimaryCurrency,
		row.ShowSecondaryCurrency, row.SecondaryCurrency, row.FxRateMode, row.Timezone), nil
}

// Update persists every field on h. Don't let the generated query narrow
// back to a subset of columns: that would silently drop a caller's Name or
// SecondaryCurrency while still returning a nil error, indistinguishable
// from a full successful write.
func (r *HouseholdRepo) Update(ctx context.Context, h domain.Household) (domain.Household, error) {
	row, err := r.q.UpdateHousehold(ctx, sqlcgen.UpdateHouseholdParams{
		ID:                    uuid(h.ID),
		Name:                  h.Name,
		FamilyName:            h.FamilyName,
		PrimaryCurrency:       h.PrimaryCurrency,
		ShowSecondaryCurrency: h.ShowSecondaryCurrency,
		SecondaryCurrency:     h.SecondaryCurrency,
		FxRateMode:            h.FXRateMode,
		Timezone:              h.Timezone,
	})
	if err != nil {
		return domain.Household{}, translate(err, "update household")
	}
	return toDomainHousehold(row.ID, row.Name, row.FamilyName, row.PrimaryCurrency,
		row.ShowSecondaryCurrency, row.SecondaryCurrency, row.FxRateMode, row.Timezone), nil
}

// Create writes h. h.ID and h.FXRateMode are ignored: the database assigns
// the id, and fx_rate_mode keeps its column default ('auto'), the only value
// the CHECK constraint makes safe to assume at creation -- see
// usecase.HouseholdRepository.Create. h.Timezone is written as given and is
// the caller's to validate: the column refuses only the empty string.
func (r *HouseholdRepo) Create(ctx context.Context, h domain.Household) (domain.Household, error) {
	row, err := r.q.CreateHousehold(ctx, sqlcgen.CreateHouseholdParams{
		Name:                  h.Name,
		FamilyName:            h.FamilyName,
		PrimaryCurrency:       h.PrimaryCurrency,
		ShowSecondaryCurrency: h.ShowSecondaryCurrency,
		SecondaryCurrency:     h.SecondaryCurrency,
		Timezone:              h.Timezone,
	})
	if err != nil {
		return domain.Household{}, translate(err, "create household")
	}
	return toDomainHousehold(row.ID, row.Name, row.FamilyName, row.PrimaryCurrency,
		row.ShowSecondaryCurrency, row.SecondaryCurrency, row.FxRateMode, row.Timezone), nil
}
