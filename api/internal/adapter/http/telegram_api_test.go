package httpadapter_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/andreasoentoro/hearth/api/internal/adapter/clock"
	"github.com/andreasoentoro/hearth/api/internal/adapter/crypto"
	httpadapter "github.com/andreasoentoro/hearth/api/internal/adapter/http"
	"github.com/andreasoentoro/hearth/api/internal/domain"
	"github.com/andreasoentoro/hearth/api/internal/usecase"
)

// --- doubles for building a *usecase.TelegramAuthService in this package's
// tests -----------------------------------------------------------------
//
// POST /auth/telegram/start only ever calls StartLink, which touches Tokens
// and Links.Create. Every other port TelegramAuthDeps requires (Accounts,
// MagicLinks, Signups, Sender) exists solely to satisfy the struct --
// HandleStart's own behaviour is usecase/telegram_auth_test.go's job, not
// this package's -- so each unused method panics: a future test that
// accidentally exercises one fails loudly at the call site instead of
// silently returning a zero value.

type fakeTelegramLinkRepo struct{}

func (fakeTelegramLinkRepo) Create(context.Context, []byte, time.Time) error { return nil }
func (fakeTelegramLinkRepo) Consume(context.Context, []byte, int64) error {
	panic("fakeTelegramLinkRepo: Consume should not be called by these tests")
}
func (fakeTelegramLinkRepo) CountLinksSince(context.Context, int64, time.Time) (int, error) {
	panic("fakeTelegramLinkRepo: CountLinksSince should not be called by these tests")
}

type unusedTelegramAccountRepo struct{}

func (unusedTelegramAccountRepo) ByChatID(context.Context, int64) (string, error) {
	panic("unusedTelegramAccountRepo: ByChatID should not be called by these tests")
}

type unusedMagicLinkRepo struct{}

func (unusedMagicLinkRepo) Create(context.Context, string, []byte, time.Time) error {
	panic("unusedMagicLinkRepo: Create should not be called by these tests")
}
func (unusedMagicLinkRepo) Consume(context.Context, []byte) (string, error) {
	panic("unusedMagicLinkRepo: Consume should not be called by these tests")
}
func (unusedMagicLinkRepo) CountSince(context.Context, string, time.Time) (int, error) {
	panic("unusedMagicLinkRepo: CountSince should not be called by these tests")
}

type unusedSignupRepo struct{}

func (unusedSignupRepo) Create(context.Context, string, []byte, time.Time) error {
	panic("unusedSignupRepo: Create should not be called by these tests")
}
func (unusedSignupRepo) CreateConsumed(context.Context, string, []byte, time.Time) error {
	panic("unusedSignupRepo: CreateConsumed should not be called by these tests")
}
func (unusedSignupRepo) CreateForTelegram(context.Context, int64, []byte, time.Time) error {
	panic("unusedSignupRepo: CreateForTelegram should not be called by these tests")
}
func (unusedSignupRepo) ByTokenHash(context.Context, []byte) (usecase.SignupDetails, error) {
	panic("unusedSignupRepo: ByTokenHash should not be called by these tests")
}
func (unusedSignupRepo) CountForEmailSince(context.Context, string, time.Time) (int, error) {
	panic("unusedSignupRepo: CountForEmailSince should not be called by these tests")
}
func (unusedSignupRepo) CountSince(context.Context, time.Time) (int, error) {
	panic("unusedSignupRepo: CountSince should not be called by these tests")
}
func (unusedSignupRepo) Provision(context.Context, string, string, usecase.HouseholdBlueprint) (usecase.ProvisionedHousehold, error) {
	panic("unusedSignupRepo: Provision should not be called by these tests")
}
func (unusedSignupRepo) Prune(context.Context, time.Time) (int64, error) {
	panic("unusedSignupRepo: Prune should not be called by these tests")
}

type unusedTelegramSender struct{}

func (unusedTelegramSender) SendMessage(context.Context, int64, string) error {
	panic("unusedTelegramSender: SendMessage should not be called by these tests")
}

// newTelegramAuthServiceForTest builds a real TelegramAuthService over the
// doubles above: StartLink is genuinely exercised end to end (a real
// crypto.TokenGenerator, a real clock), and everything HandleStart alone
// would need panics if this file ever calls it by mistake.
func newTelegramAuthServiceForTest() *usecase.TelegramAuthService {
	return usecase.NewTelegramAuthService(usecase.TelegramAuthDeps{
		Links:       fakeTelegramLinkRepo{},
		Accounts:    unusedTelegramAccountRepo{},
		MagicLinks:  unusedMagicLinkRepo{},
		Signups:     unusedSignupRepo{},
		Sender:      unusedTelegramSender{},
		Tokens:      crypto.NewTokenGenerator(),
		Clock:       clock.System{},
		BaseURL:     "http://localhost:5173",
		BotUsername: "HearthBot",
	})
}

// telegramRouter shares every dependency env's own router has (real
// Postgres-backed sign-up, sessions, and so on), with Telegram wired to svc.
// This is the same env.deps-copy-and-swap api_test.go's routerWithMemberships
// already uses for Memberships, applied here without needing a change to
// that shared file: this file is package httpadapter_test too, so env.deps
// is already in scope.
func telegramRouter(env *testEnv, svc *usecase.TelegramAuthService) http.Handler {
	d := env.deps
	d.Telegram = svc
	return httpadapter.NewRouter(d)
}

// doOn issues a JSON request against an arbitrary router. env.do (api_test.go)
// is pinned to env.router; the tests below need a second router built by
// telegramRouter above.
func doOn(h http.Handler, method, path string, body any) *httptest.ResponseRecorder {
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
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestTelegramStartReturnsADeepLink(t *testing.T) {
	env := newTestEnv(t)
	router := telegramRouter(env, newTelegramAuthServiceForTest())

	before := time.Now()
	rec := doOn(router, http.MethodPost, "/api/v1/auth/telegram/start", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}

	var body struct {
		URL       string `json:"url"`
		ExpiresAt string `json:"expiresAt"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("body: %v", err)
	}

	// Checks for the nonce after ?start=, not just the https://t.me/ prefix:
	// a handler that hardcoded the prefix onto an otherwise-empty string
	// would still pass a bare HasPrefix check.
	const wantPrefix = "https://t.me/HearthBot?start="
	nonce := strings.TrimPrefix(body.URL, wantPrefix)
	if nonce == body.URL || nonce == "" {
		t.Fatalf("url = %q, want %q followed by a non-empty nonce", body.URL, wantPrefix)
	}

	// Parsed and compared to a real instant, not just checked non-empty: a
	// zero-valued time.Time still marshals to a non-empty string
	// ("0001-01-01T00:00:00Z"), which a bare `!= ""` check would miss.
	expiresAt, err := time.Parse(time.RFC3339, body.ExpiresAt)
	if err != nil {
		t.Fatalf("expiresAt = %q, not a parseable timestamp: %v", body.ExpiresAt, err)
	}
	if !expiresAt.After(before) {
		t.Fatalf("expiresAt = %v, want a time after %v", expiresAt, before)
	}
}

// With no bot configured the route must not exist at all, so an install
// that never set up Telegram behaves exactly as it did before this feature
// -- even down to the exact answer (NOT_FOUND), the same one any unrouted
// path gets.
func TestTelegramStartIs404WhenTheFeatureIsOff(t *testing.T) {
	env := newTestEnv(t) // the existing helper: Deps.Telegram is nil
	rec := env.do(http.MethodPost, "/api/v1/auth/telegram/start", nil)
	assertErrorResponse(t, rec, http.StatusNotFound, "NOT_FOUND")
}

// The route takes no identifier, so there is nothing to enumerate -- but it
// still mints a row per call, so it must be limited per IP like sign-up is.
//
// telegramStartsPerIPPerHour (middleware_ratelimit.go) is unexported and
// this package is httpadapter_test, so its value (20) is repeated here as a
// literal -- the same convention TestSignUpPassesThroughThePerIPLimiter
// (auth_api_test.go) uses for signUpRequestsPerIPPerHour. Every request
// under the limit is checked, not just the first and the last: a limiter
// wired with limit=1 would still make the final request in a bare
// "loop then check last" test come back 429.
func TestTelegramStartIsRateLimitedPerIP(t *testing.T) {
	env := newTestEnv(t)
	router := telegramRouter(env, newTelegramAuthServiceForTest())

	const perIPLimit = 20
	for i := 0; i < perIPLimit; i++ {
		rec := doOn(router, http.MethodPost, "/api/v1/auth/telegram/start", nil)
		if rec.Code != http.StatusOK {
			t.Fatalf("request %d = %d, want 200", i, rec.Code)
		}
	}

	rec := doOn(router, http.MethodPost, "/api/v1/auth/telegram/start", nil)
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("status after exceeding the limit = %d, want 429 (body = %s)", rec.Code, rec.Body.String())
	}
}

// TestTelegramStartHasItsOwnRateLimitBudgetSeparateFromSignUp pins the
// controller's own reason this route needs its own limiter instance, not
// the sign-up group's: a person who has just signed up should not find
// Telegram sign-in already spent, and vice versa. Without a dedicated
// limiter instance, router.go could share signUpRequestsPerIPPerHour's
// limiter between the two groups and every other test in this file would
// still pass, since none of them ever calls the other route.
func TestTelegramStartHasItsOwnRateLimitBudgetSeparateFromSignUp(t *testing.T) {
	env := newTestEnv(t)
	router := telegramRouter(env, newTelegramAuthServiceForTest())

	// Spend sign-up's own per-IP budget (signUpRequestsPerIPPerHour == 5;
	// see TestSignUpPassesThroughThePerIPLimiter for why that constant is
	// repeated here as a literal rather than read directly).
	const signUpPerIPLimit = 5
	for i := 0; i < signUpPerIPLimit; i++ {
		rec := doOn(router, http.MethodPost, "/api/v1/auth/sign-up",
			map[string]string{"email": fmt.Sprintf("budget-%d@example.test", i)})
		if rec.Code != http.StatusAccepted {
			t.Fatalf("sign-up warm-up %d = %d, want 202", i, rec.Code)
		}
	}
	// Sanity check that the budget really is spent, so a false pass below
	// cannot be blamed on this loop being too short.
	rec := doOn(router, http.MethodPost, "/api/v1/auth/sign-up",
		map[string]string{"email": "one-too-many@example.test"})
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("sign-up after its own budget = %d, want 429 (sanity check)", rec.Code)
	}

	rec = doOn(router, http.MethodPost, "/api/v1/auth/telegram/start", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("telegram start after sign-up's own budget is spent = %d, want 200 -- "+
			"the two routes must not share one limiter", rec.Code)
	}
}

// --- CONTROLLER RULING R3: GET /auth/sign-up/{token} must carry channel
// too --------------------------------------------------------------------
//
// Task 9's frontend expects {"email": ..., "channel": ...} from this route.
// SignupPreview.Channel (usecase/signup.go, Task 4) already computes the
// right value; nothing before this task ever put it on the wire. The email
// shape is covered by TestSignUpPreviewAndComplete (auth_api_test.go),
// extended by this task to also assert channel == "email". This test covers
// the shape SignupPreview's own doc comment says would otherwise render as
// "a field the person forgot to fill in": a Telegram sign-up's empty email,
// sent as an actual "" value, not omitted.

// fakeSignupRepoForPreview answers ByTokenHash from one canned row; every
// other SignupRepository method panics, since handleSignUpPreview only ever
// calls Preview, and Preview only ever calls ByTokenHash.
type fakeSignupRepoForPreview struct {
	hash   []byte
	detail usecase.SignupDetails
}

func (f fakeSignupRepoForPreview) ByTokenHash(_ context.Context, hash []byte) (usecase.SignupDetails, error) {
	if !bytes.Equal(hash, f.hash) {
		return usecase.SignupDetails{}, domain.ErrNotFound
	}
	return f.detail, nil
}
func (fakeSignupRepoForPreview) Create(context.Context, string, []byte, time.Time) error {
	panic("fakeSignupRepoForPreview: Create should not be called by these tests")
}
func (fakeSignupRepoForPreview) CreateConsumed(context.Context, string, []byte, time.Time) error {
	panic("fakeSignupRepoForPreview: CreateConsumed should not be called by these tests")
}
func (fakeSignupRepoForPreview) CreateForTelegram(context.Context, int64, []byte, time.Time) error {
	panic("fakeSignupRepoForPreview: CreateForTelegram should not be called by these tests")
}
func (fakeSignupRepoForPreview) CountForEmailSince(context.Context, string, time.Time) (int, error) {
	panic("fakeSignupRepoForPreview: CountForEmailSince should not be called by these tests")
}
func (fakeSignupRepoForPreview) CountSince(context.Context, time.Time) (int, error) {
	panic("fakeSignupRepoForPreview: CountSince should not be called by these tests")
}
func (fakeSignupRepoForPreview) Provision(context.Context, string, string, usecase.HouseholdBlueprint) (usecase.ProvisionedHousehold, error) {
	panic("fakeSignupRepoForPreview: Provision should not be called by these tests")
}
func (fakeSignupRepoForPreview) Prune(context.Context, time.Time) (int64, error) {
	panic("fakeSignupRepoForPreview: Prune should not be called by these tests")
}

var _ usecase.SignupRepository = fakeSignupRepoForPreview{}

func TestSignUpPreviewShowsTelegramChannelWithNoEmail(t *testing.T) {
	tokens := crypto.NewTokenGenerator()
	raw, hash, err := tokens.NewToken()
	if err != nil {
		t.Fatalf("mint token: %v", err)
	}
	chatID := int64(918273645)
	repo := fakeSignupRepoForPreview{
		hash: hash,
		detail: usecase.SignupDetails{
			ID:             "signup-1",
			TelegramChatID: &chatID,
			ExpiresAt:      time.Now().Add(time.Hour),
		},
	}
	svc := usecase.NewSignupService(usecase.SignupDeps{
		Signups: repo,
		Tokens:  tokens,
		Clock:   clock.System{},
	})
	router := httpadapter.NewRouter(httpadapter.Deps{Signups: svc})

	rec := doOn(router, http.MethodGet, "/api/v1/auth/sign-up/"+raw, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}

	// Decoded into map[string]any, not a typed struct: a struct field can be
	// missing from the JSON and still decode to its Go zero value ("") with
	// no way to tell "absent" from "explicitly empty" apart. The map does
	// distinguish them, which is the whole point of this test -- Ruling R3
	// says the empty email must not be dropped.
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("body: %v", err)
	}
	emailVal, ok := body["email"]
	if !ok {
		t.Fatal(`the "email" key is missing entirely -- an empty email must still be sent, not omitted`)
	}
	if emailVal != "" {
		t.Fatalf("email = %v, want \"\" for a Telegram sign-up", emailVal)
	}
	if body["channel"] != "telegram" {
		t.Fatalf("channel = %v, want \"telegram\"", body["channel"])
	}
}
