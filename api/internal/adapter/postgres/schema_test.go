package postgres_test

import (
	"context"
	"testing"

	"github.com/andreasoentoro/hearth/api/internal/adapter/postgres"
	"github.com/andreasoentoro/hearth/api/internal/testsupport"
)

func TestIdentitySchemaEnforcesTheCapabilityConstraints(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()

	var householdID, userID string
	err := db.Pool().QueryRow(ctx,
		`INSERT INTO households (name, family_name) VALUES ('Andreas & Christine', 'Oentoro') RETURNING id`).
		Scan(&householdID)
	if err != nil {
		t.Fatalf("insert household: %v", err)
	}
	err = db.Pool().QueryRow(ctx,
		`INSERT INTO users (display_name, avatar_initial) VALUES ('Ethan', 'E') RETURNING id`).
		Scan(&userID)
	if err != nil {
		t.Fatalf("insert user: %v", err)
	}

	_, err = db.Pool().Exec(ctx,
		`INSERT INTO memberships (household_id, user_id, role, capabilities)
		 VALUES ($1, $2, 'limited', ARRAY['calendar','marriage'])`, householdID, userID)
	if err == nil {
		t.Fatal("the database accepted a limited member holding marriage")
	}

	_, err = db.Pool().Exec(ctx,
		`INSERT INTO memberships (household_id, user_id, role, capabilities)
		 VALUES ($1, $2, 'limited', ARRAY['spaceships'])`, householdID, userID)
	if err == nil {
		t.Fatal("the database accepted an unknown capability")
	}
}

func TestLoginAttemptsAcceptAnUnknownAddress(t *testing.T) {
	db := openTestDB(t)

	_, err := db.Pool().Exec(context.Background(),
		`INSERT INTO login_attempts (email, succeeded) VALUES ('stranger@example.com', false)`)
	if err != nil {
		t.Fatalf("a login attempt with no household or user must be recordable: %v", err)
	}
}

func openTestDB(t *testing.T) *postgres.DB {
	t.Helper()
	db, err := postgres.Open(context.Background(), testsupport.StartPostgres(t))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(db.Close)
	return db
}
