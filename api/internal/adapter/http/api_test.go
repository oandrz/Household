package httpadapter_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/andreasoentoro/hearth/api/internal/adapter/clock"
	"github.com/andreasoentoro/hearth/api/internal/adapter/crypto"
	httpadapter "github.com/andreasoentoro/hearth/api/internal/adapter/http"
	"github.com/andreasoentoro/hearth/api/internal/adapter/postgres"
	"github.com/andreasoentoro/hearth/api/internal/domain"
	"github.com/andreasoentoro/hearth/api/internal/testsupport"
	"github.com/andreasoentoro/hearth/api/internal/usecase"
)

// movableClock is a controllable usecase.Clock for the one test that needs
// to fast-forward time without sleeping: proving a session's cookies slide
// when the session is extended near expiry.
type movableClock struct{ now time.Time }

func (c *movableClock) Now() time.Time          { return c.now }
func (c *movableClock) Advance(d time.Duration) { c.now = c.now.Add(d) }

// noopMailer satisfies usecase.Mailer without talking to a real relay: these
// tests exercise the HTTP layer's wiring and authorization, not delivery,
// and there is no Mailpit available in this test binary's environment.
type noopMailer struct{}

func (noopMailer) SendMagicLink(context.Context, string, string, string) error      { return nil }
func (noopMailer) SendInvite(context.Context, string, string, string, string) error { return nil }

// testEnv wires the full router against a disposable Postgres database, with
// one seeded household carrying an owner and a limited (non-owner) member,
// both with real credentials so tests can sign in as either through the
// public API exactly as a browser would.
type testEnv struct {
	router http.Handler

	householdID string

	ownerEmail        string
	ownerPassword     string
	limitedEmail      string
	limitedPassword   string
	limitedMembership string
}

func newTestEnv(t *testing.T) *testEnv {
	t.Helper()
	return newTestEnvWithClock(t, clock.System{})
}

// newTestEnvWithClock is newTestEnv's more general form, used by the one
// test that needs to fast-forward time to prove a session's cookies slide
// when it's extended -- everything else gets the real wall clock via
// newTestEnv.
func newTestEnvWithClock(t *testing.T, clk usecase.Clock) *testEnv {
	t.Helper()

	dbURL := testsupport.StartPostgres(t)
	db, err := postgres.Open(context.Background(), dbURL)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(db.Close)

	users := postgres.NewUserRepo(db)
	households := postgres.NewHouseholdRepo(db)
	memberships := postgres.NewMembershipRepo(db)
	sessions := postgres.NewSessionRepo(db)
	magicLinks := postgres.NewMagicLinkRepo(db)
	loginAttempts := postgres.NewLoginAttemptRepo(db)
	invites := postgres.NewInviteRepo(db)
	spaces := postgres.NewSpaceRepo(db)
	notifications := postgres.NewNotificationRepo(db)

	// Cheap argon2 cost parameters: these tests perform many real sign-ins
	// under -race, and production cost parameters (65536 KiB, 3 passes)
	// would make the suite crawl. This is still the real hasher, still
	// exercising Verify -- only the cost is turned down.
	hasher := crypto.NewArgon2Hasher(1, 8*1024, 1)
	tokens := crypto.NewTokenGenerator()
	mailer := noopMailer{}

	authSvc := usecase.NewAuthService(usecase.AuthDeps{
		Users:      users,
		Members:    memberships,
		Sessions:   sessions,
		Attempts:   loginAttempts,
		MagicLinks: magicLinks,
		Mailer:     mailer,
		Hasher:     hasher,
		Tokens:     tokens,
		Clock:      clk,
		SessionTTL: httpadapter.SessionTTL,
		BaseURL:    "http://localhost:5173",
	})
	inviteSvc := usecase.NewInviteService(usecase.InviteDeps{
		Invites:    invites,
		Users:      users,
		Sessions:   sessions,
		Mailer:     mailer,
		Hasher:     hasher,
		Tokens:     tokens,
		Clock:      clk,
		SessionTTL: httpadapter.SessionTTL,
		BaseURL:    "http://localhost:5173",
	})
	memberSvc := usecase.NewMemberService(usecase.MemberDeps{Members: memberships, Sessions: sessions})
	householdSvc := usecase.NewHouseholdService(usecase.HouseholdDeps{
		Households:    households,
		Spaces:        spaces,
		Notifications: notifications,
	})

	router := httpadapter.NewRouter(httpadapter.Deps{
		Pinger:      db,
		Auth:        authSvc,
		Invites:     inviteSvc,
		Members:     memberSvc,
		Households:  householdSvc,
		Users:       users,
		Memberships: memberships,
		Sessions:    sessions,
		Tokens:      tokens,
		Clock:       clk,
		Secure:      false,
	})

	env := &testEnv{router: router}

	ctx := context.Background()
	h, err := households.Create(ctx, "Andreas & Christine", "Oentoro")
	if err != nil {
		t.Fatalf("create household: %v", err)
	}
	env.householdID = h.ID

	for _, s := range domain.BuiltinSpaces(h.ID) {
		if _, err := spaces.Create(ctx, s); err != nil {
			t.Fatalf("seed builtin space %q: %v", s.Key, err)
		}
	}

	env.ownerEmail = "andreas@hearth.family"
	env.ownerPassword = "hunter2hunter2"
	ownerHash, err := hasher.Hash(env.ownerPassword)
	if err != nil {
		t.Fatalf("hash owner password: %v", err)
	}
	owner, err := users.Create(ctx, env.ownerEmail, ownerHash, "Andreas")
	if err != nil {
		t.Fatalf("create owner: %v", err)
	}
	if _, err := memberships.Create(ctx, domain.Membership{
		HouseholdID: h.ID, UserID: owner.ID, Role: domain.RoleOwner, Capabilities: domain.AllCapabilities(),
	}); err != nil {
		t.Fatalf("create owner membership: %v", err)
	}

	// The limited member is given real credentials (unlike the design's
	// credential-less child case) specifically so tests 10 and 11 can sign
	// in as them through the public API, the same way a browser would.
	env.limitedEmail = "ethan@hearth.family"
	env.limitedPassword = "ilovechores123"
	limitedHash, err := hasher.Hash(env.limitedPassword)
	if err != nil {
		t.Fatalf("hash limited password: %v", err)
	}
	limited, err := users.Create(ctx, env.limitedEmail, limitedHash, "Ethan")
	if err != nil {
		t.Fatalf("create limited user: %v", err)
	}
	limitedMembership, err := memberships.Create(ctx, domain.Membership{
		HouseholdID: h.ID, UserID: limited.ID, Role: domain.RoleLimited,
		Capabilities: domain.Capabilities{domain.CapCalendar, domain.CapChores},
	})
	if err != nil {
		t.Fatalf("create limited membership: %v", err)
	}
	env.limitedMembership = limitedMembership.ID

	return env
}

// --- small request/response helpers ---------------------------------------

func (env *testEnv) do(method, path string, body any, cookies ...*http.Cookie) *httptest.ResponseRecorder {
	var reader io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			panic(err)
		}
		reader = bytes.NewReader(b)
	}
	req := httptest.NewRequest(method, path, reader)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	for _, c := range cookies {
		req.AddCookie(c)
	}
	rec := httptest.NewRecorder()
	env.router.ServeHTTP(rec, req)
	return rec
}

// authed issues an authenticated, CSRF-validated request: the session
// cookie, the csrf cookie, and a matching X-CSRF-Token header.
func (env *testEnv) authed(t *testing.T, method, path string, body any, session, csrf *http.Cookie) *httptest.ResponseRecorder {
	t.Helper()
	var reader io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal body: %v", err)
		}
		reader = bytes.NewReader(b)
	}
	req := httptest.NewRequest(method, path, reader)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.AddCookie(session)
	req.AddCookie(csrf)
	req.Header.Set("X-CSRF-Token", csrf.Value)
	rec := httptest.NewRecorder()
	env.router.ServeHTTP(rec, req)
	return rec
}

// signIn signs in through the public API, exactly as a browser would, and
// returns the two cookies the response sets.
func (env *testEnv) signIn(t *testing.T, email, password string) (session, csrf *http.Cookie) {
	t.Helper()
	rec := env.do(http.MethodPost, "/api/v1/auth/sign-in", map[string]string{"email": email, "password": password})
	if rec.Code != http.StatusOK {
		t.Fatalf("sign-in: status = %d, body = %s", rec.Code, rec.Body.String())
	}
	for _, c := range rec.Result().Cookies() {
		switch c.Name {
		case "hearth_session":
			session = c
		case "csrf_token":
			csrf = c
		}
	}
	if session == nil || csrf == nil {
		t.Fatalf("sign-in did not set both cookies: %+v", rec.Result().Cookies())
	}
	return session, csrf
}

type errorEnvelope struct {
	Error struct {
		Code    string         `json:"code"`
		Message string         `json:"message"`
		Details map[string]any `json:"details"`
	} `json:"error"`
}

func decodeError(t *testing.T, rec *httptest.ResponseRecorder) errorEnvelope {
	t.Helper()
	var body errorEnvelope
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode error envelope: %v (body = %s)", err, rec.Body.String())
	}
	return body
}

func assertErrorResponse(t *testing.T, rec *httptest.ResponseRecorder, status int, code string) errorEnvelope {
	t.Helper()
	if rec.Code != status {
		t.Fatalf("status = %d, want %d (body = %s)", rec.Code, status, rec.Body.String())
	}
	body := decodeError(t, rec)
	if body.Error.Code != code {
		t.Fatalf("error code = %q, want %q (body = %s)", body.Error.Code, code, rec.Body.String())
	}
	return body
}

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
		if strings.HasPrefix(route, "/api/v1/invites/") {
			return nil // pre-auth by design
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
	if checked < 12 {
		t.Fatalf("checked %d protected routes, want at least 12 -- "+
			"the walk may not be enumerating routes correctly", checked)
	}
}

// TestLimitedMemberCannotUpdateMembers covers behaviour 10. It has the
// limited member try to promote their own membership to owner -- the
// realistic case the requireOwner guard exists to stop -- rather than
// editing someone else's, but either target must be rejected identically:
// requireOwner checks the caller's own role, not whose membership is named
// in the URL.
func TestLimitedMemberCannotUpdateMembers(t *testing.T) {
	env := newTestEnv(t)
	session, csrf := env.signIn(t, env.limitedEmail, env.limitedPassword)

	rec := env.authed(t, http.MethodPatch, "/api/v1/household/members/"+env.limitedMembership,
		map[string]any{"role": "owner", "capabilities": domain.AllCapabilities().Strings()}, session, csrf)
	assertErrorResponse(t, rec, http.StatusForbidden, "FORBIDDEN")
}

// TestLimitedMemberCannotCreateSpace covers behaviour 11.
func TestLimitedMemberCannotCreateSpace(t *testing.T) {
	env := newTestEnv(t)
	session, csrf := env.signIn(t, env.limitedEmail, env.limitedPassword)

	rec := env.authed(t, http.MethodPost, "/api/v1/spaces",
		map[string]any{"name": "Movie Night", "visibility": "everyone"}, session, csrf)
	assertErrorResponse(t, rec, http.StatusForbidden, "FORBIDDEN")
}

// TestOwnerOnlyRoutesRejectALimitedMember is the sibling of
// TestEveryProtectedRouteRejectsAnUnauthenticatedCaller: it walks the live
// router and, for every mutating route (method not GET/HEAD/OPTIONS), signs
// in as a *limited* member and attaches a genuinely valid session and CSRF
// token -- so the only thing that could still reject the request is
// requireOwner -- then asserts 403 FORBIDDEN unless the route is on the
// short, commented allowlist below.
//
// An earlier version of this test was a hand-maintained table of "routes I
// believe are owner-gated," justified by a claim that chi.Walk can't be used
// here because Go function values aren't comparable, so there was no
// reliable way to check whether a route's middleware chain included
// requireOwner specifically. That claim was correct but beside the point:
// this test was never supposed to introspect the middleware chain. It
// observes behaviour, exactly as the unauthenticated matrix does for
// requireSession -- a route wired without requireOwner now simply succeeds
// when it should have been forbidden, and fails this test on that basis, no
// reflection required. A route added to the owner-gated set without
// actually being wired behind requireOwner in router.go fails this test
// rather than shipping unnoticed.
//
// TestLimitedMemberCannotUpdateMembers and TestLimitedMemberCannotCreateSpace
// above still individually pin the task's original eleven enumerated
// behaviours verbatim; this walk is the exhaustive superset the
// coordinator's route audit asked for.
func TestOwnerOnlyRoutesRejectALimitedMember(t *testing.T) {
	env := newTestEnv(t)
	session, csrf := env.signIn(t, env.limitedEmail, env.limitedPassword)

	// Every entry is a mutating route that is correctly NOT owner-gated,
	// with the reason it's exempt recorded alongside it.
	allowlist := map[string]bool{
		// Public, pre-auth: reached before any session exists at all, so
		// there is no caller identity yet to check ownership against.
		"POST /api/v1/auth/sign-in":            true,
		"POST /api/v1/auth/magic-link":         true,
		"POST /api/v1/auth/magic-link/consume": true,
		// Any signed-in member, owner or not, may end their own session --
		// ownership has nothing to do with signing yourself out.
		"POST /api/v1/auth/sign-out": true,
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
		if strings.HasPrefix(route, "/api/v1/invites/") {
			// Public, pre-auth, and mutating (POST .../accept) -- exempt for
			// the identical reason the /auth/* entries above are, just
			// expressed as a prefix (like the unauthenticated matrix does)
			// rather than a second, redundant allowlist entry.
			return nil
		}
		if allowlist[method+" "+route] {
			return nil
		}

		path := replacer.Replace(route)
		req := httptest.NewRequest(method, path, bytes.NewReader([]byte("{}")))
		req.Header.Set("Content-Type", "application/json")
		req.AddCookie(session)
		req.AddCookie(csrf)
		req.Header.Set("X-CSRF-Token", csrf.Value)
		rec := httptest.NewRecorder()
		env.router.ServeHTTP(rec, req)

		if rec.Code != http.StatusForbidden {
			t.Errorf("%s %s: status = %d, want 403 (body = %s)", method, route, rec.Code, rec.Body.String())
		} else {
			body := decodeError(t, rec)
			if body.Error.Code != "FORBIDDEN" {
				t.Errorf("%s %s: error code = %q, want FORBIDDEN", method, route, body.Error.Code)
			}
		}
		checked++
		return nil
	})
	if err != nil {
		t.Fatalf("chi.Walk: %v", err)
	}
	t.Logf("checked %d owner-gated candidate routes", checked)
	if checked < 6 {
		t.Fatalf("checked %d routes, want at least 6 -- "+
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
	session, _ := env.signIn(t, env.ownerEmail, env.ownerPassword)

	public := map[string]bool{
		"POST /api/v1/auth/sign-in":            true,
		"POST /api/v1/auth/magic-link":         true,
		"POST /api/v1/auth/magic-link/consume": true,
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
		if strings.HasPrefix(route, "/api/v1/invites/") {
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
	if checked < 7 {
		t.Fatalf("checked %d mutating routes, want at least 7 -- "+
			"the walk may not be enumerating routes correctly", checked)
	}
}

// memberListEntry mirrors member_handlers.go's memberViewDTO for decoding
// GET /household/members responses in the tests below.
type memberListEntry struct {
	ID   string `json:"id"`
	User struct {
		ID            string `json:"id"`
		Email         string `json:"email"`
		DisplayName   string `json:"displayName"`
		AvatarInitial string `json:"avatarInitial"`
	} `json:"user"`
	Role         string   `json:"role"`
	Capabilities []string `json:"capabilities"`
}

func (env *testEnv) getMembers(t *testing.T, session *http.Cookie) []memberListEntry {
	t.Helper()
	rec := env.do(http.MethodGet, "/api/v1/household/members", nil, session)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /household/members: status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var members []memberListEntry
	if err := json.NewDecoder(rec.Body).Decode(&members); err != nil {
		t.Fatalf("decode member list: %v (body = %s)", err, rec.Body.String())
	}
	return members
}

// TestMemberListRevealsEmailsToAnOwner covers the coordinator's ruling on
// GET /household/members: an owner caller sees the full roster with every
// member's real email address populated.
func TestMemberListRevealsEmailsToAnOwner(t *testing.T) {
	env := newTestEnv(t)
	session, _ := env.signIn(t, env.ownerEmail, env.ownerPassword)

	members := env.getMembers(t, session)
	if len(members) != 2 {
		t.Fatalf("members = %d, want 2 (owner + limited)", len(members))
	}
	for _, m := range members {
		if m.User.Email == "" {
			t.Fatalf("member %q (%s) has an empty email, want it populated for an owner caller",
				m.User.DisplayName, m.Role)
		}
	}
}

// TestMemberListWithholdsEmailsFromALimitedMember is
// TestMemberListRevealsEmailsToAnOwner's sibling: a limited caller sees the
// identical roster -- same member count, names, roles and capabilities --
// with every email emptied rather than the list filtered down to fewer
// rows. The member-count assertion is what would catch a future change that
// filtered rows instead of redacting the one field that needs it: a row
// filter would still make this test's email assertions pass while quietly
// hiding other members entirely.
func TestMemberListWithholdsEmailsFromALimitedMember(t *testing.T) {
	env := newTestEnv(t)

	ownerSession, _ := env.signIn(t, env.ownerEmail, env.ownerPassword)
	asOwner := env.getMembers(t, ownerSession)

	limitedSession, _ := env.signIn(t, env.limitedEmail, env.limitedPassword)
	asLimited := env.getMembers(t, limitedSession)

	if len(asLimited) != len(asOwner) {
		t.Fatalf("members seen by limited caller = %d, want %d (same as an owner) -- "+
			"emails must be redacted per-field, not filtered by row", len(asLimited), len(asOwner))
	}

	byID := make(map[string]memberListEntry, len(asOwner))
	for _, m := range asOwner {
		byID[m.ID] = m
	}

	for _, m := range asLimited {
		if m.User.Email != "" {
			t.Fatalf("member %q (%s) has email %q, want it withheld from a limited caller",
				m.User.DisplayName, m.Role, m.User.Email)
		}

		owner, ok := byID[m.ID]
		if !ok {
			t.Fatalf("member %+v (id %q) is not among the members an owner sees -- "+
				"the roster itself must be identical, only the email field should differ", m, m.ID)
		}
		if m.User.DisplayName != owner.User.DisplayName ||
			m.User.AvatarInitial != owner.User.AvatarInitial ||
			m.Role != owner.Role ||
			!slicesEqual(m.Capabilities, owner.Capabilities) {
			t.Fatalf("member %q differs beyond email: limited view = %+v, owner view = %+v", m.ID, m, owner)
		}
	}
}

func slicesEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
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

type householdResponse struct {
	ID                    string `json:"id"`
	Name                  string `json:"name"`
	FamilyName            string `json:"familyName"`
	PrimaryCurrency       string `json:"primaryCurrency"`
	ShowSecondaryCurrency bool   `json:"showSecondaryCurrency"`
	SecondaryCurrency     string `json:"secondaryCurrency"`
	FXRateMode            string `json:"fxRateMode"`
}

func (env *testEnv) getHousehold(t *testing.T, session *http.Cookie) householdResponse {
	t.Helper()
	rec := env.do(http.MethodGet, "/api/v1/household", nil, session)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /household: status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var h householdResponse
	if err := json.NewDecoder(rec.Body).Decode(&h); err != nil {
		t.Fatalf("decode household: %v (body = %s)", err, rec.Body.String())
	}
	return h
}

// TestUpdateHouseholdIsARealPatch pins the fix for Finding 1: PATCH
// /household previously assigned every field unconditionally from plain
// value fields, so an omitted field and an explicit zero value were
// indistinguishable -- sending the API spec's own documented body (which
// omits secondaryCurrency) blanked it to "", and HouseholdService.Update's
// currency validation then failed with a 500. Pointer fields fix this: an
// absent field must leave the current value untouched, and a bad currency
// must report 422, never 500.
func TestUpdateHouseholdIsARealPatch(t *testing.T) {
	env := newTestEnv(t)
	session, csrf := env.signIn(t, env.ownerEmail, env.ownerPassword)

	t.Run("every field present", func(t *testing.T) {
		rec := env.authed(t, http.MethodPatch, "/api/v1/household", map[string]any{
			"name": "The Oentoros", "familyName": "Oentoro", "primaryCurrency": "SGD",
			"showSecondaryCurrency": false, "secondaryCurrency": "IDR", "fxRateMode": "manual",
		}, session, csrf)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
		}
		var got householdResponse
		if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
			t.Fatalf("decode: %v (body = %s)", err, rec.Body.String())
		}
		if got.Name != "The Oentoros" || got.FamilyName != "Oentoro" || got.PrimaryCurrency != "SGD" ||
			got.ShowSecondaryCurrency || got.SecondaryCurrency != "IDR" || got.FXRateMode != "manual" {
			t.Fatalf("household = %+v, want every field updated to what was sent", got)
		}
	})

	t.Run("a single field present leaves the rest unchanged", func(t *testing.T) {
		before := env.getHousehold(t, session)

		rec := env.authed(t, http.MethodPatch, "/api/v1/household",
			// The spec's own documented PATCH body: only familyName,
			// primaryCurrency, showSecondaryCurrency and fxRateMode --
			// secondaryCurrency (and name) are deliberately omitted here,
			// which is exactly the shape that used to 500.
			map[string]any{"familyName": "Oentoro-Wattimena"}, session, csrf)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
		}
		var got householdResponse
		if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
			t.Fatalf("decode: %v (body = %s)", err, rec.Body.String())
		}
		if got.FamilyName != "Oentoro-Wattimena" {
			t.Fatalf("familyName = %q, want the new value", got.FamilyName)
		}
		if got.Name != before.Name ||
			got.PrimaryCurrency != before.PrimaryCurrency ||
			got.ShowSecondaryCurrency != before.ShowSecondaryCurrency ||
			got.SecondaryCurrency != before.SecondaryCurrency ||
			got.FXRateMode != before.FXRateMode {
			t.Fatalf("PATCHing only familyName changed other fields: before = %+v, after = %+v", before, got)
		}
	})

	t.Run("an invalid currency reports 422, not 500", func(t *testing.T) {
		rec := env.authed(t, http.MethodPatch, "/api/v1/household",
			map[string]any{"primaryCurrency": "nope"}, session, csrf)
		assertErrorResponse(t, rec, http.StatusUnprocessableEntity, "INVALID_CURRENCY")
	})
}

type notificationPreferencesResponse struct {
	BillReminders   bool `json:"billReminders"`
	OverspendAlerts bool `json:"overspendAlerts"`
	RetroReminder   bool `json:"retroReminder"`
	WeeklyDigest    bool `json:"weeklyDigest"`
}

// TestUpdateNotificationPreferencesIsARealPatch is
// TestUpdateHouseholdIsARealPatch's sibling for
// PATCH /notification-preferences, which had the identical bug: a plain
// bool field cannot distinguish "the caller didn't mention this toggle"
// from "the caller wants it off," so an omitted field was silently set to
// false.
func TestUpdateNotificationPreferencesIsARealPatch(t *testing.T) {
	env := newTestEnv(t)
	session, csrf := env.signIn(t, env.ownerEmail, env.ownerPassword)

	// Establish a known starting point: every toggle on.
	rec := env.authed(t, http.MethodPatch, "/api/v1/notification-preferences", map[string]any{
		"billReminders": true, "overspendAlerts": true, "retroReminder": true, "weeklyDigest": true,
	}, session, csrf)
	if rec.Code != http.StatusOK {
		t.Fatalf("setup PATCH status = %d, body = %s", rec.Code, rec.Body.String())
	}

	t.Run("a single field present leaves the rest unchanged", func(t *testing.T) {
		rec := env.authed(t, http.MethodPatch, "/api/v1/notification-preferences",
			map[string]any{"weeklyDigest": false}, session, csrf)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
		}
		var got notificationPreferencesResponse
		if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
			t.Fatalf("decode: %v (body = %s)", err, rec.Body.String())
		}
		if got.WeeklyDigest {
			t.Fatal("weeklyDigest = true, want false after the PATCH")
		}
		if !got.BillReminders || !got.OverspendAlerts || !got.RetroReminder {
			t.Fatalf("PATCHing only weeklyDigest changed other toggles: %+v", got)
		}
	})

	t.Run("every field present", func(t *testing.T) {
		rec := env.authed(t, http.MethodPatch, "/api/v1/notification-preferences", map[string]any{
			"billReminders": false, "overspendAlerts": false, "retroReminder": false, "weeklyDigest": false,
		}, session, csrf)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
		}
		var got notificationPreferencesResponse
		if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
			t.Fatalf("decode: %v (body = %s)", err, rec.Body.String())
		}
		if got.BillReminders || got.OverspendAlerts || got.RetroReminder || got.WeeklyDigest {
			t.Fatalf("preferences = %+v, want every toggle false", got)
		}
	})
}

// TestCreateSpaceWithABlankNameReturns422 pins the fix for Finding 4's other
// sentinel: usecase.ErrSpaceNameRequired had no MapDomainError case at all
// and fell through to a bare 500 for what is an entirely ordinary bad
// request -- a blank space name.
func TestCreateSpaceWithABlankNameReturns422(t *testing.T) {
	env := newTestEnv(t)
	session, csrf := env.signIn(t, env.ownerEmail, env.ownerPassword)

	rec := env.authed(t, http.MethodPost, "/api/v1/spaces",
		map[string]any{"name": "   ", "visibility": "everyone"}, session, csrf)
	assertErrorResponse(t, rec, http.StatusUnprocessableEntity, "SPACE_NAME_REQUIRED")
}
