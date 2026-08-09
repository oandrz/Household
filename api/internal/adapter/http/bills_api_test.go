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
		// pay decodes its body before it ever looks the bill up (the same
		// order Create and Update already follow), so a bodiless request as
		// owner answers 400 INVALID_BODY exactly like POST /bills and PATCH
		// /bills/{id} above, never reaching the not-found check.
		{http.MethodPost, "/api/v1/bills/" + zeroUUID + "/pay", http.StatusBadRequest, false},
		// undo needs no body at all -- chi URL params only -- so a made-up
		// bill AND payment id reaches UndoPayment, which answers the same
		// {404, "That could not be found."} shape as archive/restore above.
		{http.MethodDelete, "/api/v1/bills/" + zeroUUID + "/payments/" + zeroUUID, http.StatusNotFound, true},
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
		{http.MethodPost, "/api/v1/bills/" + zeroUUID + "/pay"},
		{http.MethodDelete, "/api/v1/bills/" + zeroUUID + "/payments/" + zeroUUID},
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

// --- Task 10: pay and undo ----------------------------------------------

// billPaymentResponseBody mirrors bill_handlers.go's billPaymentResponse --
// POST /bills/{id}/pay's whole body.
type billPaymentResponseBody struct {
	Payment billPaymentBody `json:"payment"`
	Bill    billDTOBody     `json:"bill"`
}

func decodeBillPayment(t *testing.T, rec *httptest.ResponseRecorder) billPaymentResponseBody {
	t.Helper()
	var body billPaymentResponseBody
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode bill payment: %v (body = %s)", err, rec.Body.String())
	}
	return body
}

// mustPayBill POSTs .../pay as the owner and fails the test immediately if
// the pay itself did not succeed -- setup, not the assertion under test, the
// same shape mustCreateBill uses.
func (env *testEnv) mustPayBill(t *testing.T, session, csrf *http.Cookie, billID string, body map[string]any) billPaymentResponseBody {
	t.Helper()
	rec := env.authed(t, http.MethodPost, "/api/v1/bills/"+billID+"/pay", body, session, csrf)
	if rec.Code != http.StatusOK {
		t.Fatalf("pay bill: status = %d, body = %s", rec.Code, rec.Body.String())
	}
	return decodeBillPayment(t, rec)
}

// TestBillsMarkPaidDefaultsAmountAdvancesNextDueAndAnswersPaymentAndBill is
// the happy path: amountMinor omitted falls back to the bill's own stored
// figure (the brief's own default), the response carries both halves the
// brief promises ({"payment": ..., "bill": ...}), DueOn is the occurrence
// that was due (not paidOn), and NextDue advances by one cadence period from
// that due date -- the same rule TestMarkPaidAdvancesNextDueByTheCadenceFromTheDueDate
// pins at the service layer, now proven to reach the wire.
func TestBillsMarkPaidDefaultsAmountAdvancesNextDueAndAnswersPaymentAndBill(t *testing.T) {
	env := newTestEnv(t)
	session, csrf := env.signIn(t, env.ownerEmail, env.ownerPassword)
	accountID := env.mustCreateAccountID(t, session, csrf)

	created := env.mustCreateBill(t, session, csrf, map[string]any{
		"name": "Internet", "amountMinor": 45_000, "cadence": "monthly",
		"nextDue": "2030-01-15", "payFromAccountId": accountID,
	})

	paid := env.mustPayBill(t, session, csrf, created.Bill.ID, map[string]any{
		"paidOn": "2030-01-16",
	})

	if paid.Payment.AmountMinor != 45_000 {
		t.Fatalf("payment.amountMinor = %d, want 45000 (the bill's own, since amountMinor was omitted)", paid.Payment.AmountMinor)
	}
	if paid.Payment.BillID != created.Bill.ID {
		t.Fatalf("payment.billId = %q, want %q", paid.Payment.BillID, created.Bill.ID)
	}
	if paid.Payment.DueOn != "2030-01-15" {
		t.Fatalf("payment.dueOn = %q, want 2030-01-15 -- the occurrence settled, not paidOn", paid.Payment.DueOn)
	}
	if paid.Payment.PaidOn != "2030-01-16" {
		t.Fatalf("payment.paidOn = %q, want 2030-01-16", paid.Payment.PaidOn)
	}
	if paid.Bill.NextDue == nil || *paid.Bill.NextDue != "2030-02-15" {
		t.Fatalf("bill.nextDue = %v, want 2030-02-15", paid.Bill.NextDue)
	}
	if paid.Bill.Settled {
		t.Fatal("bill.settled = true, want false: a monthly bill always has a next occurrence")
	}
}

// TestBillsMarkPaidTwiceOnTheSameOccurrenceIsConflict is the brief's own
// "paying an occurrence twice is 409". next_due always advances after a
// successful pay, so the only way to present the SAME due_on twice through
// the API is to pay, then PATCH nextDue back to the occurrence just paid --
// UNIQUE (bill_id, due_on) is what refuses the second pay, exactly the
// backstop-behind-a-race the design's own error table describes.
func TestBillsMarkPaidTwiceOnTheSameOccurrenceIsConflict(t *testing.T) {
	env := newTestEnv(t)
	session, csrf := env.signIn(t, env.ownerEmail, env.ownerPassword)
	accountID := env.mustCreateAccountID(t, session, csrf)

	created := env.mustCreateBill(t, session, csrf, map[string]any{
		"name": "Netflix", "amountMinor": 1_998, "cadence": "monthly",
		"nextDue": "2030-01-05", "payFromAccountId": accountID,
	})
	env.mustPayBill(t, session, csrf, created.Bill.ID, map[string]any{
		"amountMinor": 1_998, "paidOn": "2030-01-05",
	})

	patchRec := env.authed(t, http.MethodPatch, "/api/v1/bills/"+created.Bill.ID,
		map[string]any{"nextDue": "2030-01-05"}, session, csrf)
	if patchRec.Code != http.StatusOK {
		t.Fatalf("patch nextDue back to the paid occurrence: status = %d, body = %s", patchRec.Code, patchRec.Body.String())
	}

	rec := env.authed(t, http.MethodPost, "/api/v1/bills/"+created.Bill.ID+"/pay",
		map[string]any{"amountMinor": 1_998, "paidOn": "2030-01-06"}, session, csrf)
	assertErrorResponse(t, rec, http.StatusConflict, "ALREADY_EXISTS")
}

// TestBillsMarkPaidRejectsANonPositiveAmount is item 1 of the task's "two
// things to get right": MarkPaid does not itself validate AmountMinor > 0
// (unlike Create and Update), so without a guard a non-positive amount would
// reach bill_payments' own CHECK (amount_minor > 0) as a raw constraint
// violation and surface as a 500 -- not an acceptable answer to a bad
// request body. The guard lives in BillService.MarkPaid (see the task
// report for why), which is why this is a behaviour test here rather than a
// handler-only one: it proves the whole path, not just a decode check.
func TestBillsMarkPaidRejectsANonPositiveAmount(t *testing.T) {
	env := newTestEnv(t)
	session, csrf := env.signIn(t, env.ownerEmail, env.ownerPassword)
	accountID := env.mustCreateAccountID(t, session, csrf)
	created := env.mustCreateBill(t, session, csrf, map[string]any{
		"name": "Water", "amountMinor": 5_000, "cadence": "monthly",
		"nextDue": farFutureDate(), "payFromAccountId": accountID,
	})

	for _, amount := range []int64{0, -500} {
		rec := env.authed(t, http.MethodPost, "/api/v1/bills/"+created.Bill.ID+"/pay",
			map[string]any{"amountMinor": amount, "paidOn": "2026-01-01"}, session, csrf)
		assertErrorResponse(t, rec, http.StatusUnprocessableEntity, "BILL_AMOUNT_NOT_POSITIVE")
	}
}

// TestBillsMarkPaidRejectsAnUnparseablePaidOn is item 2 of the task's "two
// things to get right": paidOn is a date from a request body, and must fail
// closed -- an empty string (an omitted key round-trips as one, since
// payBillRequest's own field is a plain string) or garbage both answer 422,
// never a zero time silently written as the payment date.
func TestBillsMarkPaidRejectsAnUnparseablePaidOn(t *testing.T) {
	env := newTestEnv(t)
	session, csrf := env.signIn(t, env.ownerEmail, env.ownerPassword)
	accountID := env.mustCreateAccountID(t, session, csrf)
	created := env.mustCreateBill(t, session, csrf, map[string]any{
		"name": "Water", "amountMinor": 5_000, "cadence": "monthly",
		"nextDue": farFutureDate(), "payFromAccountId": accountID,
	})

	cases := []struct {
		name string
		body map[string]any
	}{
		{"omitted entirely", map[string]any{"amountMinor": 5_000}},
		{"empty string", map[string]any{"amountMinor": 5_000, "paidOn": ""}},
		{"not a date", map[string]any{"amountMinor": 5_000, "paidOn": "not-a-date"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			rec := env.authed(t, http.MethodPost, "/api/v1/bills/"+created.Bill.ID+"/pay", c.body, session, csrf)
			assertErrorResponse(t, rec, http.StatusUnprocessableEntity, "INVALID_DATE")
		})
	}
}

// TestBillsMarkPaidInAnotherHouseholdIsNotFound is the brief's own "paying a
// bill in another household is 404". zeroUUID stands in for "a bill in
// another household" -- the goals_api_test.go/budget_api_test.go convention
// (this suite has no second-household fixture to build a real
// cross-household id from), valid here because BillRepository.Get's own
// contract makes the two indistinguishable: both simply match no row scoped
// to this household.
//
// Both the amount-supplied and amount-omitted paths are covered: the first
// reaches MarkPaid's own Bills.Get, the second reaches the handler's own
// default-amount lookup first -- two different code paths that must both
// answer the same 404.
//
// The message is checked, not just the code: chi's own route-not-found
// catch-all (router.go's r.NotFound) answers the identical {404,
// "NOT_FOUND"} CODE a real refusal would, with a different MESSAGE ("That
// endpoint does not exist." vs "That could not be found.") -- exactly the
// hazard TestBillsRoutesRequireMoneyAndOwner's own comment names, and
// without this check a route that was never wired at all would pass this
// test for the wrong reason.
func TestBillsMarkPaidInAnotherHouseholdIsNotFound(t *testing.T) {
	env := newTestEnv(t)
	session, csrf := env.signIn(t, env.ownerEmail, env.ownerPassword)
	zeroUUID := "00000000-0000-0000-0000-000000000000"

	cases := []struct {
		name string
		body map[string]any
	}{
		{"amount supplied", map[string]any{"amountMinor": 1_000, "paidOn": "2030-01-01"}},
		{"amount omitted", map[string]any{"paidOn": "2030-01-01"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			rec := env.authed(t, http.MethodPost, "/api/v1/bills/"+zeroUUID+"/pay", c.body, session, csrf)
			body := assertErrorResponse(t, rec, http.StatusNotFound, "NOT_FOUND")
			if body.Error.Message != "That could not be found." {
				t.Fatalf("message = %q, want %q -- a mismatch here means this 404 came from chi's own "+
					"route-not-found catch-all, not a real BillService refusal",
					body.Error.Message, "That could not be found.")
			}
		})
	}
}

// TestBillsUndoDeletesThePaymentRewindsNextDueAndAnswers204WithNoBody proves
// step 7's own self-review question directly: the DELETE response really
// carries zero bytes (not merely status 204), and undoing genuinely rewinds
// next_due back to the undone payment's own due date -- the same
// TestUndoReversesAllThreeWrites assertion, now proven to reach the wire.
func TestBillsUndoDeletesThePaymentRewindsNextDueAndAnswers204WithNoBody(t *testing.T) {
	env := newTestEnv(t)
	session, csrf := env.signIn(t, env.ownerEmail, env.ownerPassword)
	accountID := env.mustCreateAccountID(t, session, csrf)

	created := env.mustCreateBill(t, session, csrf, map[string]any{
		"name": "Gym", "amountMinor": 8_000, "cadence": "monthly",
		"nextDue": "2030-01-20", "payFromAccountId": accountID,
	})
	paid := env.mustPayBill(t, session, csrf, created.Bill.ID, map[string]any{
		"amountMinor": 8_000, "paidOn": "2030-01-20",
	})

	rec := env.authed(t, http.MethodDelete,
		"/api/v1/bills/"+created.Bill.ID+"/payments/"+paid.Payment.ID, nil, session, csrf)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("undo: status = %d, want 204 (body = %s)", rec.Code, rec.Body.String())
	}
	if rec.Body.Len() != 0 {
		t.Fatalf("undo body = %q (%d bytes), want empty -- the one exemption to the every-2xx-carries-a-body rule",
			rec.Body.String(), rec.Body.Len())
	}

	list := decodeBillsList(t, env.authedGet(t, "/api/v1/bills", session))
	var found *billDTOBody
	for i := range list.Bills {
		if list.Bills[i].ID == created.Bill.ID {
			found = &list.Bills[i]
		}
	}
	if found == nil {
		t.Fatalf("bill %s missing from GET /bills after undo", created.Bill.ID)
	}
	if found.NextDue == nil || *found.NextDue != "2030-01-20" {
		t.Fatalf("nextDue after undo = %v, want rewound to 2030-01-20", found.NextDue)
	}
}

// TestBillsUndoRefusesAnOlderPaymentNamingTheUndoable is the brief's own
// "undoing an older payment is 409 naming the payment that can be undone" --
// the design's identical wording, appearing three times in its own spec.
// The repository already knows which due date WOULD be accepted (it
// computes MAX(due_on) to decide THIS one wasn't it); this proves that fact
// reaches the wire as BILL_PAYMENT_NOT_LATEST, both in the message and in
// details.undoableDueOn -- Task 14's frontend needs the latter to read
// without parsing prose out of the former.
func TestBillsUndoRefusesAnOlderPaymentNamingTheUndoable(t *testing.T) {
	env := newTestEnv(t)
	session, csrf := env.signIn(t, env.ownerEmail, env.ownerPassword)
	accountID := env.mustCreateAccountID(t, session, csrf)

	created := env.mustCreateBill(t, session, csrf, map[string]any{
		"name": "Netflix", "amountMinor": 1_998, "cadence": "monthly",
		"nextDue": "2030-01-15", "payFromAccountId": accountID,
	})
	first := env.mustPayBill(t, session, csrf, created.Bill.ID, map[string]any{
		"amountMinor": 1_998, "paidOn": "2030-01-15",
	})
	env.mustPayBill(t, session, csrf, created.Bill.ID, map[string]any{
		"amountMinor": 1_998, "paidOn": "2030-02-15",
	})

	rec := env.authed(t, http.MethodDelete,
		"/api/v1/bills/"+created.Bill.ID+"/payments/"+first.Payment.ID, nil, session, csrf)
	body := assertErrorResponse(t, rec, http.StatusConflict, "BILL_PAYMENT_NOT_LATEST")
	if !strings.Contains(body.Error.Message, "2030-02-15") {
		t.Fatalf("message = %q, want it to name 2030-02-15, the payment that IS undoable", body.Error.Message)
	}
	gotDue, _ := body.Error.Details["undoableDueOn"].(string)
	if gotDue != "2030-02-15" {
		t.Fatalf("details.undoableDueOn = %q, want 2030-02-15", gotDue)
	}
}

// TestBillsUndoInAnotherHouseholdIsNotFound is TestBillsMarkPaidInAnotherHouseholdIsNotFound's
// undo-side mirror, and the identical GoalRepository/BillRepository
// household-scoping contract handleDeleteGoalContribution's own comment
// states for contributions: a payment id from another household is not
// found, not forbidden. The message check is the same defence against
// chi's own route-not-found catch-all -- see the pay-side test's own
// comment.
func TestBillsUndoInAnotherHouseholdIsNotFound(t *testing.T) {
	env := newTestEnv(t)
	session, csrf := env.signIn(t, env.ownerEmail, env.ownerPassword)
	zeroUUID := "00000000-0000-0000-0000-000000000000"

	rec := env.authed(t, http.MethodDelete,
		"/api/v1/bills/"+zeroUUID+"/payments/"+zeroUUID, nil, session, csrf)
	body := assertErrorResponse(t, rec, http.StatusNotFound, "NOT_FOUND")
	if body.Error.Message != "That could not be found." {
		t.Fatalf("message = %q, want %q -- a mismatch here means this 404 came from chi's own "+
			"route-not-found catch-all, not a real BillService refusal",
			body.Error.Message, "That could not be found.")
	}
}

// --- Part 2/3: Update's archived-account gap ----------------------------

// TestBillsUpdatePayFromArchivedAccountAnswersNamedMessageNot403 is Part 3's
// own required test: once Part 2 closes Update's archived-account gap
// (bill.go's own comment on why Create and Update must agree), its
// domain.ErrForbidden must reach the SAME named 422 ACCOUNT_ARCHIVED message
// Create already gives -- via writeBillWriteError, now shared by both
// callers -- not MapDomainError's generic, contextless 403.
//
// secondAccount is created in the SAME currency as the bill (SGD, via
// mustCreateAccountID both times): a currency mismatch would answer
// BILL_CURRENCY_IMMUTABLE first (Update checks currency before the archived
// check -- bill.go's own ordering) and prove nothing about this test.
func TestBillsUpdatePayFromArchivedAccountAnswersNamedMessageNot403(t *testing.T) {
	env := newTestEnv(t)
	session, csrf := env.signIn(t, env.ownerEmail, env.ownerPassword)
	firstAccount := env.mustCreateAccountID(t, session, csrf)
	secondAccount := env.mustCreateAccountID(t, session, csrf)

	created := env.mustCreateBill(t, session, csrf, map[string]any{
		"name": "Electricity", "amountMinor": 12_000, "cadence": "monthly",
		"nextDue": farFutureDate(), "payFromAccountId": firstAccount,
	})

	archiveRec := env.authed(t, http.MethodPost, "/api/v1/accounts/"+secondAccount+"/archive", nil, session, csrf)
	if archiveRec.Code != http.StatusOK {
		t.Fatalf("archive second account: status = %d, body = %s", archiveRec.Code, archiveRec.Body.String())
	}

	rec := env.authed(t, http.MethodPatch, "/api/v1/bills/"+created.Bill.ID,
		map[string]any{"payFromAccountId": secondAccount}, session, csrf)
	assertErrorResponse(t, rec, http.StatusUnprocessableEntity, "ACCOUNT_ARCHIVED")
}
