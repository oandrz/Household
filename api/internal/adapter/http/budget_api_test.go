package httpadapter_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// --- Task 9: budget month, save and history routes -------------------------

// monthPath formats t as the "budgets/{month}" wire form: "2006-01".
func monthPath(t time.Time) string {
	return "/api/v1/budgets/" + t.Format("2006-01")
}

// firstExpenseCategory reads GET /categories (seeding the starter set on
// first call, same as CategoryService.List documents) and returns the id
// and name of the first expense-kind category -- every route under test
// needs a real category id, and this is the same list PUT /budgets' modal
// would offer.
func (env *testEnv) firstExpenseCategory(t *testing.T, session *http.Cookie) (id, name string) {
	t.Helper()
	rec := env.authedGet(t, "/api/v1/categories", session)
	if rec.Code != http.StatusOK {
		t.Fatalf("list categories: status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var body struct {
		Categories []struct {
			ID   string `json:"id"`
			Name string `json:"name"`
			Kind string `json:"kind"`
		} `json:"categories"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode categories: %v", err)
	}
	for _, c := range body.Categories {
		if c.Kind == "expense" {
			return c.ID, c.Name
		}
	}
	t.Fatal("no expense category in the starter set")
	return "", ""
}

// mustCreateAccountID is mustCreateAccount plus recovering the id the
// create response carries -- every expense fixture below needs a real
// fromAccountId.
func (env *testEnv) mustCreateAccountID(t *testing.T, session, csrf *http.Cookie) string {
	t.Helper()
	rec := env.authed(t, http.MethodPost, "/api/v1/accounts", map[string]any{
		"nickname": "Everyday spending", "type": "cash",
		"openingBalanceMinor": 500_000, "openingBalanceCurrency": "SGD",
		"openingBalanceAsOf": "2026-01-01",
	}, session, csrf)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create account: status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var body struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode created account: %v", err)
	}
	return body.ID
}

// mustCreateExpense logs one expense transaction dated occurredOn against
// categoryID/accountID, failing the test immediately if the write itself
// did not succeed -- setup, not the assertion under test.
func (env *testEnv) mustCreateExpense(t *testing.T, session, csrf *http.Cookie, occurredOn, categoryID, accountID string, amountMinor int64) {
	t.Helper()
	rec := env.authed(t, http.MethodPost, "/api/v1/transactions", map[string]any{
		"kind": "expense", "occurredOn": occurredOn, "description": "Weekly shop",
		"categoryId": categoryID, "fromAccountId": accountID, "amountMinor": amountMinor,
	}, session, csrf)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create expense: status = %d, body = %s", rec.Code, rec.Body.String())
	}
}

// mustPutBudget PUTs body to path and fails the test if the save itself did
// not succeed.
func (env *testEnv) mustPutBudget(t *testing.T, session, csrf *http.Cookie, path string, body map[string]any) {
	t.Helper()
	rec := env.authed(t, http.MethodPut, path, body, session, csrf)
	if rec.Code != http.StatusOK {
		t.Fatalf("PUT %s: status = %d, body = %s", path, rec.Code, rec.Body.String())
	}
}

// TestBudgetRoutesRequireMoneyAndOwner is transactions_api_test.go's
// TestTransactionRoutesRequireMoneyAndOwner shape applied to the three
// budget routes: reads and the write alike sit behind CapMoney AND
// requireOwner, the same as transactions and categories (router.go's own
// comment on the txn group explains why -- an unbudgeted or half-redacted
// Budget screen would be as broken as a half-redacted ledger).
//
// wantOwner pins the exact status an owner receives, not merely "not
// 401/403" -- the same reasoning the transactions matrix documents: a route
// wired with a nil deps.Budgets would pass neither guard and panic into a
// 500, which "not 401/403" would let slide by silently. PUT's owner case is
// a bare, bodyless PUT reaching the handler and failing to decode an empty
// body -- 400 INVALID_BODY -- proving the guards let the owner through and
// the handler is wired, without needing a valid payload here (that is
// covered by the round-trip and validation tests below).
func TestBudgetRoutesRequireMoneyAndOwner(t *testing.T) {
	env := newTestEnv(t)
	month := monthPath(time.Now().UTC())

	routes := []struct {
		method, path string
		wantOwner    int
	}{
		{http.MethodGet, month, http.StatusOK},
		{http.MethodGet, "/api/v1/budgets/history", http.StatusOK},
		{http.MethodPut, month, http.StatusBadRequest},
	}

	for _, route := range routes {
		t.Run(route.method+" "+route.path, func(t *testing.T) {
			rec := env.do(route.method, route.path, nil)
			if rec.Code != http.StatusUnauthorized {
				t.Fatalf("no session = %d, want 401 (body = %s)", rec.Code, rec.Body.String())
			}

			session, csrf := env.signIn(t, env.limitedEmail, env.limitedPassword)
			rec = requestRouteAs(t, env, route.method, route.path, session, csrf)
			if rec.Code != http.StatusForbidden {
				t.Fatalf("no money capability = %d, want 403 (body = %s)", rec.Code, rec.Body.String())
			}

			session, csrf = env.signIn(t, env.moneyLimitedEmail, env.moneyLimitedPassword)
			rec = requestRouteAs(t, env, route.method, route.path, session, csrf)
			if rec.Code != http.StatusForbidden {
				t.Fatalf("limited member holding money = %d, want 403 (body = %s)", rec.Code, rec.Body.String())
			}

			session, csrf = env.signIn(t, env.ownerEmail, env.ownerPassword)
			rec = requestRouteAs(t, env, route.method, route.path, session, csrf)
			if rec.Code != route.wantOwner {
				t.Fatalf("owner = %d, want %d (body = %s)", rec.Code, route.wantOwner, rec.Body.String())
			}
		})
	}
}

// TestBudgetWriteRouteRequiresCSRF mirrors
// TestTransactionWriteRoutesRequireCSRF for the one mutating budget route:
// no token at all, and a token that does not match the cookie, both refused
// by the CSRF_INVALID code specifically (not merely a 403 status, which
// requireOwner above it in the guard stack would also produce).
func TestBudgetWriteRouteRequiresCSRF(t *testing.T) {
	env := newTestEnv(t)
	session, csrf := env.signIn(t, env.ownerEmail, env.ownerPassword)
	path := monthPath(time.Now().UTC())

	req := httptest.NewRequest(http.MethodPut, path, nil)
	req.AddCookie(session)
	req.AddCookie(csrf)
	rec := httptest.NewRecorder()
	env.router.ServeHTTP(rec, req)
	assertErrorResponse(t, rec, http.StatusForbidden, "CSRF_INVALID")

	req2 := httptest.NewRequest(http.MethodPut, path, nil)
	req2.AddCookie(session)
	req2.AddCookie(csrf)
	req2.Header.Set("X-CSRF-Token", "definitely-the-wrong-value")
	rec2 := httptest.NewRecorder()
	env.router.ServeHTTP(rec2, req2)
	assertErrorResponse(t, rec2, http.StatusForbidden, "CSRF_INVALID")
}

// TestBudgetMalformedMonthIs400 pins the brief's INVALID_MONTH/400 shape --
// deliberately different from the 422 the transactions month filter answers
// for the same malformed-month case (transaction_handlers.go's
// parseTransactionFilter). Both GET and PUT parse {month} through the same
// parseBudgetMonth helper, so both must answer identically.
func TestBudgetMalformedMonthIs400(t *testing.T) {
	env := newTestEnv(t)
	session, csrf := env.signIn(t, env.ownerEmail, env.ownerPassword)

	rec := env.authedGet(t, "/api/v1/budgets/2026-7", session)
	assertErrorResponse(t, rec, http.StatusBadRequest, "INVALID_MONTH")

	rec = env.authed(t, http.MethodPut, "/api/v1/budgets/2026-7", map[string]any{"lines": []any{}}, session, csrf)
	assertErrorResponse(t, rec, http.StatusBadRequest, "INVALID_MONTH")
}

// TestBudgetMonthUnbudgetedStillReportsRealSpend is the wire-level pin of
// BudgetService.Month's empty-state contract: a month with no budget row
// answers 200 with "budget": null, while Categories and the top-level spend
// figures stay real. It also carries the Task 8 "Over requires an actual
// line" decision through to the wire for the first time -- the spending
// category shows capMinor: 0 and over: false despite real spend, not a cap
// that happens to read as exceeded.
func TestBudgetMonthUnbudgetedStillReportsRealSpend(t *testing.T) {
	env := newTestEnv(t)
	session, csrf := env.signIn(t, env.ownerEmail, env.ownerPassword)

	accountID := env.mustCreateAccountID(t, session, csrf)
	categoryID, _ := env.firstExpenseCategory(t, session)

	today := time.Now().UTC()
	env.mustCreateExpense(t, session, csrf, today.Format("2006-01-02"), categoryID, accountID, 12_000)

	rec := env.authedGet(t, monthPath(today), session)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body = %s)", rec.Code, rec.Body.String())
	}

	var body struct {
		Budget     any   `json:"budget"`
		SpentMinor int64 `json:"spentMinor"`
		Categories []struct {
			CategoryID string `json:"categoryId"`
			CapMinor   int64  `json:"capMinor"`
			SpentMinor int64  `json:"spentMinor"`
			Over       bool   `json:"over"`
		} `json:"categories"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v (body = %s)", err, rec.Body.String())
	}

	if body.Budget != nil {
		t.Fatalf("budget = %v, want null (the empty state)", body.Budget)
	}
	if body.SpentMinor != 12_000 {
		t.Fatalf("spentMinor = %d, want 12000", body.SpentMinor)
	}

	var found bool
	for _, c := range body.Categories {
		if c.CategoryID != categoryID {
			continue
		}
		found = true
		if c.CapMinor != 0 {
			t.Fatalf("category cap = %d, want 0 (no line was ever saved)", c.CapMinor)
		}
		if c.SpentMinor != 12_000 {
			t.Fatalf("category spent = %d, want 12000", c.SpentMinor)
		}
		if c.Over {
			t.Fatal("category over = true, want false: a category with no cap line can never be over " +
				"(Task 8's 'Over requires an actual line' decision)")
		}
	}
	if !found {
		t.Fatalf("category %s missing from categories: %+v", categoryID, body.Categories)
	}
}

// budgetBody is the shared decode shape for both GET and PUT budget
// responses -- toBudgetDTO in budget_handlers.go produces the same
// "budget": {...} shape from either endpoint, so one struct reads both.
type budgetBody struct {
	Budget struct {
		ExpectedIncomeMinor *int64 `json:"expectedIncomeMinor"`
		Lines               []struct {
			CategoryID string `json:"categoryId"`
			CapMinor   int64  `json:"capMinor"`
		} `json:"lines"`
	} `json:"budget"`
}

func decodeBudgetBody(t *testing.T, rec *httptest.ResponseRecorder) budgetBody {
	t.Helper()
	var body budgetBody
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode budget body: %v (body = %s)", err, rec.Body.String())
	}
	return body
}

// TestBudgetPutRoundTripsThroughGet is the brief's "PUT round-trips" case:
// a save's own response and a follow-up GET must both echo exactly what was
// sent, expected income included.
func TestBudgetPutRoundTripsThroughGet(t *testing.T) {
	env := newTestEnv(t)
	session, csrf := env.signIn(t, env.ownerEmail, env.ownerPassword)
	categoryID, _ := env.firstExpenseCategory(t, session)
	path := monthPath(time.Now().UTC())

	rec := env.authed(t, http.MethodPut, path, map[string]any{
		"expectedIncomeMinor": 910_000,
		"lines":               []map[string]any{{"categoryId": categoryID, "capMinor": 80_000}},
	}, session, csrf)
	if rec.Code != http.StatusOK {
		t.Fatalf("save: status = %d, body = %s", rec.Code, rec.Body.String())
	}
	assertBudgetRoundTrip(t, decodeBudgetBody(t, rec), categoryID)

	getRec := env.authedGet(t, path, session)
	if getRec.Code != http.StatusOK {
		t.Fatalf("get: status = %d, body = %s", getRec.Code, getRec.Body.String())
	}
	assertBudgetRoundTrip(t, decodeBudgetBody(t, getRec), categoryID)
}

func assertBudgetRoundTrip(t *testing.T, body budgetBody, categoryID string) {
	t.Helper()
	if body.Budget.ExpectedIncomeMinor == nil || *body.Budget.ExpectedIncomeMinor != 910_000 {
		t.Fatalf("expectedIncomeMinor = %v, want 910000", body.Budget.ExpectedIncomeMinor)
	}
	if len(body.Budget.Lines) != 1 || body.Budget.Lines[0].CategoryID != categoryID || body.Budget.Lines[0].CapMinor != 80_000 {
		t.Fatalf("lines = %+v, want exactly one {%s, 80000}", body.Budget.Lines, categoryID)
	}
}

// TestBudgetPutWithNoExpectedIncomeKeepsCardsHidden is the brief's
// "expectedIncomeMinor: null preserved" case: omitting the field entirely
// (not sending it as JSON null, which map[string]any can't distinguish from
// omission -- the pointer-nil convention BudgetService.Save documents makes
// them the same wire shape either way) must round-trip as nil, not a stored
// zero, on both the save's own response and a follow-up GET.
func TestBudgetPutWithNoExpectedIncomeKeepsCardsHidden(t *testing.T) {
	env := newTestEnv(t)
	session, csrf := env.signIn(t, env.ownerEmail, env.ownerPassword)
	categoryID, _ := env.firstExpenseCategory(t, session)
	path := monthPath(time.Now().UTC())

	rec := env.authed(t, http.MethodPut, path, map[string]any{
		"lines": []map[string]any{{"categoryId": categoryID, "capMinor": 50_000}},
	}, session, csrf)
	if rec.Code != http.StatusOK {
		t.Fatalf("save: status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if got := decodeBudgetBody(t, rec).Budget.ExpectedIncomeMinor; got != nil {
		t.Fatalf("save response expectedIncomeMinor = %v, want nil", *got)
	}

	getRec := env.authedGet(t, path, session)
	if got := decodeBudgetBody(t, getRec).Budget.ExpectedIncomeMinor; got != nil {
		t.Fatalf("get expectedIncomeMinor = %v, want nil", *got)
	}
}

// TestBudgetPutDuplicateCategoryDoesNotChangeTheSavedBudget is the brief's
// "PUT duplicate-category 422s and a follow-up GET shows nothing written"
// case. The pre-save step is what makes "shows nothing written" a real
// assertion rather than the trivially true "budget: null" an unbudgeted
// month would answer regardless of whether the guard worked.
func TestBudgetPutDuplicateCategoryDoesNotChangeTheSavedBudget(t *testing.T) {
	env := newTestEnv(t)
	session, csrf := env.signIn(t, env.ownerEmail, env.ownerPassword)
	categoryID, _ := env.firstExpenseCategory(t, session)
	path := monthPath(time.Now().UTC())

	env.mustPutBudget(t, session, csrf, path, map[string]any{
		"expectedIncomeMinor": 910_000,
		"lines":               []map[string]any{{"categoryId": categoryID, "capMinor": 80_000}},
	})

	rec := env.authed(t, http.MethodPut, path, map[string]any{
		"lines": []map[string]any{
			{"categoryId": categoryID, "capMinor": 10_000},
			{"categoryId": categoryID, "capMinor": 20_000},
		},
	}, session, csrf)
	assertErrorResponse(t, rec, http.StatusUnprocessableEntity, "DUPLICATE_BUDGET_LINE")

	getRec := env.authedGet(t, path, session)
	if getRec.Code != http.StatusOK {
		t.Fatalf("get: status = %d, body = %s", getRec.Code, getRec.Body.String())
	}
	assertBudgetRoundTrip(t, decodeBudgetBody(t, getRec), categoryID)
}

// TestBudgetSaveValidationErrors covers the remaining two per-field save
// guards -- a negative cap and a category id that doesn't belong to this
// household -- against one shared, never-successfully-saved month, since
// neither case is expected to write anything for the follow-up-GET check
// above to need repeating per case.
//
// The unknown-category id doubles as this route's foreign-household check:
// a budget month carries no household-scoped id in its own URL (unlike
// transactions/{id} or accounts/{id}), so there is no request shape that
// could target another household's *budget* directly. The one place
// another household's data could leak in is exactly this -- a categoryId in
// the PUT body that names a category this household does not own, be that
// because the id is simply made up or because it belongs to someone else's
// household. validateLineCategories (postgres/budget_repo.go) can't and
// doesn't distinguish the two: both fail the same ownership count check and
// answer the same 422, which is also why a single well-formed-but-unowned
// UUID is enough to exercise it here without standing up a second
// household in the shared testEnv fixture.
func TestBudgetSaveValidationErrors(t *testing.T) {
	env := newTestEnv(t)
	session, csrf := env.signIn(t, env.ownerEmail, env.ownerPassword)
	categoryID, _ := env.firstExpenseCategory(t, session)
	path := monthPath(time.Now().UTC())
	unknownCategoryID := "00000000-0000-0000-0000-000000000000"

	cases := []struct {
		name  string
		lines []map[string]any
		code  string
	}{
		{
			name:  "negative cap",
			lines: []map[string]any{{"categoryId": categoryID, "capMinor": -100}},
			code:  "NEGATIVE_BUDGET_CAP",
		},
		{
			name:  "unknown category",
			lines: []map[string]any{{"categoryId": unknownCategoryID, "capMinor": 10_000}},
			code:  "UNKNOWN_BUDGET_CATEGORY",
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			rec := env.authed(t, http.MethodPut, path, map[string]any{"lines": c.lines}, session, csrf)
			assertErrorResponse(t, rec, http.StatusUnprocessableEntity, c.code)
		})
	}
}

// TestBudgetHistoryMonthsIsClamped proves both ends of the [1, 24] clamp
// against the same fixture: budgets one, twenty-three and thirty months
// back from today. months=0 (below the floor) must still surface the
// one-month-back row, which a literal, unclamped 0 (a [today, today]
// window) would exclude; months=999 (above the ceiling) must surface the
// one- and twenty-three-month-back rows but never the thirty-month-back
// one, which an unclamped 999 would wrongly include.
func TestBudgetHistoryMonthsIsClamped(t *testing.T) {
	env := newTestEnv(t)
	session, csrf := env.signIn(t, env.ownerEmail, env.ownerPassword)
	categoryID, _ := env.firstExpenseCategory(t, session)

	now := time.Now().UTC()
	base := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
	monthsBack := map[string]time.Time{
		"back1":  base.AddDate(0, -1, 0),
		"back23": base.AddDate(0, -23, 0),
		"back30": base.AddDate(0, -30, 0),
	}
	for _, m := range monthsBack {
		env.mustPutBudget(t, session, csrf, monthPath(m), map[string]any{
			"lines": []map[string]any{{"categoryId": categoryID, "capMinor": 10_000}},
		})
	}

	// Lower clamp: months=0 must behave like months=1, not like a literal
	// zero-width window.
	rec := env.authedGet(t, "/api/v1/budgets/history?months=0", session)
	if rec.Code != http.StatusOK {
		t.Fatalf("months=0: status = %d, body = %s", rec.Code, rec.Body.String())
	}
	got := decodeBudgetHistoryMonths(t, rec)
	if len(got) != 1 || got[0] != monthsBack["back1"].Format("2006-01") {
		t.Fatalf("months=0: months = %v, want exactly [%s]", got, monthsBack["back1"].Format("2006-01"))
	}

	// Upper clamp: months=999 must behave like months=24, excluding the
	// thirty-month-back row.
	rec = env.authedGet(t, "/api/v1/budgets/history?months=999", session)
	if rec.Code != http.StatusOK {
		t.Fatalf("months=999: status = %d, body = %s", rec.Code, rec.Body.String())
	}
	got = decodeBudgetHistoryMonths(t, rec)
	wantBack1, wantBack23 := monthsBack["back1"].Format("2006-01"), monthsBack["back23"].Format("2006-01")
	wantBack30 := monthsBack["back30"].Format("2006-01")
	if len(got) != 2 {
		t.Fatalf("months=999: months = %v, want exactly 2 rows (back1 and back23, never back30)", got)
	}
	for _, m := range got {
		if m == wantBack30 {
			t.Fatalf("months=999: months = %v includes %s, which is 30 months back -- "+
				"the clamp at 24 must have excluded it", got, wantBack30)
		}
	}
	if got[0] != wantBack1 || got[1] != wantBack23 {
		t.Fatalf("months=999: months = %v, want [%s, %s] newest first", got, wantBack1, wantBack23)
	}
}

func decodeBudgetHistoryMonths(t *testing.T, rec *httptest.ResponseRecorder) []string {
	t.Helper()
	var body struct {
		Months []struct {
			Month string `json:"month"`
		} `json:"months"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode history: %v (body = %s)", err, rec.Body.String())
	}
	out := make([]string, 0, len(body.Months))
	for _, m := range body.Months {
		out = append(out, m.Month)
	}
	return out
}
