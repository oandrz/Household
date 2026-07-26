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
		row.ShowSecondaryCurrency, row.SecondaryCurrency, row.FxRateMode), nil
}

// Update persists only the columns the generated UpdateHousehold query
// accepts -- family_name, primary_currency, show_secondary_currency and
// fx_rate_mode. h.Name and h.SecondaryCurrency are not writable through this
// method; the RETURNING row reflects their unchanged stored values, and this
// method returns exactly that row rather than echoing back whatever the
// caller passed in.
func (r *HouseholdRepo) Update(ctx context.Context, h domain.Household) (domain.Household, error) {
	row, err := r.q.UpdateHousehold(ctx, sqlcgen.UpdateHouseholdParams{
		ID:                    uuid(h.ID),
		FamilyName:            h.FamilyName,
		PrimaryCurrency:       h.PrimaryCurrency,
		ShowSecondaryCurrency: h.ShowSecondaryCurrency,
		FxRateMode:            h.FXRateMode,
	})
	if err != nil {
		return domain.Household{}, translate(err, "update household")
	}
	return toDomainHousehold(row.ID, row.Name, row.FamilyName, row.PrimaryCurrency,
		row.ShowSecondaryCurrency, row.SecondaryCurrency, row.FxRateMode), nil
}

func (r *HouseholdRepo) Create(ctx context.Context, name, familyName string) (domain.Household, error) {
	row, err := r.q.CreateHousehold(ctx, sqlcgen.CreateHouseholdParams{Name: name, FamilyName: familyName})
	if err != nil {
		return domain.Household{}, translate(err, "create household")
	}
	return toDomainHousehold(row.ID, row.Name, row.FamilyName, row.PrimaryCurrency,
		row.ShowSecondaryCurrency, row.SecondaryCurrency, row.FxRateMode), nil
}
