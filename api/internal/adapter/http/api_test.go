package httpadapter_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/andreasoentoro/hearth/api/internal/adapter/clock"
	"github.com/andreasoentoro/hearth/api/internal/adapter/crypto"
	"github.com/andreasoentoro/hearth/api/internal/adapter/fx"
	httpadapter "github.com/andreasoentoro/hearth/api/internal/adapter/http"
	"github.com/andreasoentoro/hearth/api/internal/adapter/postgres"
	"github.com/andreasoentoro/hearth/api/internal/domain"
	"github.com/andreasoentoro/hearth/api/internal/testsupport"
	"github.com/andreasoentoro/hearth/api/internal/usecase"
)

// invitePreAuthRoutes is the exact, complete set of routes reached before any
// session exists at all: GET .../invites/{token} (the preview) and POST
// .../invites/{token}/accept (mutating, but pre-auth by design -- there is no
// caller identity yet to check a session, a CSRF token or ownership against).
//
// The three route-walk matrices below name these two routes explicitly
// rather than skipping anything matching a "/api/v1/invites/" prefix, which
// is what each used to do. A prefix skip silently exempts *any* future route
// added under that prefix from whichever guard the matrix checks -- a
// mutating admin route added under /invites/ later would be auto-exempt from
// the CSRF and owner checks with no test ever noticing. Naming the two
// routes that actually exist means a third one added later is walked and
// checked like every other route, not quietly waved through.
var invitePreAuthRoutes = map[string]bool{
	"GET /api/v1/invites/{token}":         true,
	"POST /api/v1/invites/{token}/accept": true,
}

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
func (noopMailer) SendSignupLink(context.Context, string, string) error             { return nil }
func (noopMailer) SendSignupForExistingAccount(context.Context, string, string) error {
	return nil
}

// signupMailer is a usecase.Mailer stub used only for SignupService, so tests
// can recover the raw token a sign-up link carried -- exactly the same need
// noopMailer's silence can't satisfy.
//
// SignupService.Request sends off the request path (see sendAsync in
// usecase/signup.go), deliberately, so a slow relay cannot make one branch
// measurably slower than another. That means the URL is not yet captured the
// instant env.do returns; lastSignupToken (below) synchronizes on sent rather
// than reading lastURL immediately, and mu guards the field because it is
// written from that background goroutine and read from the test's.
//
// Only SendSignupLink signals sent. TestSignUpAnswersIdenticallyForEveryAddress
// exercises both the fresh-address and already-registered branches in the same
// test; if SendSignupForExistingAccount also signalled, a lastSignupToken call
// could wake on that send instead and read the wrong (or an empty) URL.
type signupMailer struct {
	mu      sync.Mutex
	lastURL string
	sent    chan struct{}
}

func newSignupMailer() *signupMailer {
	return &signupMailer{sent: make(chan struct{}, 64)}
}

func (m *signupMailer) SendMagicLink(context.Context, string, string, string) error      { return nil }
func (m *signupMailer) SendInvite(context.Context, string, string, string, string) error { return nil }

func (m *signupMailer) SendSignupLink(_ context.Context, _, url string) error {
	m.mu.Lock()
	m.lastURL = url
	m.mu.Unlock()
	m.sent <- struct{}{}
	return nil
}

func (m *signupMailer) SendSignupForExistingAccount(context.Context, string, string) error {
	return nil
}

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

	// moneyLimitedEmail is a limited member who holds the money capability --
	// the state Settings' "off for kids by default" switch produces when an
	// owner turns Money on for a child.
	//
	// It exists because env.limitedEmail holds only calendar and chores, so
	// every accounts write route would refuse them at requireCapability and
	// TestOwnerOnlyRoutesRejectALimitedMember would pass without ever
	// exercising requireOwner -- a green that proves nothing about the guard
	// it is named after.
	moneyLimitedEmail    string
	moneyLimitedPassword string

	signupMailer *signupMailer
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
	signups := postgres.NewSignupRepo(db)

	// Cheap argon2 cost parameters: these tests perform many real sign-ins
	// under -race, and production cost parameters (65536 KiB, 3 passes)
	// would make the suite crawl. This is still the real hasher, still
	// exercising Verify -- only the cost is turned down.
	hasher := crypto.NewArgon2Hasher(1, 8*1024, 1)
	tokens := crypto.NewTokenGenerator()
	mailer := noopMailer{}
	sigMailer := newSignupMailer()

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
	signupSvc := usecase.NewSignupService(usecase.SignupDeps{
		Signups:    signups,
		Users:      users,
		Sessions:   sessions,
		Mailer:     sigMailer,
		Hasher:     hasher,
		Tokens:     tokens,
		Clock:      clk,
		SessionTTL: httpadapter.SessionTTL,
		BaseURL:    "http://localhost:5173",
	})
	accountSvc := usecase.NewAccountService(usecase.AccountDeps{
		Accounts:   postgres.NewAccountRepo(db),
		Households: households,
		FX:         fx.NewStaticProvider(),
		Clock:      clk,
	})

	router := httpadapter.NewRouter(httpadapter.Deps{
		Pinger:      db,
		Auth:        authSvc,
		Invites:     inviteSvc,
		Members:     memberSvc,
		Households:  householdSvc,
		Signups:     signupSvc,
		Accounts:    accountSvc,
		Users:       users,
		Memberships: memberships,
		Sessions:    sessions,
		Tokens:      tokens,
		Clock:       clk,
		Secure:      false,
	})

	env := &testEnv{router: router, signupMailer: sigMailer}

	ctx := context.Background()
	h, err := households.Create(ctx, domain.Household{
		Name: "Andreas & Christine", FamilyName: "Oentoro",
		PrimaryCurrency: "SGD", ShowSecondaryCurrency: true, SecondaryCurrency: "IDR",
	})
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

	// moneyLimitedEmail: see its doc comment on testEnv for why a second
	// limited member, holding money on top of calendar and chores, has to
	// exist alongside env.limitedEmail.
	env.moneyLimitedEmail = "maya@hearth.family"
	env.moneyLimitedPassword = "ilovepocketmoney"
	moneyLimitedHash, err := hasher.Hash(env.moneyLimitedPassword)
	if err != nil {
		t.Fatalf("hash money-limited password: %v", err)
	}
	moneyLimited, err := users.Create(ctx, env.moneyLimitedEmail, moneyLimitedHash, "Maya")
	if err != nil {
		t.Fatalf("create money-limited user: %v", err)
	}
	if _, err := memberships.Create(ctx, domain.Membership{
		HouseholdID: h.ID, UserID: moneyLimited.ID, Role: domain.RoleLimited,
		Capabilities: domain.Capabilities{domain.CapCalendar, domain.CapChores, domain.CapMoney},
	}); err != nil {
		t.Fatalf("create money-limited membership: %v", err)
	}

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

// authedGet issues an authenticated GET request: the session cookie only.
// requireCSRF exempts GET entirely, so there is no csrf_token cookie or
// X-CSRF-Token header to attach.
func (env *testEnv) authedGet(t *testing.T, path string, session *http.Cookie) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	req.AddCookie(session)
	rec := httptest.NewRecorder()
	env.router.ServeHTTP(rec, req)
	return rec
}

// mustCreateAccount is test setup, not an assertion in itself: it POSTs
// /accounts as whichever caller is passed in and fails the test immediately
// if that didn't succeed, so a broken create surfaces at the setup line
// rather than as a confusing failure in whatever the real test goes on to
// check. 201, not 200: POST /accounts creates a row, the same as POST
// /spaces and POST /household/members/invite.
func (env *testEnv) mustCreateAccount(t *testing.T, session, csrf *http.Cookie, body map[string]any) {
	t.Helper()
	rec := env.authed(t, http.MethodPost, "/api/v1/accounts", body, session, csrf)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create account: status = %d, body = %s", rec.Code, rec.Body.String())
	}
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

// lastSignupToken waits for SignupService.Request's asynchronous mail send to
// land (see signupMailer's doc comment for why that wait is necessary at
// all), then recovers the raw token from the sign-up URL it captured.
func (env *testEnv) lastSignupToken(t *testing.T) string {
	t.Helper()
	select {
	case <-env.signupMailer.sent:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for the sign-up mail to send")
	}

	env.signupMailer.mu.Lock()
	url := env.signupMailer.lastURL
	env.signupMailer.mu.Unlock()

	const prefix = "http://localhost:5173/sign-up/"
	token, ok := strings.CutPrefix(url, prefix)
	if !ok {
		t.Fatalf("sign-up URL %q does not have the expected prefix %q", url, prefix)
	}
	return token
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
		// Sign-up: reached before any session exists, exactly like the
		// three routes above -- see middleware_ratelimit.go for the one
		// thing standing between this and an open relay.
		"POST /api/v1/auth/sign-up":                  true,
		"GET /api/v1/auth/sign-up/{token}":           true,
		"POST /api/v1/auth/sign-up/{token}/complete": true,
		// Public: the sign-up form reads this before any session exists.
		"GET /api/v1/currencies": true,
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
//
// Signed in as env.moneyLimitedEmail, not env.limitedEmail: the plain limited
// fixture holds only calendar and chores, so every accounts write route below
// would refuse it at requireCapability before the request ever reached
// requireOwner, and this walk would pass without ever exercising the guard
// it is named after. env.moneyLimitedEmail also holds money and is still
// limited, so every assertion this walk already made keeps holding.
func TestOwnerOnlyRoutesRejectALimitedMember(t *testing.T) {
	env := newTestEnv(t)
	session, csrf := env.signIn(t, env.moneyLimitedEmail, env.moneyLimitedPassword)

	// Every entry is a mutating route that is correctly NOT owner-gated,
	// with the reason it's exempt recorded alongside it.
	allowlist := map[string]bool{
		// Public, pre-auth: reached before any session exists at all, so
		// there is no caller identity yet to check ownership against.
		"POST /api/v1/auth/sign-in":                  true,
		"POST /api/v1/auth/magic-link":               true,
		"POST /api/v1/auth/magic-link/consume":       true,
		"POST /api/v1/auth/sign-up":                  true,
		"POST /api/v1/auth/sign-up/{token}/complete": true,
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
		if invitePreAuthRoutes[method+" "+route] {
			// Public, pre-auth, and mutating (POST .../accept) -- exempt for
			// the identical reason the /auth/* entries above are. See
			// invitePreAuthRoutes' doc comment for why this is a named
			// two-route allowlist rather than a "/api/v1/invites/" prefix
			// skip.
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
	// 10, not the pre-accounts 6: the four accounts write routes are mutating
	// and owner-gated too, and a floor left at the old count would still pass
	// if all four vanished from the walk -- exactly the vacuous pass this
	// guard exists to catch.
	if checked < 10 {
		t.Fatalf("checked %d routes, want at least 10 -- "+
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
		"POST /api/v1/auth/sign-in":                  true,
		"POST /api/v1/auth/magic-link":               true,
		"POST /api/v1/auth/magic-link/consume":       true,
		"POST /api/v1/auth/sign-up":                  true,
		"POST /api/v1/auth/sign-up/{token}/complete": true,
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
	// floor above).
	if checked < 11 {
		t.Fatalf("checked %d mutating routes, want at least 11 -- "+
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
	if len(members) != 3 {
		t.Fatalf("members = %d, want 3 (owner + the two limited members)", len(members))
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

	// The surviving sibling of the ErrInvalidMoney fix above: fxRateMode
	// reaches the database's CHECK (fx_rate_mode IN ('auto', 'manual'))
	// constraint completely unvalidated on this path, so a caller-supplied
	// value outside that pair used to reach the constraint first and 500.
	t.Run("an invalid fxRateMode reports 422, not 500", func(t *testing.T) {
		rec := env.authed(t, http.MethodPatch, "/api/v1/household",
			map[string]any{"fxRateMode": "weekly"}, session, csrf)
		assertErrorResponse(t, rec, http.StatusUnprocessableEntity, "INVALID_FX_RATE_MODE")
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

// TestInviteMemberRejectsAnAddressThatAlreadyHasAUsersRow pins the fix for
// the invite-to-an-existing-member 500: InviteRepo.Accept unconditionally
// calls CreateUser and never reuses an existing row, so an owner inviting an
// address that already belongs to a member (a mistype, or a re-invite) used
// to get 201 with the mail sent, and the recipient would then 500 forever at
// acceptance. This must be rejected at creation, where the owner who typed
// the address can see it and act on it.
func TestInviteMemberRejectsAnAddressThatAlreadyHasAUsersRow(t *testing.T) {
	env := newTestEnv(t)
	session, csrf := env.signIn(t, env.ownerEmail, env.ownerPassword)

	rec := env.authed(t, http.MethodPost, "/api/v1/household/members/invite", map[string]any{
		"name": "Ethan Again", "email": env.limitedEmail, "role": "limited",
		"capabilities": []string{"calendar"},
	}, session, csrf)
	assertErrorResponse(t, rec, http.StatusConflict, "EMAIL_ALREADY_REGISTERED")
}

// TestSignInForARemovedMemberReturns401NotA404 pins the fix for the other
// symptom sharing the invite-500's root cause: removing a member deletes
// only its memberships row, not the users row underneath it, so the address
// still resolves through Users.ByEmail. SignIn used to call Members.ByUser
// next, get domain.ErrNotFound back, and propagate it bare -- MapDomainError
// turns that into 404, a status no other sign-in failure produces and a
// stranger's guess never gets, which itself discloses that the address once
// belonged to someone. It must fail exactly like any other sign-in failure:
// 401 INVALID_CREDENTIALS.
func TestSignInForARemovedMemberReturns401NotA404(t *testing.T) {
	env := newTestEnv(t)
	ownerSession, ownerCSRF := env.signIn(t, env.ownerEmail, env.ownerPassword)

	rec := env.authed(t, http.MethodDelete, "/api/v1/household/members/"+env.limitedMembership,
		nil, ownerSession, ownerCSRF)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("remove member: status = %d, body = %s", rec.Code, rec.Body.String())
	}

	rec = env.do(http.MethodPost, "/api/v1/auth/sign-in", map[string]string{
		"email": env.limitedEmail, "password": env.limitedPassword,
	})
	assertErrorResponse(t, rec, http.StatusUnauthorized, "INVALID_CREDENTIALS")
}

// --- Task 20 fix round: PATCH /household/members/:id was the last PATCH
// still behaving like a PUT ----------------------------------------------

// TestUpdateMemberIsARealPatch is TestUpdateHouseholdIsARealPatch's and
// TestUpdateNotificationPreferencesIsARealPatch's sibling for
// PATCH /household/members/:id, which had the identical bug for longer:
// plain (non-pointer) Role/Capabilities fields meant a caller had to send
// both together, or the omitted one decoded to its zero value and 422'd as
// an unknown role or an invalid capability set. Unlike the other two, this
// endpoint's role and capabilities also interact through domain rules that
// only make sense evaluated together (an owner must hold every capability;
// a limited member may never hold "marriage"), so beyond "absent means
// unchanged" this also pins that a role-only change is validated against
// the membership's *existing* capabilities, not a zero-valued stand-in for
// them.
//
// The seeded limited member (env.limitedMembership, capabilities
// {calendar, chores}) is used throughout: it is the one membership in
// newTestEnv whose existing capabilities are a strict subset of
// domain.AllCapabilities(), which is exactly what makes the third subtest
// below possible.
func TestUpdateMemberIsARealPatch(t *testing.T) {
	env := newTestEnv(t)
	session, csrf := env.signIn(t, env.ownerEmail, env.ownerPassword)

	t.Run("a role-only patch leaves capabilities intact", func(t *testing.T) {
		// Sent role equals the membership's current role -- a legitimate
		// "role-only" body (only the "role" key is present at all) that
		// exercises the exact regression a pointer-less fix would miss: if
		// an absent capabilities field decoded to an empty slice instead of
		// the current value, this would still succeed (an empty set is
		// valid for "limited") while silently wiping Ethan's capabilities.
		rec := env.authed(t, http.MethodPatch, "/api/v1/household/members/"+env.limitedMembership,
			map[string]any{"role": "limited"}, session, csrf)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
		}
		var got updateMemberResponse
		if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
			t.Fatalf("decode: %v (body = %s)", err, rec.Body.String())
		}
		if got.Role != "limited" {
			t.Fatalf("role = %q, want limited", got.Role)
		}
		if !slicesEqual(got.Capabilities, []string{"calendar", "chores"}) {
			t.Fatalf("capabilities = %v, want [calendar chores] unchanged by a role-only patch", got.Capabilities)
		}

		members := env.getMembers(t, session)
		persisted := mustFindMember(t, members, env.limitedMembership)
		if !slicesEqual(persisted.Capabilities, []string{"calendar", "chores"}) {
			t.Fatalf("persisted capabilities = %v, want [calendar chores] unchanged", persisted.Capabilities)
		}
	})

	t.Run("a capabilities-only patch leaves the role intact", func(t *testing.T) {
		// This is the exact shape that used to 422 INVALID_ROLE: only
		// "capabilities" is sent, no "role" key at all.
		rec := env.authed(t, http.MethodPatch, "/api/v1/household/members/"+env.limitedMembership,
			map[string]any{"capabilities": []string{"calendar", "chores", "money"}}, session, csrf)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
		}
		var got updateMemberResponse
		if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
			t.Fatalf("decode: %v (body = %s)", err, rec.Body.String())
		}
		if got.Role != "limited" {
			t.Fatalf("role = %q, want limited (unchanged by a capabilities-only patch)", got.Role)
		}
		if !slicesEqual(got.Capabilities, []string{"calendar", "chores", "money"}) {
			t.Fatalf("capabilities = %v, want [calendar chores money]", got.Capabilities)
		}

		members := env.getMembers(t, session)
		persisted := mustFindMember(t, members, env.limitedMembership)
		if persisted.Role != "limited" {
			t.Fatalf("persisted role = %q, want limited unchanged", persisted.Role)
		}
	})

	t.Run("a role-only promotion to owner with a partial capability set still 422s", func(t *testing.T) {
		// At this point env.limitedMembership holds {calendar, chores,
		// money} (the previous subtest's result) -- still missing
		// "marriage", so promoting it to owner without also sending every
		// capability must be validated against those *existing*
		// capabilities and rejected, not validated against an empty or
		// full stand-in value that would let it through incorrectly.
		rec := env.authed(t, http.MethodPatch, "/api/v1/household/members/"+env.limitedMembership,
			map[string]any{"role": "owner"}, session, csrf)
		assertErrorResponse(t, rec, http.StatusUnprocessableEntity, "INVALID_CAPABILITIES")

		// And the rejection must not have partially applied: still limited,
		// still exactly the capabilities from before this subtest ran.
		members := env.getMembers(t, session)
		persisted := mustFindMember(t, members, env.limitedMembership)
		if persisted.Role != "limited" {
			t.Fatalf("persisted role = %q, want limited -- a rejected update must not apply", persisted.Role)
		}
		if !slicesEqual(persisted.Capabilities, []string{"calendar", "chores", "money"}) {
			t.Fatalf("persisted capabilities = %v, want [calendar chores money] unchanged by the rejected PATCH",
				persisted.Capabilities)
		}
	})
}

type updateMemberResponse struct {
	ID           string   `json:"id"`
	Role         string   `json:"role"`
	Capabilities []string `json:"capabilities"`
}

func mustFindMember(t *testing.T, members []memberListEntry, id string) memberListEntry {
	t.Helper()
	for _, m := range members {
		if m.ID == id {
			return m
		}
	}
	t.Fatalf("no member with id %q in %+v", id, members)
	return memberListEntry{}
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
// -- which is why householdResponse above exists as the identical kind of
// local mirror for GET /household.
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

// --- Task 38: accounts, the first capability gate, and redaction ----------

// TestAccountsListRequiresTheMoneyCapability is the first capability gate in
// the product. Until this route existed, requireCapability was defined and
// unused, so the promise that the server enforces capabilities independently
// of the UI was vacuous.
func TestAccountsListRequiresTheMoneyCapability(t *testing.T) {
	env := newTestEnv(t)
	session, _ := env.signIn(t, env.limitedEmail, env.limitedPassword) // calendar + chores

	rec := env.authedGet(t, "/api/v1/accounts", session)
	assertErrorResponse(t, rec, http.StatusForbidden, "FORBIDDEN")
}

// TestAccountsWriteRequiresOwnership is the half the capability gate does not
// cover: a limited member who *does* hold money can read the screen and must
// not be able to change it. Kids look, parents manage.
func TestAccountsWriteRequiresOwnership(t *testing.T) {
	env := newTestEnv(t)
	session, csrf := env.signIn(t, env.moneyLimitedEmail, env.moneyLimitedPassword)

	rec := env.authed(t, http.MethodPost, "/api/v1/accounts", map[string]any{
		"nickname": "Sneaky", "type": "cash",
		"openingBalanceMinor": 100, "openingBalanceCurrency": "SGD",
		"openingBalanceAsOf": "2026-07-26",
	}, session, csrf)
	assertErrorResponse(t, rec, http.StatusForbidden, "FORBIDDEN")
}

// TestAccountsAreRedactedForALimitedMember asserts the amount fields are
// ABSENT, not zero. A zeroed balance still reads as a real one, and a zeroed
// net worth says "this family has nothing" -- a different and worse untruth
// than saying nothing.
//
// The redacted entry's key set is asserted exactly, not just that "balance"
// and "balanceAsOf" happen to be missing: redactedAccounts builds the field
// nils onto the full accountDTO (account_handlers.go), which is a blacklist
// on the field axis even though the role check ten lines above it is a
// deliberate whitelist. A blacklist fails open -- add a new money-carrying
// field to accountDTO later and every limited member receives it, with
// nothing here going red, because "balance"/"balanceAsOf" absent would still
// be true. Asserting the whole key set instead forces exactly that addition
// to be a deliberate decision, at the one moment it matters.
func TestAccountsAreRedactedForALimitedMember(t *testing.T) {
	env := newTestEnv(t)
	ownerSession, ownerCSRF := env.signIn(t, env.ownerEmail, env.ownerPassword)

	// One visible to limited members, one not.
	env.mustCreateAccount(t, ownerSession, ownerCSRF, map[string]any{
		"nickname": "OCBC Joint Savings", "type": "cash",
		"openingBalanceMinor": 4_690_000, "openingBalanceCurrency": "SGD",
		"openingBalanceAsOf": "2026-07-26", "visibleToLimitedMembers": true,
	})
	env.mustCreateAccount(t, ownerSession, ownerCSRF, map[string]any{
		"nickname": "DBS Everyday", "type": "cash",
		"openingBalanceMinor": 824_055, "openingBalanceCurrency": "SGD",
		"openingBalanceAsOf": "2026-07-26", "visibleToLimitedMembers": false,
	})

	session, _ := env.signIn(t, env.moneyLimitedEmail, env.moneyLimitedPassword)
	rec := env.authedGet(t, "/api/v1/accounts", session)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body = %s)", rec.Code, rec.Body.String())
	}

	var raw map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &raw); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if _, present := raw["summary"]; present {
		t.Error("summary is present for a limited member; it must be omitted entirely")
	}

	accounts, ok := raw["accounts"].([]any)
	if !ok || len(accounts) != 1 {
		t.Fatalf("accounts = %v, want exactly the one shared account", raw["accounts"])
	}
	entry := accounts[0].(map[string]any)
	if entry["nickname"] != "OCBC Joint Savings" {
		t.Errorf("nickname = %v, want the shared account", entry["nickname"])
	}

	// The exact set accountDTO produces once Balance and BalanceAsOf are
	// nilled out: both carry `omitempty`, so a nil pointer drops the key
	// entirely rather than serialising as null. ID/OwnerMembershipID/
	// OwnerName/ArchivedAt have no `omitempty` and stay present as null.
	wantKeys := []string{
		"id", "nickname", "type", "ownerMembershipId", "ownerName",
		"countTowardNetWorth", "visibleToLimitedMembers", "archivedAt",
	}
	if len(entry) != len(wantKeys) {
		t.Fatalf("redacted account has keys %v, want exactly %v", mapKeys(entry), wantKeys)
	}
	for _, k := range wantKeys {
		if _, present := entry[k]; !present {
			t.Errorf("redacted account is missing expected key %q (got %v)", k, mapKeys(entry))
		}
	}
}

// mapKeys is a decodeError-style test helper: it exists only to put a
// readable key list into a failure message, since Go maps do not stringify
// in a stable order on their own.
func mapKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// TestOwnerSeesEveryAccountAndTheSummary is the control for the test above: a
// redaction test that passed because the endpoint returns nothing to anybody
// would be worthless.
func TestOwnerSeesEveryAccountAndTheSummary(t *testing.T) {
	env := newTestEnv(t)
	session, csrf := env.signIn(t, env.ownerEmail, env.ownerPassword)

	env.mustCreateAccount(t, session, csrf, map[string]any{
		"nickname": "DBS Everyday", "type": "cash",
		"openingBalanceMinor": 824_055, "openingBalanceCurrency": "SGD",
		"openingBalanceAsOf": "2026-07-26",
	})
	env.mustCreateAccount(t, session, csrf, map[string]any{
		"nickname": "Car loan", "type": "loan",
		"openingBalanceMinor": 1_450_000, "openingBalanceCurrency": "SGD",
		"openingBalanceAsOf": "2026-07-26",
	})

	rec := env.authedGet(t, "/api/v1/accounts", session)
	var got struct {
		Accounts []struct {
			Nickname string `json:"nickname"`
			Balance  struct {
				AmountMinor int64  `json:"amountMinor"`
				Currency    string `json:"currency"`
			} `json:"balance"`
		} `json:"accounts"`
		Summary *struct {
			NetWorthMinor int64 `json:"netWorthMinor"`
			Computable    bool  `json:"computable"`
		} `json:"summary"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(got.Accounts) != 2 {
		t.Fatalf("accounts = %d, want 2", len(got.Accounts))
	}
	if got.Summary == nil {
		t.Fatal("summary is missing for an owner")
	}
	if !got.Summary.Computable || got.Summary.NetWorthMinor != -625_945 {
		t.Errorf("summary = %+v, want a computable net worth of -625945 (824055 - 1450000)", got.Summary)
	}
}

// TestAccountErrorCodesMatchTheSpecTable pins the wire contract for the design
// doc's own §6.3 table at the one level nothing had asserted it before: each
// of these five codes existed only as a string literal in errors.go and, for
// two of them, a second literal in account_handlers.go, with nothing
// confirming the two agreed or that either matched what the table promises.
//
// This is a contract test, not a regression test for a live breakage: today,
// a wrong code costs nothing, because AccountModal's error paragraph falls
// back to a generic message whenever apiErrorMessage doesn't recognise the
// code it was given. The cost arrives the day a caller starts keying off one
// of these strings specifically — a typo here would then fail silently,
// against a suite that stayed green.
func TestAccountErrorCodesMatchTheSpecTable(t *testing.T) {
	env := newTestEnv(t)
	session, csrf := env.signIn(t, env.ownerEmail, env.ownerPassword)

	cases := []struct {
		name string
		body map[string]any
		code string
	}{
		{
			name: "a blank nickname",
			body: map[string]any{
				"nickname": "   ", "type": "cash",
				"openingBalanceMinor": 100, "openingBalanceCurrency": "SGD",
				"openingBalanceAsOf": "2026-07-26",
			},
			code: "NICKNAME_REQUIRED",
		},
		{
			name: "a type this API does not recognise",
			body: map[string]any{
				"nickname": "Mystery account", "type": "bitcoin_wallet",
				"openingBalanceMinor": 100, "openingBalanceCurrency": "SGD",
				"openingBalanceAsOf": "2026-07-26",
			},
			code: "INVALID_TYPE",
		},
		{
			name: "a loan entered as a negative balance",
			body: map[string]any{
				"nickname": "Car loan", "type": "loan",
				"openingBalanceMinor": -145_000, "openingBalanceCurrency": "SGD",
				"openingBalanceAsOf": "2026-07-26",
			},
			code: "INVALID_BALANCE",
		},
		{
			name: "an opening balance dated in the future",
			body: map[string]any{
				"nickname": "DBS Everyday", "type": "cash",
				"openingBalanceMinor": 100, "openingBalanceCurrency": "SGD",
				"openingBalanceAsOf": "2099-01-01",
			},
			code: "INVALID_AS_OF",
		},
		{
			name: "an owner who is not a member of this household",
			body: map[string]any{
				"nickname": "DBS Everyday", "type": "cash",
				"openingBalanceMinor": 100, "openingBalanceCurrency": "SGD",
				"openingBalanceAsOf": "2026-07-26",
				"ownerMembershipId": "00000000-0000-0000-0000-000000000000",
			},
			code: "INVALID_OWNER",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := env.authed(t, http.MethodPost, "/api/v1/accounts", tc.body, session, csrf)
			assertErrorResponse(t, rec, http.StatusUnprocessableEntity, tc.code)
		})
	}
}
