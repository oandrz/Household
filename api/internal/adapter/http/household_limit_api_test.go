package httpadapter_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	httpadapter "github.com/andreasoentoro/hearth/api/internal/adapter/http"
)

// limitedRouter is env's router with the production household limits and a
// frozen clock, so a test can spend the allowance and know it stays spent.
func limitedRouter(env *testEnv) http.Handler {
	frozen := time.Date(2026, 10, 17, 12, 0, 0, 0, time.UTC)
	d := env.deps
	d.HouseholdLimiter = httpadapter.NewHouseholdLimiter(httpadapter.ProductionHouseholdLimits,
		func() time.Time { return frozen })
	return httpadapter.NewRouter(d)
}

func serveAs(router http.Handler, method, path string, session, csrf *http.Cookie) *httptest.ResponseRecorder {
	body := "{}"
	if method == http.MethodGet || method == http.MethodHead {
		body = ""
	}
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if session != nil {
		req.AddCookie(session)
	}
	if csrf != nil {
		req.AddCookie(csrf)
		req.Header.Set("X-CSRF-Token", csrf.Value)
	}
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec
}

// spendAllowance uses up the household's whole bucket through a cheap read.
func spendAllowance(t *testing.T, router http.Handler, session *http.Cookie) {
	t.Helper()
	for i := range httpadapter.ProductionHouseholdLimits.Burst {
		if rec := serveAs(router, http.MethodGet, "/api/v1/auth/me", session, nil); rec.Code != http.StatusOK {
			t.Fatalf("request %d of the allowance = %d, want 200 (body %s)", i+1, rec.Code, rec.Body.String())
		}
	}
}

func TestAHouseholdOverItsAllowanceGets429WithRetryAfter(t *testing.T) {
	env := newTestEnv(t)
	router := limitedRouter(env)
	session, _ := env.signIn(t, env.ownerEmail, env.ownerPassword)
	spendAllowance(t, router, session)

	rec := serveAs(router, http.MethodGet, "/api/v1/auth/me", session, nil)
	body := assertErrorResponse(t, rec, http.StatusTooManyRequests, "RATE_LIMITED")
	if want := "This household is sending too many requests. Wait a few seconds and try again."; body.Error.Message != want {
		t.Errorf("message = %q, want %q", body.Error.Message, want)
	}
	if got := rec.Header().Get("Retry-After"); got != "1" {
		t.Errorf("Retry-After = %q, want \"1\"", got)
	}
}

func TestTheHouseholdLimitNeverAnswersAStranger(t *testing.T) {
	env := newTestEnv(t)
	router := limitedRouter(env)
	session, _ := env.signIn(t, env.ownerEmail, env.ownerPassword)
	spendAllowance(t, router, session)

	rec := serveAs(router, http.MethodGet, "/api/v1/household", nil, nil)
	assertErrorResponse(t, rec, http.StatusUnauthorized, "UNAUTHENTICATED")
}

func TestTheHouseholdLimitAnswersBeforeOwnerOnlyGuards(t *testing.T) {
	env := newTestEnv(t)
	router := limitedRouter(env)
	owner, _ := env.signIn(t, env.ownerEmail, env.ownerPassword)
	spendAllowance(t, router, owner)

	limited, limitedCSRF := env.signIn(t, env.limitedEmail, env.limitedPassword)
	rec := serveAs(router, http.MethodPatch, "/api/v1/household", limited, limitedCSRF)
	assertErrorResponse(t, rec, http.StatusTooManyRequests, "RATE_LIMITED")
}

// TestEverySignedInRouteIsHouseholdLimited is the sixth router walk: with
// the household's allowance spent, every route behind requireSession must
// answer 429. A route added outside the limited groups fails here.
func TestEverySignedInRouteIsHouseholdLimited(t *testing.T) {
	env := newTestEnv(t)
	router := limitedRouter(env)
	session, csrf := env.signIn(t, env.ownerEmail, env.ownerPassword)
	spendAllowance(t, router, session)

	replacer := strings.NewReplacer(
		"{id}", "00000000-0000-0000-0000-000000000000",
		"{token}", "some-invite-token",
	)
	checked := 0
	err := chi.Walk(router.(chi.Routes), func(method, route string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		if !strings.HasPrefix(route, "/api/v1") || invitePreAuthRoutes[method+" "+route] || publicRoutes[method+" "+route] {
			return nil
		}
		rec := serveAs(router, method, replacer.Replace(route), session, csrf)
		if rec.Code != http.StatusTooManyRequests {
			t.Errorf("%s %s: status = %d, want 429 (body = %s)", method, route, rec.Code, rec.Body.String())
		}
		checked++
		return nil
	})
	if err != nil {
		t.Fatalf("chi.Walk: %v", err)
	}
	t.Logf("checked %d signed-in routes", checked)
	// 102 is this walk's own measured output. Its only job is catching a
	// walk that silently stopped enumerating routes; when a route is added,
	// re-run the walk and set this to exactly what it reports.
	if checked < 102 {
		t.Fatalf("checked %d signed-in routes, want at least 102 -- "+
			"the walk may not be enumerating routes correctly", checked)
	}
}

func TestTheRouterGivesEveryRequestADeadline(t *testing.T) {
	env := newTestEnv(t)
	d := env.deps
	var ok bool
	d.Pinger = deadlineProbe{seen: &ok}
	httpadapter.NewRouter(d).ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/readyz", nil))
	if !ok {
		t.Fatal("GET /readyz ran without a request deadline on its context")
	}
}

type deadlineProbe struct{ seen *bool }

func (p deadlineProbe) Ping(ctx context.Context) error {
	_, *p.seen = ctx.Deadline()
	return nil
}
