package postgres_test

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/andreasoentoro/hearth/api/internal/adapter/postgres"
	"github.com/andreasoentoro/hearth/api/internal/domain"
)

// A measure carrying BOTH a goal link and a typed target renders two
// different answers to the same question. The domain refuses it too; this
// proves the database does not depend on the domain being correct.
func TestVisionMeasureCannotBeBothTypedAndLinked(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	householdID := insertTestHousehold(t, db)

	var visionID, pillarID string
	if err := db.Pool().QueryRow(ctx,
		`INSERT INTO visions (household_id, year, theme) VALUES ($1, 2026, 'Slow down together') RETURNING id`,
		householdID).Scan(&visionID); err != nil {
		t.Fatalf("insert vision: %v", err)
	}
	if err := db.Pool().QueryRow(ctx,
		`INSERT INTO vision_pillars (vision_id, position, name) VALUES ($1, 0, 'Us before logistics') RETURNING id`,
		visionID).Scan(&pillarID); err != nil {
		t.Fatalf("insert pillar: %v", err)
	}
	goalID := insertTestGoal(t, db, householdID)

	_, err := db.Pool().Exec(ctx,
		`INSERT INTO vision_measures (pillar_id, position, label, current_value, target_value, goal_id)
		 VALUES ($1, 0, 'Emergency fund', 2, 4, $2)`, pillarID, goalID)
	// Any error would pass a bare err == nil check -- a typo in the column
	// list or a dropped connection looks the same as a refused row. Unwrap to
	// the driver error and check which constraint fired, so this test proves
	// the database refused the ambiguous measure specifically, not merely
	// that something went wrong.
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.ConstraintName != "measure_is_typed_or_linked" {
		t.Fatalf("expected measure_is_typed_or_linked to refuse a measure that is both typed and linked, got: %v", err)
	}
}

// Deleting a goal must unlink the measure, not fail. ON DELETE SET NULL is
// an UPDATE, and Postgres enforces CHECK constraints on UPDATE -- so without
// the constraint's third (all-null) branch this delete raises a violation
// inside the GOALS feature, where nobody would think to look at Vision.
func TestDeletingALinkedGoalUnlinksTheMeasureInsteadOfFailing(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	householdID := insertTestHousehold(t, db)
	goalID := insertTestGoal(t, db, householdID)

	var visionID, pillarID, measureID string
	if err := db.Pool().QueryRow(ctx,
		`INSERT INTO visions (household_id, year, theme) VALUES ($1, 2026, 'Slow down together') RETURNING id`,
		householdID).Scan(&visionID); err != nil {
		t.Fatalf("insert vision: %v", err)
	}
	if err := db.Pool().QueryRow(ctx,
		`INSERT INTO vision_pillars (vision_id, position, name) VALUES ($1, 0, 'Money without fear') RETURNING id`,
		visionID).Scan(&pillarID); err != nil {
		t.Fatalf("insert pillar: %v", err)
	}
	if err := db.Pool().QueryRow(ctx,
		`INSERT INTO vision_measures (pillar_id, position, label, goal_id)
		 VALUES ($1, 0, 'Emergency fund', $2) RETURNING id`, pillarID, goalID).Scan(&measureID); err != nil {
		t.Fatalf("insert measure: %v", err)
	}

	if _, err := db.Pool().Exec(ctx, `DELETE FROM goals WHERE id = $1`, goalID); err != nil {
		t.Fatalf("delete goal: %v", err)
	}

	var goalRef *string
	var current, target *int32
	if err := db.Pool().QueryRow(ctx,
		`SELECT goal_id, current_value, target_value FROM vision_measures WHERE id = $1`,
		measureID).Scan(&goalRef, &current, &target); err != nil {
		t.Fatalf("measure should still exist after its goal was deleted: %v", err)
	}
	if goalRef != nil || current != nil || target != nil {
		t.Fatalf("expected an unlinked, figureless measure; got goal=%v current=%v target=%v", goalRef, current, target)
	}
}

// insertTestGoal inserts the minimum goal row needed to link a vision
// measure. planned_monthly_minor has no default (see 00007_goals.sql), so
// it must be supplied even though these tests never look at it.
func insertTestGoal(t *testing.T, db *postgres.DB, householdID string) string {
	t.Helper()
	var id string
	if err := db.Pool().QueryRow(context.Background(),
		`INSERT INTO goals (household_id, name, target_amount_minor, currency, planned_monthly_minor)
		 VALUES ($1, 'Emergency fund', 1000000, 'SGD', 0) RETURNING id`, householdID).Scan(&id); err != nil {
		t.Fatalf("insert goal: %v", err)
	}
	return id
}

func TestVisionRepoGetReportsNotFoundForAYearNeverSet(t *testing.T) {
	db := openTestDB(t)
	repo := postgres.NewVisionRepo(db)
	householdID := insertTestHousehold(t, db)

	_, err := repo.Get(context.Background(), householdID, 2026)
	if !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("want domain.ErrNotFound, got %v", err)
	}
}

func TestVisionRepoGetReadsPillarsMeasuresAndMilestonesInPositionOrder(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	repo := postgres.NewVisionRepo(db)
	householdID := insertTestHousehold(t, db)

	var visionID string
	if err := db.Pool().QueryRow(ctx,
		`INSERT INTO visions (household_id, year, theme, description)
		 VALUES ($1, 2026, 'Slow down together', 'Fewer commitments.') RETURNING id`,
		householdID).Scan(&visionID); err != nil {
		t.Fatalf("insert vision: %v", err)
	}
	// Two pillars, inserted out of position order on purpose -- position 1
	// first, then position 0 -- the same pattern the measures and milestones
	// below already use, so this proves ORDER BY position rather than
	// insertion order. A prior version of this test used only one pillar,
	// under which ListVisionPillars' ORDER BY could be deleted with nothing
	// going red.
	if _, err := db.Pool().Exec(ctx,
		`INSERT INTO vision_pillars (vision_id, position, name, description)
		 VALUES ($1, 1, 'Money without fear', '')`, visionID); err != nil {
		t.Fatalf("insert pillar 1: %v", err)
	}
	var pillarID string
	if err := db.Pool().QueryRow(ctx,
		`INSERT INTO vision_pillars (vision_id, position, name, description)
		 VALUES ($1, 0, 'Us before logistics', 'Partners first.') RETURNING id`,
		visionID).Scan(&pillarID); err != nil {
		t.Fatalf("insert pillar 0: %v", err)
	}
	// Inserted out of order on purpose: the ORDER BY is what this asserts.
	if _, err := db.Pool().Exec(ctx,
		`INSERT INTO vision_measures (pillar_id, position, label, current_value, target_value)
		 VALUES ($1, 1, 'Weekends away', 2, 4), ($1, 0, 'Date nights / month', 2, 2)`, pillarID); err != nil {
		t.Fatalf("insert measures: %v", err)
	}
	if _, err := db.Pool().Exec(ctx,
		`INSERT INTO vision_milestones (vision_id, position, year, title, note)
		 VALUES ($1, 1, 2029, 'Bigger place', ''), ($1, 0, 2027, 'Sabbatical', 'Indonesia')`, visionID); err != nil {
		t.Fatalf("insert milestones: %v", err)
	}

	got, err := repo.Get(ctx, householdID, 2026)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Theme != "Slow down together" || got.Version != 1 {
		t.Fatalf("theme/version wrong: %+v", got)
	}
	if len(got.Pillars) != 2 {
		t.Fatalf("want two pillars, got %+v", got.Pillars)
	}
	if got.Pillars[0].Name != "Us before logistics" {
		t.Fatalf("pillars out of position order: %+v", got.Pillars)
	}
	if len(got.Pillars[0].Measures) != 2 {
		t.Fatalf("want the position-0 pillar's two measures, got %+v", got.Pillars[0].Measures)
	}
	if got.Pillars[0].Measures[0].Label != "Date nights / month" {
		t.Fatalf("measures out of position order: %+v", got.Pillars[0].Measures)
	}
	if got.Pillars[0].Measures[0].Kind != domain.MeasureTyped {
		t.Fatalf("want a typed measure, got kind %q", got.Pillars[0].Measures[0].Kind)
	}
	if len(got.Milestones) != 2 || got.Milestones[0].Title != "Sabbatical" {
		t.Fatalf("milestones out of position order: %+v", got.Milestones)
	}
}

func TestVisionRepoGetReadsABrokenLinkAsBroken(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	repo := postgres.NewVisionRepo(db)
	householdID := insertTestHousehold(t, db)
	goalID := insertTestGoal(t, db, householdID)

	var visionID, pillarID string
	_ = db.Pool().QueryRow(ctx,
		`INSERT INTO visions (household_id, year, theme) VALUES ($1, 2026, 'T') RETURNING id`,
		householdID).Scan(&visionID)
	_ = db.Pool().QueryRow(ctx,
		`INSERT INTO vision_pillars (vision_id, position, name) VALUES ($1, 0, 'P') RETURNING id`,
		visionID).Scan(&pillarID)
	if _, err := db.Pool().Exec(ctx,
		`INSERT INTO vision_measures (pillar_id, position, label, goal_id) VALUES ($1, 0, 'Emergency fund', $2)`,
		pillarID, goalID); err != nil {
		t.Fatalf("insert measure: %v", err)
	}
	if _, err := db.Pool().Exec(ctx, `DELETE FROM goals WHERE id = $1`, goalID); err != nil {
		t.Fatalf("delete goal: %v", err)
	}

	got, err := repo.Get(ctx, householdID, 2026)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Pillars[0].Measures[0].Kind != domain.MeasureBroken {
		t.Fatalf("want MeasureBroken after the goal was deleted, got %q", got.Pillars[0].Measures[0].Kind)
	}
}

func TestVisionSaveCreatesAtVersionZeroAndRefusesASecondCreate(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	repo := postgres.NewVisionRepo(db)
	householdID := insertTestHousehold(t, db)

	draft := domain.Vision{HouseholdID: householdID, Year: 2026, Theme: "Slow down together", Version: 0}
	saved, err := repo.Save(ctx, draft)
	if err != nil {
		t.Fatalf("first create: %v", err)
	}
	if saved.Version != 1 {
		t.Fatalf("a create must land at version 1, got %d", saved.Version)
	}

	// The first-save race: both partners read the empty vision, both hold 0.
	_, err = repo.Save(ctx, draft)
	if !errors.Is(err, domain.ErrVisionChanged) {
		t.Fatalf("want ErrVisionChanged for a second version-0 save, got %v", err)
	}
}

func TestVisionSaveRefusesAStaleVersion(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	repo := postgres.NewVisionRepo(db)
	householdID := insertTestHousehold(t, db)

	first, err := repo.Save(ctx, domain.Vision{HouseholdID: householdID, Year: 2026, Theme: "A"})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	second, err := repo.Save(ctx, domain.Vision{HouseholdID: householdID, Year: 2026, Theme: "B", Version: first.Version})
	if err != nil {
		t.Fatalf("second save: %v", err)
	}
	// The returned version must be the POST-write one, because it is the token
	// the next save sends. A query whose RETURNING gave back the version it
	// read would make every subsequent save conflict against itself.
	if second.Version != first.Version+1 {
		t.Fatalf("want the incremented version back, got %d after %d", second.Version, first.Version)
	}
	// first.Version is now stale.
	_, err = repo.Save(ctx, domain.Vision{HouseholdID: householdID, Year: 2026, Theme: "C", Version: first.Version})
	if !errors.Is(err, domain.ErrVisionChanged) {
		t.Fatalf("want ErrVisionChanged, got %v", err)
	}
}

func TestVisionSaveIsOneTransaction(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	repo := postgres.NewVisionRepo(db)
	householdID := insertTestHousehold(t, db)

	good := domain.Vision{
		HouseholdID: householdID, Year: 2026, Theme: "Slow down together",
		Pillars: []domain.Pillar{{Name: "Us before logistics", Measures: []domain.Measure{
			{Label: "Date nights / month", Kind: domain.MeasureTyped, Current: 2, Target: 2},
		}}},
	}
	saved, err := repo.Save(ctx, good)
	if err != nil {
		t.Fatalf("first save: %v", err)
	}

	// The injected fault: a milestone year outside vision_milestones' own
	// CHECK range, submitted AFTER a valid pillar and measure have already
	// been inserted in this transaction.
	bad := good
	bad.Version = saved.Version
	bad.Theme = "Overwritten"
	bad.Pillars = []domain.Pillar{{Name: "Replaced", Measures: nil}}
	bad.Milestones = []domain.Milestone{{Year: 9999, Title: "Out of range"}}
	if _, err := repo.Save(ctx, bad); err == nil {
		t.Fatal("expected the out-of-range milestone year to fail the save")
	}

	after, err := repo.Get(ctx, householdID, 2026)
	if err != nil {
		t.Fatalf("get after the failed save: %v", err)
	}
	if after.Theme != "Slow down together" {
		t.Fatalf("the parent row was not rolled back: theme is %q", after.Theme)
	}
	if len(after.Pillars) != 1 || after.Pillars[0].Name != "Us before logistics" {
		t.Fatalf("children were not rolled back: %+v", after.Pillars)
	}
	if len(after.Pillars[0].Measures) != 1 {
		t.Fatalf("measures were not rolled back: %+v", after.Pillars[0].Measures)
	}
}

func TestVisionSaveRefusesAGoalFromAnotherHousehold(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	repo := postgres.NewVisionRepo(db)
	mine := insertTestHousehold(t, db)
	theirs := insertTestHousehold(t, db)
	theirGoal := insertTestGoal(t, db, theirs)

	_, err := repo.Save(ctx, domain.Vision{
		HouseholdID: mine, Year: 2026, Theme: "T",
		Pillars: []domain.Pillar{{Name: "P", Measures: []domain.Measure{
			{Label: "Emergency fund", Kind: domain.MeasureLinked, GoalID: theirGoal},
		}}},
	})
	if !errors.Is(err, domain.ErrVisionGoalUnknown) {
		t.Fatalf("want ErrVisionGoalUnknown, got %v", err)
	}
}

// The zero-row UPDATE's two possible causes must not be collapsed into one
// answer: this is the "deleted" leg, the twin of
// TestVisionSaveRefusesAStaleVersion's "someone else saved first" leg. A
// version-guarded save against a household-year that no longer exists must
// report domain.ErrNotFound, never domain.ErrVisionChanged -- a caller that
// saw ErrVisionChanged would reload and retry forever against a row that can
// never come back.
func TestVisionSaveReportsNotFoundForADeletedVision(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	repo := postgres.NewVisionRepo(db)
	householdID := insertTestHousehold(t, db)

	first, err := repo.Save(ctx, domain.Vision{HouseholdID: householdID, Year: 2026, Theme: "A"})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, err := db.Pool().Exec(ctx, `DELETE FROM visions WHERE household_id = $1 AND year = 2026`, householdID); err != nil {
		t.Fatalf("delete vision: %v", err)
	}

	_, err = repo.Save(ctx, domain.Vision{HouseholdID: householdID, Year: 2026, Theme: "B", Version: first.Version})
	if !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("want domain.ErrNotFound for a deleted vision, got %v", err)
	}
}

// This repository does not get to assume v.Year was already validated --
// VisionService normally checks it first, but this test calls the
// repository directly, the same way a caller that forgot the check would.
// 67562 is the value versionParam's own reasoning uses for Version:
// int16(67562) == 2026, so an unguarded cast would silently write this save
// against the wrong household-year instead of refusing it.
func TestVisionSaveRefusesAYearOutOfRange(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	repo := postgres.NewVisionRepo(db)
	householdID := insertTestHousehold(t, db)

	_, err := repo.Save(ctx, domain.Vision{HouseholdID: householdID, Year: 67562, Theme: "T"})
	if !errors.Is(err, domain.ErrVisionYearOutOfRange) {
		t.Fatalf("want ErrVisionYearOutOfRange, got %v", err)
	}
}

// The same guard, on a milestone's own Year rather than the vision's --
// InsertVisionMilestoneParams.Year is a second, independent int16 cast.
func TestVisionSaveRefusesAMilestoneYearOutOfRange(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	repo := postgres.NewVisionRepo(db)
	householdID := insertTestHousehold(t, db)

	_, err := repo.Save(ctx, domain.Vision{
		HouseholdID: householdID, Year: 2026, Theme: "T",
		Milestones: []domain.Milestone{{Year: 67562, Title: "Bad"}},
	})
	if !errors.Is(err, domain.ErrVisionYearOutOfRange) {
		t.Fatalf("want ErrVisionYearOutOfRange, got %v", err)
	}
}
