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

// TestTransactionSchemaRefusesNonsenseRows pins the constraints that make a
// wrong balance unrepresentable. Each insert below is a row the service also
// refuses; the database is the second line of defence, and a second line
// nobody tests is decoration.
func TestTransactionSchemaRefusesNonsenseRows(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()

	householdID := insertTestHousehold(t, db)

	newAccount := func(nickname string) string {
		var id string
		if err := db.Pool().QueryRow(ctx,
			`INSERT INTO accounts (household_id, nickname, type, opening_balance_minor,
			                       opening_balance_currency, opening_balance_as_of)
			 VALUES ($1, $2, 'cash', 0, 'SGD', DATE '2026-07-01') RETURNING id`,
			householdID, nickname).Scan(&id); err != nil {
			t.Fatalf("insert account %s: %v", nickname, err)
		}
		return id
	}
	from, to := newAccount("DBS"), newAccount("OCBC")

	// A real category, because the "a transfer carrying a category" case below
	// needs a non-NULL category_id to exercise transfer_has_no_category at
	// all. A subselect over an empty table yields NULL, the constraint is
	// satisfied, the insert succeeds -- and the subtest fails claiming the
	// database accepted something it never saw.
	var categoryID string
	if err := db.Pool().QueryRow(ctx,
		`INSERT INTO categories (household_id, name, kind, sort_order)
		 VALUES ($1, 'Groceries', 'expense', 1) RETURNING id`, householdID).
		Scan(&categoryID); err != nil {
		t.Fatalf("insert category: %v", err)
	}

	cases := []struct {
		name string
		sql  string
		args []any
		// constraint is the name the violation must carry, not just the fact
		// that some insert failed. Later tasks map this exact name to a
		// domain error (see the brief's Interfaces section); a rename here
		// would leave every subtest green while breaking that mapping.
		constraint string
	}{
		{
			name: "an expense with a destination account",
			sql: `INSERT INTO transactions (household_id, kind, occurred_on, description,
			          from_account_id, to_account_id, amount_minor, amount_currency)
			      VALUES ($1, 'expense', DATE '2026-07-18', 'Cold Storage', $2, $3, 5230, 'SGD')`,
			args:       []any{householdID, from, to},
			constraint: "accounts_match_kind",
		},
		{
			name: "a transfer with only one leg",
			sql: `INSERT INTO transactions (household_id, kind, occurred_on, description,
			          from_account_id, amount_minor, amount_currency)
			      VALUES ($1, 'transfer', DATE '2026-07-18', 'To BCA', $2, 50000, 'SGD')`,
			args:       []any{householdID, from},
			constraint: "accounts_match_kind",
		},
		{
			name: "a transfer from an account to itself",
			sql: `INSERT INTO transactions (household_id, kind, occurred_on, description,
			          from_account_id, to_account_id, amount_minor, amount_currency)
			      VALUES ($1, 'transfer', DATE '2026-07-18', 'Round trip', $2, $2, 50000, 'SGD')`,
			args:       []any{householdID, from},
			constraint: "accounts_match_kind",
		},
		{
			name: "a received amount with no currency",
			sql: `INSERT INTO transactions (household_id, kind, occurred_on, description,
			          from_account_id, to_account_id, amount_minor, amount_currency,
			          received_amount_minor)
			      VALUES ($1, 'transfer', DATE '2026-07-18', 'To BCA', $2, $3, 50000, 'SGD', 49800)`,
			args:       []any{householdID, from, to},
			constraint: "received_amount_pairs",
		},
		{
			name: "a received amount on an expense",
			sql: `INSERT INTO transactions (household_id, kind, occurred_on, description,
			          from_account_id, amount_minor, amount_currency,
			          received_amount_minor, received_amount_currency)
			      VALUES ($1, 'expense', DATE '2026-07-18', 'Cold Storage', $2, 5230, 'SGD', 5230, 'SGD')`,
			args:       []any{householdID, from},
			constraint: "received_amount_is_a_transfer_thing",
		},
		{
			name: "a transfer carrying a category",
			sql: `INSERT INTO transactions (household_id, kind, occurred_on, description,
			          from_account_id, to_account_id, amount_minor, amount_currency, category_id)
			      VALUES ($1, 'transfer', DATE '2026-07-18', 'To BCA', $2, $3, 50000, 'SGD', $4)`,
			args:       []any{householdID, from, to, categoryID},
			constraint: "transfer_has_no_category",
		},
		{
			name: "a zero amount",
			sql: `INSERT INTO transactions (household_id, kind, occurred_on, description,
			          from_account_id, amount_minor, amount_currency)
			      VALUES ($1, 'expense', DATE '2026-07-18', 'Free', $2, 0, 'SGD')`,
			args: []any{householdID, from},
			// Not one of the named constraints in the brief's Interfaces
			// section: this is the plain inline CHECK on amount_minor, and
			// Postgres names an unlabelled column check <table>_<column>_check.
			constraint: "transactions_amount_minor_check",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := db.Pool().Exec(ctx, tc.sql, tc.args...)
			if err == nil {
				t.Fatal("the database accepted it")
			}
			if !strings.Contains(err.Error(), tc.constraint) {
				t.Fatalf("err = %v, want a %s violation", err, tc.constraint)
			}
		})
	}
}

// TestCategoryNamesAreUniquePerHousehold pins the constraint EnsureSeeded's
// ON CONFLICT depends on. Without it the seed is not idempotent and two
// simultaneous first requests produce two starter sets.
func TestCategoryNamesAreUniquePerHousehold(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()

	householdID := insertTestHousehold(t, db)
	insert := `INSERT INTO categories (household_id, name, kind, sort_order)
	           VALUES ($1, 'Groceries', 'expense', 1)`
	if _, err := db.Pool().Exec(ctx, insert, householdID); err != nil {
		t.Fatalf("first insert: %v", err)
	}
	if _, err := db.Pool().Exec(ctx, insert, householdID); err == nil {
		t.Fatal("the database accepted a duplicate category name")
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
