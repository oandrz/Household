package postgres

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/andreasoentoro/hearth/api/internal/adapter/postgres/sqlcgen"
	"github.com/andreasoentoro/hearth/api/internal/domain"
)

// VisionRepo keeps the pool alongside the pool-backed *sqlcgen.Queries, like
// BudgetRepo and GoalRepo: Save replaces a whole document and must begin its
// own transaction, which a *sqlcgen.Queries built once at construction time
// cannot do.
type VisionRepo struct {
	q    *sqlcgen.Queries
	pool *pgxpool.Pool
}

func NewVisionRepo(db *DB) *VisionRepo {
	return &VisionRepo{q: sqlcgen.New(db.Pool()), pool: db.Pool()}
}

func (r *VisionRepo) Get(ctx context.Context, householdID string, year int) (domain.Vision, error) {
	row, err := r.q.GetVision(ctx, sqlcgen.GetVisionParams{
		HouseholdID: uuid(householdID),
		Year:        int16(year),
	})
	if err != nil {
		return domain.Vision{}, translate(err, "get vision")
	}

	pillarRows, err := r.q.ListVisionPillars(ctx, row.ID)
	if err != nil {
		return domain.Vision{}, translate(err, "list vision pillars")
	}
	measureRows, err := r.q.ListVisionMeasures(ctx, row.ID)
	if err != nil {
		return domain.Vision{}, translate(err, "list vision measures")
	}
	milestoneRows, err := r.q.ListVisionMilestones(ctx, row.ID)
	if err != nil {
		return domain.Vision{}, translate(err, "list vision milestones")
	}

	// One pass over the measures, grouped by pillar id, rather than a query
	// per pillar: ListVisionMeasures already returns them in (pillar
	// position, measure position) order, so appending in encounter order
	// preserves both orderings without a second sort.
	byPillar := make(map[string][]domain.Measure, len(pillarRows))
	for _, m := range measureRows {
		byPillar[uuidToString(m.PillarID)] = append(byPillar[uuidToString(m.PillarID)], toMeasure(m))
	}

	pillars := make([]domain.Pillar, 0, len(pillarRows))
	for _, p := range pillarRows {
		pillars = append(pillars, domain.Pillar{
			ID:          uuidToString(p.ID),
			Name:        p.Name,
			Description: p.Description,
			Measures:    byPillar[uuidToString(p.ID)],
		})
	}

	milestones := make([]domain.Milestone, 0, len(milestoneRows))
	for _, m := range milestoneRows {
		milestones = append(milestones, domain.Milestone{
			ID:    uuidToString(m.ID),
			Year:  int(m.Year),
			Title: m.Title,
			Note:  m.Note,
		})
	}

	return domain.Vision{
		ID:          uuidToString(row.ID),
		HouseholdID: householdID,
		Year:        int(row.Year),
		Theme:       row.Theme,
		Description: row.Description,
		Version:     int(row.Version),
		Pillars:     pillars,
		Milestones:  milestones,
	}, nil
}

// toMeasure decides which of the three kinds a stored row is. The broken case
// is not defensive programming -- vision_measures' own CHECK permits it
// because ON DELETE SET NULL produces it, so a measure whose goal was deleted
// arrives here with all three value columns null and must be reported as
// MeasureBroken rather than silently read as a typed measure of 0 of 0.
//
// goal_id is read via GoalID.Valid, not a nil check: emit_pointers_for_null_types
// only wraps scalar columns (current_value, target_value) in pointers --
// pgtype.UUID already carries its own nullability in its Valid field, so
// sqlc leaves it unwrapped. current_value/target_value are still *int32,
// which is what the second branch's nil checks rely on.
func toMeasure(m sqlcgen.VisionMeasure) domain.Measure {
	measure := domain.Measure{
		ID:    uuidToString(m.ID),
		Label: m.Label,
	}
	switch {
	case m.GoalID.Valid:
		measure.Kind = domain.MeasureLinked
		measure.GoalID = uuidToString(m.GoalID)
	case m.TargetValue != nil && m.CurrentValue != nil:
		measure.Kind = domain.MeasureTyped
		measure.Current = int(*m.CurrentValue)
		measure.Target = int(*m.TargetValue)
	default:
		measure.Kind = domain.MeasureBroken
	}
	return measure
}
