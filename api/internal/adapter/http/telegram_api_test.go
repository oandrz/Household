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
// Only StartLink is exercised (POST /auth/telegram/start); HandleStart's
// behaviour is usecase/telegram_auth_test.go's job, so every other port
// (Accounts, MagicLinks, Signups, Sender, Invites) -- and Links' methods
// other than Create -- panic rather than silently returning a zero value.

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

type unusedInviteKnocker struct{}

func (unusedInviteKnocker) Knock(context.Context, string, int64, string) (string, error) {
	panic("unusedInviteKnocker: Knock should not be called by these tests")
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
		Invites:     unusedInviteKnocker{},
	})
}

// telegramRouter shares every dependency env's own router has (real
// Postgres-backed sign-up, sessions, and so on), with Telegram wired to svc
// -- the same env.deps-copy-and-swap api_test.go's routerWithMemberships
// uses, reused here without touching that file since this package already
// has env.deps in scope.
func telegramRouter(env *testEnv, svc *usecase.TelegramAuthService) http.Handler {
	d := env.deps
	d.Telegram = svc
	return httpadapter.NewRouter(d)
}

// doOn issues a JSON request against an arbitrary router: env.do (api_test.go)
// is pinned to env.router, but the tests below need a second router built by
// telegramRouter above. cookies is variadic and optional, matching env.do's
// shape, so every pre-existing zero-cookie call below still compiles.
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

// TestTelegramStartIs404WhenTheFeatureIsOff pins that with no bot
// configured, the route must not exist at all -- an install that never set
// up Telegram gets the exact NOT_FOUND any unrouted path gets.
//
// This is one of two distinct ways this route answers 404 -- don't conflate
// them and delete one as redundant. This test pins the "no bot configured"
// gate (Deps.Telegram == nil).
// TestTelegramSignInFlagOffAnswers404EvenWithABotConfigured pins the other:
// domain.FlagTelegramSignIn switched off via requireFeature, with a bot
// properly configured.
func TestTelegramStartIs404WhenTheFeatureIsOff(t *testing.T) {
	env := newTestEnv(t) // the existing helper: Deps.Telegram is nil
	rec := env.do(http.MethodPost, "/api/v1/auth/telegram/start", nil)
	assertErrorResponse(t, rec, http.StatusNotFound, "NOT_FOUND")
}

// TestTelegramSignInFlagOffAnswers404EvenWithABotConfigured is
// TestTelegramStartIs404WhenTheFeatureIsOff's sibling for the OTHER way this
// route can be hidden: a bot IS configured, but domain.FlagTelegramSignIn is
// switched off. Both produce 404 NOT_FOUND, which invites dropping one as
// redundant -- don't: this is the only test anywhere that references
// FlagTelegramSignIn, so without it router.go's requireFeature call (or the
// wrong flag constant) could be deleted and nothing would fail, since the
// flag defaults true and the no-bot test never touches it.
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
// pins the body itself, not only the status and code the test above checks:
// those alone would still pass if the handler's message ever drifted from
// router.go's own NotFound handler (say, to "Telegram sign-in is not
// configured."), letting a caller tell "no such route" apart from "Telegram
// not configured" by the message even though both answer 404 NOT_FOUND.
// Comparing byte for byte proves the message carries no distinguisher --
// not that none exists anywhere in the response: the per-IP limiter sits
// ahead of this handler unconditionally (router.go), so a 21st POST from the
// same IP inside the hour answers 429 forever, where an unrouted path never
// would. That's not a leak: a configured install rate-limits identically to
// an unconfigured one, so all a caller learns is "this build has the
// route", true of every install of this version. Mutation-tested: changing
// the handler's message makes this test fail; restoring it passes again.
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

// TestTelegramStartIsRateLimitedPerIP pins the per-IP limit: the route
// takes no identifier, so there is nothing to enumerate -- but it still
// mints a row per call, so it must be limited per IP like sign-up is.
//
// telegramStartsPerIPPerHour (middleware_ratelimit.go) is unexported and this
// package is httpadapter_test, so its value (20) is repeated here as a
// literal, the same convention TestSignUpPassesThroughThePerIPLimiter uses for
// signUpRequestsPerIPPerHour. Every request under the limit is checked, not
// just the first and last: a limiter wired with limit=1 would still pass a
// bare "loop then check last" test.
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

// TestTelegramStartHasItsOwnRateLimitBudgetSeparateFromSignUp pins why this
// route needs its own limiter instance, not the sign-up group's: a person
// who just signed up should not find Telegram sign-in already spent, and
// vice versa. Without a dedicated instance, router.go could share
// signUpRequestsPerIPPerHour's limiter between the groups and every other
// test in this file would still pass, since none of them call both routes.
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

// --- GET /auth/sign-up/{token} must carry channel too -----------------------
//
// The frontend expects {"email": ..., "channel": ...}; SignupPreview.Channel
// (usecase/signup.go) already computes it, the handler just puts it on the
// wire. TestSignUpPreviewAndComplete (auth_api_test.go) covers the email
// shape, including channel == "email". This test covers the Telegram
// shape: an empty email sent as an actual "" value, not omitted
// (SignupPreview's doc warns an omitted one looks like a forgotten field).

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
// AdminService.GlobalFlags resolves each flag to its compile-time default
// (signups_open defaults true). It exists because the sign-up preview
// route below calls deps.Admin.GlobalFlags on every request via
// requireFeature, so a minimal Deps needs a working Admin too. The other two
// AdminDeps ports are left nil deliberately: this route never reaches
// IsPlatformAdmin or RecordAudit.
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
	// distinguish them, which is the whole point of this test: the empty
	// email must not be dropped.
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

// --- the routes for connecting and disconnecting a Telegram chat
// (GET/DELETE /auth/telegram, POST /auth/telegram/link,
// GET /auth/telegram/link/{id}, POST /auth/telegram/link/{id}/confirm) ------

// telegramLinkRepoTestDouble is a minimal, stateful TelegramLinkRepository
// double for the guard tests below. Consume, CountLinksSince and Prune are
// the chat-side half of the flow and the prune job -- unreached from these
// browser-route tests -- and panic like this file's other unused* doubles.
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

// telegramLinkRoutes is the five link/unlink routes, reused by every guard
// test below so a route added or removed here is felt by all of them at
// once.
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
// outermost guard: no cookie at all, on the default env (bot unconfigured),
// must still answer 401, not the 404 a nil Deps.TelegramLink would give --
// requireSession runs before the handler ever gets to check that.
func TestTelegramLinkRoutesRefuseWithoutASession(t *testing.T) {
	env := newTestEnv(t)
	for _, r := range telegramLinkRoutes {
		rec := env.do(r.method, r.path, nil)
		assertErrorResponse(t, rec, http.StatusUnauthorized, "UNAUTHENTICATED")
	}
}

// TestTelegramLinkRoutesRefuseAnAPIToken pins requireCookieSession: a
// personal API token authenticates the caller (requireSession succeeds) but
// must not bind or unbind a chat -- the same rule minting a token itself
// follows (ADR 7): a leaked token must not become a channel that outlives
// its own revocation.
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
// these five routes: a bot IS configured (Deps.TelegramLink non-nil, via
// telegramLinkRouter) but domain.FlagTelegramSignIn is switched off.
// requireFeature sits ahead of requireCSRF in the group (router.go), so a
// bare session cookie -- no CSRF cookie or header -- is enough to prove the
// flag, not CSRF, answered 404 here.
func TestTelegramLinkRoutesAre404WhenTheFlagIsOff(t *testing.T) {
	env := newTestEnv(t)
	router := telegramLinkRouter(env, newTelegramLinkServiceForTest(env))
	session, _ := env.signIn(t, env.ownerEmail, env.ownerPassword)

	// The flag defaults on, and a bot is configured: the route works. This
	// is what proves the flag, not an unregistered route, answers 404 below
	// -- without it this test would still pass against a route that plain
	// doesn't exist.
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
// ways this group can answer 404: Deps.TelegramLink itself nil, the default
// env every other test in this file starts from. Kept separate from the
// flag-off test above for the same reason
// TestTelegramStartIs404WhenTheFeatureIsOff and its sibling are: identical
// wire response, different gates -- merging them would let either gate be
// deleted unnoticed.
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

// TestTelegramLinkPollingRouteNeedsNoCSRFHeader pins why all five routes
// share one group instead of splitting reads from writes: requireCSRF
// returns early for GET, HEAD and OPTIONS (middleware_csrf.go), so GET
// /auth/telegram/link/{id} needs only the session cookie, no X-CSRF-Token
// header. If this route were ever moved into a group requiring the header
// unconditionally, this is the test that would go red.
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
