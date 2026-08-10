package httpadapter_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

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
// The three route-walk matrices that reference this map --
// TestEveryProtectedRouteRejectsAnUnauthenticatedCaller and
// TestEveryMutatingRouteRequiresCSRF in auth_api_test.go, and
// TestOwnerOnlyRoutesRejectALimitedMember in household_api_test.go -- name
// these two routes explicitly rather than skipping anything matching a
// "/api/v1/invites/" prefix, which is what each used to do. A prefix skip
// silently exempts *any* future route
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
	accountRepo := postgres.NewAccountRepo(db)
	categoryRepo := postgres.NewCategoryRepo(db)
	fxProvider := fx.NewStaticProvider()
	accountSvc := usecase.NewAccountService(usecase.AccountDeps{
		Accounts:   accountRepo,
		Households: households,
		FX:         fxProvider,
		Clock:      clk,
	})
	categorySvc := usecase.NewCategoryService(categoryRepo)
	transactionRepo := postgres.NewTransactionRepo(db)
	transactionSvc := usecase.NewTransactionService(usecase.TransactionDeps{
		Transactions: transactionRepo,
		Categories:   categoryRepo,
		Accounts:     accountRepo,
		Households:   households,
		FX:           fxProvider,
		Clock:        clk,
	})
	goalRepo := postgres.NewGoalRepo(db)
	goalSvc := usecase.NewGoalService(usecase.GoalDeps{
		Goals:      goalRepo,
		Households: households,
		FX:         fxProvider,
	})
	budgetSvc := usecase.NewBudgetService(usecase.BudgetDeps{
		Budgets:      postgres.NewBudgetRepo(db),
		Transactions: transactionRepo,
		Categories:   categoryRepo,
		Households:   households,
		Members:      memberships,
		FX:           fxProvider,
		// Goals is read only by BudgetService.RollOver (Task 9's route) to
		// fetch the target goal before writing a contribution -- wired here,
		// alongside Deps.Goals below, even though no route in this task
		// reaches it, because a nil port reachable from an already-wired
		// service is a panic waiting for the next task to trip over.
		Goals: goalRepo,
	})
	billSvc := usecase.NewBillService(usecase.BillDeps{
		Bills:      postgres.NewBillRepo(db),
		Households: households,
		FX:         fxProvider,
		Accounts:   accountRepo,
		Categories: categoryRepo,
	})

	router := httpadapter.NewRouter(httpadapter.Deps{
		Pinger:       db,
		Auth:         authSvc,
		Invites:      inviteSvc,
		Members:      memberSvc,
		Households:   householdSvc,
		Signups:      signupSvc,
		Accounts:     accountSvc,
		Transactions: transactionSvc,
		Categories:   categorySvc,
		Budgets:      budgetSvc,
		Goals:        goalSvc,
		Bills:        billSvc,
		Users:        users,
		Memberships:  memberships,
		Sessions:     sessions,
		Tokens:       tokens,
		Clock:        clk,
		Secure:       false,
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
