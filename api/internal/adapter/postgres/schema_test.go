package postgres_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

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
			name: "a negative received amount",
			sql: `INSERT INTO transactions (household_id, kind, occurred_on, description,
			          from_account_id, to_account_id, amount_minor, amount_currency,
			          received_amount_minor, received_amount_currency)
			      VALUES ($1, 'transfer', DATE '2026-07-18', 'To BCA', $2, $3, 50000, 'SGD', -100, 'SGD')`,
			args:       []any{householdID, from, to},
			constraint: "received_amount_is_positive",
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

// TestBudgetsSchema pins budgets' shape: the table exists, expected_income_minor
// stays nullable (NULL means "chose not to say", never zero -- see the
// migration's comment), and (household_id, month) is unique so Upsert can rely
// on ON CONFLICT rather than a read-then-write race.
func TestBudgetsSchema(t *testing.T) {
	db := openTestDB(t)
	pool := db.Pool()
	ctx := context.Background()

	t.Run("table exists", func(t *testing.T) {
		var count int
		if err := pool.QueryRow(ctx,
			`SELECT count(*) FROM information_schema.tables WHERE table_name = 'budgets'`).Scan(&count); err != nil {
			t.Fatalf("query: %v", err)
		}
		if count != 1 {
			t.Fatal("budgets table not found; migration did not run")
		}
	})

	t.Run("expected_income_minor is nullable and has no default", func(t *testing.T) {
		// A DEFAULT 0 column is just as "nullable" as one with no default --
		// is_nullable alone can't tell them apart, and the whole point of the
		// column (see the migration's comment) is that NULL and zero mean
		// different things. Assert column_default is NULL, then prove it with
		// a real insert that omits the column and reads NULL back.
		var columnDefault *string
		if err := pool.QueryRow(ctx,
			`SELECT column_default FROM information_schema.columns
			 WHERE table_name = 'budgets' AND column_name = 'expected_income_minor'`).Scan(&columnDefault); err != nil {
			t.Fatalf("query: %v", err)
		}
		if columnDefault != nil {
			t.Fatalf("expected_income_minor column_default = %q, want NULL (no default)", *columnDefault)
		}

		householdID := insertTestHousehold(t, db)
		var readBack *int64
		if err := pool.QueryRow(ctx,
			`INSERT INTO budgets (household_id, month) VALUES ($1, DATE '2026-07-01')
			 RETURNING expected_income_minor`, householdID).Scan(&readBack); err != nil {
			t.Fatalf("insert: %v", err)
		}
		if readBack != nil {
			t.Fatalf("expected_income_minor = %d, want NULL when not provided", *readBack)
		}
	})

	t.Run("household_id and month are unique together", func(t *testing.T) {
		householdID := insertTestHousehold(t, db)
		insert := `INSERT INTO budgets (household_id, month) VALUES ($1, $2)`
		if _, err := pool.Exec(ctx, insert, householdID, "2026-07-01"); err != nil {
			t.Fatalf("first insert: %v", err)
		}
		if _, err := pool.Exec(ctx, insert, householdID, "2026-07-01"); err == nil {
			t.Fatal("the database accepted a second budget for the same household and month")
		}
		// Discriminates the pair from a single-column UNIQUE on household_id
		// alone: a different month for the same household must still succeed.
		if _, err := pool.Exec(ctx, insert, householdID, "2026-08-01"); err != nil {
			t.Fatalf("same household, different month: %v", err)
		}
	})

	t.Run("deleting the household cascades to its budgets", func(t *testing.T) {
		householdID := insertTestHousehold(t, db)
		var budgetID string
		if err := pool.QueryRow(ctx,
			`INSERT INTO budgets (household_id, month) VALUES ($1, DATE '2026-08-01') RETURNING id`,
			householdID).Scan(&budgetID); err != nil {
			t.Fatalf("insert budget: %v", err)
		}
		if _, err := pool.Exec(ctx, `DELETE FROM households WHERE id = $1`, householdID); err != nil {
			t.Fatalf("delete household: %v", err)
		}
		var count int
		if err := pool.QueryRow(ctx, `SELECT count(*) FROM budgets WHERE id = $1`, budgetID).Scan(&count); err != nil {
			t.Fatalf("query: %v", err)
		}
		if count != 0 {
			t.Fatal("budget survived its household's deletion; ON DELETE CASCADE is missing")
		}
	})
}

// TestBudgetLinesSchema pins budget_lines' shape: one cap per category per
// budget, a non-negative cap, cascading with its parent budget, and a
// category reference that refuses to cascade -- categories archive, they
// don't delete, so a line pointing at one must block the delete instead of
// silently disappearing.
func TestBudgetLinesSchema(t *testing.T) {
	db := openTestDB(t)
	pool := db.Pool()
	ctx := context.Background()

	// newBudget and newCategory take the caller's own *testing.T (like
	// insertTestHousehold does) rather than closing over TestBudgetLinesSchema's
	// t. A helper that calls t.Fatalf on the parent test, invoked from inside
	// a t.Run subtest, unwinds the wrong goroutine's test: the subtest never
	// gets a PASS/FAIL of its own and siblings can be skipped. See the RED
	// evidence in the task report.
	newBudget := func(t *testing.T, householdID string, month string) string {
		t.Helper()
		var id string
		if err := pool.QueryRow(ctx,
			`INSERT INTO budgets (household_id, month) VALUES ($1, $2) RETURNING id`,
			householdID, month).Scan(&id); err != nil {
			t.Fatalf("insert budget: %v", err)
		}
		return id
	}
	newCategory := func(t *testing.T, householdID, name string) string {
		t.Helper()
		var id string
		if err := pool.QueryRow(ctx,
			`INSERT INTO categories (household_id, name, kind, sort_order)
			 VALUES ($1, $2, 'expense', 1) RETURNING id`, householdID, name).Scan(&id); err != nil {
			t.Fatalf("insert category: %v", err)
		}
		return id
	}

	t.Run("table exists", func(t *testing.T) {
		var count int
		if err := pool.QueryRow(ctx,
			`SELECT count(*) FROM information_schema.tables WHERE table_name = 'budget_lines'`).Scan(&count); err != nil {
			t.Fatalf("query: %v", err)
		}
		if count != 1 {
			t.Fatal("budget_lines table not found; migration did not run")
		}
	})

	t.Run("cap_minor must not be negative", func(t *testing.T) {
		householdID := insertTestHousehold(t, db)
		budgetID := newBudget(t, householdID, "2026-07-01")
		categoryID := newCategory(t, householdID, "Groceries")

		_, err := pool.Exec(ctx,
			`INSERT INTO budget_lines (budget_id, category_id, cap_minor) VALUES ($1, $2, -100)`,
			budgetID, categoryID)
		if err == nil {
			t.Fatal("the database accepted a negative cap_minor")
		}
		if !strings.Contains(err.Error(), "budget_lines_cap_minor_check") {
			t.Fatalf("err = %v, want a budget_lines_cap_minor_check violation", err)
		}
	})

	t.Run("budget_id and category_id are unique together", func(t *testing.T) {
		householdID := insertTestHousehold(t, db)
		budgetID := newBudget(t, householdID, "2026-09-01")
		categoryID := newCategory(t, householdID, "Dining out")

		insert := `INSERT INTO budget_lines (budget_id, category_id, cap_minor) VALUES ($1, $2, 50000)`
		if _, err := pool.Exec(ctx, insert, budgetID, categoryID); err != nil {
			t.Fatalf("first insert: %v", err)
		}
		if _, err := pool.Exec(ctx, insert, budgetID, categoryID); err == nil {
			t.Fatal("the database accepted two lines for the same budget and category")
		}
		// Discriminates the pair from a single-column UNIQUE on budget_id
		// alone: a different category on the same budget must still succeed.
		otherCategoryID := newCategory(t, householdID, "Dining out 2")
		if _, err := pool.Exec(ctx, insert, budgetID, otherCategoryID); err != nil {
			t.Fatalf("same budget, different category: %v", err)
		}
	})

	t.Run("deleting the budget cascades to its lines", func(t *testing.T) {
		householdID := insertTestHousehold(t, db)
		budgetID := newBudget(t, householdID, "2026-10-01")
		categoryID := newCategory(t, householdID, "Transport")

		var lineID string
		if err := pool.QueryRow(ctx,
			`INSERT INTO budget_lines (budget_id, category_id, cap_minor) VALUES ($1, $2, 20000) RETURNING id`,
			budgetID, categoryID).Scan(&lineID); err != nil {
			t.Fatalf("insert line: %v", err)
		}
		if _, err := pool.Exec(ctx, `DELETE FROM budgets WHERE id = $1`, budgetID); err != nil {
			t.Fatalf("delete budget: %v", err)
		}
		var count int
		if err := pool.QueryRow(ctx, `SELECT count(*) FROM budget_lines WHERE id = $1`, lineID).Scan(&count); err != nil {
			t.Fatalf("query: %v", err)
		}
		if count != 0 {
			t.Fatal("line survived its budget's deletion; ON DELETE CASCADE is missing")
		}
	})

	t.Run("deleting a category referenced by a line is refused", func(t *testing.T) {
		householdID := insertTestHousehold(t, db)
		budgetID := newBudget(t, householdID, "2026-11-01")
		categoryID := newCategory(t, householdID, "Utilities")

		if _, err := pool.Exec(ctx,
			`INSERT INTO budget_lines (budget_id, category_id, cap_minor) VALUES ($1, $2, 30000)`,
			budgetID, categoryID); err != nil {
			t.Fatalf("insert line: %v", err)
		}
		_, err := pool.Exec(ctx, `DELETE FROM categories WHERE id = $1`, categoryID)
		if err == nil {
			t.Fatal("the database deleted a category still referenced by a budget line; NO ACTION is missing")
		}
	})
}

// TestGoalsSchema pins goals' shape: the table and its columns exist as
// 00007 declares, a household's goal names are unique so an archived goal's
// name collision can offer restore instead of a bare 409 (see the
// migration's comment), and deleting a household cascades to its goals.
func TestGoalsSchema(t *testing.T) {
	db := openTestDB(t)
	pool := db.Pool()
	ctx := context.Background()

	t.Run("table exists", func(t *testing.T) {
		var count int
		if err := pool.QueryRow(ctx,
			`SELECT count(*) FROM information_schema.tables WHERE table_name = 'goals'`).Scan(&count); err != nil {
			t.Fatalf("query: %v", err)
		}
		if count != 1 {
			t.Fatal("goals table not found; migration did not run")
		}
	})

	t.Run("column set matches the migration", func(t *testing.T) {
		want := map[string]string{
			"id":                    "NO",
			"household_id":          "NO",
			"name":                  "NO",
			"target_amount_minor":   "NO",
			"currency":              "NO",
			"target_month":          "YES",
			"planned_monthly_minor": "NO",
			"archived_at":           "YES",
			"created_at":            "NO",
			"updated_at":            "NO",
		}
		got := columnNullability(t, pool, "goals")
		if len(got) != len(want) {
			t.Fatalf("goals has %d columns, want %d: got %v", len(got), len(want), got)
		}
		for name, nullable := range want {
			if got[name] != nullable {
				t.Fatalf("goals.%s is_nullable = %q, want %q", name, got[name], nullable)
			}
		}
	})

	t.Run("household_id and name are unique together", func(t *testing.T) {
		householdID := insertTestHousehold(t, db)
		insert := `INSERT INTO goals (household_id, name, target_amount_minor, currency, planned_monthly_minor)
		           VALUES ($1, 'Emergency fund', 600000, 'SGD', 50000)`
		if _, err := pool.Exec(ctx, insert, householdID); err != nil {
			t.Fatalf("first insert: %v", err)
		}
		if _, err := pool.Exec(ctx, insert, householdID); err == nil {
			t.Fatal("the database accepted two goals with the same name for one household")
		}
		// Discriminates the pair from a single-column UNIQUE on household_id
		// alone: a different name for the same household must still succeed.
		otherName := `INSERT INTO goals (household_id, name, target_amount_minor, currency, planned_monthly_minor)
		              VALUES ($1, 'New car', 2000000, 'SGD', 100000)`
		if _, err := pool.Exec(ctx, otherName, householdID); err != nil {
			t.Fatalf("same household, different name: %v", err)
		}
	})

	t.Run("deleting the household cascades to its goals", func(t *testing.T) {
		householdID := insertTestHousehold(t, db)
		var goalID string
		if err := pool.QueryRow(ctx,
			`INSERT INTO goals (household_id, name, target_amount_minor, currency, planned_monthly_minor)
			 VALUES ($1, 'New car', 2000000, 'SGD', 100000) RETURNING id`,
			householdID).Scan(&goalID); err != nil {
			t.Fatalf("insert goal: %v", err)
		}
		if _, err := pool.Exec(ctx, `DELETE FROM households WHERE id = $1`, householdID); err != nil {
			t.Fatalf("delete household: %v", err)
		}
		var count int
		if err := pool.QueryRow(ctx, `SELECT count(*) FROM goals WHERE id = $1`, goalID).Scan(&count); err != nil {
			t.Fatalf("query: %v", err)
		}
		if count != 0 {
			t.Fatal("goal survived its household's deletion; ON DELETE CASCADE is missing")
		}
	})
}

// TestGoalContributionsSchema pins goal_contributions' shape: the table and
// its columns exist, the three CHECKs that keep a row honest hold, the
// partial unique index backstopping RollOverToGoal is real (and genuinely
// partial -- rows that are not rollovers must not collide with each other),
// and deleting a goal still referenced by a contribution is refused, because
// a goal is archived, never deleted (see the migration's comment).
func TestGoalContributionsSchema(t *testing.T) {
	db := openTestDB(t)
	pool := db.Pool()
	ctx := context.Background()

	newGoal := func(t *testing.T, householdID, name string) string {
		t.Helper()
		var id string
		if err := pool.QueryRow(ctx,
			`INSERT INTO goals (household_id, name, target_amount_minor, currency, planned_monthly_minor)
			 VALUES ($1, $2, 600000, 'SGD', 50000) RETURNING id`,
			householdID, name).Scan(&id); err != nil {
			t.Fatalf("insert goal: %v", err)
		}
		return id
	}

	t.Run("table exists", func(t *testing.T) {
		var count int
		if err := pool.QueryRow(ctx,
			`SELECT count(*) FROM information_schema.tables WHERE table_name = 'goal_contributions'`).Scan(&count); err != nil {
			t.Fatalf("query: %v", err)
		}
		if count != 1 {
			t.Fatal("goal_contributions table not found; migration did not run")
		}
	})

	t.Run("column set matches the migration", func(t *testing.T) {
		want := map[string]string{
			"id":                  "NO",
			"goal_id":             "NO",
			"household_id":        "NO",
			"amount_minor":        "NO",
			"occurred_on":         "NO",
			"note":                "NO",
			"source":              "NO",
			"source_budget_month": "YES",
			"created_at":          "NO",
		}
		got := columnNullability(t, pool, "goal_contributions")
		if len(got) != len(want) {
			t.Fatalf("goal_contributions has %d columns, want %d: got %v", len(got), len(want), got)
		}
		for name, nullable := range want {
			if got[name] != nullable {
				t.Fatalf("goal_contributions.%s is_nullable = %q, want %q", name, got[name], nullable)
			}
		}
	})

	t.Run("refuses nonsense rows", func(t *testing.T) {
		householdID := insertTestHousehold(t, db)
		goalID := newGoal(t, householdID, "Emergency fund")

		cases := []struct {
			name string
			sql  string
			// constraint is the name the violation must carry -- see the
			// comment on the equivalent field in
			// TestTransactionSchemaRefusesNonsenseRows.
			constraint string
		}{
			{
				name: "a zero amount",
				sql: `INSERT INTO goal_contributions (goal_id, household_id, amount_minor, occurred_on, source)
				      VALUES ($1, $2, 0, DATE '2026-07-18', 'manual')`,
				constraint: "goal_contributions_amount_minor_check",
			},
			{
				name: "a non-rollover row naming a budget month",
				sql: `INSERT INTO goal_contributions (goal_id, household_id, amount_minor, occurred_on, source, source_budget_month)
				      VALUES ($1, $2, 5000, DATE '2026-07-18', 'manual', DATE '2026-07-01')`,
				constraint: "budget_month_is_a_rollover_thing",
			},
			{
				name: "a rollover row with no budget month",
				sql: `INSERT INTO goal_contributions (goal_id, household_id, amount_minor, occurred_on, source)
				      VALUES ($1, $2, 5000, DATE '2026-07-18', 'budget_rollover')`,
				constraint: "rollover_names_its_month",
			},
		}

		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				_, err := pool.Exec(ctx, tc.sql, goalID, householdID)
				if err == nil {
					t.Fatal("the database accepted it")
				}
				if !strings.Contains(err.Error(), tc.constraint) {
					t.Fatalf("err = %v, want a %s violation", err, tc.constraint)
				}
			})
		}
	})

	t.Run("only one rollover per household-month", func(t *testing.T) {
		householdID := insertTestHousehold(t, db)
		goalID := newGoal(t, householdID, "Emergency fund")
		insert := `INSERT INTO goal_contributions (goal_id, household_id, amount_minor, occurred_on, source, source_budget_month)
		           VALUES ($1, $2, 12000, DATE '2026-07-28', 'budget_rollover', DATE '2026-07-01')`
		if _, err := pool.Exec(ctx, insert, goalID, householdID); err != nil {
			t.Fatalf("first insert: %v", err)
		}
		if _, err := pool.Exec(ctx, insert, goalID, householdID); err == nil {
			t.Fatal("the database accepted a second rollover for the same household and month")
		}
		// Discriminates source_budget_month being part of the key from a
		// UNIQUE(household_id) alone: a different month for the same
		// household must still succeed.
		otherMonth := `INSERT INTO goal_contributions (goal_id, household_id, amount_minor, occurred_on, source, source_budget_month)
		               VALUES ($1, $2, 8000, DATE '2026-08-28', 'budget_rollover', DATE '2026-08-01')`
		if _, err := pool.Exec(ctx, otherMonth, goalID, householdID); err != nil {
			t.Fatalf("same household, different month: %v", err)
		}
		// Two manual contributions -- source_budget_month NULL on both --
		// must not collide. This does not distinguish the index's WHERE
		// source = 'budget_rollover' clause from a plain UNIQUE over the same
		// two columns: Postgres treats NULL as distinct from NULL either way,
		// and budget_month_is_a_rollover_thing already forbids a non-NULL
		// source_budget_month on a non-rollover row, so the predicate is
		// unreachable by construction -- belt and braces, per the migration's
		// comment, not a behaviour this test can isolate.
		manual := `INSERT INTO goal_contributions (goal_id, household_id, amount_minor, occurred_on, source)
		          VALUES ($1, $2, 1000, DATE '2026-07-20', 'manual')`
		if _, err := pool.Exec(ctx, manual, goalID, householdID); err != nil {
			t.Fatalf("first manual contribution: %v", err)
		}
		if _, err := pool.Exec(ctx, manual, goalID, householdID); err != nil {
			t.Fatalf("second manual contribution (NULL source_budget_month must not collide): %v", err)
		}
	})

	t.Run("deleting a goal referenced by a contribution is refused", func(t *testing.T) {
		householdID := insertTestHousehold(t, db)
		goalID := newGoal(t, householdID, "Emergency fund")
		if _, err := pool.Exec(ctx,
			`INSERT INTO goal_contributions (goal_id, household_id, amount_minor, occurred_on, source)
			 VALUES ($1, $2, 5000, DATE '2026-07-18', 'manual')`, goalID, householdID); err != nil {
			t.Fatalf("insert contribution: %v", err)
		}
		_, err := pool.Exec(ctx, `DELETE FROM goals WHERE id = $1`, goalID)
		if err == nil {
			t.Fatal("the database deleted a goal still referenced by a contribution; NO ACTION is missing")
		}
	})
}

// TestBudgetsRolloverSchema pins the two columns and the CHECK 00007 added to
// budgets: the rollover stamp is set whole (both columns or neither -- half a
// stamp is a budget that claims to be rolled over into no goal, or into a
// goal it never says), and a rolled-over budget still names a real goal, NO
// ACTION, because goals are never deleted.
func TestBudgetsRolloverSchema(t *testing.T) {
	db := openTestDB(t)
	pool := db.Pool()
	ctx := context.Background()

	newGoal := func(t *testing.T, householdID string) string {
		t.Helper()
		var id string
		if err := pool.QueryRow(ctx,
			`INSERT INTO goals (household_id, name, target_amount_minor, currency, planned_monthly_minor)
			 VALUES ($1, 'Emergency fund', 600000, 'SGD', 50000) RETURNING id`,
			householdID).Scan(&id); err != nil {
			t.Fatalf("insert goal: %v", err)
		}
		return id
	}
	newBudget := func(t *testing.T, householdID, month string) string {
		t.Helper()
		var id string
		if err := pool.QueryRow(ctx,
			`INSERT INTO budgets (household_id, month) VALUES ($1, $2) RETURNING id`,
			householdID, month).Scan(&id); err != nil {
			t.Fatalf("insert budget: %v", err)
		}
		return id
	}

	t.Run("rolled_over_at and rollover_goal_id are both nullable", func(t *testing.T) {
		nullable := columnNullability(t, pool, "budgets")
		if nullable["rolled_over_at"] != "YES" {
			t.Fatalf("rolled_over_at is_nullable = %q, want YES", nullable["rolled_over_at"])
		}
		if nullable["rollover_goal_id"] != "YES" {
			t.Fatalf("rollover_goal_id is_nullable = %q, want YES", nullable["rollover_goal_id"])
		}
	})

	t.Run("the rollover stamp is set whole or not at all", func(t *testing.T) {
		householdID := insertTestHousehold(t, db)
		goalID := newGoal(t, householdID)
		budgetID := newBudget(t, householdID, "2026-07-01")

		_, err := pool.Exec(ctx,
			`UPDATE budgets SET rolled_over_at = now() WHERE id = $1`, budgetID)
		if err == nil {
			t.Fatal("the database accepted a rollover timestamp with no goal")
		}
		if !strings.Contains(err.Error(), "rollover_stamp_is_whole") {
			t.Fatalf("err = %v, want a rollover_stamp_is_whole violation", err)
		}

		_, err = pool.Exec(ctx,
			`UPDATE budgets SET rollover_goal_id = $1 WHERE id = $2`, goalID, budgetID)
		if err == nil {
			t.Fatal("the database accepted a rollover goal with no timestamp")
		}
		if !strings.Contains(err.Error(), "rollover_stamp_is_whole") {
			t.Fatalf("err = %v, want a rollover_stamp_is_whole violation", err)
		}

		if _, err := pool.Exec(ctx,
			`UPDATE budgets SET rolled_over_at = now(), rollover_goal_id = $1 WHERE id = $2`,
			goalID, budgetID); err != nil {
			t.Fatalf("setting both together: %v", err)
		}
	})

	t.Run("deleting a goal referenced by a rolled-over budget is refused", func(t *testing.T) {
		householdID := insertTestHousehold(t, db)
		goalID := newGoal(t, householdID)
		budgetID := newBudget(t, householdID, "2026-08-01")
		if _, err := pool.Exec(ctx,
			`UPDATE budgets SET rolled_over_at = now(), rollover_goal_id = $1 WHERE id = $2`,
			goalID, budgetID); err != nil {
			t.Fatalf("stamp budget as rolled over: %v", err)
		}
		_, err := pool.Exec(ctx, `DELETE FROM goals WHERE id = $1`, goalID)
		if err == nil {
			t.Fatal("the database deleted a goal still named by a rolled-over budget; NO ACTION is missing")
		}
	})
}

// TestBillsSchema pins the two constraints bills and bill_payments enforce
// beyond plain column shape: a NULL next_due is only legal for a settled
// one-off (see the migration's comment on only_a_one_off_has_no_next_due),
// and one occurrence of a bill can be paid only once, the belt-and-braces
// UNIQUE (bill_id, due_on) that backstops BillService's own check against a
// double-clicked Mark paid.
func TestBillsSchema(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)

	t.Run("only a one-off may have a NULL next_due", func(t *testing.T) {
		h, acct := seedHouseholdAndAccount(t, ctx, db)
		_, err := db.Pool().Exec(ctx, `
			INSERT INTO bills (household_id, name, amount_minor, cadence, next_due,
			                   due_anchor_day, pay_from_account_id)
			VALUES ($1, 'Broken', 1000, 'monthly', NULL, 1, $2)`, h, acct)
		if err == nil {
			t.Fatal("expected only_a_one_off_has_no_next_due to refuse a monthly bill with no next date")
		}
		// Names the constraint, not just any failure -- see the comment on
		// the equivalent field in TestTransactionSchemaRefusesNonsenseRows: a
		// later migration that absorbs this case into some other check (a new
		// NOT NULL column, say) must not leave this subtest green while
		// silently losing the rule it pins.
		if !strings.Contains(err.Error(), "only_a_one_off_has_no_next_due") {
			t.Fatalf("err = %v, want an only_a_one_off_has_no_next_due violation", err)
		}
	})

	t.Run("one occurrence can be paid only once", func(t *testing.T) {
		h, acct := seedHouseholdAndAccount(t, ctx, db)
		bill := insertBill(t, ctx, db, h, acct, "SP utilities", "monthly", "2026-08-08")
		pay := func() error {
			_, err := db.Pool().Exec(ctx, `
				INSERT INTO bill_payments (bill_id, household_id, due_on, paid_on, amount_minor)
				VALUES ($1, $2, '2026-08-08', '2026-08-08', 14230)`, bill, h)
			return err
		}
		if err := pay(); err != nil {
			t.Fatalf("first payment: %v", err)
		}
		if err := pay(); err == nil {
			t.Fatal("expected UNIQUE (bill_id, due_on) to refuse a second payment of one occurrence")
		}
	})
}

// seedHouseholdAndAccount inserts the minimum household and account bills'
// two required foreign keys need (household_id and pay_from_account_id), for
// tests that only care about valid IDs and have no other requirement on
// either row.
func seedHouseholdAndAccount(t *testing.T, ctx context.Context, db *postgres.DB) (householdID, accountID string) {
	t.Helper()
	householdID = insertTestHousehold(t, db)
	if err := db.Pool().QueryRow(ctx,
		`INSERT INTO accounts (household_id, nickname, type, opening_balance_minor,
		                      opening_balance_currency, opening_balance_as_of)
		 VALUES ($1, 'Test account', 'cash', 0, 'SGD', DATE '2026-07-01') RETURNING id`,
		householdID).Scan(&accountID); err != nil {
		t.Fatalf("insert account: %v", err)
	}
	return householdID, accountID
}

// insertBill inserts a bill with a fixed amount, deriving due_anchor_day from
// nextDue's day-of-month the way BillService will at create -- so a caller
// only states what the test actually varies (cadence, next_due) and still
// gets a schema-valid row.
func insertBill(t *testing.T, ctx context.Context, db *postgres.DB, householdID, accountID, name, cadence, nextDue string) string {
	t.Helper()
	due, err := time.Parse("2006-01-02", nextDue)
	if err != nil {
		t.Fatalf("parse nextDue %q: %v", nextDue, err)
	}
	var id string
	if err := db.Pool().QueryRow(ctx,
		`INSERT INTO bills (household_id, name, amount_minor, cadence, next_due,
		                   due_anchor_day, pay_from_account_id)
		 VALUES ($1, $2, 10000, $3, $4, $5, $6) RETURNING id`,
		householdID, name, cadence, nextDue, due.Day(), accountID).Scan(&id); err != nil {
		t.Fatalf("insert bill: %v", err)
	}
	return id
}

// columnNullability maps a table's columns to information_schema's
// is_nullable ("YES"/"NO"), so a schema test can assert an entire column set
// -- names and nullability together -- in one query instead of one column at
// a time.
func columnNullability(t *testing.T, pool *pgxpool.Pool, table string) map[string]string {
	t.Helper()
	ctx := context.Background()
	rows, err := pool.Query(ctx,
		`SELECT column_name, is_nullable FROM information_schema.columns WHERE table_name = $1`, table)
	if err != nil {
		t.Fatalf("query columns for %s: %v", table, err)
	}
	defer rows.Close()
	got := map[string]string{}
	for rows.Next() {
		var name, nullable string
		if err := rows.Scan(&name, &nullable); err != nil {
			t.Fatalf("scan: %v", err)
		}
		got[name] = nullable
	}
	return got
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
