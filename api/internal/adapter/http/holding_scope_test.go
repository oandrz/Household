package httpadapter

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// Every holding handler runs only after requireSession, so scope is always
// populated in production. This test calls handlers directly with no scope --
// what a routing mistake would produce -- and pins that they refuse rather
// than continue with an empty household id.
//
// The report matters most: other routes look up "" and 404 (wrong but loud);
// the report would 200 with an empty body, telling someone not signed in that
// they own nothing. CLAUDE.md requires failing closed on values you didn't
// construct -- an absent scope is exactly that.
//
// Internal test, not httpadapter_test: the handlers are unexported, following
// transaction_cursor_test.go rather than adding an export shim just for this.
func TestAHoldingHandlerWithNoScopeRefuses(t *testing.T) {
	deps := Deps{}
	cases := []struct {
		name    string
		handler http.HandlerFunc
		method  string
		target  string
	}{
		{"report", handleHoldingReport(deps), http.MethodGet, "/api/v1/holdings/report?kind=quarter"},
		{"list holdings", handleListHoldings(deps), http.MethodGet, "/api/v1/holdings"},
		{"list income", handleListHoldingIncome(deps), http.MethodGet, "/api/v1/holdings/x/income"},
		{"list events", handleListHoldingEvents(deps), http.MethodGet, "/api/v1/holdings/x/events"},
		{"create holding", handleCreateHolding(deps), http.MethodPost, "/api/v1/holdings"},
		{"create income", handleCreateHoldingIncome(deps), http.MethodPost, "/api/v1/holdings/x/income"},
		{"delete income", handleDeleteHoldingIncome(deps), http.MethodDelete, "/api/v1/holdings/x/income/y"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			tc.handler(rec, httptest.NewRequest(tc.method, tc.target, nil))
			if rec.Code != http.StatusUnauthorized {
				t.Fatalf("code = %d, want 401 (body = %s)", rec.Code, rec.Body.String())
			}
		})
	}
}
