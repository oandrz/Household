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
// BudgetRepo and GoalRepo, because Save must begin its own transaction,
// which a *sqlcgen.Queries built at construction time cannot do.
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

	// One pass over the measures, grouped by pillar id, avoids a query per
	// pillar: ListVisionMeasures already returns them in position order, so
	// appending in encounter order preserves it without a second sort.
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

// toMeasure decides which of the three kinds a stored row is. Broken is
// real, not defensive: vision_measures' CHECK permits it because ON DELETE
// SET NULL produces it, so a measure whose goal was deleted must report
// MeasureBroken, not a typed measure of 0 of 0.
//
// goal_id uses GoalID.Valid, not a nil check: sqlc leaves pgtype.UUID
// unwrapped since it already carries nullability in Valid, unlike the
// scalar current_value/target_value columns, which emit_pointers_for_null_types
// makes *int32.
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

		// Only row.ID is used below -- row.Version deliberately is not: the
		// returned version comes from the fresh read-back after commit
		// (below), not from this call, so a wrong value here would go
		// undetected.
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
				// Fail closed: Kind arrives validated, but this switch still
				// refuses anything it doesn't recognise, rather than writing
				// a row that satisfies no branch of measure_is_typed_or_linked.
				switch m.Kind {
				case domain.MeasureTyped:
					current, target := int32(m.Current), int32(m.Target)
					params.CurrentValue, params.TargetValue = &current, &target
				case domain.MeasureLinked:
					// GoalID is pgtype.UUID by value, not *pgtype.UUID -- the
					// same unwrapped-because-Valid-already-exists reasoning
					// toMeasure's comment gives on the read side.
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
			// Guarded, not a bare int16(m.Year) -- see yearParam's doc comment
			// for why.
			year, ok := yearParam(m.Year)
			if !ok {
				return domain.ErrVisionYearOutOfRange
			}
			if err := q.InsertVisionMilestone(ctx, sqlcgen.InsertVisionMilestoneParams{
				VisionID: row.ID,
				Position: int16(i),
				Year:     year,
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

	// Read back rather than returning the draft: the replace above deleted
	// and reinserted every pillar, measure and milestone, so the ids the
	// caller sent are now stale. Nothing reads them today (MeasureView
	// carries no id); this would surface only once measures get stable ids.
	//
	// Runs on the pool, after commit, not on the closed transaction: MVCC
	// guarantees this read sees the write's own commit or a later one. The
	// only race is another save landing in the gap, which returns a valid
	// document and version -- just maybe not this write's content. No lost
	// update.
	return r.Get(ctx, v.HouseholdID, v.Year)
}

// upsertParent is the whole of the concurrency contract, and the two branches
// are genuinely different operations rather than one upsert with a flag.
func (r *VisionRepo) upsertParent(ctx context.Context, q *sqlcgen.Queries, v domain.Vision) (sqlcgen.Vision, error) {
	// Guarded up front, before either branch (see yearParam's doc comment):
	// year is reused below in the update branch's existence check too.
	year, ok := yearParam(v.Year)
	if !ok {
		return sqlcgen.Vision{}, domain.ErrVisionYearOutOfRange
	}

	if v.Version == 0 {
		// A create: CreateVision is ON CONFLICT DO NOTHING, so pgx.ErrNoRows
		// here means the row appeared while this editor was typing -- the
		// first-save race two owners hit in January, both reading the empty
		// vision at version 0.
		row, err := q.CreateVision(ctx, sqlcgen.CreateVisionParams{
			HouseholdID: uuid(v.HouseholdID),
			Year:        year,
			Theme:       v.Theme,
			Description: v.Description,
		})
		if errors.Is(err, pgx.ErrNoRows) {
			return sqlcgen.Vision{}, domain.ErrVisionChanged
		}
		if err != nil {
			return sqlcgen.Vision{}, translate(err, "create vision")
		}
		// Field-by-field, not a sqlcgen.Vision(row) conversion: CreateVisionRow
		// has no CreatedAt/UpdatedAt, so the field counts don't match and a
		// type conversion wouldn't compile.
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
		// column is a Postgres integer. Refusing here stops a silent
		// truncation from matching some other row's version.
		return sqlcgen.Vision{}, domain.ErrVisionChanged
	}
	row, err := q.UpdateVision(ctx, sqlcgen.UpdateVisionParams{
		HouseholdID: uuid(v.HouseholdID),
		Year:        year,
		Theme:       v.Theme,
		Description: v.Description,
		Version:     version,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		// Zero rows is ambiguous -- deleted, or the other partner saved
		// first -- and those need different answers (RetroRepo.Update's
		// three-leg switch is the model): treating a re-read failure as
		// "your partner saved first" would be a false claim the household
		// would act on by retrying forever.
		//
		// This existence check MUST run on q (this transaction's
		// connection), never on r.Get: r.Get is pool-backed, and calling it
		// from inside an open transaction would need a SECOND connection
		// while the first stays checked out. Enough concurrent saves on this
		// path at once, against pool.go's MaxConns, self-deadlocks rather
		// than slows down. RetroRepo holds no pool field for the identical
		// reason: its writes are single statements that never re-read
		// inside an open transaction.
		_, getErr := q.GetVision(ctx, sqlcgen.GetVisionParams{
			HouseholdID: uuid(v.HouseholdID),
			Year:        year,
		})
		switch {
		case errors.Is(getErr, pgx.ErrNoRows):
			// Deleted: must read back as ErrNotFound, never "reload and try
			// again" -- RetroRepo.Update's own comment gives the reasoning.
			return sqlcgen.Vision{}, domain.ErrNotFound
		case getErr == nil:
			// Still there, at a version this UPDATE's WHERE clause did not
			// match: the other partner saved first.
			return sqlcgen.Vision{}, domain.ErrVisionChanged
		default:
			// May fail for a reason unrelated to concurrency (a cancelled
			// context, a timeout). Return that real failure rather than
			// folding it into ErrVisionChanged, so a transient error isn't
			// reported as a false claim about their partner.
			return sqlcgen.Vision{}, translate(getErr, "get vision (existence check)")
		}
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

// yearParam converts the port's int Year into the wire int16, refusing
// (ok=false) rather than truncating a value outside what a vision year can
// be. v.Year and m.Year come from a request body with no repository-level
// guarantee they were validated first -- CLAUDE.md's "fail closed on
// values you did not construct" -- and int16(67562) == 2026 would
// otherwise let a bad value silently target the wrong household-year.
// Bounded by domain.Min/MaxVisionYear, not just int16's range, matching
// visions.year's own CHECK constraint. versionParam (retro_repo.go) is the
// identical guard for Version.
func yearParam(year int) (int16, bool) {
	if year < domain.MinVisionYear || year > domain.MaxVisionYear {
		return 0, false
	}
	return int16(year), true
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
