package postgres

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
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

// Save replaces one household-year's vision wholesale, inside one
// transaction: create or version-check the parent, verify every linked goal
// belongs to this household, delete every child, then insert the submitted
// ones. Any failure rolls the whole thing back via pgx.BeginFunc, so a bad
// milestone can never leave the parent updated with its pillars half
// replaced (TestVisionSaveIsOneTransaction). BudgetRepo.Upsert is the model.
func (r *VisionRepo) Save(ctx context.Context, v domain.Vision) (domain.Vision, error) {
	err := pgx.BeginFunc(ctx, r.pool, func(tx pgx.Tx) error {
		q := r.q.WithTx(tx)

		// Only row.ID is used below -- row.Version is not, deliberately: the
		// version this method hands back comes from the fresh read-back
		// after commit (see the comment on that return), not from what this
		// call reported, so a wrong Version here would be caught by nothing
		// short of that final read matching reality.
		row, err := r.upsertParent(ctx, q, v)
		if err != nil {
			return err
		}

		if err := validateMeasureGoals(ctx, q, v); err != nil {
			return err
		}

		// Full replace, never merge (the port's own doc comment). Deleting
		// the pillars cascades to their measures, so there is no third
		// delete: vision_measures.pillar_id is ON DELETE CASCADE.
		if err := q.DeleteVisionPillars(ctx, row.ID); err != nil {
			return translate(err, "delete vision pillars")
		}
		if err := q.DeleteVisionMilestones(ctx, row.ID); err != nil {
			return translate(err, "delete vision milestones")
		}

		for i, p := range v.Pillars {
			pillarID, err := q.InsertVisionPillar(ctx, sqlcgen.InsertVisionPillarParams{
				VisionID:    row.ID,
				Position:    int16(i),
				Name:        p.Name,
				Description: p.Description,
			})
			if err != nil {
				return translate(err, "insert vision pillar")
			}
			for j, m := range p.Measures {
				params := sqlcgen.InsertVisionMeasureParams{
					PillarID: pillarID,
					Position: int16(j),
					Label:    m.Label,
				}
				// Fail closed: Kind reached here through the domain's own
				// Validate, but this switch still refuses anything it does
				// not recognise rather than writing a row that satisfies no
				// branch of measure_is_typed_or_linked.
				switch m.Kind {
				case domain.MeasureTyped:
					current, target := int32(m.Current), int32(m.Target)
					params.CurrentValue, params.TargetValue = &current, &target
				case domain.MeasureLinked:
					// InsertVisionMeasureParams.GoalID is pgtype.UUID by
					// value, not *pgtype.UUID: sqlc leaves it unwrapped
					// because pgtype.UUID already carries its own
					// nullability in .Valid (the same reasoning toMeasure's
					// comment gives on the read side).
					params.GoalID = uuid(m.GoalID)
				default:
					return domain.ErrVisionMeasureAmbiguous
				}
				if err := q.InsertVisionMeasure(ctx, params); err != nil {
					return translate(err, "insert vision measure")
				}
			}
		}

		for i, m := range v.Milestones {
			if err := q.InsertVisionMilestone(ctx, sqlcgen.InsertVisionMilestoneParams{
				VisionID: row.ID,
				Position: int16(i),
				Year:     int16(m.Year),
				Title:    m.Title,
				Note:     m.Note,
			}); err != nil {
				return translate(err, "insert vision milestone")
			}
		}
		return nil
	})
	if err != nil {
		return domain.Vision{}, err
	}

	// Read back rather than returning the draft. The replace above DELETED and
	// reinserted every pillar, measure and milestone, so the ids the caller
	// sent name rows that no longer exist -- returning the draft would hand
	// back a document whose child ids are all stale. Nothing reads them today
	// (MeasureView carries no id), which is exactly why this would sit
	// unnoticed until the change the spec's decision 5 anticipates: the day
	// something references a measure, stable ids arrive, and a Save that had
	// been quietly lying about them would spring on that change rather than
	// on this one.
	return r.Get(ctx, v.HouseholdID, v.Year)
}

// upsertParent is the whole of the concurrency contract, and the two branches
// are genuinely different operations rather than one upsert with a flag.
func (r *VisionRepo) upsertParent(ctx context.Context, q *sqlcgen.Queries, v domain.Vision) (sqlcgen.Vision, error) {
	if v.Version == 0 {
		// A create. CreateVision is ON CONFLICT DO NOTHING, so pgx.ErrNoRows
		// here means the row appeared while this editor was typing -- the
		// first-save race two owners hit in January, when both read the empty
		// vision and both hold version 0.
		row, err := q.CreateVision(ctx, sqlcgen.CreateVisionParams{
			HouseholdID: uuid(v.HouseholdID),
			Year:        int16(v.Year),
			Theme:       v.Theme,
			Description: v.Description,
		})
		if errors.Is(err, pgx.ErrNoRows) {
			return sqlcgen.Vision{}, domain.ErrVisionChanged
		}
		if err != nil {
			return sqlcgen.Vision{}, translate(err, "create vision")
		}
		// Field-by-field, not a sqlcgen.Vision(row) conversion: sqlc's
		// CreateVisionRow has no CreatedAt/UpdatedAt, so its field count
		// does not match the sqlcgen.Vision table model and a type
		// conversion between them does not compile.
		return sqlcgen.Vision{
			ID:          row.ID,
			HouseholdID: row.HouseholdID,
			Year:        row.Year,
			Theme:       row.Theme,
			Description: row.Description,
			Version:     row.Version,
		}, nil
	}

	version, ok := versionParam(v.Version)
	if !ok {
		// A version outside int32's range can never be the stored one -- the
		// column is a Postgres integer, so every real value already fits.
		// Refusing here is what stops a silent truncation matching some other
		// row's version.
		return sqlcgen.Vision{}, domain.ErrVisionChanged
	}
	row, err := q.UpdateVision(ctx, sqlcgen.UpdateVisionParams{
		HouseholdID: uuid(v.HouseholdID),
		Year:        int16(v.Year),
		Theme:       v.Theme,
		Description: v.Description,
		Version:     version,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		// Zero rows is ambiguous: deleted, or the other partner saved first.
		// One cheap read tells them apart, because a deleted vision must read
		// back as ErrNotFound and never as "reload and try again" --
		// RetroRepo.Update's own comment.
		if _, getErr := r.Get(ctx, v.HouseholdID, v.Year); errors.Is(getErr, domain.ErrNotFound) {
			return sqlcgen.Vision{}, domain.ErrNotFound
		}
		return sqlcgen.Vision{}, domain.ErrVisionChanged
	}
	if err != nil {
		return sqlcgen.Vision{}, translate(err, "update vision")
	}
	// Same field-by-field reasoning as the create branch above:
	// UpdateVisionRow's field set does not match sqlcgen.Vision either.
	return sqlcgen.Vision{
		ID:          row.ID,
		HouseholdID: row.HouseholdID,
		Year:        row.Year,
		Theme:       row.Theme,
		Description: row.Description,
		Version:     row.Version,
	}, nil
}

// validateMeasureGoals refuses a measure naming a goal outside this
// household, inside the same transaction as the write. vision_measures' FK
// only proves a goal exists somewhere -- the identical hole
// validateLineCategories closes for budget lines.
func validateMeasureGoals(ctx context.Context, q *sqlcgen.Queries, v domain.Vision) error {
	seen := map[string]struct{}{}
	var ids []pgtype.UUID
	for _, p := range v.Pillars {
		for _, m := range p.Measures {
			if m.Kind != domain.MeasureLinked || m.GoalID == "" {
				continue
			}
			if _, dup := seen[m.GoalID]; dup {
				continue
			}
			seen[m.GoalID] = struct{}{}
			ids = append(ids, uuid(m.GoalID))
		}
	}
	if len(ids) == 0 {
		return nil
	}
	count, err := q.CountGoalsInHousehold(ctx, sqlcgen.CountGoalsInHouseholdParams{
		HouseholdID: uuid(v.HouseholdID),
		GoalIds:     ids,
	})
	if err != nil {
		return translate(err, "count goals in household")
	}
	if int(count) != len(ids) {
		return domain.ErrVisionGoalUnknown
	}
	return nil
}
