package httpadapter_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// --- Task 9: bill routes -----------------------------------------------

// billDTOBody mirrors bill_handlers.go's billDTO wire shape -- the brief's
// own field list, verbatim.
type billDTOBody struct {
	ID                 string  `json:"id"`
	Name               string  `json:"name"`
	AmountMinor        int64   `json:"amountMinor"`
	Currency           string  `json:"currency"`
	Cadence            string  `json:"cadence"`
	NextDue            *string `json:"nextDue"`
	CategoryID         string  `json:"categoryId"`
	CategoryName       string  `json:"categoryName"`
	PayFromAccountID   string  `json:"payFromAccountId"`
	AccountName        string  `json:"accountName"`
	PaidByMembershipID string  `json:"paidByMembershipId"`
	Autopay            bool    `json:"autopay"`
	IsSubscription     bool    `json:"isSubscription"`
	Overdue            bool    `json:"overdue"`
	DueSoon            bool    `json:"dueSoon"`
	Settled            bool    `json:"settled"`
	ArchivedAt         *string `json:"archivedAt"`
}

type billResponseBody struct {
	Bill billDTOBody `json:"bill"`
}

type nextDueBillBody struct {
	BillID      string `json:"billId"`
	BillName    string `json:"billName"`
	DueOn       string `json:"dueOn"`
	AmountMinor int64  `json:"amountMinor"`
	Currency    string `json:"currency"`
	Overdue     bool   `json:"overdue"`
	Autopay     bool   `json:"autopay"`
}

type billsSummaryBody struct {
	Currency                  string           `json:"currency"`
	DueThisMonthMinor         int64            `json:"dueThisMonthMinor"`
	PaidSoFarMinor            int64            `json:"paidSoFarMinor"`
	NextDue                   *nextDueBillBody `json:"nextDue"`
	AutopayCount              int              `json:"autopayCount"`
	BillCount                 int              `json:"billCount"`
	SubscriptionsMonthlyMinor int64            `json:"subscriptionsMonthlyMinor"`
	SubscriptionsAnnualMinor  int64            `json:"subscriptionsAnnualMinor"`
	ExcludedNoRate            int              `json:"excludedNoRate"`
}

type billPaymentBody struct {
	ID          string `json:"id"`
	BillID      string `json:"billId"`
	BillName    string `json:"billName"`
	DueOn       string `json:"dueOn"`
	PaidOn      string `json:"paidOn"`
	AmountMinor int64  `json:"amountMinor"`
	Currency    string `json:"currency"`
	Autopay     bool   `json:"autopay"`
}

type billsListResponseBody struct {
	Bills         []billDTOBody     `json:"bills"`
	PaidThisMonth []billPaymentBody `json:"paidThisMonth"`
	Summary       billsSummaryBody  `json:"summary"`
}

func decodeBillsList(t *testing.T, rec *httptest.ResponseRecorder) billsListResponseBody {
	t.Helper()
	var body billsListResponseBody
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode bills list: %v (body = %s)", err, rec.Body.String())
	}
	return body
}

func decodeBill(t *testing.T, rec *httptest.ResponseRecorder) billResponseBody {
	t.Helper()
	var body billResponseBody
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode bill: %v (body = %s)", err, rec.Body.String())
	}
	return body
}

// mustCreateBill POSTs body to /bills as the owner and fails the test
// immediately if the create itself did not succeed -- setup, not the
// assertion under test, the same shape mustCreateGoal uses.
func (env *testEnv) mustCreateBill(t *testing.T, session, csrf *http.Cookie, body map[string]any) billResponseBody {
	t.Helper()
	rec := env.authed(t, http.MethodPost, "/api/v1/bills", body, session, csrf)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create bill: status = %d, body = %s", rec.Code, rec.Body.String())
	}
	return decodeBill(t, rec)
}

// mustCreateAccountWithCurrency is mustCreateAccountID with a caller-chosen
// currency -- budget_api_test.go's own helper is always SGD, and the
// currency-immutable test below needs a second currency to point a bill's
// payFromAccountId at.
func (env *testEnv) mustCreateAccountWithCurrency(t *testing.T, session, csrf *http.Cookie, currency string) string {
	t.Helper()
	rec := env.authed(t, http.MethodPost, "/api/v1/accounts", map[string]any{
		"nickname": "Account in " + currency, "type": "cash",
		"openingBalanceMinor": 100_000, "openingBalanceCurrency": currency,
		"openingBalanceAsOf": "2026-01-01",
	}, session, csrf)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create %s account: status = %d, body = %s", currency, rec.Code, rec.Body.String())
	}
	var body struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode created account: %v", err)
	}
	return body.ID
}

// farFutureDate is a next_due comfortably outside every "due soon" and
// "overdue" window, regardless of which day this suite happens to run on --
// the fixture every test below uses unless it is specifically exercising
// those derived flags, which usecase/bill_test.go already covers in far more
// depth than an HTTP-layer wiring test needs to repeat.
func farFutureDate() string {
	return time.Now().UTC().AddDate(0, 0, 400).Format("2006-01-02")
}

// --- route-walk matrix ------------------------------------------------------

// TestBillRoutesRequireMoneyAndOwner is TestGoalRoutesRequireMoneyAndOwner's
// shape applied to the five bill routes: reads and writes alike sit behind
// CapMoney AND requireOwner, the same as transactions, categories, budgets
// and goals -- a bills screen with every figure blank reads as broken for a
// limited member the same way a half-redacted ledger would (router.go's own
// comment on the txn group).
//
// wantOwner pins the exact status an owner receives, not merely "not
// 401/403": a route wired with a nil deps.Bills would pass both guards and
// panic into a 500, which a bare non-401/403 check would let slide by
// unnoticed.
//
// The archive and restore rows carry a second check the generic matrix
// above them does not: chi's own route-not-found catch-all
// (router.go's r.NotFound) answers the identical {404, "NOT_FOUND"} shape a
// real BillService.SetArchived refusal would, so a mistyped or never-wired
// route would still show 404 here and pass for the wrong reason. Asserting
// the message text -- "That could not be found." only comes from
// errors.go's domain.ErrNotFound case, never from the router's own catch-all
// message "That endpoint does not exist." -- is what budget_api_test.go's
// TestBudgetRolloverNoBudgetRowIsNotFound already does for the identical
// hazard on the rollover route.
func TestBillsRoutesRequireMoneyAndOwner(t *testing.T) {
	env := newTestEnv(t)
	zeroUUID := "00000000-0000-0000-0000-000000000000"

	routes := []struct {
		method, path      string
		wantOwner         int
		wantOwnerNotFound bool
	}{
		{http.MethodGet, "/api/v1/bills", http.StatusOK, false},
		{http.MethodPost, "/api/v1/bills", http.StatusBadRequest, false},
		{http.MethodPatch, "/api/v1/bills/" + zeroUUID, http.StatusBadRequest, false},
		{http.MethodPost, "/api/v1/bills/" + zeroUUID + "/archive", http.StatusNotFound, true},
		{http.MethodPost, "/api/v1/bills/" + zeroUUID + "/restore", http.StatusNotFound, true},
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
			if route.wantOwnerNotFound {
				var body struct {
					Error struct {
						Code    string `json:"code"`
						Message string `json:"message"`
					} `json:"error"`
				}
				if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
					t.Fatalf("decode error body: %v (body = %s)", err, rec.Body.String())
				}
				if body.Error.Message != "That could not be found." {
					t.Fatalf("message = %q, want %q -- a mismatch here means this 404 came from chi's own "+
						"route-not-found catch-all, not a real BillService refusal",
						body.Error.Message, "That could not be found.")
				}
			}
		})
	}
}

// TestBillWriteRoutesRequireCSRF mirrors TestCategoryWriteRoutesRequireCSRF
// for the four mutating bill routes: no token at all, and a token that does
// not match the cookie, both refused by the CSRF_INVALID code specifically.
func TestBillsWriteRoutesRequireCSRF(t *testing.T) {
	env := newTestEnv(t)
	session, csrf := env.signIn(t, env.ownerEmail, env.ownerPassword)
	zeroUUID := "00000000-0000-0000-0000-000000000000"

	routes := []struct{ method, path string }{
		{http.MethodPost, "/api/v1/bills"},
		{http.MethodPatch, "/api/v1/bills/" + zeroUUID},
		{http.MethodPost, "/api/v1/bills/" + zeroUUID + "/archive"},
		{http.MethodPost, "/api/v1/bills/" + zeroUUID + "/restore"},
	}

	for _, route := range routes {
		t.Run(route.method+" "+route.path, func(t *testing.T) {
			req := httptest.NewRequest(route.method, route.path, nil)
			req.AddCookie(session)
			req.AddCookie(csrf)
			rec := httptest.NewRecorder()
			env.router.ServeHTTP(rec, req)
			assertErrorResponse(t, rec, http.StatusForbidden, "CSRF_INVALID")

			req2 := httptest.NewRequest(route.method, route.path, nil)
			req2.AddCookie(session)
			req2.AddCookie(csrf)
			req2.Header.Set("X-CSRF-Token", "definitely-the-wrong-value")
			rec2 := httptest.NewRecorder()
			env.router.ServeHTTP(rec2, req2)
			assertErrorResponse(t, rec2, http.StatusForbidden, "CSRF_INVALID")
		})
	}
}

// --- behaviour tests ---------------------------------------------------

// TestBillCreateRoundTripsThroughGet is the wire-level pin that a created
// bill's own response and a follow-up GET describe the same row, joined
// names included -- CategoryName and AccountName only exist because
// BillRecord joins them (ports.go's own comment), so this is what proves
// the join reaches the wire, not just the ids.
func TestBillsCreateRoundTripsThroughGet(t *testing.T) {
	env := newTestEnv(t)
	session, csrf := env.signIn(t, env.ownerEmail, env.ownerPassword)
	categoryID, categoryName := env.firstExpenseCategory(t, session)
	accountID := env.mustCreateAccountID(t, session, csrf)

	created := env.mustCreateBill(t, session, csrf, map[string]any{
		"name": "Internet", "amountMinor": 45_000, "cadence": "monthly",
		"nextDue": farFutureDate(), "categoryId": categoryID, "payFromAccountId": accountID,
		"autopay": true,
	})
	b := created.Bill
	if b.Name != "Internet" || b.AmountMinor != 45_000 || b.Cadence != "monthly" {
		t.Fatalf("create response = %+v, want name/amount/cadence to match the request", b)
	}
	if b.Currency != "SGD" {
		t.Fatalf("currency = %q, want SGD (the pay-from account's own currency)", b.Currency)
	}
	if b.CategoryID != categoryID || b.CategoryName != categoryName {
		t.Fatalf("category = %q/%q, want %q/%q", b.CategoryID, b.CategoryName, categoryID, categoryName)
	}
	if !b.Autopay {
		t.Fatal("autopay = false, want true")
	}
	if b.Settled {
		t.Fatal("settled = true, want false: a freshly created bill always carries a next_due")
	}
	if b.ArchivedAt != nil {
		t.Fatalf("archivedAt = %v, want nil for a bill that was never archived", *b.ArchivedAt)
	}

	getRec := env.authedGet(t, "/api/v1/bills", session)
	if getRec.Code != http.StatusOK {
		t.Fatalf("get: status = %d, body = %s", getRec.Code, getRec.Body.String())
	}
	list := decodeBillsList(t, getRec)
	var found *billDTOBody
	for i := range list.Bills {
		if list.Bills[i].ID == b.ID {
			found = &list.Bills[i]
		}
	}
	if found == nil {
		t.Fatalf("bill %s missing from GET /bills", b.ID)
	}
	if found.Name != b.Name || found.AccountName == "" {
		t.Fatalf("list row = %+v, want it to describe the same bill with a non-empty account name", found)
	}
	if list.Summary.BillCount != 1 {
		t.Fatalf("summary.billCount = %d, want 1", list.Summary.BillCount)
	}
	if list.Summary.AutopayCount != 1 {
		t.Fatalf("summary.autopayCount = %d, want 1 (the bill above has autopay = true)", list.Summary.AutopayCount)
	}
}

// TestBillCreateSerialisesSettledAndNullableFields pins two wire-shape facts
// a Go zero-value round-trip cannot distinguish from their absence: settled
// must be a real JSON key (Task 7's own review found it missing from an
// earlier sketch of this DTO -- decoding into a Go bool cannot tell "the
// server sent false" from "the server sent nothing at all", the exact trap
// assertRolloverFieldsNull's own comment names for a *time.Time field), and
// archivedAt must serialise as literal null, not be an absent key -- the
// distinction createGoalRequest's own currency-field comment and Task 11's
// zod schemas both rely on.
func TestBillsCreateSerialisesSettledAndNullableFields(t *testing.T) {
	env := newTestEnv(t)
	session, csrf := env.signIn(t, env.ownerEmail, env.ownerPassword)
	accountID := env.mustCreateAccountID(t, session, csrf)

	rec := env.authed(t, http.MethodPost, "/api/v1/bills", map[string]any{
		"name": "Water", "amountMinor": 5_000, "cadence": "monthly",
		"nextDue": farFutureDate(), "payFromAccountId": accountID,
	}, session, csrf)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create: status = %d, body = %s", rec.Code, rec.Body.String())
	}

	var raw struct {
		Bill map[string]json.RawMessage `json:"bill"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &raw); err != nil {
		t.Fatalf("decode raw: %v (body = %s)", err, rec.Body.String())
	}
	settled, ok := raw.Bill["settled"]
	if !ok {
		t.Fatalf(`response has no "settled" key at all: %s`, rec.Body.String())
	}
	if string(settled) != "false" {
		t.Fatalf(`"settled" = %s, want literal false`, settled)
	}
	archivedAt, ok := raw.Bill["archivedAt"]
	if !ok {
		t.Fatalf(`response has no "archivedAt" key at all: %s`, rec.Body.String())
	}
	if string(archivedAt) != "null" {
		t.Fatalf(`"archivedAt" = %s, want literal null`, archivedAt)
	}
}

// TestBillCreateDuplicateNameOffersRestoreWhenArchived is the brief's
// "ErrBillNameTaken -> 409 whose body names the taken name and whether the
// holder is archived" case, following writeGoalNameConflict's own precedent:
// an archived collision gets the richer 409 with the archived bill's id in
// details, so the modal can offer Restore instead of a dead end.
func TestBillsCreateDuplicateNameOffersRestoreWhenArchived(t *testing.T) {
	env := newTestEnv(t)
	session, csrf := env.signIn(t, env.ownerEmail, env.ownerPassword)
	accountID := env.mustCreateAccountID(t, session, csrf)

	created := env.mustCreateBill(t, session, csrf, map[string]any{
		"name": "Streaming", "amountMinor": 1_500, "cadence": "monthly",
		"nextDue": farFutureDate(), "payFromAccountId": accountID,
	})

	archiveRec := env.authed(t, http.MethodPost, "/api/v1/bills/"+created.Bill.ID+"/archive", nil, session, csrf)
	if archiveRec.Code != http.StatusOK {
		t.Fatalf("archive: status = %d, body = %s", archiveRec.Code, archiveRec.Body.String())
	}

	rec := env.authed(t, http.MethodPost, "/api/v1/bills", map[string]any{
		"name": "Streaming", "amountMinor": 2_000, "cadence": "monthly",
		"nextDue": farFutureDate(), "payFromAccountId": accountID,
	}, session, csrf)
	body := assertErrorResponse(t, rec, http.StatusConflict, "BILL_NAME_TAKEN")
	gotID, _ := body.Error.Details["archivedBillId"].(string)
	if gotID != created.Bill.ID {
		t.Fatalf("details.archivedBillId = %q, want %q", gotID, created.Bill.ID)
	}
}

// TestBillUpdateCurrencyMismatchNamesBothCurrencies is BillService.Update's
// own doc comment realised on the wire: the service returns the bare
// domain.ErrBillCurrencyImmutable sentinel deliberately, and "the message
// naming both currencies is the HTTP layer's job, not this one's" -- so this
// asserts the message actually names them, not merely that the status is
// 422.
func TestBillsUpdateCurrencyMismatchNamesBothCurrencies(t *testing.T) {
	env := newTestEnv(t)
	session, csrf := env.signIn(t, env.ownerEmail, env.ownerPassword)
	sgdAccount := env.mustCreateAccountID(t, session, csrf)
	idrAccount := env.mustCreateAccountWithCurrency(t, session, csrf, "IDR")

	created := env.mustCreateBill(t, session, csrf, map[string]any{
		"name": "Electricity", "amountMinor": 12_000, "cadence": "monthly",
		"nextDue": farFutureDate(), "payFromAccountId": sgdAccount,
	})

	rec := env.authed(t, http.MethodPatch, "/api/v1/bills/"+created.Bill.ID,
		map[string]any{"payFromAccountId": idrAccount}, session, csrf)
	body := assertErrorResponse(t, rec, http.StatusUnprocessableEntity, "BILL_CURRENCY_IMMUTABLE")
	if !strings.Contains(body.Error.Message, "SGD") || !strings.Contains(body.Error.Message, "IDR") {
		t.Fatalf("message = %q, want it to name both SGD and IDR", body.Error.Message)
	}
}

// TestBillArchiveOmitsFromListRestoreUndoes mirrors
// TestCategoryArchiveOmitsFromListRestoreUndoes: the default list must stop
// offering an archived bill, ?include_archived=true must keep showing it
// with archivedAt set, and Restore must put it straight back in the default
// list -- and every one of the three responses (archive, list, restore)
// must itself carry a body, since SetArchived's own doc comment says it
// returns the full record specifically so this handler never needs a second
// Get to answer.
func TestBillsArchiveOmitsFromListRestoreUndoes(t *testing.T) {
	env := newTestEnv(t)
	session, csrf := env.signIn(t, env.ownerEmail, env.ownerPassword)
	accountID := env.mustCreateAccountID(t, session, csrf)
	created := env.mustCreateBill(t, session, csrf, map[string]any{
		"name": "Gym", "amountMinor": 8_000, "cadence": "monthly",
		"nextDue": farFutureDate(), "payFromAccountId": accountID,
	})

	archiveRec := env.authed(t, http.MethodPost, "/api/v1/bills/"+created.Bill.ID+"/archive", nil, session, csrf)
	if archiveRec.Code != http.StatusOK {
		t.Fatalf("archive: status = %d, body = %s", archiveRec.Code, archiveRec.Body.String())
	}
	archived := decodeBill(t, archiveRec)
	if archived.Bill.ArchivedAt == nil {
		t.Fatal("archive response archivedAt = nil, want set")
	}

	defaultList := decodeBillsList(t, env.authedGet(t, "/api/v1/bills", session))
	for _, b := range defaultList.Bills {
		if b.ID == created.Bill.ID {
			t.Fatalf("default list still has %s after archiving", created.Bill.ID)
		}
	}

	includeRec := env.authedGet(t, "/api/v1/bills?include_archived=true", session)
	if includeRec.Code != http.StatusOK {
		t.Fatalf("list include_archived: status = %d, body = %s", includeRec.Code, includeRec.Body.String())
	}
	includeList := decodeBillsList(t, includeRec)
	var stillThere bool
	for _, b := range includeList.Bills {
		if b.ID == created.Bill.ID {
			stillThere = true
			if b.ArchivedAt == nil {
				t.Fatal("include_archived list shows archivedAt = nil, want set")
			}
		}
	}
	if !stillThere {
		t.Fatalf("include_archived=true list is missing %s entirely", created.Bill.ID)
	}

	restoreRec := env.authed(t, http.MethodPost, "/api/v1/bills/"+created.Bill.ID+"/restore", nil, session, csrf)
	if restoreRec.Code != http.StatusOK {
		t.Fatalf("restore: status = %d, body = %s", restoreRec.Code, restoreRec.Body.String())
	}
	restored := decodeBill(t, restoreRec)
	if restored.Bill.ArchivedAt != nil {
		t.Fatal("restore response archivedAt != nil, want nil")
	}

	backInList := decodeBillsList(t, env.authedGet(t, "/api/v1/bills", session))
	var found bool
	for _, b := range backInList.Bills {
		if b.ID == created.Bill.ID {
			found = true
		}
	}
	if !found {
		t.Fatalf("default list still missing %s after restoring", created.Bill.ID)
	}
}

// TestBillCreateValidationErrors covers the per-field create guards: an
// empty name, a non-positive amount, and a cadence this code did not
// construct -- the same "arrives from a request body, refused the same way
// a bad database column would be" rule ErrUnknownCadence's own comment in
// domain/errors.go states.
func TestBillsCreateValidationErrors(t *testing.T) {
	env := newTestEnv(t)
	session, csrf := env.signIn(t, env.ownerEmail, env.ownerPassword)
	accountID := env.mustCreateAccountID(t, session, csrf)

	cases := []struct {
		name string
		body map[string]any
		code string
	}{
		{
			name: "empty name",
			body: map[string]any{"name": "  ", "amountMinor": 1_000, "cadence": "monthly",
				"nextDue": farFutureDate(), "payFromAccountId": accountID},
			code: "BILL_NAME_REQUIRED",
		},
		{
			name: "non-positive amount",
			body: map[string]any{"name": "Rent", "amountMinor": 0, "cadence": "monthly",
				"nextDue": farFutureDate(), "payFromAccountId": accountID},
			code: "BILL_AMOUNT_NOT_POSITIVE",
		},
		{
			name: "unknown cadence",
			body: map[string]any{"name": "Rent", "amountMinor": 1_000, "cadence": "weekly",
				"nextDue": farFutureDate(), "payFromAccountId": accountID},
			code: "INVALID_CADENCE",
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			rec := env.authed(t, http.MethodPost, "/api/v1/bills", c.body, session, csrf)
			assertErrorResponse(t, rec, http.StatusUnprocessableEntity, c.code)
		})
	}
}

// TestBillCreateForbiddenWhenAccountArchived is BillService.Create's own
// "an archived pay-from account" refusal (domain.ErrForbidden), realised on
// the wire with a message naming the reason -- MapDomainError's own generic
// ErrForbidden case answers a bare 403 with no detail, so this proves
// handleCreateBill intercepts the sentinel before that shared case ever
// sees it.
func TestBillsCreateForbiddenWhenAccountArchived(t *testing.T) {
	env := newTestEnv(t)
	session, csrf := env.signIn(t, env.ownerEmail, env.ownerPassword)
	accountID := env.mustCreateAccountID(t, session, csrf)

	archiveRec := env.authed(t, http.MethodPost, "/api/v1/accounts/"+accountID+"/archive", nil, session, csrf)
	if archiveRec.Code != http.StatusOK {
		t.Fatalf("archive account: status = %d, body = %s", archiveRec.Code, archiveRec.Body.String())
	}

	rec := env.authed(t, http.MethodPost, "/api/v1/bills", map[string]any{
		"name": "Rent", "amountMinor": 1_000, "cadence": "monthly",
		"nextDue": farFutureDate(), "payFromAccountId": accountID,
	}, session, csrf)
	assertErrorResponse(t, rec, http.StatusUnprocessableEntity, "ACCOUNT_ARCHIVED")
}

// TestBillCreatePayerFromAnotherHouseholdIsInvalidOwner confirms the brief's
// item 5: domain.ErrAccountOwnerNotInHousehold is already mapped to 422
// INVALID_OWNER by the shared switch in errors.go, and BillService.Create
// returns it unchanged when paidByMembershipId names a membership this
// household does not have -- nothing bill-specific needed to be added for
// this case.
func TestBillsCreatePayerFromAnotherHouseholdIsInvalidOwner(t *testing.T) {
	env := newTestEnv(t)
	session, csrf := env.signIn(t, env.ownerEmail, env.ownerPassword)
	accountID := env.mustCreateAccountID(t, session, csrf)
	zeroUUID := "00000000-0000-0000-0000-000000000000"

	rec := env.authed(t, http.MethodPost, "/api/v1/bills", map[string]any{
		"name": "Rent", "amountMinor": 1_000, "cadence": "monthly",
		"nextDue": farFutureDate(), "payFromAccountId": accountID,
		"paidByMembershipId": zeroUUID,
	}, session, csrf)
	assertErrorResponse(t, rec, http.StatusUnprocessableEntity, "INVALID_OWNER")
}
