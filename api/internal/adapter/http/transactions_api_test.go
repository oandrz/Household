package httpadapter_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// TestTransactionWriteRoutesRequireCSRF drives the three mutating
// transactions routes with no CSRF token, and with one that doesn't match
// the cookie.
//
// TestCSRFIsRequiredForMutatingRequests (auth_api_test.go) proves requireCSRF
// works in general, not that this route group is behind it: deleting
// `w.Use(requireCSRF)` from the transactions group in router.go left the
// whole suite green, since every other test reaches these routes through
// env.authed, which always supplies the token.
//
// Two details are load-bearing: the session is an owner's, since requireCSRF
// runs *after* requireCapability(CapMoney) and requireOwner, so a lesser
// caller is refused before reaching it; and the assertion checks
// CSRF_INVALID, not bare 403, since requireOwner's refusal looks the same.
func TestTransactionWriteRoutesRequireCSRF(t *testing.T) {
	env := newTestEnv(t)
	session, csrf := env.signIn(t, env.ownerEmail, env.ownerPassword)

	zeroUUID := "00000000-0000-0000-0000-000000000000"
	routes := []struct{ method, path string }{
		{http.MethodPost, "/api/v1/transactions"},
		{http.MethodPatch, "/api/v1/transactions/" + zeroUUID},
		{http.MethodDelete, "/api/v1/transactions/" + zeroUUID},
	}

	for _, route := range routes {
		t.Run(route.method+" "+route.path, func(t *testing.T) {
			// No X-CSRF-Token header at all.
			req := httptest.NewRequest(route.method, route.path, nil)
			req.AddCookie(session)
			req.AddCookie(csrf)
			rec := httptest.NewRecorder()
			env.router.ServeHTTP(rec, req)
			assertErrorResponse(t, rec, http.StatusForbidden, "CSRF_INVALID")

			// Header present, but not the cookie's value.
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

// --- transactions and categories routes --------------------------------------

// requestRouteAs issues route as the caller identified by session/csrf: GET
// uses authedGet (no CSRF cookie or header -- GET is exempt), everything
// else uses authed. One helper keeps the caller shapes below hitting the
// guard chain the same way a browser would, rather than each improvising.
func requestRouteAs(t *testing.T, env *testEnv, method, path string, session, csrf *http.Cookie) *httptest.ResponseRecorder {
	t.Helper()
	if method == http.MethodGet {
		return env.authedGet(t, path, session)
	}
	return env.authed(t, method, path, nil, session, csrf)
}

// TestTransactionRoutesRequireMoneyAndOwner proves that every transactions
// and categories route requires money AND owner, reads included -- unlike
// accounts, whose read is open to any money holder.
//
// A limited member's accounts view shows names with no amounts. Applied to
// a ledger, that would leave every figure blank next to a "Spent this
// month" that must be absent, not zero -- a page that reads as broken. So
// for a limited member the money capability means "see which accounts
// exist" and nothing further; this test names that difference on purpose
// rather than letting it look like an inconsistency to "fix".
//
// The third caller shape, env.moneyLimitedEmail (a limited member who DOES
// hold money), is what separates this test from
// TestAccountsListRequiresTheMoneyCapability: env.limitedEmail alone fails
// at requireCapability first and would never exercise requireOwner even if
// it were deleted.
//
// Known gap, not fixable: this matrix can't independently prove
// requireCapability(domain.CapMoney) is present -- that needs an owner
// without money, and no caller here can build that state.
// domain.ValidateMembershipChange and the owners_hold_all_capabilities CHECK
// constraint both refuse it (confirmed empirically against a raw
// MembershipRepo.Create). So every caller shape is either an owner (nothing
// for requireCapability to refuse) or a non-owner (already refused by
// requireOwner); removing requireCapability left this test green, as
// predicted. The same "must not lean on an invariant enforced elsewhere"
// risk router.go names above txn.Use(requireOwner): unfalsifiable today
// because the invariant holds in two places, not because the guard does
// nothing.
func TestTransactionRoutesRequireMoneyAndOwner(t *testing.T) {
	env := newTestEnv(t)

	zeroUUID := "00000000-0000-0000-0000-000000000000"
	// wantOwner pins the exact status per route, not merely "not 401/403":
	// that looser check let this test pass while deps.Transactions and
	// deps.Categories were both nil in the harness, because a nil-wired
	// service panics into a 500 past both guards. The real values (200 for
	// an empty-ledger read, 400 for a rejected nil body, 404 for an
	// update/delete against a missing id) make that failure loud instead of
	// silent.
	routes := []struct {
		method, path string
		wantOwner    int
	}{
		{http.MethodGet, "/api/v1/transactions", http.StatusOK},
		{http.MethodPost, "/api/v1/transactions", http.StatusBadRequest},
		{http.MethodPatch, "/api/v1/transactions/" + zeroUUID, http.StatusBadRequest},
		{http.MethodDelete, "/api/v1/transactions/" + zeroUUID, http.StatusNotFound},
		{http.MethodGet, "/api/v1/categories", http.StatusOK},
	}

	for _, route := range routes {
		t.Run(route.method+" "+route.path, func(t *testing.T) {
			// No session at all.
			rec := env.do(route.method, route.path, nil)
			if rec.Code != http.StatusUnauthorized {
				t.Fatalf("no session = %d, want 401 (body = %s)", rec.Code, rec.Body.String())
			}

			// Signed in, but without the money capability.
			session, csrf := env.signIn(t, env.limitedEmail, env.limitedPassword)
			rec = requestRouteAs(t, env, route.method, route.path, session, csrf)
			if rec.Code != http.StatusForbidden {
				t.Fatalf("no money capability = %d, want 403 (body = %s)", rec.Code, rec.Body.String())
			}

			// A limited member who DOES hold money. Refused anyway -- this is
			// the case that separates transactions from accounts.
			session, csrf = env.signIn(t, env.moneyLimitedEmail, env.moneyLimitedPassword)
			rec = requestRouteAs(t, env, route.method, route.path, session, csrf)
			if rec.Code != http.StatusForbidden {
				t.Fatalf("limited member holding money = %d, want 403 (body = %s)", rec.Code, rec.Body.String())
			}

			// An owner reaches the handler: the exact status pins both that the
			// guards let them through and that the service is actually wired
			// (see wantOwner above).
			session, csrf = env.signIn(t, env.ownerEmail, env.ownerPassword)
			rec = requestRouteAs(t, env, route.method, route.path, session, csrf)
			if rec.Code != route.wantOwner {
				t.Fatalf("owner = %d, want %d (body = %s)", rec.Code, route.wantOwner, rec.Body.String())
			}
		})
	}
}

// TestListTransactionsDefaultsListAndSummaryToTheSameMonth drives the ledger
// with no month parameter -- the state the screen opens in.
//
// It pins handleListTransactions's contract that the list and the two
// figures above it describe the same month. Don't default only the
// summary's month and leave filter.Month zero ("every month" per
// TransactionFilter): the screen read "0 in August 2026" above ten July
// rows.
//
// The assertion checks the listed dates against summary.month, not the
// count alone: a count check stays green if both halves are wrong in the
// same direction.
func TestListTransactionsDefaultsListAndSummaryToTheSameMonth(t *testing.T) {
	env := newTestEnv(t)
	session, csrf := env.signIn(t, env.ownerEmail, env.ownerPassword)

	accountID := env.mustCreateAccountID(t, session, csrf)
	categoryID, _ := env.firstExpenseCategory(t, session)

	thisMonth, lastMonth := thisMonthAndLast()
	env.mustCreateExpense(t, session, csrf, thisMonth.Format(dayLayout), categoryID, accountID, 2_000)
	env.mustCreateExpense(t, session, csrf, lastMonth.Format(dayLayout), categoryID, accountID, 3_000)

	body := env.listTransactions(t, session, "/api/v1/transactions")

	want := thisMonth.Format("2006-01")
	if body.Summary.Month != want {
		t.Fatalf("summary.month = %q, want the current month %q", body.Summary.Month, want)
	}
	if body.Summary.Count != len(body.Transactions) {
		t.Fatalf("summary.count = %d but the list carries %d rows; the two halves of one screen disagree",
			body.Summary.Count, len(body.Transactions))
	}
	for _, txn := range body.Transactions {
		if !strings.HasPrefix(txn.OccurredOn, body.Summary.Month) {
			t.Errorf("listed a transaction on %s while the summary describes %s",
				txn.OccurredOn, body.Summary.Month)
		}
	}
}

// TestListTransactionsWidensToEveryMonthOnMonthAll pins the one deliberate
// way out of the default: month=all lists every month.
//
// The summary deliberately stays on the current month, and this test says
// so rather than leaving it to be discovered -- see parseTransactionFilter
// for why it stays single-month.
func TestListTransactionsWidensToEveryMonthOnMonthAll(t *testing.T) {
	env := newTestEnv(t)
	session, csrf := env.signIn(t, env.ownerEmail, env.ownerPassword)

	accountID := env.mustCreateAccountID(t, session, csrf)
	categoryID, _ := env.firstExpenseCategory(t, session)

	thisMonth, lastMonth := thisMonthAndLast()
	env.mustCreateExpense(t, session, csrf, thisMonth.Format(dayLayout), categoryID, accountID, 2_000)
	env.mustCreateExpense(t, session, csrf, lastMonth.Format(dayLayout), categoryID, accountID, 3_000)

	body := env.listTransactions(t, session, "/api/v1/transactions?month=all")

	if len(body.Transactions) != 2 {
		t.Fatalf("month=all listed %d rows, want both months' 2", len(body.Transactions))
	}
	if body.Summary.Month != thisMonth.Format("2006-01") {
		t.Errorf("summary.month = %q, want the current month %q -- month=all widens the list, not the figure",
			body.Summary.Month, thisMonth.Format("2006-01"))
	}
}

// TestListTransactionsRefusesAnUnreadableMonth keeps the widening on the one
// spelled word: an unparseable month is refused rather than silently
// widening the ledger, which is what treating any unrecognised value as
// "all" would do.
func TestListTransactionsRefusesAnUnreadableMonth(t *testing.T) {
	env := newTestEnv(t)
	session, _ := env.signIn(t, env.ownerEmail, env.ownerPassword)

	rec := env.authedGet(t, "/api/v1/transactions?month=every", session)
	assertErrorResponse(t, rec, http.StatusUnprocessableEntity, "INVALID_MONTH")
}

// dayLayout is the wire format POST /transactions takes for occurredOn.
const dayLayout = "2006-01-02"

// thisMonthAndLast returns one day inside the current month and one inside
// the previous one.
//
// The previous month comes from the day before the 1st of this month, not
// from subtracting a month via AddDate: AddDate normalises an overflowing
// day forward, so time.Now().AddDate(0, -1, 0) on the 31st can land back
// inside the current month, silently passing a test whose "row outside this
// month" isn't.
func thisMonthAndLast() (thisMonth, lastMonth time.Time) {
	now := time.Now().UTC()
	thisMonth = time.Date(now.Year(), now.Month(), 1, 12, 0, 0, 0, time.UTC)
	return thisMonth, thisMonth.AddDate(0, 0, -1)
}

// transactionsListBody is the half of the list response these tests read: the
// dates the ledger shows, and the month the figures above it claim to
// describe.
type transactionsListBody struct {
	Transactions []struct {
		OccurredOn string `json:"occurredOn"`
	} `json:"transactions"`
	Summary struct {
		Month string `json:"month"`
		Count int    `json:"count"`
	} `json:"summary"`
}

func (env *testEnv) listTransactions(t *testing.T, session *http.Cookie, path string) transactionsListBody {
	t.Helper()
	rec := env.authedGet(t, path, session)
	if rec.Code != http.StatusOK {
		t.Fatalf("list transactions: status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var body transactionsListBody
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode transactions: %v", err)
	}
	return body
}

// Idempotency-Key on POST /transactions: the spec's HTTP contract
// (docs/superpowers/specs/2026-09-08-hearth-idempotent-import-design.md).
func (env *testEnv) postTransactionWithKey(t *testing.T, session, csrf *http.Cookie, key string, body map[string]any) *httptest.ResponseRecorder {
	t.Helper()
	b, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/transactions", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(session)
	req.AddCookie(csrf)
	req.Header.Set("X-CSRF-Token", csrf.Value)
	if key != "" {
		req.Header.Set("Idempotency-Key", key)
	}
	rec := httptest.NewRecorder()
	env.router.ServeHTTP(rec, req)
	return rec
}

func TestCreateTransactionWithTheSameKeyTwiceWritesOneRow(t *testing.T) {
	env := newTestEnv(t)
	session, csrf := env.signIn(t, env.ownerEmail, env.ownerPassword)
	accountID := env.mustCreateAccountID(t, session, csrf)
	categoryID, _ := env.firstExpenseCategory(t, session)
	thisMonth, _ := thisMonthAndLast()
	body := map[string]any{
		"kind": "expense", "occurredOn": thisMonth.Format(dayLayout), "description": "Coffee",
		"categoryId": categoryID, "fromAccountId": accountID, "amountMinor": 650,
	}

	first := env.postTransactionWithKey(t, session, csrf, "row-1", body)
	if first.Code != http.StatusCreated {
		t.Fatalf("first: %d %s", first.Code, first.Body.String())
	}
	second := env.postTransactionWithKey(t, session, csrf, "row-1", body)
	if second.Code != http.StatusOK {
		t.Fatalf("replay must answer 200, got %d %s", second.Code, second.Body.String())
	}
	var a, b struct {
		ID string `json:"id"`
	}
	json.Unmarshal(first.Body.Bytes(), &a)
	json.Unmarshal(second.Body.Bytes(), &b)
	if a.ID == "" || a.ID != b.ID {
		t.Fatalf("replay returned id %q, want the original %q", b.ID, a.ID)
	}
	if got := len(env.listTransactions(t, session, "/api/v1/transactions").Transactions); got != 1 {
		t.Fatalf("ledger holds %d rows, want 1", got)
	}
}

func TestCreateTransactionRefusesAKeyReusedForADifferentBody(t *testing.T) {
	env := newTestEnv(t)
	session, csrf := env.signIn(t, env.ownerEmail, env.ownerPassword)
	accountID := env.mustCreateAccountID(t, session, csrf)
	categoryID, _ := env.firstExpenseCategory(t, session)
	thisMonth, _ := thisMonthAndLast()
	body := map[string]any{
		"kind": "expense", "occurredOn": thisMonth.Format(dayLayout), "description": "Coffee",
		"categoryId": categoryID, "fromAccountId": accountID, "amountMinor": 650,
	}
	if rec := env.postTransactionWithKey(t, session, csrf, "row-1", body); rec.Code != http.StatusCreated {
		t.Fatalf("first: %d %s", rec.Code, rec.Body.String())
	}
	body["amountMinor"] = 651
	rec := env.postTransactionWithKey(t, session, csrf, "row-1", body)
	if rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), "IDEMPOTENCY_KEY_REUSED") {
		t.Fatalf("want 409 IDEMPOTENCY_KEY_REUSED, got %d %s", rec.Code, rec.Body.String())
	}
	if got := len(env.listTransactions(t, session, "/api/v1/transactions").Transactions); got != 1 {
		t.Fatalf("ledger holds %d rows, want 1", got)
	}
}

func TestCreateTransactionWithoutAKeyStillWritesEveryTime(t *testing.T) {
	env := newTestEnv(t)
	session, csrf := env.signIn(t, env.ownerEmail, env.ownerPassword)
	accountID := env.mustCreateAccountID(t, session, csrf)
	categoryID, _ := env.firstExpenseCategory(t, session)
	thisMonth, _ := thisMonthAndLast()
	body := map[string]any{
		"kind": "expense", "occurredOn": thisMonth.Format(dayLayout), "description": "Coffee",
		"categoryId": categoryID, "fromAccountId": accountID, "amountMinor": 650,
	}
	for i := 0; i < 2; i++ {
		if rec := env.postTransactionWithKey(t, session, csrf, "", body); rec.Code != http.StatusCreated {
			t.Fatalf("call %d: %d %s", i, rec.Code, rec.Body.String())
		}
	}
	if got := len(env.listTransactions(t, session, "/api/v1/transactions").Transactions); got != 2 {
		t.Fatalf("ledger holds %d rows, want 2", got)
	}
}

func TestCreateTransactionRefusesAMalformedKey(t *testing.T) {
	env := newTestEnv(t)
	session, csrf := env.signIn(t, env.ownerEmail, env.ownerPassword)
	accountID := env.mustCreateAccountID(t, session, csrf)
	categoryID, _ := env.firstExpenseCategory(t, session)
	thisMonth, _ := thisMonthAndLast()
	body := map[string]any{
		"kind": "expense", "occurredOn": thisMonth.Format(dayLayout), "description": "Coffee",
		"categoryId": categoryID, "fromAccountId": accountID, "amountMinor": 650,
	}
	rec := env.postTransactionWithKey(t, session, csrf, "has space", body)
	if rec.Code != http.StatusUnprocessableEntity || !strings.Contains(rec.Body.String(), "IDEMPOTENCY_KEY_INVALID") {
		t.Fatalf("want 422 IDEMPOTENCY_KEY_INVALID, got %d %s", rec.Code, rec.Body.String())
	}

	// Present but empty is refused too, not read as "no key": a client that
	// meant to send a key and sent nothing must not get an unkeyed write.
	b, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/transactions", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(session)
	req.AddCookie(csrf)
	req.Header.Set("X-CSRF-Token", csrf.Value)
	req.Header.Set("Idempotency-Key", "")
	rec = httptest.NewRecorder()
	env.router.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("an empty Idempotency-Key header must be refused, got %d %s", rec.Code, rec.Body.String())
	}
	if got := len(env.listTransactions(t, session, "/api/v1/transactions").Transactions); got != 0 {
		t.Fatalf("an empty header must write nothing, ledger holds %d", got)
	}
}
