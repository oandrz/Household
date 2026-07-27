package postgres_test

import (
	"context"
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

// The reason avatar_initial was widened: strings.ToUpper can grow a rune, and
// char(1) rejects the result outright.
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

func openTestDB(t *testing.T) *postgres.DB {
	t.Helper()
	db, err := postgres.Open(context.Background(), testsupport.StartPostgres(t))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(db.Close)
	return db
}
