package httpadapter_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

type holdingBody struct {
	ID               string  `json:"id"`
	Name             string  `json:"name"`
	Currency         string  `json:"currency"`
	HeldNano         int64   `json:"heldNano"`
	Held             string  `json:"held"`
	CostMinor        int64   `json:"costMinor"`
	RealisedMinor    int64   `json:"realisedMinor"`
	MarketValueMinor int64   `json:"marketValueMinor"`
	HasMarketValue   bool    `json:"hasMarketValue"`
	ValuedAt         *string `json:"valuedAt"`
}

type portfolioBody struct {
	Holdings      []holdingBody `json:"holdings"`
	NotInNetWorth bool          `json:"notInNetWorth"`
}

func decodePortfolio(t *testing.T, rec *httptest.ResponseRecorder) portfolioBody {
	t.Helper()
	var body portfolioBody
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode portfolio: %v (body = %s)", err, rec.Body.String())
	}
	return body
}

// The guard stack, route by route. A portfolio is a table whose every figure
// is money, so a limited member is refused outright rather than served a page
// of blanks -- the same choice the ledger makes, and the reason no redaction
// code exists here to forget.
func TestHoldingRoutesRequireMoneyAndOwner(t *testing.T) {
	env := newTestEnv(t)
	zeroUUID := "00000000-0000-0000-0000-000000000000"

	routes := []struct {
		method, path string
		wantOwner    int
	}{
		{http.MethodGet, "/api/v1/holdings", http.StatusOK},
		{http.MethodGet, "/api/v1/holdings/" + zeroUUID + "/events", http.StatusNotFound},
		{http.MethodGet, "/api/v1/holdings/" + zeroUUID + "/valuations", http.StatusNotFound},
		{http.MethodPost, "/api/v1/holdings", http.StatusBadRequest},
		{http.MethodPatch, "/api/v1/holdings/" + zeroUUID, http.StatusBadRequest},
		{http.MethodPost, "/api/v1/holdings/" + zeroUUID + "/archive", http.StatusNotFound},
		{http.MethodPost, "/api/v1/holdings/" + zeroUUID + "/restore", http.StatusNotFound},
		{http.MethodPost, "/api/v1/holdings/" + zeroUUID + "/events", http.StatusBadRequest},
		{http.MethodDelete, "/api/v1/holdings/" + zeroUUID + "/events/" + zeroUUID, http.StatusNotFound},
		{http.MethodPost, "/api/v1/holdings/" + zeroUUID + "/valuations", http.StatusBadRequest},
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

// The guard must run BEFORE the service, not merely produce a 403 after it.
// The proof is at the database: a refused create must leave no row, so the
// owner's own portfolio is still empty afterwards.
func TestALimitedMembersCreateReachesNoService(t *testing.T) {
	env := newTestEnv(t)
	session, csrf := env.signIn(t, env.moneyLimitedEmail, env.moneyLimitedPassword)

	rec := env.authed(t, http.MethodPost, "/api/v1/holdings", map[string]any{
		"accountId": "00000000-0000-0000-0000-000000000000",
		"name":      "Snuck through", "instrument": "gold", "unit": "gram",
	}, session, csrf)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("limited member create = %d, want 403", rec.Code)
	}

	session, csrf = env.signIn(t, env.ownerEmail, env.ownerPassword)
	rec = env.authed(t, http.MethodGet, "/api/v1/holdings", nil, session, csrf)
	for _, h := range decodePortfolio(t, rec).Holdings {
		if h.Name == "Snuck through" {
			t.Fatal("a refused create still wrote a holding -- the guard is running after the service")
		}
	}
}

// A holding belongs in an investment account, and the refusal is at the wire,
// not only in a unit test.
func TestCreatingAHoldingOnACashAccountIsRefused(t *testing.T) {
	env := newTestEnv(t)
	session, csrf := env.signIn(t, env.ownerEmail, env.ownerPassword)
	cash := newHoldingAccount(t, env, session, csrf, "DBS Everyday", "cash")

	rec := env.authed(t, http.MethodPost, "/api/v1/holdings", map[string]any{
		"accountId": cash, "name": "Gold", "instrument": "gold", "unit": "gram",
	}, session, csrf)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("code = %d, want 422 (body = %s)", rec.Code, rec.Body.String())
	}
	if !bodyHasCode(rec, "ACCOUNT_NOT_INVESTMENT") {
		t.Fatalf("body = %s, want ACCOUNT_NOT_INVESTMENT", rec.Body.String())
	}
}

// The whole milestone-1 flow at the wire: a holding, two lots at different
// prices, a valuation, and the figures that come back. This is the path the
// portfolio page renders.
func TestPortfolioReportsPositionCostAndMarketValue(t *testing.T) {
	env := newTestEnv(t)
	session, csrf := env.signIn(t, env.ownerEmail, env.ownerPassword)
	account := newHoldingAccount(t, env, session, csrf, "Brokerage", "investment")

	rec := env.authed(t, http.MethodPost, "/api/v1/holdings", map[string]any{
		"accountId": account, "name": "Gold bar", "instrument": "gold", "unit": "gram",
	}, session, csrf)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create = %d (body = %s)", rec.Code, rec.Body.String())
	}
	var created struct {
		Holding holdingBody `json:"holding"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode: %v", err)
	}
	id := created.Holding.ID

	// 100.5 grams at S$100.00, then 99.5 grams at S$120.00 -- a fractional
	// quantity on purpose, because that is the case a plain integer count
	// could not carry.
	for _, lot := range []struct {
		quantity string
		minor    int64
		on       string
	}{
		{"100.5", 1_005_000, "2026-07-02"},
		{"99.5", 1_194_000, "2026-07-03"},
	} {
		rec = env.authed(t, http.MethodPost, "/api/v1/holdings/"+id+"/events", map[string]any{
			"kind": "acquisition", "quantity": lot.quantity,
			"amountMinor": lot.minor, "occurredOn": lot.on,
		}, session, csrf)
		if rec.Code != http.StatusCreated {
			t.Fatalf("buy %s = %d (body = %s)", lot.quantity, rec.Code, rec.Body.String())
		}
	}

	rec = env.authed(t, http.MethodPost, "/api/v1/holdings/"+id+"/valuations", map[string]any{
		"unitPriceMinor": 13000, "asOf": "2026-07-10",
	}, session, csrf)
	// A valuation upserts, so it answers 200 rather than 201: a second price
	// for one day is a correction, not a new thing.
	if rec.Code != http.StatusOK {
		t.Fatalf("valuation = %d, want 200 (body = %s)", rec.Code, rec.Body.String())
	}

	rec = env.authed(t, http.MethodGet, "/api/v1/holdings", nil, session, csrf)
	body := decodePortfolio(t, rec)
	if !body.NotInNetWorth {
		t.Fatal("notInNetWorth = false; milestone 1 keeps holdings out of net worth and the page says so")
	}
	if len(body.Holdings) != 1 {
		t.Fatalf("len = %d, want 1", len(body.Holdings))
	}
	got := body.Holdings[0]
	if got.Held != "200" {
		t.Fatalf("Held = %q, want 200 grams (100.5 + 99.5)", got.Held)
	}
	if got.HeldNano != 200*1_000_000_000 {
		t.Fatalf("HeldNano = %d, want 200 units", got.HeldNano)
	}
	if got.CostMinor != 2_199_000 {
		t.Fatalf("CostMinor = %d, want 2199000", got.CostMinor)
	}
	// 200 grams at S$130.00 each.
	if !got.HasMarketValue || got.MarketValueMinor != 2_600_000 {
		t.Fatalf("market value = %d (has = %v), want 2600000", got.MarketValueMinor, got.HasMarketValue)
	}
	if got.ValuedAt == nil || *got.ValuedAt != "2026-07-10" {
		t.Fatalf("ValuedAt = %v, want 2026-07-10 -- the page shows how stale a price is", got.ValuedAt)
	}
}

// The PRD's "no figure and a reason" rule, at the wire. A holding nobody has
// priced reports hasMarketValue false and a zero figure the page must not
// render -- not a market value of zero, which reads as "this is worthless".
func TestAnUnpricedHoldingReportsNoMarketValue(t *testing.T) {
	env := newTestEnv(t)
	session, csrf := env.signIn(t, env.ownerEmail, env.ownerPassword)
	account := newHoldingAccount(t, env, session, csrf, "Brokerage", "investment")
	rec := env.authed(t, http.MethodPost, "/api/v1/holdings", map[string]any{
		"accountId": account, "name": "Unpriced", "instrument": "other", "unit": "unit",
	}, session, csrf)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create = %d (body = %s)", rec.Code, rec.Body.String())
	}

	rec = env.authed(t, http.MethodGet, "/api/v1/holdings", nil, session, csrf)
	got := decodePortfolio(t, rec).Holdings[0]
	if got.HasMarketValue {
		t.Fatal("hasMarketValue = true with no price recorded")
	}
}

// A quantity crosses the wire as a string a person would type. Nine decimal
// places is the limit, and anything finer is refused rather than truncated --
// storing a different number from the one that was typed is worse than saying
// no.
func TestAQuantityFinerThanABillionthIsRefused(t *testing.T) {
	env := newTestEnv(t)
	session, csrf := env.signIn(t, env.ownerEmail, env.ownerPassword)
	account := newHoldingAccount(t, env, session, csrf, "Brokerage", "investment")
	rec := env.authed(t, http.MethodPost, "/api/v1/holdings", map[string]any{
		"accountId": account, "name": "Gold", "instrument": "gold", "unit": "gram",
	}, session, csrf)
	var created struct {
		Holding holdingBody `json:"holding"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &created)

	rec = env.authed(t, http.MethodPost, "/api/v1/holdings/"+created.Holding.ID+"/events", map[string]any{
		"kind": "acquisition", "quantity": "0.0000000001",
		"amountMinor": 1000, "occurredOn": "2026-07-02",
	}, session, csrf)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("code = %d, want 422 (body = %s)", rec.Code, rec.Body.String())
	}
	if !bodyHasCode(rec, "INVALID_QUANTITY") {
		t.Fatalf("body = %s, want INVALID_QUANTITY", rec.Body.String())
	}
}

// Another household's holding id must read as ABSENT at the wire, not as
// forbidden -- a 403 would confirm the row exists.
func TestAnotherHouseholdsHoldingIsNotFound(t *testing.T) {
	env := newTestEnv(t)
	session, csrf := env.signIn(t, env.ownerEmail, env.ownerPassword)

	rec := env.authed(t, http.MethodGet, "/api/v1/holdings/00000000-0000-0000-0000-000000000000/events", nil, session, csrf)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("code = %d, want 404 (body = %s)", rec.Code, rec.Body.String())
	}
}

// The account-type refusal on the SHIPPED accounts route. Task 4 added this
// guard to a walked feature, so it is proved at the wire as well as in a
// service test.
func TestAnAccountHoldingInvestmentsCannotChangeType(t *testing.T) {
	env := newTestEnv(t)
	session, csrf := env.signIn(t, env.ownerEmail, env.ownerPassword)
	account := newHoldingAccount(t, env, session, csrf, "Brokerage", "investment")
	rec := env.authed(t, http.MethodPost, "/api/v1/holdings", map[string]any{
		"accountId": account, "name": "Gold", "instrument": "gold", "unit": "gram",
	}, session, csrf)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create holding = %d (body = %s)", rec.Code, rec.Body.String())
	}

	rec = env.authed(t, http.MethodPatch, "/api/v1/accounts/"+account, map[string]any{"type": "cash"}, session, csrf)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("type change = %d, want 422 (body = %s)", rec.Code, rec.Body.String())
	}
	if !bodyHasCode(rec, "ACCOUNT_HAS_HOLDINGS") {
		t.Fatalf("body = %s, want ACCOUNT_HAS_HOLDINGS", rec.Body.String())
	}

	// The guard is about the TYPE, not about touching the account: a rename
	// still works.
	rec = env.authed(t, http.MethodPatch, "/api/v1/accounts/"+account, map[string]any{"nickname": "Moomoo SG"}, session, csrf)
	if rec.Code != http.StatusOK {
		t.Fatalf("rename = %d, want 200 (body = %s)", rec.Code, rec.Body.String())
	}
}

// newHoldingAccount creates an account and returns its id. api_test.go's own
// mustCreateAccount returns nothing, and every test here needs the id to hang
// a holding on.
func newHoldingAccount(t *testing.T, env *testEnv, session, csrf *http.Cookie, nickname, kind string) string {
	t.Helper()
	rec := env.authed(t, http.MethodPost, "/api/v1/accounts", map[string]any{
		"nickname": nickname, "type": kind,
		"openingBalanceMinor": 0, "openingBalanceCurrency": "SGD",
		"openingBalanceAsOf": "2026-07-01",
	}, session, csrf)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create %s account: %d (body = %s)", kind, rec.Code, rec.Body.String())
	}
	var body struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode account: %v", err)
	}
	return body.ID
}

// bodyHasCode asserts on the error CODE rather than the message, so rewording
// a sentence for a household does not break a test about which refusal fired.
func bodyHasCode(rec *httptest.ResponseRecorder, want string) bool {
	var body struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		return false
	}
	return body.Error.Code == want
}
