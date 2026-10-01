package httpadapter_test

import (
	"encoding/json"
	"math"
	"net/http"
	"testing"

	"github.com/andreasoentoro/hearth/api/internal/domain"
)

// One expense of int64 max used to be accepted (201). The account's balance
// is a sum of its transactions, so from then on every read that touched the
// account answered 500 -- including the ledger the row would have been
// deleted from. The refusal has to happen at the door, and the household has
// to still read afterwards.
func TestAnExpenseTooLargeToAddUpIsRefusedAndTheHouseholdStillReads(t *testing.T) {
	env := newTestEnv(t)
	session, csrf := env.signIn(t, env.ownerEmail, env.ownerPassword)
	accountID := env.mustCreateAccountID(t, session, csrf)
	categoryID, _ := env.firstExpenseCategory(t, session)
	thisMonth, _ := thisMonthAndLast()
	expense := func(amountMinor int64) map[string]any {
		return map[string]any{
			"kind": "expense", "occurredOn": thisMonth.Format(dayLayout), "description": "QA edge",
			"categoryId": categoryID, "fromAccountId": accountID, "amountMinor": amountMinor,
		}
	}

	// int64 max through the API, and S$50,000,000,000,000,000 as typed twice
	// into the Add transaction form: the two ways the defect was reached.
	for _, amount := range []int64{math.MaxInt64, 5_000_000_000_000_000_000, domain.MaxAmountMinor + 1} {
		rec := env.authed(t, http.MethodPost, "/api/v1/transactions", expense(amount), session, csrf)
		if rec.Code != http.StatusUnprocessableEntity || !bodyHasCode(rec, "AMOUNT_TOO_LARGE") {
			t.Fatalf("amount %d: got %d %s, want 422 AMOUNT_TOO_LARGE", amount, rec.Code, rec.Body.String())
		}
	}

	// The largest amounts that ARE accepted must not break the sum either:
	// two of them on one account is twice the ceiling, far inside an int64.
	for range 2 {
		rec := env.authed(t, http.MethodPost, "/api/v1/transactions", expense(domain.MaxAmountMinor), session, csrf)
		if rec.Code != http.StatusCreated {
			t.Fatalf("amount at the ceiling: got %d %s, want 201", rec.Code, rec.Body.String())
		}
	}

	for _, path := range []string{"/api/v1/accounts", "/api/v1/transactions", "/api/v1/budgets/history"} {
		if rec := env.authedGet(t, path, session); rec.Code != http.StatusOK {
			t.Errorf("GET %s after the refusals = %d %s, want 200", path, rec.Code, rec.Body.String())
		}
	}
	if got := len(env.listTransactions(t, session, "/api/v1/transactions").Transactions); got != 2 {
		t.Fatalf("ledger holds %d rows, want only the 2 accepted ones", got)
	}
}

// Every amount a person can send, on every money route, gets the same
// ceiling and the same answer. A route missing from this table is a route
// whose amounts nobody has checked.
func TestEveryAmountFieldOnTheWireRefusesAFigurePastTheCeiling(t *testing.T) {
	env := newTestEnv(t)
	session, csrf := env.signIn(t, env.ownerEmail, env.ownerPassword)

	const tooLarge = domain.MaxAmountMinor + 1
	thisMonth, _ := thisMonthAndLast()
	today := thisMonth.Format(dayLayout)
	budgetPath := "/api/v1/budgets/" + thisMonth.Format("2006-01")

	accountID := env.mustCreateAccountID(t, session, csrf)
	idrAccountID := env.mustCreateAccountWithCurrency(t, session, csrf, "IDR")
	categoryID, _ := env.firstExpenseCategory(t, session)

	var created struct {
		ID string `json:"id"`
	}
	rec := env.authed(t, http.MethodPost, "/api/v1/transactions", map[string]any{
		"kind": "expense", "occurredOn": today, "description": "Coffee",
		"categoryId": categoryID, "fromAccountId": accountID, "amountMinor": 650,
	}, session, csrf)
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil || created.ID == "" {
		t.Fatalf("create transaction: %d %s", rec.Code, rec.Body.String())
	}
	transactionID := created.ID

	billID := env.mustCreateBill(t, session, csrf, map[string]any{
		"name": "Internet", "amountMinor": 45_000, "cadence": "monthly",
		"nextDue": farFutureDate(), "payFromAccountId": accountID,
	}).Bill.ID
	goalID := env.mustCreateGoal(t, session, csrf, map[string]any{
		"name": "Holiday", "targetMinor": 600_000, "plannedMonthlyMinor": 100_000,
	}).Goal.ID

	brokerage := newHoldingAccount(t, env, session, csrf, "Brokerage", "investment")
	newHolding := func(currency string) string {
		t.Helper()
		rec := env.authed(t, http.MethodPost, "/api/v1/holdings", map[string]any{
			"accountId": brokerage, "name": "Gold " + currency, "instrument": "gold",
			"unit": "gram", "currency": currency,
		}, session, csrf)
		var body struct {
			Holding holdingBody `json:"holding"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil || body.Holding.ID == "" {
			t.Fatalf("create holding: %d %s", rec.Code, rec.Body.String())
		}
		return body.Holding.ID
	}
	sgd := newHolding("SGD") // the seed household's primary currency
	usd := newHolding("USD") // foreign, so a primary amount is required

	for _, probe := range []struct {
		name, method, path string
		body               map[string]any
	}{
		{"account opening balance", http.MethodPost, "/api/v1/accounts", map[string]any{
			"nickname": "Too much", "type": "cash", "openingBalanceMinor": tooLarge,
			"openingBalanceCurrency": "SGD", "openingBalanceAsOf": "2026-01-01",
		}},
		{"account opening balance, negative", http.MethodPost, "/api/v1/accounts", map[string]any{
			"nickname": "Too little", "type": "cash", "openingBalanceMinor": -tooLarge,
			"openingBalanceCurrency": "SGD", "openingBalanceAsOf": "2026-01-01",
		}},
		{"account opening balance on edit", http.MethodPatch, "/api/v1/accounts/" + accountID, map[string]any{
			"openingBalanceMinor": tooLarge,
		}},
		{"transaction amount on edit", http.MethodPatch, "/api/v1/transactions/" + transactionID, map[string]any{
			"amountMinor": tooLarge,
		}},
		{"transfer amount received", http.MethodPost, "/api/v1/transactions", map[string]any{
			"kind": "transfer", "occurredOn": today, "description": "To rupiah",
			"fromAccountId": accountID, "toAccountId": idrAccountID,
			"amountMinor": 50_000, "receivedAmountMinor": tooLarge,
		}},
		{"bill amount", http.MethodPost, "/api/v1/bills", map[string]any{
			"name": "Rent", "amountMinor": tooLarge, "cadence": "monthly",
			"nextDue": farFutureDate(), "payFromAccountId": accountID,
		}},
		{"bill amount on edit", http.MethodPatch, "/api/v1/bills/" + billID, map[string]any{
			"amountMinor": tooLarge,
		}},
		{"bill payment amount", http.MethodPost, "/api/v1/bills/" + billID + "/pay", map[string]any{
			"amountMinor": tooLarge, "paidOn": today,
		}},
		{"budget cap", http.MethodPut, budgetPath, map[string]any{
			"lines": []map[string]any{{"categoryId": categoryID, "capMinor": tooLarge}},
		}},
		{"budget expected income", http.MethodPut, budgetPath, map[string]any{
			"expectedIncomeMinor": tooLarge,
			"lines":               []map[string]any{{"categoryId": categoryID, "capMinor": 1000}},
		}},
		{"goal target", http.MethodPost, "/api/v1/goals", map[string]any{
			"name": "Too far", "targetMinor": tooLarge, "plannedMonthlyMinor": 100_000,
		}},
		{"goal planned monthly", http.MethodPost, "/api/v1/goals", map[string]any{
			"name": "Too fast", "targetMinor": 600_000, "plannedMonthlyMinor": tooLarge,
		}},
		{"goal starting balance", http.MethodPost, "/api/v1/goals", map[string]any{
			"name": "Too rich", "targetMinor": 600_000, "plannedMonthlyMinor": 100_000,
			"startingBalanceMinor": tooLarge,
		}},
		{"goal target on edit", http.MethodPatch, "/api/v1/goals/" + goalID, map[string]any{
			"targetMinor": tooLarge,
		}},
		{"goal contribution", http.MethodPost, "/api/v1/goals/" + goalID + "/contributions", map[string]any{
			"amountMinor": tooLarge, "occurredOn": today,
		}},
		{"goal withdrawal", http.MethodPost, "/api/v1/goals/" + goalID + "/contributions", map[string]any{
			"amountMinor": -tooLarge, "occurredOn": today,
		}},
		{"holding event amount", http.MethodPost, "/api/v1/holdings/" + sgd + "/events", map[string]any{
			"kind": "acquisition", "quantity": "1", "amountMinor": tooLarge, "occurredOn": "2026-07-02",
		}},
		{"holding event primary amount", http.MethodPost, "/api/v1/holdings/" + usd + "/events", map[string]any{
			"kind": "acquisition", "quantity": "1", "amountMinor": 500,
			"primaryAmountMinor": tooLarge, "occurredOn": "2026-07-02",
		}},
		{"holding valuation unit price", http.MethodPost, "/api/v1/holdings/" + sgd + "/valuations", map[string]any{
			"unitPriceMinor": tooLarge, "asOf": "2026-07-02",
		}},
		{"holding income amount", http.MethodPost, "/api/v1/holdings/" + sgd + "/income", map[string]any{
			"kind": "income", "amountMinor": tooLarge, "receivedOn": "2026-07-02",
		}},
	} {
		rec := env.authed(t, probe.method, probe.path, probe.body, session, csrf)
		if rec.Code != http.StatusUnprocessableEntity || !bodyHasCode(rec, "AMOUNT_TOO_LARGE") {
			t.Errorf("%s: got %d %s, want 422 AMOUNT_TOO_LARGE", probe.name, rec.Code, rec.Body.String())
		}
	}
}
