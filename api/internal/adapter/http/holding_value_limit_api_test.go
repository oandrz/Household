package httpadapter_test

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/andreasoentoro/hearth/api/internal/domain"
)

// A holding's worth is quantity times unit price. 100,000 units and a price
// of 1e14 minor units each pass the amount ceiling, and their product does
// not fit in an int64. The price used to be stored, its own POST answered
// 500, and GET /holdings answered 500 for the household from then on. These
// tests are that defect turned round: the write is refused with a 422 the
// form can show, nothing is stored, and every read still answers.

// holdingReadsStillAnswer is the half of each test that matters most to a
// household: after a refusal, the pages that add the holding up still load.
func holdingReadsStillAnswer(t *testing.T, env *testEnv, session *http.Cookie) {
	t.Helper()
	for _, path := range []string{"/api/v1/holdings", "/api/v1/holdings/report?kind=quarter"} {
		if rec := env.authedGet(t, path, session); rec.Code != http.StatusOK {
			t.Errorf("GET %s after the refusal = %d %s, want 200", path, rec.Code, rec.Body.String())
		}
	}
}

func TestAPriceThatOverflowsAHoldingsWorthIsRefusedAndTheHouseholdStillReads(t *testing.T) {
	env := newTestEnv(t)
	session, csrf := env.signIn(t, env.ownerEmail, env.ownerPassword)
	account := newHoldingAccount(t, env, session, csrf, "Brokerage", "investment")
	id := newHolding(t, env, session, csrf, account, "D05")

	rec := env.authed(t, http.MethodPost, "/api/v1/holdings/"+id+"/events", map[string]any{
		"kind": "acquisition", "quantity": "100000", "amountMinor": 5_000_000, "occurredOn": "2026-07-02",
	}, session, csrf)
	if rec.Code != http.StatusCreated {
		t.Fatalf("buy 100,000 units = %d %s, want 201", rec.Code, rec.Body.String())
	}

	rec = env.authed(t, http.MethodPost, "/api/v1/holdings/"+id+"/valuations", map[string]any{
		"unitPriceMinor": domain.MaxAmountMinor, "asOf": "2026-07-10",
	}, session, csrf)
	if rec.Code != http.StatusUnprocessableEntity || !bodyHasCode(rec, "HOLDING_VALUE_TOO_LARGE") {
		t.Fatalf("price of 1e14 on 100,000 units = %d %s, want 422 HOLDING_VALUE_TOO_LARGE", rec.Code, rec.Body.String())
	}

	// Refused BEFORE it was stored: a 422 that left the row behind would
	// still break every read below.
	var prices struct {
		Valuations []json.RawMessage `json:"valuations"`
	}
	rec = env.authedGet(t, "/api/v1/holdings/"+id+"/valuations", session)
	if err := json.Unmarshal(rec.Body.Bytes(), &prices); err != nil || rec.Code != http.StatusOK {
		t.Fatalf("list valuations = %d %s (%v)", rec.Code, rec.Body.String(), err)
	}
	if len(prices.Valuations) != 0 {
		t.Fatalf("the refused price was stored: %s", rec.Body.String())
	}
	holdingReadsStillAnswer(t, env, session)

	// An ordinary price is still accepted, and the figure it produces is
	// right: 100,000 units at S$10.00.
	rec = env.authed(t, http.MethodPost, "/api/v1/holdings/"+id+"/valuations", map[string]any{
		"unitPriceMinor": 1_000, "asOf": "2026-07-10",
	}, session, csrf)
	if rec.Code != http.StatusOK {
		t.Fatalf("an ordinary price = %d %s, want 200", rec.Code, rec.Body.String())
	}
	portfolio := decodePortfolio(t, env.authedGet(t, "/api/v1/holdings", session))
	if len(portfolio.Holdings) != 1 || portfolio.Holdings[0].MarketValueMinor != 100_000_000 {
		t.Fatalf("portfolio = %+v, want one holding worth 100000000", portfolio.Holdings)
	}
}

// The same two figures in the other order. A price on a holding that holds
// nothing is fine, so the purchase is the write that must be refused.
func TestAPurchaseThatOverflowsAHoldingsWorthIsRefusedAndTheHouseholdStillReads(t *testing.T) {
	env := newTestEnv(t)
	session, csrf := env.signIn(t, env.ownerEmail, env.ownerPassword)
	account := newHoldingAccount(t, env, session, csrf, "Brokerage", "investment")
	id := newHolding(t, env, session, csrf, account, "D05")

	rec := env.authed(t, http.MethodPost, "/api/v1/holdings/"+id+"/valuations", map[string]any{
		"unitPriceMinor": domain.MaxAmountMinor, "asOf": "2026-07-10",
	}, session, csrf)
	if rec.Code != http.StatusOK {
		t.Fatalf("a price on a holding that holds nothing = %d %s, want 200", rec.Code, rec.Body.String())
	}

	rec = env.authed(t, http.MethodPost, "/api/v1/holdings/"+id+"/events", map[string]any{
		"kind": "acquisition", "quantity": "100000", "amountMinor": 5_000_000, "occurredOn": "2026-07-02",
	}, session, csrf)
	if rec.Code != http.StatusUnprocessableEntity || !bodyHasCode(rec, "HOLDING_VALUE_TOO_LARGE") {
		t.Fatalf("buy 100,000 units at a price of 1e14 = %d %s, want 422 HOLDING_VALUE_TOO_LARGE", rec.Code, rec.Body.String())
	}

	var events struct {
		Events []json.RawMessage `json:"events"`
	}
	rec = env.authedGet(t, "/api/v1/holdings/"+id+"/events", session)
	if err := json.Unmarshal(rec.Body.Bytes(), &events); err != nil || rec.Code != http.StatusOK {
		t.Fatalf("list events = %d %s (%v)", rec.Code, rec.Body.String(), err)
	}
	if len(events.Events) != 0 {
		t.Fatalf("the refused purchase was stored: %s", rec.Body.String())
	}
	holdingReadsStillAnswer(t, env, session)
}
