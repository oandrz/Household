package postgres_test

import (
	"context"
	"strings"
	"testing"
	"time"

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

func TestSignupsSchema(t *testing.T) {
	db := openTestDB(t)
	pool := db.Pool()
	ctx := context.Background()

	t.Run("token_hash is unique", func(t *testing.T) {
		hash := []byte("a-token-hash-32-bytes-long------")
		expires := time.Now().Add(24 * time.Hour)
		if _, err := pool.Exec(ctx,
			`INSERT INTO signups (email, token_hash, expires_at) VALUES ($1, $2, $3)`,
			"first@example.test", hash, expires); err != nil {
			t.Fatalf("first insert: %v", err)
		}
		_, err := pool.Exec(ctx,
			`INSERT INTO signups (email, token_hash, expires_at) VALUES ($1, $2, $3)`,
			"second@example.test", hash, expires)
		if err == nil {
			t.Fatal("expected a unique violation on token_hash")
		}
	})

	t.Run("email is deliberately not unique", func(t *testing.T) {
		expires := time.Now().Add(24 * time.Hour)
		for i, hash := range [][]byte{[]byte("hash-one-aaaaaaaaaaaaaaaaaaaaaaa"), []byte("hash-two-bbbbbbbbbbbbbbbbbbbbbbb")} {
			if _, err := pool.Exec(ctx,
				`INSERT INTO signups (email, token_hash, expires_at) VALUES ($1, $2, $3)`,
				"repeat@example.test", hash, expires); err != nil {
				t.Fatalf("insert %d for the same address: %v", i, err)
			}
		}
	})

	t.Run("email is case-insensitive, like every other address column", func(t *testing.T) {
		expires := time.Now().Add(24 * time.Hour)
		if _, err := pool.Exec(ctx,
			`INSERT INTO signups (email, token_hash, expires_at) VALUES ($1, $2, $3)`,
			"Mixed@Example.Test", []byte("hash-three-ccccccccccccccccccccc"), expires); err != nil {
			t.Fatalf("insert: %v", err)
		}
		var count int
		if err := pool.QueryRow(ctx,
			`SELECT count(*) FROM signups WHERE email = $1`, "mixed@example.test").Scan(&count); err != nil {
			t.Fatalf("query: %v", err)
		}
		if count != 1 {
			t.Fatalf("citext lookup found %d rows, want 1", count)
		}
	})
}

// The reason avatar_initial was widened: cases.Upper(language.Und) (used by
// initialOf, not strings.ToUpper -- the standard library applies simple case
// mapping only and leaves 'ß' unchanged) can grow a rune, and char(1) rejects
// the result outright.
func TestAvatarInitialHoldsAMultiCharacterUppercase(t *testing.T) {
	db := openTestDB(t)
	pool := db.Pool()
	ctx := context.Background()
	if _, err := pool.Exec(ctx,
		`INSERT INTO users (display_name, avatar_initial) VALUES ($1, $2)`,
		"Straße", "SS"); err != nil {
		t.Fatalf("insert a two-character initial: %v", err)
	}
}

// TestAccountsSchema asserts what the accounts table's constraints promise
// beyond what TestAccountsRefusesANegativeLiability already covers: that the
// migration actually ran, and that ownership is optional while household
// membership is not -- the nullability half of "NULL means shared" in
// domain.Account's doc comment.
func TestAccountsSchema(t *testing.T) {
	db := openTestDB(t)
	pool := db.Pool()
	ctx := context.Background()

	t.Run("table exists", func(t *testing.T) {
		var count int
		if err := pool.QueryRow(ctx,
			`SELECT count(*) FROM information_schema.tables WHERE table_name = 'accounts'`).Scan(&count); err != nil {
			t.Fatalf("query: %v", err)
		}
		if count != 1 {
			t.Fatal("accounts table not found; migration did not run")
		}
	})

	t.Run("household_id is required, owner_membership_id is not", func(t *testing.T) {
		nullable := map[string]string{}
		rows, err := pool.Query(ctx,
			`SELECT column_name, is_nullable FROM information_schema.columns
			 WHERE table_name = 'accounts' AND column_name IN ('household_id', 'owner_membership_id')`)
		if err != nil {
			t.Fatalf("query: %v", err)
		}
		defer rows.Close()
		for rows.Next() {
			var column, isNullable string
			if err := rows.Scan(&column, &isNullable); err != nil {
				t.Fatalf("scan: %v", err)
			}
			nullable[column] = isNullable
		}
		if nullable["household_id"] != "NO" {
			t.Fatalf("household_id is_nullable = %q, want NO", nullable["household_id"])
		}
		if nullable["owner_membership_id"] != "YES" {
			t.Fatalf("owner_membership_id is_nullable = %q, want YES", nullable["owner_membership_id"])
		}
	})
}

// TestAccountsRefusesANegativeLiability proves the CHECK constraint is real,
// not just documentation. The application enforces the same rule in
// AccountService, but this is the layer that holds when something writes
// around it.
func TestAccountsRefusesANegativeLiability(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()

	householdID := insertTestHousehold(t, db)

	_, err := db.Pool().Exec(ctx, `
		INSERT INTO accounts (household_id, nickname, type,
		                      opening_balance_minor, opening_balance_currency,
		                      opening_balance_as_of)
		VALUES ($1, 'Car loan', 'loan', -1450000, 'SGD', current_date)`,
		householdID)
	if err == nil {
		t.Fatal("insert succeeded; the liabilities_are_not_negative constraint is missing")
	}
	if !strings.Contains(err.Error(), "liabilities_are_not_negative") {
		t.Fatalf("err = %v, want a liabilities_are_not_negative violation", err)
	}
}

// insertTestHousehold inserts the minimum household row needed to satisfy a
// foreign key, for tests that only care about a valid household_id and have
// no other requirement on the household itself.
func insertTestHousehold(t *testing.T, db *postgres.DB) string {
	t.Helper()
	var id string
	err := db.Pool().QueryRow(context.Background(),
		`INSERT INTO households (name, family_name) VALUES ('Test', 'Household') RETURNING id`).Scan(&id)
	if err != nil {
		t.Fatalf("insert household: %v", err)
	}
	return id
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
