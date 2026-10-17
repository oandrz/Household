package httpadapter_test

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/andreasoentoro/hearth/api/internal/domain"
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

// Every capped text field, on every signed-in route that writes it, answers
// 422 TEXT_TOO_LONG. A route whose service forgot the domain rule would
// reach the database CHECK instead and answer 500, which this sweep catches.
// The public sign-up and invite-accept routes are covered by their service
// tests.
func TestEveryCappedTextFieldAnswers422NotA500(t *testing.T) {
	env := newTestEnv(t)
	session, csrf := env.signIn(t, env.ownerEmail, env.ownerPassword)
	name81 := strings.Repeat("a", 81)
	unit21 := strings.Repeat("g", 21)
	desc201 := strings.Repeat("a", 201)
	note501 := strings.Repeat("a", 501)

	category, _ := env.firstExpenseCategory(t, session)
	account := env.mustCreateAccountID(t, session, csrf)
	goal := env.mustCreateGoal(t, session, csrf, map[string]any{
		"name": "Emergency fund", "targetMinor": 100_000, "currency": "SGD", "plannedMonthlyMinor": 10_000,
	}).Goal
	bill := env.mustCreateBill(t, session, csrf, map[string]any{
		"name": "Internet", "amountMinor": 45_000, "cadence": "monthly",
		"nextDue": "2026-12-20", "payFromAccountId": account,
	}).Bill
	txRec := env.authed(t, http.MethodPost, "/api/v1/transactions", map[string]any{
		"kind": "expense", "occurredOn": "2026-01-05", "description": "Weekly shop",
		"categoryId": category, "fromAccountId": account, "amountMinor": 1_250,
	}, session, csrf)
	if txRec.Code != http.StatusCreated {
		t.Fatalf("create transaction: %d %s", txRec.Code, txRec.Body)
	}
	var created struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(txRec.Body.Bytes(), &created); err != nil || created.ID == "" {
		t.Fatalf("decode transaction: %v (body %s)", err, txRec.Body)
	}
	transaction := created.ID
	brokerage := newHoldingAccount(t, env, session, csrf, "Brokerage", "investment")
	holding := newHolding(t, env, session, csrf, brokerage, "D05")
	retroRec := env.authed(t, http.MethodPost, "/api/v1/retros", nil, session, csrf)
	if retroRec.Code != http.StatusCreated {
		t.Fatalf("start retro: %d %s", retroRec.Code, retroRec.Body)
	}
	var retro retroDetailBody
	if err := json.Unmarshal(retroRec.Body.Bytes(), &retro); err != nil {
		t.Fatal(err)
	}

	// The email invite channel sits behind a flag; turn it on so the invite
	// row reaches the name rule.
	if err := env.featureFlags.SetGlobal(context.Background(), string(domain.FlagEmailInvites), true, ""); err != nil {
		t.Fatalf("enable email invites: %v", err)
	}

	rows := []struct {
		name, method, path string
		body               map[string]any
	}{
		{"account nickname on create", http.MethodPost, "/api/v1/accounts", func() map[string]any {
			b := accountBody()
			b["nickname"] = name81
			return b
		}()},
		{"account nickname on edit", http.MethodPatch, "/api/v1/accounts/" + account, map[string]any{"nickname": name81}},
		{"category name on create", http.MethodPost, "/api/v1/categories", map[string]any{"name": name81}},
		{"category name on rename", http.MethodPatch, "/api/v1/categories/" + category, map[string]any{"name": name81}},
		{"goal name on create", http.MethodPost, "/api/v1/goals", map[string]any{
			"name": name81, "targetMinor": 100_000, "currency": "SGD", "plannedMonthlyMinor": 10_000}},
		{"goal name on edit", http.MethodPatch, "/api/v1/goals/" + goal.ID, map[string]any{"name": name81}},
		{"goal contribution note", http.MethodPost, "/api/v1/goals/" + goal.ID + "/contributions", map[string]any{
			"amountMinor": 2_500, "occurredOn": "2026-01-05", "note": note501}},
		{"bill name on create", http.MethodPost, "/api/v1/bills", map[string]any{
			"name": name81, "amountMinor": 45_000, "cadence": "monthly", "nextDue": "2026-12-20", "payFromAccountId": account}},
		{"bill name on edit", http.MethodPatch, "/api/v1/bills/" + bill.ID, map[string]any{"name": name81}},
		{"transaction description on create", http.MethodPost, "/api/v1/transactions", map[string]any{
			"kind": "expense", "occurredOn": "2026-01-05", "description": desc201,
			"categoryId": category, "fromAccountId": account, "amountMinor": 1_250}},
		{"transaction description on edit", http.MethodPatch, "/api/v1/transactions/" + transaction, map[string]any{"description": desc201}},
		{"holding name on create", http.MethodPost, "/api/v1/holdings", map[string]any{
			"accountId": brokerage, "name": name81, "instrument": "stock", "unit": "share"}},
		{"holding unit on create", http.MethodPost, "/api/v1/holdings", map[string]any{
			"accountId": brokerage, "name": "Gold", "instrument": "stock", "unit": unit21}},
		{"holding name on edit", http.MethodPatch, "/api/v1/holdings/" + holding, map[string]any{
			"name": name81, "instrument": "stock", "unit": "share"}},
		{"holding unit on edit", http.MethodPatch, "/api/v1/holdings/" + holding, map[string]any{
			"name": "D05", "instrument": "stock", "unit": unit21}},
		{"holding buy note", http.MethodPost, "/api/v1/holdings/" + holding + "/events", map[string]any{
			"kind": "acquisition", "quantity": "10", "amountMinor": 1_000, "occurredOn": "2026-01-05", "note": note501}},
		{"holding valuation note", http.MethodPost, "/api/v1/holdings/" + holding + "/valuations", map[string]any{
			"unitPriceMinor": 120, "asOf": "2026-01-05", "note": note501}},
		{"holding income note", http.MethodPost, "/api/v1/holdings/" + holding + "/income", map[string]any{
			"kind": "income", "amountMinor": 500, "receivedOn": "2026-01-05", "note": note501}},
		{"space name", http.MethodPost, "/api/v1/spaces", map[string]any{"name": name81, "visibility": "everyone"}},
		{"invitee name", http.MethodPost, "/api/v1/household/members/invite", map[string]any{
			"name": name81, "email": "kid@example.com", "role": "limited", "capabilities": []string{"calendar"}, "channel": "email"}},
		{"household name", http.MethodPatch, "/api/v1/household", map[string]any{"name": name81}},
		{"family name", http.MethodPatch, "/api/v1/household", map[string]any{"familyName": name81}},
		// Padded to one over: a service that stores the value as given must
		// count it as given, or the database CHECK answers 500 instead.
		{"household name padded with spaces", http.MethodPatch, "/api/v1/household", map[string]any{
			"name": strings.Repeat("a", 78) + "   "}},
		{"holding buy note padded with a newline", http.MethodPost, "/api/v1/holdings/" + holding + "/events", map[string]any{
			"kind": "acquisition", "quantity": "10", "amountMinor": 1_000, "occurredOn": "2026-01-05",
			"note": strings.Repeat("a", 500) + "\n"}},
		{"retro action", http.MethodPost, "/api/v1/retros/" + retro.Retro.Month + "/actions", map[string]any{"body": note501}},
	}
	for _, r := range rows {
		t.Run(r.name, func(t *testing.T) {
			rec := env.authed(t, r.method, r.path, r.body, session, csrf)
			assertErrorResponse(t, rec, http.StatusUnprocessableEntity, "TEXT_TOO_LONG")
		})
	}
}
