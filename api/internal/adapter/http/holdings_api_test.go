package httpadapter_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
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
		{http.MethodGet, "/api/v1/holdings/report?kind=quarter", http.StatusOK},
		{http.MethodGet, "/api/v1/holdings/" + zeroUUID + "/income", http.StatusNotFound},
		{http.MethodPost, "/api/v1/holdings/" + zeroUUID + "/income", http.StatusBadRequest},
		{http.MethodDelete, "/api/v1/holdings/" + zeroUUID + "/income/" + zeroUUID, http.StatusNotFound},
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

// An id this household does not have reads as ABSENT, never as forbidden -- a
// 403 would confirm the row exists somewhere. (The cross-household case proper
// is covered where the rows can actually be created, in
// postgres/holding_repo_test.go.)
func TestAnUnknownHoldingIdIsNotFound(t *testing.T) {
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

// --- the period report and income at the wire --------------------------------

type componentBody struct {
	NativeMinor  int64 `json:"nativeMinor"`
	PrimaryMinor int64 `json:"primaryMinor"`
}

type periodReturnBody struct {
	Unrealised       *componentBody `json:"unrealised"`
	Realised         componentBody  `json:"realised"`
	Income           componentBody  `json:"income"`
	Fees             componentBody  `json:"fees"`
	Total            *componentBody `json:"total"`
	Reason           string         `json:"reason"`
	OpeningPriceAsOf *string        `json:"openingPriceAsOf"`
	ClosingPriceAsOf *string        `json:"closingPriceAsOf"`
}

type reportPeriodBody struct {
	Kind    string `json:"kind"`
	Year    int    `json:"year"`
	Index   int    `json:"index"`
	Label   string `json:"label"`
	Start   string `json:"start"`
	End     string `json:"end"`
	Current bool   `json:"current"`
}

type reportHoldingBody struct {
	ID       string             `json:"id"`
	Name     string             `json:"name"`
	Currency string             `json:"currency"`
	Returns  []periodReturnBody `json:"returns"`
}

type reportBody struct {
	Kind            string              `json:"kind"`
	PrimaryCurrency string              `json:"primaryCurrency"`
	Periods         []reportPeriodBody  `json:"periods"`
	Holdings        []reportHoldingBody `json:"holdings"`
}

func decodeReport(t *testing.T, rec *httptest.ResponseRecorder) reportBody {
	t.Helper()
	var body reportBody
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode report: %v (body = %s)", err, rec.Body.String())
	}
	return body
}

// today in UTC, which is what the server's clock compares a date against. The
// report tests date their rows today so that the period containing them is
// always the current one -- a hard-coded month would put this test in a
// different quarter depending on when it runs.
func serverToday() string { return time.Now().UTC().Format("2006-01-02") }

// /holdings/report must not be swallowed by the /holdings/{id}/... routes
// registered beside it. If it were, this would be an id lookup and 404.
func TestTheReportRouteIsNotAnIdLookup(t *testing.T) {
	env := newTestEnv(t)
	session, csrf := env.signIn(t, env.ownerEmail, env.ownerPassword)

	rec := env.authed(t, http.MethodGet, "/api/v1/holdings/report?kind=quarter", nil, session, csrf)
	if rec.Code != http.StatusOK {
		t.Fatalf("report = %d, want 200 (body = %s)", rec.Code, rec.Body.String())
	}
	if len(decodeReport(t, rec).Periods) == 0 {
		t.Fatal("the report answered with no periods at all")
	}
}

// The window length has ONE home, and it is the server. A frontend default
// would be a second copy of the rule, free to drift from this one.
func TestTheReportDefaultsItsWindowPerPeriodKind(t *testing.T) {
	env := newTestEnv(t)
	session, csrf := env.signIn(t, env.ownerEmail, env.ownerPassword)

	for _, c := range []struct {
		kind string
		want int
	}{{"quarter", 6}, {"half", 4}, {"year", 3}} {
		rec := env.authed(t, http.MethodGet, "/api/v1/holdings/report?kind="+c.kind, nil, session, csrf)
		if rec.Code != http.StatusOK {
			t.Fatalf("%s report = %d (body = %s)", c.kind, rec.Code, rec.Body.String())
		}
		body := decodeReport(t, rec)
		if len(body.Periods) != c.want {
			t.Errorf("%s: got %d periods, want the server's default of %d", c.kind, len(body.Periods), c.want)
		}
		if body.Kind != c.kind {
			t.Errorf("kind = %q, want %q", body.Kind, c.kind)
		}
		if !body.Periods[len(body.Periods)-1].Current {
			t.Errorf("%s: the last period must be the one the household is in", c.kind)
		}
	}
}

func TestTheReportRefusesAKindOrCountItCannotDraw(t *testing.T) {
	env := newTestEnv(t)
	session, csrf := env.signIn(t, env.ownerEmail, env.ownerPassword)

	for _, query := range []string{"?kind=month", "?kind=quarter&count=0", "?kind=quarter&count=99", "?kind=quarter&count=many", ""} {
		rec := env.authed(t, http.MethodGet, "/api/v1/holdings/report"+query, nil, session, csrf)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("%q = %d, want 400 (body = %s)", query, rec.Code, rec.Body.String())
		}
	}
}

// The whole report in one round trip: a holding bought and priced today, a
// dividend and a fee, read back as the three components plus their total --
// and the date the closing price carries, so the screen can say how old it is.
func TestTheReportCarriesEveryComponentAndThePriceDates(t *testing.T) {
	env := newTestEnv(t)
	session, csrf := env.signIn(t, env.ownerEmail, env.ownerPassword)
	account := newHoldingAccount(t, env, session, csrf, "Brokerage", "investment")
	today := serverToday()

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

	// 10 grams for S$1,000.00, worth S$120.00 each today: S$200.00 unrealised.
	rec = env.authed(t, http.MethodPost, "/api/v1/holdings/"+id+"/events", map[string]any{
		"kind": "acquisition", "quantity": "10", "amountMinor": 100000, "occurredOn": today,
	}, session, csrf)
	if rec.Code != http.StatusCreated {
		t.Fatalf("buy = %d (body = %s)", rec.Code, rec.Body.String())
	}
	rec = env.authed(t, http.MethodPost, "/api/v1/holdings/"+id+"/valuations", map[string]any{
		"unitPriceMinor": 12000, "asOf": today,
	}, session, csrf)
	if rec.Code != http.StatusOK {
		t.Fatalf("valuation = %d (body = %s)", rec.Code, rec.Body.String())
	}
	for _, row := range []struct {
		kind  string
		minor int64
	}{{"income", 4500}, {"fee", 500}} {
		rec = env.authed(t, http.MethodPost, "/api/v1/holdings/"+id+"/income", map[string]any{
			"kind": row.kind, "amountMinor": row.minor, "receivedOn": today,
		}, session, csrf)
		if rec.Code != http.StatusCreated {
			t.Fatalf("%s = %d (body = %s)", row.kind, rec.Code, rec.Body.String())
		}
	}

	rec = env.authed(t, http.MethodGet, "/api/v1/holdings/report?kind=quarter&count=2", nil, session, csrf)
	body := decodeReport(t, rec)
	if body.PrimaryCurrency != "SGD" {
		t.Fatalf("primaryCurrency = %q, want SGD", body.PrimaryCurrency)
	}
	if len(body.Holdings) != 1 || len(body.Holdings[0].Returns) != 2 {
		t.Fatalf("got %d holdings with %d returns, want 1 with 2", len(body.Holdings), len(body.Holdings[0].Returns))
	}
	current := body.Holdings[0].Returns[1]

	if current.Unrealised == nil || current.Unrealised.NativeMinor != 20000 {
		t.Errorf("unrealised = %v, want 20000", current.Unrealised)
	}
	if current.Income.NativeMinor != 4500 || current.Fees.NativeMinor != 500 {
		t.Errorf("income = %d, fees = %d, want 4500 and 500", current.Income.NativeMinor, current.Fees.NativeMinor)
	}
	// Fees are reported positive and already taken out of the total.
	if current.Total == nil || current.Total.NativeMinor != 24000 {
		t.Errorf("total = %v, want 24000", current.Total)
	}
	// The date crosses the wire as a date, not as a boolean. A screen that
	// only knows a price EXISTS cannot say how stale it is, which is this
	// feature's top product risk.
	if current.ClosingPriceAsOf == nil || *current.ClosingPriceAsOf != today {
		t.Errorf("closingPriceAsOf = %v, want %q", current.ClosingPriceAsOf, today)
	}
	// Nothing was held at the open, so no price was consulted there.
	if current.OpeningPriceAsOf != nil {
		t.Errorf("openingPriceAsOf = %v, want null", current.OpeningPriceAsOf)
	}
	if current.Reason != "" {
		t.Errorf("reason = %q, want empty on a computable period", current.Reason)
	}
	// The quarter before this holding existed reports ZERO, and reports it
	// without a reason: nothing was held at either end, so nothing needed a
	// price and nothing was earned. That is knowable, unlike a quarter that
	// was held through and never priced -- which blanks instead.
	previous := body.Holdings[0].Returns[0]
	if previous.Unrealised == nil || previous.Unrealised.NativeMinor != 0 {
		t.Errorf("the previous quarter = %+v, want a provable zero", previous.Unrealised)
	}
	if previous.Reason != "" {
		t.Errorf("reason = %q, want none -- holding nothing is not unknowable", previous.Reason)
	}
	if previous.ClosingPriceAsOf != nil {
		t.Errorf("closingPriceAsOf = %v, want null -- no price was consulted", previous.ClosingPriceAsOf)
	}
}

func TestIncomeRoundTripsAndDeletes(t *testing.T) {
	env := newTestEnv(t)
	session, csrf := env.signIn(t, env.ownerEmail, env.ownerPassword)
	account := newHoldingAccount(t, env, session, csrf, "Brokerage", "investment")

	rec := env.authed(t, http.MethodPost, "/api/v1/holdings", map[string]any{
		"accountId": account, "name": "D05", "instrument": "stock", "unit": "share",
	}, session, csrf)
	var created struct {
		Holding holdingBody `json:"holding"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode: %v", err)
	}
	id := created.Holding.ID

	rec = env.authed(t, http.MethodPost, "/api/v1/holdings/"+id+"/income", map[string]any{
		"kind": "income", "amountMinor": 4500, "receivedOn": serverToday(), "note": "Q3 dividend",
	}, session, csrf)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create income = %d (body = %s)", rec.Code, rec.Body.String())
	}
	var one struct {
		Income struct {
			ID          string `json:"id"`
			Kind        string `json:"kind"`
			AmountMinor int64  `json:"amountMinor"`
			Currency    string `json:"currency"`
			ReceivedOn  string `json:"receivedOn"`
			Note        string `json:"note"`
		} `json:"income"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &one); err != nil {
		t.Fatalf("decode income: %v", err)
	}
	if one.Income.Currency != "SGD" || one.Income.AmountMinor != 4500 || one.Income.Note != "Q3 dividend" {
		t.Fatalf("created income = %+v", one.Income)
	}

	rec = env.authed(t, http.MethodGet, "/api/v1/holdings/"+id+"/income", nil, session, csrf)
	var list struct {
		Income []struct {
			ID   string `json:"id"`
			Kind string `json:"kind"`
		} `json:"income"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &list); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	if len(list.Income) != 1 || list.Income[0].Kind != "income" {
		t.Fatalf("list = %+v, want the one dividend", list.Income)
	}

	rec = env.authed(t, http.MethodDelete, "/api/v1/holdings/"+id+"/income/"+one.Income.ID, nil, session, csrf)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("delete = %d, want 204 (body = %s)", rec.Code, rec.Body.String())
	}
	rec = env.authed(t, http.MethodGet, "/api/v1/holdings/"+id+"/income", nil, session, csrf)
	if err := json.Unmarshal(rec.Body.Bytes(), &list); err != nil {
		t.Fatalf("decode list after delete: %v", err)
	}
	if len(list.Income) != 0 {
		t.Fatalf("list after delete = %+v, want empty", list.Income)
	}
}

// A dividend dated tomorrow is a typo, refused at the wire and not only in a
// unit test -- the same guard events and valuations carry.
func TestFutureDatedIncomeIsRefusedAtTheWire(t *testing.T) {
	env := newTestEnv(t)
	session, csrf := env.signIn(t, env.ownerEmail, env.ownerPassword)
	account := newHoldingAccount(t, env, session, csrf, "Brokerage", "investment")

	rec := env.authed(t, http.MethodPost, "/api/v1/holdings", map[string]any{
		"accountId": account, "name": "D05", "instrument": "stock", "unit": "share",
	}, session, csrf)
	var created struct {
		Holding holdingBody `json:"holding"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode: %v", err)
	}

	tomorrow := time.Now().UTC().AddDate(0, 0, 2).Format("2006-01-02")
	rec = env.authed(t, http.MethodPost, "/api/v1/holdings/"+created.Holding.ID+"/income", map[string]any{
		"kind": "income", "amountMinor": 4500, "receivedOn": tomorrow,
	}, session, csrf)
	// 422, not 400: the request is well formed and the date is simply not
	// allowed -- the same answer a future-dated event and valuation already
	// give (errors.go maps ErrHoldingDateInFuture once, for all three).
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("future-dated income = %d, want 422 (body = %s)", rec.Code, rec.Body.String())
	}
}
