package httpadapter_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	httpadapter "github.com/andreasoentoro/hearth/api/internal/adapter/http"
	"github.com/andreasoentoro/hearth/api/internal/domain"
	"github.com/andreasoentoro/hearth/api/internal/usecase"
)

// --- the eleven behaviours -------------------------------------------------

// TestSignInAndLockoutFlow covers behaviours 1-3: a correct sign-in sets
// both cookies with the right flags, a wrong password reports two tries
// left, and continuing to guess locks the household with a 423 and a
// lockedUntil timestamp. All three share one household's lockout state, so
// they run as one continuous flow rather than three independent tests.
func TestSignInAndLockoutFlow(t *testing.T) {
	env := newTestEnv(t)

	// 1: correct password -> 200, sets hearth_session (HttpOnly,
	// SameSite=Lax) and csrf_token (not HttpOnly).
	rec := env.do(http.MethodPost, "/api/v1/auth/sign-in",
		map[string]string{"email": env.ownerEmail, "password": env.ownerPassword})
	if rec.Code != http.StatusOK {
		t.Fatalf("sign-in status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var session, csrf *http.Cookie
	for _, c := range rec.Result().Cookies() {
		switch c.Name {
		case "hearth_session":
			session = c
		case "csrf_token":
			csrf = c
		}
	}
	if session == nil || !session.HttpOnly || session.SameSite != http.SameSiteLaxMode {
		t.Fatalf("session cookie = %+v, want HttpOnly + SameSite=Lax", session)
	}
	if csrf == nil || csrf.HttpOnly {
		t.Fatalf("csrf cookie = %+v, want present and not HttpOnly", csrf)
	}

	// 2: wrong password -> 401 INVALID_CREDENTIALS, attemptsRemaining 2.
	rec = env.do(http.MethodPost, "/api/v1/auth/sign-in",
		map[string]string{"email": env.ownerEmail, "password": "wrong-password"})
	body := assertErrorResponse(t, rec, http.StatusUnauthorized, "INVALID_CREDENTIALS")
	if got, ok := body.Error.Details["attemptsRemaining"].(float64); !ok || got != 2 {
		t.Fatalf("attemptsRemaining = %v, want 2", body.Error.Details["attemptsRemaining"])
	}

	// Two more wrong attempts: the third wrong attempt overall trips the
	// lock (domain.DefaultLockoutPolicy's MaxAttempts is 3).
	for i := 0; i < 2; i++ {
		rec = env.do(http.MethodPost, "/api/v1/auth/sign-in",
			map[string]string{"email": env.ownerEmail, "password": "wrong-password"})
	}

	// 3: the household is now locked -> 423 HOUSEHOLD_LOCKED with lockedUntil.
	body = assertErrorResponse(t, rec, http.StatusLocked, "HOUSEHOLD_LOCKED")
	if _, ok := body.Error.Details["lockedUntil"]; !ok {
		t.Fatalf("details missing lockedUntil: %+v", body.Error.Details)
	}
}

// TestAuthMeRequiresASession covers behaviour 4.
func TestAuthMeRequiresASession(t *testing.T) {
	env := newTestEnv(t)

	rec := env.do(http.MethodGet, "/api/v1/auth/me", nil)
	assertErrorResponse(t, rec, http.StatusUnauthorized, "UNAUTHENTICATED")
}

// TestAuthMeReturnsTheFullBundle covers behaviour 5.
func TestAuthMeReturnsTheFullBundle(t *testing.T) {
	env := newTestEnv(t)
	session, _ := env.signIn(t, env.ownerEmail, env.ownerPassword)

	rec := env.do(http.MethodGet, "/api/v1/auth/me", nil, session)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}

	var got struct {
		User struct {
			ID    string `json:"id"`
			Email string `json:"email"`
		} `json:"user"`
		Household struct {
			ID string `json:"id"`
		} `json:"household"`
		Membership struct {
			Role         string   `json:"role"`
			Capabilities []string `json:"capabilities"`
		} `json:"membership"`
		Capabilities []string `json:"capabilities"`
		Spaces       []struct {
			Key string `json:"key"`
		} `json:"spaces"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v (body = %s)", err, rec.Body.String())
	}

	if got.User.Email != env.ownerEmail {
		t.Fatalf("user.email = %q, want %q", got.User.Email, env.ownerEmail)
	}
	if got.Household.ID != env.householdID {
		t.Fatalf("household.id = %q, want %q", got.Household.ID, env.householdID)
	}
	if got.Membership.Role != "owner" {
		t.Fatalf("membership.role = %q, want owner", got.Membership.Role)
	}
	if len(got.Capabilities) != len(domain.AllCapabilities()) {
		t.Fatalf("capabilities = %v, want all %d", got.Capabilities, len(domain.AllCapabilities()))
	}
	if len(got.Spaces) != 3 {
		t.Fatalf("spaces = %v, want the 3 seeded builtins", got.Spaces)
	}
}

// TestCSRFIsRequiredForMutatingRequests covers behaviours 6-7, using
// sign-out as the mutating route under test.
func TestCSRFIsRequiredForMutatingRequests(t *testing.T) {
	env := newTestEnv(t)
	session, csrf := env.signIn(t, env.ownerEmail, env.ownerPassword)

	// 6: no X-CSRF-Token header at all.
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/sign-out", nil)
	req.AddCookie(session)
	req.AddCookie(csrf)
	rec := httptest.NewRecorder()
	env.router.ServeHTTP(rec, req)
	assertErrorResponse(t, rec, http.StatusForbidden, "CSRF_INVALID")

	// 7: header present but does not match the cookie.
	req2 := httptest.NewRequest(http.MethodPost, "/api/v1/auth/sign-out", nil)
	req2.AddCookie(session)
	req2.AddCookie(csrf)
	req2.Header.Set("X-CSRF-Token", "definitely-the-wrong-value")
	rec2 := httptest.NewRecorder()
	env.router.ServeHTTP(rec2, req2)
	assertErrorResponse(t, rec2, http.StatusForbidden, "CSRF_INVALID")
}

// TestSignOutRevokesTheSession covers behaviour 8.
func TestSignOutRevokesTheSession(t *testing.T) {
	env := newTestEnv(t)
	session, csrf := env.signIn(t, env.ownerEmail, env.ownerPassword)

	rec := env.authed(t, http.MethodPost, "/api/v1/auth/sign-out", nil, session, csrf)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("sign-out status = %d, body = %s", rec.Code, rec.Body.String())
	}

	rec = env.do(http.MethodGet, "/api/v1/auth/me", nil, session)
	assertErrorResponse(t, rec, http.StatusUnauthorized, "UNAUTHENTICATED")
}

// TestEveryProtectedRouteRejectsAnUnauthenticatedCaller covers behaviour 9.
// It walks the real, fully wired router (rather than a hand-maintained list
// of routes) so that a route registered without its session guard -- the
// exact failure this task exists to prevent -- cannot hide from the test by
// simply not being mentioned in it.
func TestEveryProtectedRouteRejectsAnUnauthenticatedCaller(t *testing.T) {
	env := newTestEnv(t)

	public := map[string]bool{
		"POST /api/v1/auth/sign-in":            true,
		"POST /api/v1/auth/magic-link":         true,
		"POST /api/v1/auth/magic-link/consume": true,
		// Sign-up: reached before any session exists, exactly like the
		// three routes above -- see middleware_ratelimit.go for the one
		// thing standing between this and an open relay.
		"POST /api/v1/auth/sign-up":                  true,
		"GET /api/v1/auth/sign-up/{token}":           true,
		"POST /api/v1/auth/sign-up/{token}/complete": true,
		// Public: the sign-up form reads this before any session exists.
		"GET /api/v1/currencies": true,
		// Telegram sign-in: reached before any session exists, same as
		// sign-up above. It answers 404 rather than 401 when no bot is
		// configured (telegram_handlers.go's own doc comment) -- the same
		// answer any unrouted path gets -- which this walk would otherwise
		// flag as a route that "forgot" its 401 guard.
		"POST /api/v1/auth/telegram/start": true,
	}

	routes, ok := env.router.(chi.Routes)
	if !ok {
		t.Fatal("router does not implement chi.Routes")
	}

	replacer := strings.NewReplacer(
		"{id}", "00000000-0000-0000-0000-000000000000",
		"{token}", "some-invite-token",
	)

	checked := 0
	err := chi.Walk(routes, func(method, route string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		if !strings.HasPrefix(route, "/api/v1") {
			return nil // /healthz, /readyz
		}
		if invitePreAuthRoutes[method+" "+route] {
			return nil // pre-auth by design -- see invitePreAuthRoutes' doc comment
		}
		if public[method+" "+route] {
			return nil
		}

		path := replacer.Replace(route)
		req := httptest.NewRequest(method, path, nil)
		rec := httptest.NewRecorder()
		env.router.ServeHTTP(rec, req)

		if rec.Code != http.StatusUnauthorized {
			t.Errorf("%s %s: status = %d, want 401 (body = %s)", method, route, rec.Code, rec.Body.String())
		} else {
			body := decodeError(t, rec)
			if body.Error.Code != "UNAUTHENTICATED" {
				t.Errorf("%s %s: error code = %q, want UNAUTHENTICATED", method, route, body.Error.Code)
			}
		}
		checked++
		return nil
	})
	if err != nil {
		t.Fatalf("chi.Walk: %v", err)
	}
	// A guard against a vacuous pass: if the walk somehow enumerated nothing
	// (a routing regression, a chi API change), the loop above asserts
	// nothing and the test would pass for the wrong reason.
	t.Logf("checked %d protected routes", checked)
	if checked < 17 {
		t.Fatalf("checked %d protected routes, want at least 17 -- "+
			"the walk may not be enumerating routes correctly", checked)
	}
}

// TestEveryMutatingRouteRequiresCSRF is the CSRF guard's sibling of the same
// two matrices: it walks the live router and, for every mutating route,
// sends a request carrying a *valid* session cookie but no X-CSRF-Token
// header (and no csrf_token cookie), and asserts 403 CSRF_INVALID.
// TestCSRFIsRequiredForMutatingRequests above already pins the missing-
// header and mismatched-header cases concretely on one route (sign-out);
// this walk is the exhaustive check that no other mutating route was wired
// outside the requireCSRF group.
//
// The three public /auth/* routes and everything under /invites/ are
// skipped -- not via some second allowlist, but because they are
// structurally pre-CSRF: router.go never wraps them in requireCSRF at all
// (there is no session yet to fixate before one exists), so calling them
// without a header succeeds or fails on their own terms, never with
// CSRF_INVALID.
func TestEveryMutatingRouteRequiresCSRF(t *testing.T) {
	env := newTestEnv(t)
	// The owner is made a platform admin so this walk can reach the CSRF
	// check on the /admin subtree at all. router.go stacks requireCSRF
	// INNERMOST there, behind requirePlatformAdmin, so that a forged admin
	// request still writes an audit row -- which means a caller with no
	// platform_admins row meets the 404 first and this matrix would assert
	// nothing about CSRF on those routes.
	//
	// Do not delete this line to "simplify the fixture": it is what keeps
	// POST /api/v1/admin/session inside the walk instead of allowlisted out
	// of it. Granting platform admin is safe for every OTHER assertion here
	// because it changes the behaviour of exactly one middleware --
	// requirePlatformAdmin is IsPlatformAdmin's only caller, and nothing
	// else in non-test code reads platform_admins.
	env.makePlatformAdmin(t, env.ownerEmail)
	session, _ := env.signIn(t, env.ownerEmail, env.ownerPassword)

	public := map[string]bool{
		"POST /api/v1/auth/sign-in":                  true,
		"POST /api/v1/auth/magic-link":               true,
		"POST /api/v1/auth/magic-link/consume":       true,
		"POST /api/v1/auth/sign-up":                  true,
		"POST /api/v1/auth/sign-up/{token}/complete": true,
		// Structurally pre-CSRF, same as the sign-up routes above -- there
		// is no session yet to fixate before one exists.
		"POST /api/v1/auth/telegram/start": true,
	}

	routes, ok := env.router.(chi.Routes)
	if !ok {
		t.Fatal("router does not implement chi.Routes")
	}

	replacer := strings.NewReplacer("{id}", "00000000-0000-0000-0000-000000000000")

	checked := 0
	err := chi.Walk(routes, func(method, route string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		switch method {
		case http.MethodGet, http.MethodHead, http.MethodOptions:
			return nil
		}
		if invitePreAuthRoutes[method+" "+route] {
			return nil
		}
		if public[method+" "+route] {
			return nil
		}

		path := replacer.Replace(route)
		req := httptest.NewRequest(method, path, bytes.NewReader([]byte("{}")))
		req.Header.Set("Content-Type", "application/json")
		req.AddCookie(session)
		// Deliberately no csrf_token cookie and no X-CSRF-Token header.
		rec := httptest.NewRecorder()
		env.router.ServeHTTP(rec, req)

		if rec.Code != http.StatusForbidden {
			t.Errorf("%s %s: status = %d, want 403 (body = %s)", method, route, rec.Code, rec.Body.String())
		} else {
			body := decodeError(t, rec)
			if body.Error.Code != "CSRF_INVALID" {
				t.Errorf("%s %s: error code = %q, want CSRF_INVALID", method, route, body.Error.Code)
			}
		}
		checked++
		return nil
	})
	if err != nil {
		t.Fatalf("chi.Walk: %v", err)
	}
	t.Logf("checked %d mutating routes", checked)
	// 11, not the pre-accounts 7: the same four accounts write routes are
	// mutating and CSRF-gated too (see the identical reasoning on the 10
	// floor in TestOwnerOnlyRoutesRejectALimitedMember, household_api_test.go).
	if checked < 11 {
		t.Fatalf("checked %d mutating routes, want at least 11 -- "+
			"the walk may not be enumerating routes correctly", checked)
	}
}

// --- fix round 3 -----------------------------------------------------------

// TestSessionCookiesSlideWhenExtended pins the fix for the missing cookie
// refresh: requireSession already extended the database row when a session
// drifted inside its extension window, but never re-issued the cookies
// carrying that new expiry, so the browser discarded hearth_session (and
// csrf_token, set with the identical fixed lifetime) on the original
// sign-in-plus-30-days schedule no matter how actively the session was
// used. A request made inside the window must now come back with a
// refreshed Set-Cookie for both, with the same token values and a later
// expiry.
func TestSessionCookiesSlideWhenExtended(t *testing.T) {
	clk := &movableClock{now: time.Now().UTC()}
	env := newTestEnvWithClock(t, clk)
	session, csrf := env.signIn(t, env.ownerEmail, env.ownerPassword)

	// Move to inside the extension window: under a day left of the 30-day
	// session.
	clk.Advance(httpadapter.SessionTTL - time.Hour)

	rec := env.do(http.MethodGet, "/api/v1/auth/me", nil, session, csrf)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}

	var newSession, newCSRF *http.Cookie
	for _, c := range rec.Result().Cookies() {
		switch c.Name {
		case "hearth_session":
			newSession = c
		case "csrf_token":
			newCSRF = c
		}
	}
	if newSession == nil {
		t.Fatal("expected a refreshed hearth_session Set-Cookie")
	}
	if newCSRF == nil {
		t.Fatal("expected a refreshed csrf_token Set-Cookie")
	}
	if newSession.Value != session.Value {
		t.Fatalf("session token value changed: got %q, want unchanged %q", newSession.Value, session.Value)
	}
	if newCSRF.Value != csrf.Value {
		t.Fatalf("csrf token value changed: got %q, want unchanged %q", newCSRF.Value, csrf.Value)
	}
	if !newSession.Expires.After(session.Expires) {
		t.Fatalf("session cookie did not slide: new expiry %v, old expiry %v", newSession.Expires, session.Expires)
	}
	if !newCSRF.Expires.After(csrf.Expires) {
		t.Fatalf("csrf cookie did not slide: new expiry %v, old expiry %v", newCSRF.Expires, csrf.Expires)
	}
}

// TestSignInRejectsAnOversizedBody pins the fix for the unbounded body read:
// a body over the shared size limit must be rejected with 413 before it is
// ever handed to json.Decode, let alone SignIn. Deliberately tested on a
// public route -- sign-in is reachable pre-auth and pre-CSRF, which is
// exactly where an unbounded read is most dangerous: nothing has gated the
// request yet.
func TestSignInRejectsAnOversizedBody(t *testing.T) {
	env := newTestEnv(t)

	oversizedPassword := strings.Repeat("a", 2*1024*1024) // 2 MiB, far past the 1 KiB limit
	body := `{"email":"a@b.c","password":"` + oversizedPassword + `"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/sign-in", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	env.router.ServeHTTP(rec, req)

	assertErrorResponse(t, rec, http.StatusRequestEntityTooLarge, "PAYLOAD_TOO_LARGE")
}

// --- Task 28: self-serve sign-up --------------------------------------------

// The HTTP half of the indistinguishability property. The service-level test
// (usecase/signup_test.go) pins the read sequence; this pins what a caller can
// actually see.
func TestSignUpAnswersIdenticallyForEveryAddress(t *testing.T) {
	env := newTestEnv(t)

	// The seeded household's owner address exists; the other two do not.
	for _, email := range []string{usecase.AndreasEmail, "stranger@example.test", "another@example.test"} {
		rec := env.do(http.MethodPost, "/api/v1/auth/sign-up", map[string]string{"email": email})
		if rec.Code != http.StatusAccepted {
			t.Fatalf("POST sign-up for %q = %d, want 202", email, rec.Code)
		}
		var body map[string]string
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatalf("body for %q is not JSON: %v", email, err)
		}
		if body["status"] != "accepted" {
			t.Fatalf("body for %q = %v, want {\"status\":\"accepted\"}", email, body)
		}
	}
}

// Repeating the same request past the hourly limit must not change the
// answer. 5 attempts, not more: every request through env.do shares the same
// fixed RemoteAddr (see TestSignUpPassesThroughThePerIPLimiter's doc comment),
// so this loop is already spending this test's own per-IP budget
// (signUpRequestsPerIPPerHour) as well as the per-address one this test is
// actually about -- a 6th call here would hit the per-IP limiter instead and
// prove nothing about the per-address limit this test exists to check.
func TestSignUpStaysSilentPastTheRateLimit(t *testing.T) {
	env := newTestEnv(t)
	for i := 0; i < 5; i++ {
		rec := env.do(http.MethodPost, "/api/v1/auth/sign-up",
			map[string]string{"email": "persistent@example.test"})
		if rec.Code != http.StatusAccepted {
			t.Fatalf("attempt %d = %d, want 202 -- a 429 here is an oracle in its own right", i, rec.Code)
		}
	}
}

// TestSignUpPassesThroughThePerIPLimiter is unlike every other sign-up test in
// this file: it proves the router's *wiring*, not the limiter type in
// isolation (middleware_ratelimit_test.go already covers ipRateLimiter and
// rateLimitByIP directly). Every other sign-up test here sends too few
// requests to ever reach signUpRequestsPerIPPerHour, so none of them would
// notice if router.go's su.Use(rateLimitByIP(...)) line were ever deleted.
// This one sends enough to find out.
//
// httptest.NewRequest gives every request in this file the same fixed
// RemoteAddr, so repeated calls through env.do share one bucket exactly as a
// real repeat caller would.
func TestSignUpPassesThroughThePerIPLimiter(t *testing.T) {
	env := newTestEnv(t)

	// signUpRequestsPerIPPerHour (router.go) is unexported and this package is
	// httpadapter_test, so its value is repeated here as a literal -- keep the
	// two in lockstep if that constant ever changes. Requests beyond
	// signupPerHourLimit (3, per-address) for this one address are still
	// silently declined by SignupService.Request itself and still answer 202
	// -- only the per-IP limiter answers 429, and only past this count.
	const perIPLimit = 5
	for i := 0; i < perIPLimit; i++ {
		rec := env.do(http.MethodPost, "/api/v1/auth/sign-up",
			map[string]string{"email": "ip-limited@example.test"})
		if rec.Code != http.StatusAccepted {
			t.Fatalf("request %d = %d, want 202", i, rec.Code)
		}
	}

	rec := env.do(http.MethodPost, "/api/v1/auth/sign-up",
		map[string]string{"email": "ip-limited@example.test"})
	assertErrorResponse(t, rec, http.StatusTooManyRequests, "RATE_LIMITED")
}

// signUpMeBundle mirrors the shape of meResponseBody (auth_handlers.go) for
// decoding the sign-up completion response. meResponseBody itself is
// unexported outside package httpadapter -- this package is httpadapter_test
// -- which is why householdResponse (household_api_test.go) exists as the
// identical kind of local mirror for GET /household.
type signUpMeBundle struct {
	Household  householdResponse `json:"household"`
	Membership struct {
		Role string `json:"role"`
	} `json:"membership"`
	Spaces []struct {
		Key string `json:"key"`
	} `json:"spaces"`
}

func TestSignUpPreviewAndComplete(t *testing.T) {
	env := newTestEnv(t)

	rec := env.do(http.MethodPost, "/api/v1/auth/sign-up", map[string]string{"email": "founder@example.test"})
	if rec.Code != http.StatusAccepted {
		t.Fatalf("request = %d, want 202", rec.Code)
	}
	token := env.lastSignupToken(t)

	t.Run("preview returns the address", func(t *testing.T) {
		rec := env.do(http.MethodGet, "/api/v1/auth/sign-up/"+token, nil)
		if rec.Code != http.StatusOK {
			t.Fatalf("preview = %d, want 200", rec.Code)
		}
		var body map[string]string
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatalf("body: %v", err)
		}
		if body["email"] != "founder@example.test" {
			t.Fatalf("email = %q, want founder@example.test", body["email"])
		}
		// CONTROLLER RULING R3: the create-household screen needs channel
		// alongside email (usecase.SignupPreview's own doc comment). The
		// Telegram-channel shape -- empty email, channel "telegram" -- is
		// covered separately by TestSignUpPreviewShowsTelegramChannelWithNoEmail
		// (telegram_api_test.go), since building that row needs a fake
		// SignupRepository rather than this file's real-Postgres testEnv.
		if body["channel"] != "email" {
			t.Fatalf("channel = %q, want email", body["channel"])
		}
	})

	t.Run("an unknown token is 404", func(t *testing.T) {
		rec := env.do(http.MethodGet, "/api/v1/auth/sign-up/never-issued", nil)
		assertErrorResponse(t, rec, http.StatusNotFound, "NOT_FOUND")
	})

	t.Run("a blank household name is 422", func(t *testing.T) {
		rec := env.do(http.MethodPost, "/api/v1/auth/sign-up/"+token+"/complete", map[string]string{
			"householdName": "  ", "displayName": "Ade", "primaryCurrency": "SGD",
			"password": "a-long-enough-password",
		})
		assertErrorResponse(t, rec, http.StatusUnprocessableEntity, "HOUSEHOLD_NAME_REQUIRED")
	})

	t.Run("an unknown currency is 422", func(t *testing.T) {
		rec := env.do(http.MethodPost, "/api/v1/auth/sign-up/"+token+"/complete", map[string]string{
			"householdName": "Ade & Kris", "displayName": "Ade", "primaryCurrency": "ZZZ",
			"password": "a-long-enough-password",
		})
		assertErrorResponse(t, rec, http.StatusUnprocessableEntity, "INVALID_CURRENCY")
	})

	// JPY is a well-formed, active ISO 4217 code -- unlike ZZZ above -- but it
	// has zero minor units, and domain.Money.String() hard-codes two decimal
	// places. GET /api/v1/currencies never offers it (see
	// TestCurrenciesIsPublicAndOnlyOffersTwoMinorUnitCodes), but this proves
	// the same rule holds for a client that posts the code directly, bypassing
	// the form's own currency list.
	t.Run("a currency Money cannot render is 422", func(t *testing.T) {
		rec := env.do(http.MethodPost, "/api/v1/auth/sign-up/"+token+"/complete", map[string]string{
			"householdName": "Ade & Kris", "displayName": "Ade", "primaryCurrency": "JPY",
			"password": "a-long-enough-password",
		})
		assertErrorResponse(t, rec, http.StatusUnprocessableEntity, "INVALID_CURRENCY")
	})

	t.Run("a short password is 422", func(t *testing.T) {
		rec := env.do(http.MethodPost, "/api/v1/auth/sign-up/"+token+"/complete", map[string]string{
			"householdName": "Ade & Kris", "displayName": "Ade", "primaryCurrency": "SGD",
			"password": "short",
		})
		assertErrorResponse(t, rec, http.StatusUnprocessableEntity, "PASSWORD_TOO_SHORT")
	})

	// Every rejection above left the token usable, which is why this still works.
	t.Run("completing signs the new owner in", func(t *testing.T) {
		rec := env.do(http.MethodPost, "/api/v1/auth/sign-up/"+token+"/complete", map[string]string{
			"householdName": "Ade & Kris", "displayName": "Ade", "primaryCurrency": "SGD",
			"password": "a-long-enough-password",
		})
		if rec.Code != http.StatusOK {
			t.Fatalf("complete = %d, want 200: %s", rec.Code, rec.Body.String())
		}

		var body signUpMeBundle
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatalf("body is not a me bundle: %v", err)
		}
		if body.Household.Name != "Ade & Kris" {
			t.Fatalf("household name = %q", body.Household.Name)
		}
		if body.Household.PrimaryCurrency != "SGD" || body.Household.SecondaryCurrency != "SGD" ||
			body.Household.ShowSecondaryCurrency {
			t.Fatalf("currency fields = %+v, want SGD/SGD/false", body.Household)
		}
		if body.Membership.Role != "owner" {
			t.Fatalf("role = %q, want owner", body.Membership.Role)
		}
		if len(body.Spaces) != 3 {
			t.Fatalf("got %d spaces, want the three builtins", len(body.Spaces))
		}

		var session, csrf bool
		for _, c := range rec.Result().Cookies() {
			switch c.Name {
			case "hearth_session":
				session = true
			case "csrf_token":
				csrf = true
			}
		}
		if !session || !csrf {
			t.Fatalf("cookies: session=%v csrf=%v, want both", session, csrf)
		}
	})

	t.Run("the token cannot be used twice", func(t *testing.T) {
		rec := env.do(http.MethodPost, "/api/v1/auth/sign-up/"+token+"/complete", map[string]string{
			"householdName": "Second household", "displayName": "Ade", "primaryCurrency": "SGD",
			"password": "a-long-enough-password",
		})
		assertErrorResponse(t, rec, http.StatusConflict, "SIGNUP_ALREADY_USED")
	})

	t.Run("preview of a consumed token is 409, not 410", func(t *testing.T) {
		rec := env.do(http.MethodGet, "/api/v1/auth/sign-up/"+token, nil)
		assertErrorResponse(t, rec, http.StatusConflict, "SIGNUP_ALREADY_USED")
	})
}

func TestCurrenciesIsPublicAndOnlyOffersTwoMinorUnitCodes(t *testing.T) {
	env := newTestEnv(t)
	rec := env.do(http.MethodGet, "/api/v1/currencies", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("= %d, want 200 with no session", rec.Code)
	}
	var body struct {
		Currencies []struct {
			Code, Symbol, Name string
		} `json:"currencies"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("body: %v", err)
	}
	if len(body.Currencies) < 100 {
		t.Fatalf("got %d currencies, want the two-minor-unit majority of ISO 4217", len(body.Currencies))
	}
	byCode := map[string]bool{}
	for _, c := range body.Currencies {
		byCode[c.Code] = true
	}
	if !byCode["SGD"] || !byCode["USD"] || !byCode["BRL"] {
		t.Fatal("want SGD, USD and BRL offered")
	}
	// Money.String() hard-codes two decimal places, so a household that picked
	// one of these would have every amount rendered wrong.
	for _, code := range []string{"JPY", "KRW", "KWD", "BHD", "ISK"} {
		if byCode[code] {
			t.Fatalf("%s is offered, but Money.String() renders it wrong", code)
		}
	}
}

// Sign-up is pre-auth and pre-session, so it must not require CSRF -- there is
// no csrf_token cookie to double-submit yet.
func TestSignUpRoutesDoNotRequireCSRF(t *testing.T) {
	env := newTestEnv(t)
	rec := env.do(http.MethodPost, "/api/v1/auth/sign-up", map[string]string{"email": "nocsrf@example.test"})
	if rec.Code != http.StatusAccepted {
		t.Fatalf("= %d, want 202 with no CSRF token", rec.Code)
	}
}
