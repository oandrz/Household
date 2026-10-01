package httpadapter_test

import (
	"encoding/json"
	"net/http"
	"testing"
)

// These tests pin one rule over HTTP: a recorded fact may not be dated after
// the household's today, and a plan may (ADR 12). A transaction, a bill
// payment and a goal contribution are facts. They share the clock the other
// household-day tests use (eveningClock): 23:00 UTC, when a household in
// Singapore is already on the next day.

// QA ISSUE-013, the date half. A transaction took any date at all, so an
// expense dated 2099 lowered today's balance, while a holding refused even
// today. Now the three money facts a person dates by hand follow the rule
// holdings already did.
func TestAMoneyFactMayBeDatedTheHouseholdsTodayAndNoLater(t *testing.T) {
	clk, utcDay := eveningClock()
	env := newTestEnvWithClock(t, clk)
	session, csrf := env.signIn(t, env.ownerEmail, env.ownerPassword)

	// One day ahead of the server's UTC date: today for a household in
	// Singapore, tomorrow for one on UTC.
	singaporeToday := utcDay.AddDate(0, 0, 1).Format("2006-01-02")
	singaporeTomorrow := utcDay.AddDate(0, 0, 2).Format("2006-01-02")

	category, _ := env.firstExpenseCategory(t, session)
	account := env.mustCreateAccountID(t, session, csrf)

	// Both of these carry a date well ahead of today and are accepted: a
	// bill's next due date and a goal's target month are plans.
	bill := env.mustCreateBill(t, session, csrf, map[string]any{
		"name": "Internet", "amountMinor": 45_000, "cadence": "monthly",
		"nextDue": utcDay.AddDate(0, 0, 20).Format("2006-01-02"), "payFromAccountId": account,
	}).Bill
	goal := env.mustCreateGoal(t, session, csrf, map[string]any{
		"name": "Emergency fund", "targetMinor": 100_000, "currency": "SGD",
		"targetMonth": utcDay.AddDate(1, 0, 0).Format("2006-01"), "plannedMonthlyMinor": 10_000,
	}).Goal

	writes := []struct {
		name     string
		path     string
		body     func(date string) map[string]any
		accepted int
	}{
		{"a transaction", "/api/v1/transactions", func(date string) map[string]any {
			return map[string]any{
				"kind": "expense", "occurredOn": date, "description": "Weekly shop",
				"categoryId": category, "fromAccountId": account, "amountMinor": 1_250,
			}
		}, http.StatusCreated},
		{"a goal contribution", "/api/v1/goals/" + goal.ID + "/contributions", func(date string) map[string]any {
			return map[string]any{"amountMinor": 2_500, "occurredOn": date}
		}, http.StatusCreated},
		{"a bill payment", "/api/v1/bills/" + bill.ID + "/pay", func(date string) map[string]any {
			return map[string]any{"paidOn": date}
		}, http.StatusOK},
	}

	// What a refused write must leave behind: no ledger row (a bill payment
	// writes one too) and nothing in the goal.
	assertNothingWritten := func(t *testing.T) {
		t.Helper()
		if got := env.listTransactions(t, session, "/api/v1/transactions?month=all"); len(got.Transactions) != 0 {
			t.Errorf("%d ledger row(s) exist after refused writes, want none", len(got.Transactions))
		}
		goals := decodeGoalsList(t, env.authedGet(t, "/api/v1/goals", session))
		if len(goals.Goals) != 1 || goals.Goals[0].ContributedMinor != 0 {
			t.Errorf("goals = %+v, want the one goal with nothing contributed", goals.Goals)
		}
	}

	// The control, and the reason this test can fail: the household is still
	// on UTC here, so the very date accepted below is tomorrow and refused.
	// Without it, a server that accepted any date at all would pass.
	for _, w := range writes {
		t.Run(w.name+" dated the server's tomorrow is refused while the household is on UTC", func(t *testing.T) {
			rec := env.authed(t, http.MethodPost, w.path, w.body(singaporeToday), session, csrf)
			assertErrorResponse(t, rec, http.StatusUnprocessableEntity, "INVALID_DATE")
		})
	}
	t.Run("a refused write leaves nothing behind", assertNothingWritten)

	// hearthctl's add and its CSV import post to the same route with a
	// token, which has its own middleware.
	t.Run("a token is refused the same way", func(t *testing.T) {
		token := env.mustCreateToken(t, session, csrf, "script").Token
		rec := env.bearer(t, http.MethodPost, "/api/v1/transactions", writes[0].body(singaporeToday), token)
		assertErrorResponse(t, rec, http.StatusUnprocessableEntity, "INVALID_DATE")
	})

	env.setTimezone(t, session, csrf, "Asia/Singapore")

	for _, w := range writes {
		t.Run(w.name+" dated the household's tomorrow is still refused on Singapore", func(t *testing.T) {
			rec := env.authed(t, http.MethodPost, w.path, w.body(singaporeTomorrow), session, csrf)
			assertErrorResponse(t, rec, http.StatusUnprocessableEntity, "INVALID_DATE")
		})
	}
	t.Run("and those leave nothing behind either", assertNothingWritten)

	for _, w := range writes {
		t.Run(w.name+" dated the household's today is accepted once it is on Singapore", func(t *testing.T) {
			rec := env.authed(t, http.MethodPost, w.path, w.body(singaporeToday), session, csrf)
			if rec.Code != w.accepted {
				t.Fatalf("status = %d, want %d (body = %s)", rec.Code, w.accepted, rec.Body.String())
			}
		})
	}
}

// An edit is checked only when it changes the date. The row here ends up
// dated after the household's today without anyone typing a future date: it
// was saved as today in Singapore, and the household then moved its zone to
// UTC, where that day has not started. Its description must still be
// editable, and the edit form sends the date back with every save.
func TestEditingATransactionChecksItsDateOnlyWhenTheDateChanges(t *testing.T) {
	clk, utcDay := eveningClock()
	env := newTestEnvWithClock(t, clk)
	session, csrf := env.signIn(t, env.ownerEmail, env.ownerPassword)
	env.setTimezone(t, session, csrf, "Asia/Singapore")

	serverToday := utcDay.Format("2006-01-02")
	singaporeToday := utcDay.AddDate(0, 0, 1).Format("2006-01-02")
	singaporeTomorrow := utcDay.AddDate(0, 0, 2).Format("2006-01-02")

	category, _ := env.firstExpenseCategory(t, session)
	account := env.mustCreateAccountID(t, session, csrf)
	rec := env.authed(t, http.MethodPost, "/api/v1/transactions", map[string]any{
		"kind": "expense", "occurredOn": singaporeToday, "description": "Weekly shop",
		"categoryId": category, "fromAccountId": account, "amountMinor": 1_250,
	}, session, csrf)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create: status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var created struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil || created.ID == "" {
		t.Fatalf("decode the created transaction: %v (body = %s)", err, rec.Body.String())
	}
	path := "/api/v1/transactions/" + created.ID

	// From here the stored row is dated tomorrow, as the household sees it.
	env.setTimezone(t, session, csrf, "UTC")

	patch := func(t *testing.T, body map[string]any) (status int, description, occurredOn string) {
		t.Helper()
		rec := env.authed(t, http.MethodPatch, path, body, session, csrf)
		var got struct {
			Description string `json:"description"`
			OccurredOn  string `json:"occurredOn"`
		}
		if rec.Code == http.StatusOK {
			if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
				t.Fatalf("decode: %v (body = %s)", err, rec.Body.String())
			}
		}
		return rec.Code, got.Description, got.OccurredOn
	}

	t.Run("an edit that does not name the date saves", func(t *testing.T) {
		status, description, occurredOn := patch(t, map[string]any{"description": "Cold Storage"})
		if status != http.StatusOK || description != "Cold Storage" || occurredOn != singaporeToday {
			t.Fatalf("= %d %q %s, want 200, the new description and the date untouched", status, description, occurredOn)
		}
	})

	t.Run("an edit that sends the same date back saves", func(t *testing.T) {
		status, description, _ := patch(t, map[string]any{"description": "Cold Storage, Great World", "occurredOn": singaporeToday})
		if status != http.StatusOK || description != "Cold Storage, Great World" {
			t.Fatalf("= %d %q, want 200 and the new description", status, description)
		}
	})

	t.Run("an edit to a different future date is refused", func(t *testing.T) {
		rec := env.authed(t, http.MethodPatch, path, map[string]any{"occurredOn": singaporeTomorrow}, session, csrf)
		assertErrorResponse(t, rec, http.StatusUnprocessableEntity, "INVALID_DATE")
	})

	t.Run("an edit that moves it to today saves", func(t *testing.T) {
		status, _, occurredOn := patch(t, map[string]any{"occurredOn": serverToday})
		if status != http.StatusOK || occurredOn != serverToday {
			t.Fatalf("= %d %s, want 200 and %s", status, occurredOn, serverToday)
		}
	})
}
