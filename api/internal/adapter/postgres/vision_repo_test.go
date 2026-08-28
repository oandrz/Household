package postgres_test

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/andreasoentoro/hearth/api/internal/adapter/postgres"
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
