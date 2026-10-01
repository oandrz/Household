package postgres_test

import (
	"database/sql"
	"testing"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"

	"github.com/andreasoentoro/hearth/api/internal/testsupport"
)

// The migration that adds households.timezone has two jobs, and a database
// built from scratch only ever exercises one of them. This test rolls back to
// the version before it, so that a household exists BEFORE the column does --
// the state every real install is in when it deploys.
func TestTimezoneMigrationPutsExistingHouseholdsOnSingaporeAndNewRowsOnUTC(t *testing.T) {
	const versionBeforeTimezone = 21
	// The tests run in this package's directory.
	const migrationsDir = "../../../migrations"

	db, err := sql.Open("pgx", testsupport.StartPostgres(t))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	if err := goose.DownTo(db, migrationsDir, versionBeforeTimezone); err != nil {
		t.Fatalf("roll back to version %d: %v", versionBeforeTimezone, err)
	}
	var existingID string
	if err := db.QueryRow(
		`INSERT INTO households (name, family_name) VALUES ('Before', 'Before') RETURNING id`).Scan(&existingID); err != nil {
		t.Fatalf("insert the household that predates the column: %v", err)
	}

	if err := goose.Up(db, migrationsDir); err != nil {
		t.Fatalf("migrate up again: %v", err)
	}

	var existingZone string
	if err := db.QueryRow(`SELECT timezone FROM households WHERE id = $1`, existingID).Scan(&existingZone); err != nil {
		t.Fatalf("read the existing household: %v", err)
	}
	if existingZone != "Asia/Singapore" {
		t.Errorf("a household that existed before the migration is on %q, want Asia/Singapore", existingZone)
	}

	var newZone string
	if err := db.QueryRow(
		`INSERT INTO households (name, family_name) VALUES ('After', 'After') RETURNING timezone`).Scan(&newZone); err != nil {
		t.Fatalf("insert a household after the migration: %v", err)
	}
	if newZone != "UTC" {
		t.Errorf("a row written without a zone is on %q, want the column default UTC", newZone)
	}

	if _, err := db.Exec(`UPDATE households SET timezone = '' WHERE id = $1`, existingID); err == nil {
		t.Error("the column accepted an empty time zone")
	}
}
