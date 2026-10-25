package httpadapter_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"
	"time"
)

// cashflowMonthBody is the part of one month the tests read.
type cashflowMonthBody struct {
	Month          string   `json:"month"`
	Status         string   `json:"status"`
	Grey           []string `json:"grey"`
	OutMinor       int64    `json:"outMinor"`
	Reconciliation *struct {
		Status     string `json:"status"`
		SpentMinor int64  `json:"spentMinor"`
		Lines      []struct {
			Kind        string `json:"kind"`
			Name        string `json:"name"`
			AmountMinor int64  `json:"amountMinor"`
			PaidOn      string `json:"paidOn"`
		} `json:"lines"`
	} `json:"reconciliation"`
	Confirmation *struct {
		ConfirmedAt     time.Time `json:"confirmedAt"`
		ConfirmedByName string    `json:"confirmedByName"`
	} `json:"confirmation"`
}

type cashflowBody struct {
	Currency string `json:"currency"`
	Light    struct {
		Colour  string `json:"colour"`
		Reasons []any  `json:"reasons"`
	} `json:"light"`
	Current cashflowMonthBody   `json:"current"`
	Recent  []cashflowMonthBody `json:"recent"`
	Year    struct {
		Year        int                 `json:"year"`
		Months      []cashflowMonthBody `json:"months"`
		BillsToCome []any               `json:"billsToCome"`
	} `json:"year"`
}

func getCashflow(t *testing.T, env *testEnv, query string, session *http.Cookie) cashflowBody {
	t.Helper()
	rec := env.authedGet(t, "/api/v1/cashflow"+query, session)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /cashflow%s: status = %d, body = %s", query, rec.Code, rec.Body.String())
	}
	var body cashflowBody
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode cashflow: %v", err)
	}
	return body
}

func lastMonthPath() string {
	now := time.Now().UTC()
	last := time.Date(now.Year(), now.Month()-1, 1, 0, 0, 0, 0, time.UTC)
	return "/api/v1/cashflow/confirmations/" + last.Format("2006-01")
}

func TestCashflowPageShape(t *testing.T) {
	env := newTestEnv(t)
	session, _ := env.signIn(t, env.ownerEmail, env.ownerPassword)

	body := getCashflow(t, env, "", session)

	if len(body.Recent) != 12 || len(body.Year.Months) != 12 {
		t.Fatalf("recent = %d, year.months = %d, want 12 and 12", len(body.Recent), len(body.Year.Months))
	}
	if body.Currency == "" {
		t.Error("currency is empty")
	}
	if !slices.Contains([]string{"grey", "red", "yellow", "green"}, body.Light.Colour) {
		t.Errorf("light.colour = %q is outside the vocabulary", body.Light.Colour)
	}
	statuses := []string{"solid", "grey", "current", "future", "none"}
	for _, m := range append(body.Recent, body.Year.Months...) {
		if !slices.Contains(statuses, m.Status) {
			t.Errorf("month %s status = %q is outside the vocabulary", m.Month, m.Status)
		}
	}
	if body.Year.Year != time.Now().UTC().Year() {
		t.Errorf("year.year = %d, want the current year", body.Year.Year)
	}
}

func TestCashflowPageNeverEmitsNullArrays(t *testing.T) {
	env := newTestEnv(t)
	session, _ := env.signIn(t, env.ownerEmail, env.ownerPassword)
	rec := env.authedGet(t, "/api/v1/cashflow", session)

	var raw map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &raw); err != nil {
		t.Fatal(err)
	}
	light := raw["light"].(map[string]any)
	if _, isArray := light["reasons"].([]any); !isArray {
		t.Errorf("light.reasons = %v, want an array", light["reasons"])
	}
	year := raw["year"].(map[string]any)
	if _, isArray := year["billsToCome"].([]any); !isArray {
		t.Errorf("year.billsToCome = %v, want an array", year["billsToCome"])
	}
	current := raw["current"].(map[string]any)
	for _, key := range []string{"grey", "unconvertibleCurrencies", "contributions"} {
		if _, isArray := current[key].([]any); !isArray {
			t.Errorf("current.%s = %v, want an array", key, current[key])
		}
	}
}

func TestCashflowYearOutOfRange(t *testing.T) {
	env := newTestEnv(t)
	session, _ := env.signIn(t, env.ownerEmail, env.ownerPassword)
	nextYear := time.Now().UTC().Year() + 1

	cases := []struct {
		name, query string
		status      int
		code        string
	}{
		{"before the first allowed year", "?year=1999", http.StatusUnprocessableEntity, "CASHFLOW_YEAR_OUT_OF_RANGE"},
		{"not a number", "?year=abc", http.StatusBadRequest, "INVALID_YEAR"},
		{"a future year", "?year=" + itoa(nextYear), http.StatusUnprocessableEntity, "CASHFLOW_YEAR_OUT_OF_RANGE"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			rec := env.authedGet(t, "/api/v1/cashflow"+c.query, session)
			assertErrorResponse(t, rec, c.status, c.code)
		})
	}
}

func itoa(n int) string {
	b, _ := json.Marshal(n)
	return string(b)
}

func TestConfirmRefusesUnfinishedMonth(t *testing.T) {
	env := newTestEnv(t)
	session, csrf := env.signIn(t, env.ownerEmail, env.ownerPassword)

	current := "/api/v1/cashflow/confirmations/" + time.Now().UTC().Format("2006-01")
	rec := env.authed(t, http.MethodPut, current, nil, session, csrf)
	assertErrorResponse(t, rec, http.StatusUnprocessableEntity, "CASHFLOW_MONTH_OUT_OF_RANGE")

	rec = env.authed(t, http.MethodPut, "/api/v1/cashflow/confirmations/2026-8", nil, session, csrf)
	assertErrorResponse(t, rec, http.StatusBadRequest, "INVALID_MONTH")
}

func TestConfirmThenUndo(t *testing.T) {
	env := newTestEnv(t)
	session, csrf := env.signIn(t, env.ownerEmail, env.ownerPassword)
	path := lastMonthPath()
	lastMonth := path[len("/api/v1/cashflow/confirmations/"):]

	rec := env.authed(t, http.MethodPut, path, nil, session, csrf)
	if rec.Code != http.StatusOK {
		t.Fatalf("PUT: status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var put struct {
		Month           string `json:"month"`
		ConfirmedByName string `json:"confirmedByName"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &put); err != nil {
		t.Fatal(err)
	}
	if put.Month != lastMonth || put.ConfirmedByName == "" {
		t.Fatalf("PUT body = %+v, want month %s and the confirmer's name", put, lastMonth)
	}

	confirmationOf := func() *struct {
		ConfirmedAt     time.Time `json:"confirmedAt"`
		ConfirmedByName string    `json:"confirmedByName"`
	} {
		for _, m := range getCashflow(t, env, "", session).Recent {
			if m.Month == lastMonth {
				return m.Confirmation
			}
		}
		t.Fatalf("last month %s is not in recent", lastMonth)
		return nil
	}
	got := confirmationOf()
	if got == nil || got.ConfirmedByName != put.ConfirmedByName {
		t.Fatalf("after PUT, confirmation = %+v, want one by %q", got, put.ConfirmedByName)
	}

	rec = env.authed(t, http.MethodDelete, path, nil, session, csrf)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("DELETE: status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if got := confirmationOf(); got != nil {
		t.Fatalf("after DELETE, confirmation = %+v, want null", got)
	}
}

func TestCashflowWritesNeedCSRF(t *testing.T) {
	env := newTestEnv(t)
	session, csrf := env.signIn(t, env.ownerEmail, env.ownerPassword)
	path := lastMonthPath()

	for _, method := range []string{http.MethodPut, http.MethodDelete} {
		t.Run(method, func(t *testing.T) {
			req := httptest.NewRequest(method, path, nil)
			req.AddCookie(session)
			req.AddCookie(csrf)
			rec := httptest.NewRecorder()
			env.router.ServeHTTP(rec, req)
			assertErrorResponse(t, rec, http.StatusForbidden, "CSRF_INVALID")
		})
	}
}

func TestCashflowRoutesRequireMoneyAndOwner(t *testing.T) {
	env := newTestEnv(t)
	routes := []struct{ method, path string }{
		{http.MethodGet, "/api/v1/cashflow"},
		{http.MethodPut, lastMonthPath()},
		{http.MethodDelete, lastMonthPath()},
	}
	callers := []struct{ name, email, password string }{
		{"limited member without money", env.limitedEmail, env.limitedPassword},
		{"limited member with money", env.moneyLimitedEmail, env.moneyLimitedPassword},
	}
	for _, route := range routes {
		rec := env.do(route.method, route.path, nil)
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("%s %s with no session = %d, want 401", route.method, route.path, rec.Code)
		}
		for _, c := range callers {
			session, csrf := env.signIn(t, c.email, c.password)
			rec := requestRouteAs(t, env, route.method, route.path, session, csrf)
			if rec.Code != http.StatusForbidden {
				t.Errorf("%s %s as %s = %d, want 403 (body = %s)", route.method, route.path, c.name, rec.Code, rec.Body.String())
			}
		}
	}
}

// TestCashflowSumStartsFromTheLedgersSpent proves the worked sum begins at the
// number the Transactions page shows for the same month, then adds a bill
// paid in full, its first share and a loan transfer to reach money out.
func TestCashflowSumStartsFromTheLedgersSpent(t *testing.T) {
	env := newTestEnv(t)
	session, csrf := env.signIn(t, env.ownerEmail, env.ownerPassword)
	categoryID, _ := env.firstExpenseCategory(t, session)
	cashID := env.mustCreateAccountID(t, session, csrf)
	loanRec := env.authed(t, http.MethodPost, "/api/v1/accounts", map[string]any{
		"nickname": "Car loan", "type": "loan", "openingBalanceMinor": 1_450_000,
		"openingBalanceCurrency": "SGD", "openingBalanceAsOf": "2026-01-01",
	}, session, csrf)
	var loan struct {
		ID string `json:"id"`
	}
	if loanRec.Code != http.StatusCreated || json.Unmarshal(loanRec.Body.Bytes(), &loan) != nil {
		t.Fatalf("create loan account: status = %d, body = %s", loanRec.Code, loanRec.Body.String())
	}
	loanID := loan.ID

	now := time.Now().UTC()
	last := time.Date(now.Year(), now.Month()-1, 1, 0, 0, 0, 0, time.UTC)
	day := func(d int) string { return last.AddDate(0, 0, d-1).Format("2006-01-02") }

	env.mustCreateExpense(t, session, csrf, day(5), categoryID, cashID, 30_000)
	bill := env.mustCreateBill(t, session, csrf, map[string]any{
		"name": "Car insurance", "amountMinor": 120_000, "cadence": "yearly",
		"nextDue": day(10), "categoryId": categoryID, "payFromAccountId": cashID,
	})
	env.mustPayBill(t, session, csrf, bill.Bill.ID, map[string]any{"paidOn": day(12)})
	rec := env.authed(t, http.MethodPost, "/api/v1/transactions", map[string]any{
		"kind": "transfer", "occurredOn": day(15), "description": "Loan repayment",
		"fromAccountId": cashID, "toAccountId": loanID, "amountMinor": 40_000,
	}, session, csrf)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create transfer: status = %d, body = %s", rec.Code, rec.Body.String())
	}

	month := last.Format("2006-01")
	var lastMonth *cashflowMonthBody
	body := getCashflow(t, env, "", session)
	for i := range body.Recent {
		if body.Recent[i].Month == month {
			lastMonth = &body.Recent[i]
		}
	}
	if lastMonth == nil || lastMonth.Reconciliation == nil {
		t.Fatalf("last month %s has no reconciliation: %+v", month, lastMonth)
	}
	r := lastMonth.Reconciliation
	if r.Status != "shown" {
		t.Fatalf("status = %q, want shown", r.Status)
	}

	rec = env.authedGet(t, "/api/v1/transactions?month="+month, session)
	var ledger struct {
		Summary struct {
			SpentMinor int64 `json:"spentMinor"`
		} `json:"summary"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &ledger); err != nil {
		t.Fatalf("decode transactions: %v", err)
	}
	if r.SpentMinor != ledger.Summary.SpentMinor {
		t.Errorf("sum starts at %d, the Transactions page says spent %d", r.SpentMinor, ledger.Summary.SpentMinor)
	}
	total := r.SpentMinor
	kinds := map[string]bool{}
	for _, l := range r.Lines {
		total += l.AmountMinor
		kinds[l.Kind] = true
	}
	if total != lastMonth.OutMinor {
		t.Errorf("spent plus lines = %d, money out = %d", total, lastMonth.OutMinor)
	}
	for _, k := range []string{"bill_in_full", "bill_share", "loan"} {
		if !kinds[k] {
			t.Errorf("no %s line in %+v", k, r.Lines)
		}
	}
}

// TestCashflowSumIsNullForMonthsWithoutFigures pins when the sum is sent: for
// every month that has figures, never for one that has none.
func TestCashflowSumIsNullForMonthsWithoutFigures(t *testing.T) {
	env := newTestEnv(t)
	session, _ := env.signIn(t, env.ownerEmail, env.ownerPassword)
	rec := env.authedGet(t, "/api/v1/cashflow", session)

	var raw struct {
		LastFinished map[string]any   `json:"lastFinished"`
		Current      map[string]any   `json:"current"`
		Recent       []map[string]any `json:"recent"`
		Year         struct {
			Months []map[string]any `json:"months"`
		} `json:"year"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &raw); err != nil {
		t.Fatal(err)
	}
	months := append(append([]map[string]any{raw.Current, raw.LastFinished}, raw.Recent...), raw.Year.Months...)
	for _, m := range months {
		if m == nil {
			continue
		}
		status, _ := m["status"].(string)
		rc := m["reconciliation"]
		if status == "none" || status == "future" {
			if rc != nil {
				t.Errorf("month %v (%s) has a reconciliation: %v", m["month"], status, rc)
			}
			continue
		}
		obj, ok := rc.(map[string]any)
		if !ok {
			t.Errorf("month %v (%s) reconciliation = %v, want an object", m["month"], status, rc)
			continue
		}
		for _, key := range []string{"lines", "unratedCurrencies"} {
			if _, isArray := obj[key].([]any); !isArray {
				t.Errorf("month %v reconciliation.%s = %v, want an array", m["month"], key, obj[key])
			}
		}
	}
}
