package httpadapter_test

import (
	"context"
	"net/http"
	"strings"
	"testing"
)

// accountBody is the body mustCreateAccountID sends.
func accountBody() map[string]any {
	return map[string]any{
		"nickname": "Everyday spending", "type": "cash",
		"openingBalanceMinor": 500_000, "openingBalanceCurrency": "SGD",
		"openingBalanceAsOf": "2026-01-01",
	}
}

// fillAccounts inserts n archived accounts straight into the database, so a
// test can reach the cap without n requests. Archived, because archived
// rows count.
func fillAccounts(t *testing.T, env *testEnv, n int) {
	t.Helper()
	_, err := env.db.Pool().Exec(context.Background(),
		`INSERT INTO accounts (household_id, nickname, type, opening_balance_minor, opening_balance_currency,
		                       opening_balance_as_of, archived_at)
		 SELECT $1, 'Filler ' || g, 'cash', 0, 'SGD', DATE '2026-01-01', now() FROM generate_series(1, $2) g`,
		env.householdID, n)
	if err != nil {
		t.Fatalf("fill accounts: %v", err)
	}
}

func countHouseholdRows(t *testing.T, env *testEnv, table string) int {
	t.Helper()
	var n int
	if err := env.db.Pool().QueryRow(context.Background(),
		`SELECT count(*) FROM `+table+` WHERE household_id = $1`, env.householdID).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestAtTheAccountCapTheAPIAnswersLimitReached(t *testing.T) {
	env := newTestEnv(t)
	session, csrf := env.signIn(t, env.ownerEmail, env.ownerPassword)
	fillAccounts(t, env, 200-countHouseholdRows(t, env, "accounts")-1)

	if rec := env.authed(t, http.MethodPost, "/api/v1/accounts", accountBody(), session, csrf); rec.Code != http.StatusCreated {
		t.Fatalf("the 200th account: status %d, body %s", rec.Code, rec.Body)
	}
	rec := env.authed(t, http.MethodPost, "/api/v1/accounts", accountBody(), session, csrf)
	got := assertErrorResponse(t, rec, http.StatusConflict, "LIMIT_REACHED")
	want := "This household has reached Hearth's limit of 200 accounts. Archived accounts count towards it."
	if got.Error.Message != want {
		t.Fatalf("message = %q, want %q", got.Error.Message, want)
	}
}

// The cap never comes before the guards: at the cap, a signed-out caller
// still gets 401 and a member without the money capability still gets 403.
func TestTheCapNeverAnswersBeforeTheGuards(t *testing.T) {
	env := newTestEnv(t)
	fillAccounts(t, env, 200)

	rec := doOn(env.router, http.MethodPost, "/api/v1/accounts", accountBody())
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("signed out at the cap: status %d, want 401 (body %s)", rec.Code, rec.Body)
	}
	session, csrf := env.signIn(t, env.limitedEmail, env.limitedPassword)
	rec = env.authed(t, http.MethodPost, "/api/v1/accounts", accountBody(), session, csrf)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("no capability at the cap: status %d, want 403 (body %s)", rec.Code, rec.Body)
	}
}

func TestAFullMonthNamesTheMonth(t *testing.T) {
	env := newTestEnv(t)
	session, csrf := env.signIn(t, env.ownerEmail, env.ownerPassword)
	category, _ := env.firstExpenseCategory(t, session)
	account := env.mustCreateAccountID(t, session, csrf)
	_, err := env.db.Pool().Exec(context.Background(),
		`INSERT INTO transactions (household_id, kind, occurred_on, description, from_account_id, amount_minor, amount_currency)
		 SELECT $1, 'expense', DATE '2026-03-15', 'Filler', $2, 100, 'SGD' FROM generate_series(1, 3000)`,
		env.householdID, account)
	if err != nil {
		t.Fatal(err)
	}
	rec := env.authed(t, http.MethodPost, "/api/v1/transactions", map[string]any{
		"kind": "expense", "occurredOn": "2026-03-20", "description": "One too many",
		"categoryId": category, "fromAccountId": account, "amountMinor": 100,
	}, session, csrf)
	got := assertErrorResponse(t, rec, http.StatusConflict, "LIMIT_REACHED")
	want := "March 2026 already has 3,000 transactions, the most Hearth keeps for one month."
	if got.Error.Message != want {
		t.Fatalf("message = %q, want %q", got.Error.Message, want)
	}
}

func TestTextTooLongAnswers422WithTheField(t *testing.T) {
	env := newTestEnv(t)
	session, csrf := env.signIn(t, env.ownerEmail, env.ownerPassword)
	category, _ := env.firstExpenseCategory(t, session)
	account := env.mustCreateAccountID(t, session, csrf)
	rec := env.authed(t, http.MethodPost, "/api/v1/transactions", map[string]any{
		"kind": "expense", "occurredOn": "2026-03-20", "description": strings.Repeat("a", 201),
		"categoryId": category, "fromAccountId": account, "amountMinor": 100,
	}, session, csrf)
	got := assertErrorResponse(t, rec, http.StatusUnprocessableEntity, "TEXT_TOO_LONG")
	if got.Error.Message != "A description can be at most 200 characters." {
		t.Fatalf("message = %q", got.Error.Message)
	}
}

func TestABudgetOutsideTheWindowAnswers422(t *testing.T) {
	env := newTestEnv(t)
	session, csrf := env.signIn(t, env.ownerEmail, env.ownerPassword)
	rec := env.authed(t, http.MethodPut, "/api/v1/budgets/1999-12", map[string]any{"lines": []any{}}, session, csrf)
	assertErrorResponse(t, rec, http.StatusUnprocessableEntity, "BUDGET_MONTH_OUT_OF_RANGE")
}
