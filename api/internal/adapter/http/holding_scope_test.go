package httpadapter

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// Every holding handler is reached only through requireSession, so the scope it
// reads is always there -- today. This calls them with no scope at all, which
// is what a routing mistake would do, and pins that they refuse rather than
// carrying on with an empty household id.
//
// It matters most for the report. The other routes would look something up
// under "" and answer 404: wrong, but loud. The report would answer 200 with an
// empty body -- a screen telling someone who is not signed in that they own
// nothing. CLAUDE.md's rule is to fail closed on values you did not construct,
// and an absent scope is the clearest such value there is.
//
// An internal test rather than one in httpadapter_test, following
// transaction_cursor_test.go: the handlers are unexported and adding an export
// shim to production code so a test can reach them would be the tail wagging.
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
