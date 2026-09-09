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

func (fakeTelegramLinkRepo) Create(context.Context, string, []byte, time.Time) (string, error) {
	return "link-1", nil
}
func (fakeTelegramLinkRepo) Consume(context.Context, []byte, int64, string) (usecase.TelegramLinkRedemption, error) {
	panic("fakeTelegramLinkRepo: Consume should not be called by these tests")
}
func (fakeTelegramLinkRepo) ByID(context.Context, string) (usecase.TelegramLinkRequest, error) {
	panic("fakeTelegramLinkRepo: ByID should not be called by these tests")
}
func (fakeTelegramLinkRepo) CountMintsSince(context.Context, string, time.Time) (int, error) {
	panic("fakeTelegramLinkRepo: CountMintsSince should not be called by these tests")
}
func (fakeTelegramLinkRepo) CountLinksSince(context.Context, int64, time.Time) (int, error) {
	panic("fakeTelegramLinkRepo: CountLinksSince should not be called by these tests")
}
func (fakeTelegramLinkRepo) Prune(context.Context, time.Time) (int64, error) {
	panic("fakeTelegramLinkRepo: Prune should not be called by these tests")
}

type unusedTelegramAccountRepo struct{}

func (unusedTelegramAccountRepo) ByChatID(context.Context, int64) (string, error) {
	panic("unusedTelegramAccountRepo: ByChatID should not be called by these tests")
}
func (unusedTelegramAccountRepo) ByUserID(context.Context, string) (usecase.TelegramBinding, error) {
	panic("unusedTelegramAccountRepo: ByUserID should not be called by these tests")
}
func (unusedTelegramAccountRepo) Create(context.Context, usecase.TelegramBinding) error {
	panic("unusedTelegramAccountRepo: Create should not be called by these tests")
}
func (unusedTelegramAccountRepo) Delete(context.Context, string) error {
	panic("unusedTelegramAccountRepo: Delete should not be called by these tests")
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
// telegramRouter above. cookies is variadic and optional, the same shape
// env.do (api_test.go) already uses, so every pre-existing zero-cookie call
// below still compiles unchanged.
func doOn(h http.Handler, method, path string, body any, cookies ...*http.Cookie) *httptest.ResponseRecorder {
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
	h.ServeHTTP(rec, req)
	return rec
}

// doOnAuthed is doOn's authenticated, CSRF-validated form for a router other
// than env.router: the session cookie, the csrf cookie, and a matching
// X-CSRF-Token header, mirroring testEnv.authed (api_test.go) for the second
// router telegramLinkRouter below builds.
func doOnAuthed(h http.Handler, method, path string, body any, session, csrf *http.Cookie) *httptest.ResponseRecorder {
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
	req.AddCookie(session)
	req.AddCookie(csrf)
	req.Header.Set("X-CSRF-Token", csrf.Value)
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
//
// This is one of two ways this route answers 404, and the two must not be
// conflated into "redundant" and one deleted: this test pins the pre-existing
// gate, Deps.Telegram == nil (no bot configured at all).
// TestTelegramSignInFlagOffAnswers404EvenWithABotConfigured below pins the
// other -- domain.FlagTelegramSignIn switched off, with a bot properly
// configured -- which is the one requireFeature actually adds in this task.
func TestTelegramStartIs404WhenTheFeatureIsOff(t *testing.T) {
	env := newTestEnv(t) // the existing helper: Deps.Telegram is nil
	rec := env.do(http.MethodPost, "/api/v1/auth/telegram/start", nil)
	assertErrorResponse(t, rec, http.StatusNotFound, "NOT_FOUND")
}

// TestTelegramSignInFlagOffAnswers404EvenWithABotConfigured is
// TestTelegramStartIs404WhenTheFeatureIsOff's sibling for the OTHER way this
// route can be hidden: a bot IS configured (Deps.Telegram non-nil, via
// telegramRouter) but domain.FlagTelegramSignIn is switched off. The two
// produce identical wire responses -- 404 NOT_FOUND -- and it would be easy
// to assume they exercise the same guard and drop one as redundant. They do
// not: this one is the only test anywhere that references
// FlagTelegramSignIn at all, so without it router.go's
// requireFeature(deps, domain.FlagTelegramSignIn) call could be deleted, or
// swapped for the wrong flag constant, and nothing would fail -- the flag
// defaults to true, and the no-bot test above never touches it.
func TestTelegramSignInFlagOffAnswers404EvenWithABotConfigured(t *testing.T) {
	env := newTestEnv(t)
	router := telegramRouter(env, newTelegramAuthServiceForTest())

	// The flag defaults on, and a bot is configured: the route works.
	rec := doOn(router, http.MethodPost, "/api/v1/auth/telegram/start", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("telegram_sign_in on = %d, want 200 (body = %s)", rec.Code, rec.Body.String())
	}

	if err := env.featureFlags.SetGlobal(context.Background(),
		string(domain.FlagTelegramSignIn), false, ""); err != nil {
		t.Fatalf("SetGlobal: %v", err)
	}

	rec = doOn(router, http.MethodPost, "/api/v1/auth/telegram/start", nil)
	assertErrorResponse(t, rec, http.StatusNotFound, "NOT_FOUND")
}

// TestTelegramStartIs404WhenTheFeatureIsOffIsByteIdenticalToAnUnroutedPath
// pins the body itself, not only the status and code the test above already
// checks. Status and code alone would still pass if the handler's message
// text ever drifted from router.go's own NotFound handler -- say, to
// "Telegram sign-in is not configured." -- and that drift is exactly the
// distinguisher the comment above (and the design doc) claims does not
// exist: a caller could then tell "no such route" apart from "Telegram not
// configured" by the message alone, even though both still answer 404
// NOT_FOUND. Comparing the two responses byte for byte is what actually
// proves the message itself carries no distinguisher. It does not prove there
// is none anywhere in the response: the per-IP limiter sits ahead of this
// handler unconditionally (router.go), so a 21st POST from the same IP inside
// the hour answers 429 RATE_LIMITED forever, where a genuinely unrouted path
// never would. That gap is not a leak, though -- a configured install
// rate-limits identically to an unconfigured one, so what a caller learns
// from it is only "this build has the route", true of every install of this
// version. See the mutation proof recorded in the final-fix-wave report:
// changing the handler's message makes this test fail, and restoring it
// makes this test pass again.
func TestTelegramStartIs404WhenTheFeatureIsOffIsByteIdenticalToAnUnroutedPath(t *testing.T) {
	env := newTestEnv(t) // the existing helper: Deps.Telegram is nil

	telegramOff := env.do(http.MethodPost, "/api/v1/auth/telegram/start", nil)
	// A path this router has never registered a handler for -- reaches
	// router.go's own r.NotFound, not any Telegram-aware code at all.
	genuinelyUnrouted := env.do(http.MethodGet, "/api/v1/this-route-does-not-exist", nil)

	if telegramOff.Code != genuinelyUnrouted.Code {
		t.Fatalf("status = %d, unrouted path's status = %d, want them equal",
			telegramOff.Code, genuinelyUnrouted.Code)
	}
	if telegramOff.Body.String() != genuinelyUnrouted.Body.String() {
		t.Fatalf("Telegram-off body = %s\nunrouted-path body = %s\nwant them byte-identical",
			telegramOff.Body.String(), genuinelyUnrouted.Body.String())
	}
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

// noFeatureFlagOverrides answers "no overrides recorded" for every flag, so
// AdminService.GlobalFlags resolves every flag to its compile-time default --
// signups_open defaults true (domain.AllFlags). It exists because Task 6 put
// requireFeature in front of the sign-up preview route below, and that
// route's pre-auth branch calls deps.Admin.GlobalFlags regardless of which
// test is asking, so a Deps literal naming only Signups (as this file's
// tests already did, to avoid a database) now needs a working, if minimal,
// Admin too. The other two AdminDeps ports are left nil deliberately: this
// route never reaches IsPlatformAdmin or RecordAudit, and wiring them would
// only make it look like it did.
type noFeatureFlagOverrides struct{}

func (noFeatureFlagOverrides) OverridesFor(context.Context, string) (map[string]bool, map[string]bool, error) {
	panic("noFeatureFlagOverrides: OverridesFor should not be called by these tests")
}

func (noFeatureFlagOverrides) GlobalOverrides(context.Context) (map[string]bool, error) {
	return nil, nil
}

func (noFeatureFlagOverrides) AllHouseholdOverrides(context.Context) ([]usecase.HouseholdFlagOverride, error) {
	panic("noFeatureFlagOverrides: AllHouseholdOverrides should not be called by these tests")
}

func (noFeatureFlagOverrides) SetGlobal(context.Context, string, bool, string) error {
	panic("noFeatureFlagOverrides: SetGlobal should not be called by these tests")
}

func (noFeatureFlagOverrides) SetHousehold(context.Context, string, string, bool, string) error {
	panic("noFeatureFlagOverrides: SetHousehold should not be called by these tests")
}

func (noFeatureFlagOverrides) ClearHousehold(context.Context, string, string) error {
	panic("noFeatureFlagOverrides: ClearHousehold should not be called by these tests")
}

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
	router := httpadapter.NewRouter(httpadapter.Deps{
		Signups: svc,
		Admin:   usecase.NewAdminService(usecase.AdminDeps{Flags: noFeatureFlagOverrides{}}),
	})

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

// --- Task 6: the routes for connecting and disconnecting a Telegram chat
// (GET/DELETE /auth/telegram, POST /auth/telegram/link,
// GET /auth/telegram/link/{id}, POST /auth/telegram/link/{id}/confirm) ------

// telegramLinkRepoTestDouble is a minimal, stateful TelegramLinkRepository
// double for the guard tests below. Consume, CountLinksSince and Prune are
// the chat-side half of the flow and the prune job -- never reached from
// these browser-route tests -- and panic like this file's other unused*
// doubles above.
type telegramLinkRepoTestDouble struct {
	rows map[string]usecase.TelegramLinkRequest
	n    int
}

func newTelegramLinkRepoTestDouble() *telegramLinkRepoTestDouble {
	return &telegramLinkRepoTestDouble{rows: map[string]usecase.TelegramLinkRequest{}}
}

func (d *telegramLinkRepoTestDouble) Create(_ context.Context, userID string, _ []byte, expiresAt time.Time) (string, error) {
	d.n++
	id := fmt.Sprintf("link-test-%d", d.n)
	d.rows[id] = usecase.TelegramLinkRequest{ID: id, UserID: userID, ExpiresAt: expiresAt}
	return id, nil
}

func (d *telegramLinkRepoTestDouble) Consume(context.Context, []byte, int64, string) (usecase.TelegramLinkRedemption, error) {
	panic("telegramLinkRepoTestDouble: Consume should not be called by these tests")
}

func (d *telegramLinkRepoTestDouble) ByID(_ context.Context, id string) (usecase.TelegramLinkRequest, error) {
	row, ok := d.rows[id]
	if !ok {
		return usecase.TelegramLinkRequest{}, domain.ErrNotFound
	}
	return row, nil
}

func (d *telegramLinkRepoTestDouble) CountMintsSince(context.Context, string, time.Time) (int, error) {
	return 0, nil
}

func (d *telegramLinkRepoTestDouble) CountLinksSince(context.Context, int64, time.Time) (int, error) {
	panic("telegramLinkRepoTestDouble: CountLinksSince should not be called by these tests")
}

func (d *telegramLinkRepoTestDouble) Prune(context.Context, time.Time) (int64, error) {
	panic("telegramLinkRepoTestDouble: Prune should not be called by these tests")
}

var _ usecase.TelegramLinkRepository = (*telegramLinkRepoTestDouble)(nil)

// telegramAccountRepoTestDouble is a minimal, stateful TelegramAccountRepository
// double: an empty binding table, so Status derives "waiting" for a link
// nobody has redeemed yet -- everything the guard tests below need from it.
type telegramAccountRepoTestDouble struct {
	byUser map[string]usecase.TelegramBinding
}

func newTelegramAccountRepoTestDouble() *telegramAccountRepoTestDouble {
	return &telegramAccountRepoTestDouble{byUser: map[string]usecase.TelegramBinding{}}
}

func (d *telegramAccountRepoTestDouble) ByChatID(_ context.Context, chatID int64) (string, error) {
	for uid, b := range d.byUser {
		if b.ChatID == chatID {
			return uid, nil
		}
	}
	return "", domain.ErrNotFound
}

func (d *telegramAccountRepoTestDouble) ByUserID(_ context.Context, userID string) (usecase.TelegramBinding, error) {
	b, ok := d.byUser[userID]
	if !ok {
		return usecase.TelegramBinding{}, domain.ErrNotFound
	}
	return b, nil
}

func (d *telegramAccountRepoTestDouble) Create(_ context.Context, b usecase.TelegramBinding) error {
	d.byUser[b.UserID] = b
	return nil
}

func (d *telegramAccountRepoTestDouble) Delete(_ context.Context, userID string) error {
	delete(d.byUser, userID)
	return nil
}

var _ usecase.TelegramAccountRepository = (*telegramAccountRepoTestDouble)(nil)

// newTelegramLinkServiceForTest builds a real TelegramLinkService for the
// guard tests below: Start and Status are genuinely exercised (a real
// crypto.TokenGenerator, a real clock, env's own Postgres-backed Users) over
// the two stateful doubles above.
func newTelegramLinkServiceForTest(env *testEnv) *usecase.TelegramLinkService {
	return usecase.NewTelegramLinkService(usecase.TelegramLinkDeps{
		Links:       newTelegramLinkRepoTestDouble(),
		Accounts:    newTelegramAccountRepoTestDouble(),
		Users:       env.users,
		Tokens:      crypto.NewTokenGenerator(),
		Clock:       clock.System{},
		BotUsername: "HearthBot",
	})
}

// telegramLinkRouter is telegramRouter's sibling for the link/unlink routes:
// deps.TelegramLink swapped for svc, everything else shared with env's own
// router -- the same env.deps-copy-and-swap telegramRouter above already
// uses for Deps.Telegram.
func telegramLinkRouter(env *testEnv, svc *usecase.TelegramLinkService) http.Handler {
	d := env.deps
	d.TelegramLink = svc
	return httpadapter.NewRouter(d)
}

// telegramLinkRoutes is the exact five routes this task adds, reused by
// every guard test below so a route added or removed here is felt by all of
// them at once.
var telegramLinkRoutes = []struct{ method, path string }{
	{http.MethodGet, "/api/v1/auth/telegram"},
	{http.MethodDelete, "/api/v1/auth/telegram"},
	{http.MethodPost, "/api/v1/auth/telegram/link"},
	{http.MethodGet, "/api/v1/auth/telegram/link/some-link-id"},
	{http.MethodPost, "/api/v1/auth/telegram/link/some-link-id/confirm"},
}

// telegramLinkWriteRoutes is the three of those five that mutate, for the
// guards (CSRF, ownership-of-guards-not-owner) that only apply to a write.
var telegramLinkWriteRoutes = []struct{ method, path string }{
	{http.MethodDelete, "/api/v1/auth/telegram"},
	{http.MethodPost, "/api/v1/auth/telegram/link"},
	{http.MethodPost, "/api/v1/auth/telegram/link/some-link-id/confirm"},
}

// TestTelegramLinkRoutesRefuseWithoutASession pins requireSession as the
// outermost guard of the group: no cookie at all, on the default env (bot
// unconfigured) -- and it must still be 401, not the 404 a nil
// Deps.TelegramLink would answer, because requireSession runs before the
// handler ever gets a chance to check that.
func TestTelegramLinkRoutesRefuseWithoutASession(t *testing.T) {
	env := newTestEnv(t)
	for _, r := range telegramLinkRoutes {
		rec := env.do(r.method, r.path, nil)
		assertErrorResponse(t, rec, http.StatusUnauthorized, "UNAUTHENTICATED")
	}
}

// TestTelegramLinkRoutesRefuseAnAPIToken pins requireCookieSession: a
// personal API token authenticates the caller (requireSession succeeds) but
// must not be able to bind or unbind a chat -- the same rule minting a
// token itself follows (ADR 7, spec decision 8), because a leaked token
// must not be able to make itself into a channel that outlives its own
// revocation.
func TestTelegramLinkRoutesRefuseAnAPIToken(t *testing.T) {
	env := newTestEnv(t)
	session, csrf := env.signIn(t, env.ownerEmail, env.ownerPassword)
	tok := env.mustCreateToken(t, session, csrf, "telegram-guard-test")

	for _, r := range telegramLinkRoutes {
		rec := env.bearer(t, r.method, r.path, nil, tok.Token)
		assertErrorResponse(t, rec, http.StatusForbidden, "SESSION_REQUIRED")
	}
}

// TestTelegramLinkPostRefusesWithoutCSRF pins requireCSRF on the three
// mutating routes: a genuinely valid session cookie, but no csrf_token
// cookie and no X-CSRF-Token header.
func TestTelegramLinkPostRefusesWithoutCSRF(t *testing.T) {
	env := newTestEnv(t)
	session, _ := env.signIn(t, env.ownerEmail, env.ownerPassword)

	for _, r := range telegramLinkWriteRoutes {
		rec := env.do(r.method, r.path, nil, session)
		assertErrorResponse(t, rec, http.StatusForbidden, "CSRF_INVALID")
	}
}

// TestTelegramLinkRoutesAre404WhenTheFlagIsOff is
// TestTelegramSignInFlagOffAnswers404EvenWithABotConfigured's sibling for
// this task's five routes: a bot IS configured (Deps.TelegramLink non-nil,
// via telegramLinkRouter) but domain.FlagTelegramSignIn is switched off.
// requireFeature sits ahead of requireCSRF in the group (router.go), so a
// bare session cookie -- no CSRF cookie or header at all -- is enough to
// prove the flag, not CSRF, is what answered 404 here.
func TestTelegramLinkRoutesAre404WhenTheFlagIsOff(t *testing.T) {
	env := newTestEnv(t)
	router := telegramLinkRouter(env, newTelegramLinkServiceForTest(env))
	session, _ := env.signIn(t, env.ownerEmail, env.ownerPassword)

	// The flag defaults on, and a bot is configured: the route works. This
	// is what makes the loop below prove the flag, not the routes being
	// unregistered, is what answers 404 -- without it this test would still
	// pass against an unrouted path (the RED step showed exactly that).
	rec := doOn(router, http.MethodGet, "/api/v1/auth/telegram", nil, session)
	if rec.Code != http.StatusOK {
		t.Fatalf("telegram_sign_in on = %d, want 200 (body = %s)", rec.Code, rec.Body.String())
	}

	if err := env.featureFlags.SetGlobal(context.Background(),
		string(domain.FlagTelegramSignIn), false, ""); err != nil {
		t.Fatalf("SetGlobal: %v", err)
	}

	for _, r := range telegramLinkRoutes {
		rec := doOn(router, r.method, r.path, nil, session)
		assertErrorResponse(t, rec, http.StatusNotFound, "NOT_FOUND")
	}
}

// TestTelegramLinkRoutesAre404WithNoBotConfigured is the other of the two
// ways this group can answer 404 -- Deps.TelegramLink itself nil, the
// default env every other test in this file starts from -- pinned
// separately from the flag-off test above for the same reason
// TestTelegramStartIs404WhenTheFeatureIsOff and
// TestTelegramSignInFlagOffAnswers404EvenWithABotConfigured are kept apart:
// the two produce the identical wire response but exercise different gates,
// and collapsing them into one test would let either gate be deleted with
// nothing left to notice.
func TestTelegramLinkRoutesAre404WithNoBotConfigured(t *testing.T) {
	env := newTestEnv(t) // Deps.TelegramLink is nil
	session, csrf := env.signIn(t, env.ownerEmail, env.ownerPassword)

	for _, r := range telegramLinkRoutes {
		var rec *httptest.ResponseRecorder
		if r.method == http.MethodGet {
			rec = env.authedGet(t, r.path, session)
		} else {
			rec = env.authed(t, r.method, r.path, nil, session, csrf)
		}
		assertErrorResponse(t, rec, http.StatusNotFound, "NOT_FOUND")
	}
}

// TestTelegramLinkPollingRouteNeedsNoCSRFHeader pins the reason all five
// routes share one group instead of splitting reads from writes: requireCSRF
// returns early for GET, HEAD and OPTIONS (middleware_csrf.go), so
// GET /auth/telegram/link/{id} needs no X-CSRF-Token header at all, only the
// session cookie. If this route were ever moved into a group that required
// the header unconditionally, this is the test that would go red.
func TestTelegramLinkPollingRouteNeedsNoCSRFHeader(t *testing.T) {
	env := newTestEnv(t)
	router := telegramLinkRouter(env, newTelegramLinkServiceForTest(env))
	session, csrf := env.signIn(t, env.ownerEmail, env.ownerPassword)

	rec := doOnAuthed(router, http.MethodPost, "/api/v1/auth/telegram/link", nil, session, csrf)
	if rec.Code != http.StatusOK {
		t.Fatalf("mint: %d %s", rec.Code, rec.Body.String())
	}
	var start struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &start); err != nil || start.ID == "" {
		t.Fatalf("start body: %v %s", err, rec.Body.String())
	}

	// No csrf_token cookie and no X-CSRF-Token header at all -- only the
	// session cookie.
	rec = doOn(router, http.MethodGet, "/api/v1/auth/telegram/link/"+start.ID, nil, session)
	if rec.Code != http.StatusOK {
		t.Fatalf("status without a CSRF header: %d %s", rec.Code, rec.Body.String())
	}
}
