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

	"github.com/go-chi/chi/v5"

	"github.com/andreasoentoro/hearth/api/internal/adapter/clock"
	"github.com/andreasoentoro/hearth/api/internal/adapter/crypto"
	httpadapter "github.com/andreasoentoro/hearth/api/internal/adapter/http"
	"github.com/andreasoentoro/hearth/api/internal/adapter/postgres"
	"github.com/andreasoentoro/hearth/api/internal/domain"
	"github.com/andreasoentoro/hearth/api/internal/testsupport"
	"github.com/andreasoentoro/hearth/api/internal/usecase"
)

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
	sysClock := clock.System{}
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
		Clock:      sysClock,
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
		Clock:      sysClock,
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
		Clock:       sysClock,
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
