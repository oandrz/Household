package httpadapter_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	httpadapter "github.com/andreasoentoro/hearth/api/internal/adapter/http"
	"github.com/andreasoentoro/hearth/api/internal/domain"
	"github.com/andreasoentoro/hearth/api/internal/usecase"
)

// TestAdminRoutesAre404ToANonAdmin is the shape of the whole gate: to anyone
// who is not a platform admin, /admin is indistinguishable from a typo. A 403
// would confirm the surface exists and that they found the right path.
func TestAdminRoutesAre404ToANonAdmin(t *testing.T) {
	env := newTestEnv(t)
	session, csrf := env.signIn(t, env.ownerEmail, env.ownerPassword)

	rec := env.authedGet(t, "/api/v1/admin/flags", session)
	assertErrorResponse(t, rec, http.StatusNotFound, "NOT_FOUND")

	rec = env.authed(t, http.MethodPost, "/api/v1/admin/session",
		map[string]string{"password": env.ownerPassword}, session, csrf)
	assertErrorResponse(t, rec, http.StatusNotFound, "NOT_FOUND")
}

// errPlatformAdminLookupFailed stands in for what a database outage produces:
// an error that is emphatically not domain.ErrNotFound.
var errPlatformAdminLookupFailed = errors.New("platform admin lookup failed")

// failingPlatformAdmins is a usecase.PlatformAdminRepository that cannot
// answer anything. Every method fails, not just Get: a half-working double
// invites a future test to lean on the half that works and quietly stop
// testing what this one is for.
type failingPlatformAdmins struct{}

func (failingPlatformAdmins) Get(context.Context, string) (domain.PlatformAdmin, error) {
	return domain.PlatformAdmin{}, errPlatformAdminLookupFailed
}

func (failingPlatformAdmins) Grant(context.Context, string, string) error {
	return errPlatformAdminLookupFailed
}

func (failingPlatformAdmins) Revoke(context.Context, string) error {
	return errPlatformAdminLookupFailed
}

func (failingPlatformAdmins) List(context.Context) ([]usecase.PlatformAdminListing, error) {
	return nil, errPlatformAdminLookupFailed
}

// TestAdminLookupFailureIs500NotHidden is the second of the gate's three
// properties, and the reason requirePlatformAdmin does not route its error
// branch through MapDomainError.
//
// "The database is down" must never read as a clean "you are not an admin".
// The caller here IS a platform admin, which is what makes the failure mode
// concrete: if an outage answered 404, the one person who could fix it would
// be told the page does not exist, and would have no reason to look at the
// database at all.
//
// Only the Admins port is swapped -- the router, the session, and every other
// dependency are the real ones, on a router built for this one request. That
// is the seam routerWithMemberships established for the same reason: there is
// no way to make a live Postgres fail this specific lookup on demand.
// adminRouterWith is the shared form of it, used by the three tests that each
// break one admin port.
func TestAdminLookupFailureIs500NotHidden(t *testing.T) {
	env := newTestEnv(t)
	env.makePlatformAdmin(t, env.ownerEmail)
	session, _ := env.signIn(t, env.ownerEmail, env.ownerPassword)

	router := env.adminRouterWith(failingPlatformAdmins{}, nil, nil)

	rec := get(t, router, "/api/v1/admin/flags", session)
	assertErrorResponse(t, rec, http.StatusInternalServerError, "INTERNAL")
}

// failingAdminAudit is a usecase.AdminAuditRepository that cannot write. It
// stands in for the log being full, locked, or gone -- the state in which an
// admin surface must close rather than carry on unaudited.
type failingAdminAudit struct{}

var errAuditWriteFailed = errors.New("audit write failed")

func (failingAdminAudit) Record(context.Context, usecase.AdminAuditEntry) error {
	return errAuditWriteFailed
}

func (failingAdminAudit) Recent(context.Context, int) ([]usecase.AdminAuditEntry, error) {
	return nil, errAuditWriteFailed
}

// panickingFeatureFlags makes the flags handler panic rather than merely
// fail. That is the difference that gives
// TestTheAuditRowIsWrittenBeforeTheHandlerRuns its teeth: a handler that
// returns an error still lets an audit call placed AFTER it run, so a
// returning double could not tell the two orderings apart. A panic unwinds
// straight past anything downstream to recoverer, so the row exists only if
// it was written first.
//
// OverridesFor is the one method that does NOT panic. Task 6 put
// AdminService.FlagsFor (which calls OverridesFor) on every authenticated
// request, inside requireSession -- upstream of requirePlatformAdmin and
// auditAdmin. A double that panicked there too would blow up before the
// request ever reached the admin subtree this test is about, so the audit
// row would never be written and the test would fail for a reason that has
// nothing to do with auditAdmin's ordering. Leaving it healthy keeps the
// panic where the test needs it: inside GET /admin/flags's own handler,
// which calls GlobalOverrides and AllHouseholdOverrides, not OverridesFor.
type panickingFeatureFlags struct{}

func (panickingFeatureFlags) OverridesFor(context.Context, string) (map[string]bool, map[string]bool, error) {
	return nil, nil, nil
}

func (panickingFeatureFlags) GlobalOverrides(context.Context) (map[string]bool, error) {
	panic("flags exploded")
}

func (panickingFeatureFlags) AllHouseholdOverrides(context.Context) ([]usecase.HouseholdFlagOverride, error) {
	panic("flags exploded")
}

func (panickingFeatureFlags) SetGlobal(context.Context, string, bool, string) error {
	panic("flags exploded")
}

func (panickingFeatureFlags) SetHousehold(context.Context, string, string, bool, string) error {
	panic("flags exploded")
}

func (panickingFeatureFlags) ClearHousehold(context.Context, string, string) error {
	panic("flags exploded")
}

// adminRouterWith builds a router sharing every one of env's dependencies
// except Admin, which is rebuilt from the three ports passed in. Nil means
// "use the env's real one", so each test names only the port it is breaking.
func (env *testEnv) adminRouterWith(
	admins usecase.PlatformAdminRepository,
	flags usecase.FeatureFlagRepository,
	audit usecase.AdminAuditRepository,
) http.Handler {
	if admins == nil {
		admins = env.platformAdmins
	}
	if flags == nil {
		flags = env.featureFlags
	}
	if audit == nil {
		audit = env.adminAudit
	}
	d := env.deps
	d.Admin = usecase.NewAdminService(usecase.AdminDeps{
		Admins: admins, Flags: flags, Audit: audit, Clock: env.deps.Clock,
	})
	return httpadapter.NewRouter(d)
}

// get issues a GET carrying only the session cookie, against a router the
// test built rather than env.router.
func get(t *testing.T, router http.Handler, path string, session *http.Cookie) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	req.AddCookie(session)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec
}

// TestTheAdminGrantExpires pins adminGrantTTL and the comparison that reads
// it. Without this, widening the constant to 30 hours -- or reducing
// requireAdminGrant's condition to a bare nil check, which drops the expiry
// comparison altogether -- leaves every other admin test green.
//
// The successful read before the clock moves is not padding: it is what makes
// the second read's 401 mean "the grant expired" rather than "the grant never
// worked". Together they also pin that the grant is NOT extended by activity
// the way a session is (see sessionExtendThreshold) -- the read happens inside
// the window and buys no more time.
func TestTheAdminGrantExpires(t *testing.T) {
	clk := &movableClock{now: time.Now().UTC()}
	env := newTestEnvWithClock(t, clk)
	env.makePlatformAdmin(t, env.ownerEmail)
	session, csrf := env.signIn(t, env.ownerEmail, env.ownerPassword)

	rec := env.authed(t, http.MethodPost, "/api/v1/admin/session",
		map[string]string{"password": env.ownerPassword}, session, csrf)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("POST /admin/session = %d, body %s; want 204", rec.Code, rec.Body.String())
	}

	if rec = env.authedGet(t, "/api/v1/admin/flags", session); rec.Code != http.StatusOK {
		t.Fatalf("GET /admin/flags inside the grant = %d, body %s; want 200", rec.Code, rec.Body.String())
	}

	// Past adminGrantTTL (30 minutes), but nowhere near the 30-day session
	// expiry -- so what lapses here is the grant alone, not the session.
	clk.Advance(31 * time.Minute)

	rec = env.authedGet(t, "/api/v1/admin/flags", session)
	assertErrorResponse(t, rec, http.StatusUnauthorized, "ADMIN_REAUTH_REQUIRED")
}

// TestAnUnwritableAuditLogClosesTheSurface pins auditAdmin's refusal branch.
// Deleting that branch and serving the request anyway leaves every other
// admin test green, which is precisely the silently-unaudited admin surface
// the audit table exists to make impossible.
//
// The grant is minted against env.router, whose audit log works. Only the
// read afterwards goes to the router with the broken one -- otherwise the
// re-auth itself would be refused and the test would never reach the
// behaviour it is named after.
func TestAnUnwritableAuditLogClosesTheSurface(t *testing.T) {
	env := newTestEnv(t)
	env.makePlatformAdmin(t, env.ownerEmail)
	session, csrf := env.signIn(t, env.ownerEmail, env.ownerPassword)
	env.authed(t, http.MethodPost, "/api/v1/admin/session",
		map[string]string{"password": env.ownerPassword}, session, csrf)

	router := env.adminRouterWith(nil, nil, failingAdminAudit{})

	rec := get(t, router, "/api/v1/admin/flags", session)
	assertErrorResponse(t, rec, http.StatusServiceUnavailable, "AUDIT_UNAVAILABLE")
}

// TestTheAuditRowIsWrittenBeforeTheHandlerRuns pins the ordering inside
// auditAdmin. The row goes in before next.ServeHTTP so that a handler which
// panics still leaves a trace of the attempt -- the case where the log
// matters most is the one where the request did not finish.
//
// The flags port panics rather than returning an error, deliberately: see
// panickingFeatureFlags. recoverer turns the panic into the 500 asserted
// below, and the audit row must exist regardless.
func TestTheAuditRowIsWrittenBeforeTheHandlerRuns(t *testing.T) {
	env := newTestEnv(t)
	env.makePlatformAdmin(t, env.ownerEmail)
	session, csrf := env.signIn(t, env.ownerEmail, env.ownerPassword)
	env.authed(t, http.MethodPost, "/api/v1/admin/session",
		map[string]string{"password": env.ownerPassword}, session, csrf)

	router := env.adminRouterWith(nil, panickingFeatureFlags{}, nil)

	before := env.auditRowCount(t)
	rec := get(t, router, "/api/v1/admin/flags", session)
	after := env.auditRowCount(t)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("GET /admin/flags with a panicking handler = %d, body %s; want 500",
			rec.Code, rec.Body.String())
	}
	if after != before+1 {
		t.Fatalf("audit rows went %d -> %d; a request whose handler panicked must "+
			"still have been logged before it ran", before, after)
	}
}

// TestAdminRoutesNeedAGrant: being a platform admin is not by itself the key.
// The session cookie lives 30 days; the surface opens only after the password
// is entered again.
func TestAdminRoutesNeedAGrant(t *testing.T) {
	env := newTestEnv(t)
	env.makePlatformAdmin(t, env.ownerEmail)
	session, _ := env.signIn(t, env.ownerEmail, env.ownerPassword)

	rec := env.authedGet(t, "/api/v1/admin/flags", session)
	assertErrorResponse(t, rec, http.StatusUnauthorized, "ADMIN_REAUTH_REQUIRED")
}

// TestAdminSessionMintsAGrant walks the whole happy path: re-authenticate,
// then the surface answers.
func TestAdminSessionMintsAGrant(t *testing.T) {
	env := newTestEnv(t)
	env.makePlatformAdmin(t, env.ownerEmail)
	session, csrf := env.signIn(t, env.ownerEmail, env.ownerPassword)

	rec := env.authed(t, http.MethodPost, "/api/v1/admin/session",
		map[string]string{"password": env.ownerPassword}, session, csrf)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("POST /admin/session = %d, body %s; want 204", rec.Code, rec.Body.String())
	}

	rec = env.authedGet(t, "/api/v1/admin/flags", session)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /admin/flags after re-auth = %d, body %s; want 200", rec.Code, rec.Body.String())
	}
}

// TestAdminSessionRefusesTheWrongPassword: a failed re-auth must leave the
// surface exactly as shut as it was before the attempt.
func TestAdminSessionRefusesTheWrongPassword(t *testing.T) {
	env := newTestEnv(t)
	env.makePlatformAdmin(t, env.ownerEmail)
	session, csrf := env.signIn(t, env.ownerEmail, env.ownerPassword)

	rec := env.authed(t, http.MethodPost, "/api/v1/admin/session",
		map[string]string{"password": "not-the-password"}, session, csrf)
	assertErrorResponse(t, rec, http.StatusUnauthorized, "INVALID_CREDENTIALS")

	rec = env.authedGet(t, "/api/v1/admin/flags", session)
	assertErrorResponse(t, rec, http.StatusUnauthorized, "ADMIN_REAUTH_REQUIRED")
}

// TestAdminReauthLockoutLeavesHouseholdSignInWorking is the separation the
// second ledger exists for, asserted end to end.
func TestAdminReauthLockoutLeavesHouseholdSignInWorking(t *testing.T) {
	env := newTestEnv(t)
	env.makePlatformAdmin(t, env.ownerEmail)
	session, csrf := env.signIn(t, env.ownerEmail, env.ownerPassword)

	for i := 0; i < 3; i++ {
		env.authed(t, http.MethodPost, "/api/v1/admin/session",
			map[string]string{"password": "wrong"}, session, csrf)
	}

	rec := env.authed(t, http.MethodPost, "/api/v1/admin/session",
		map[string]string{"password": env.ownerPassword}, session, csrf)
	assertErrorResponse(t, rec, http.StatusLocked, "ADMIN_LOCKED")

	// The household is untouched: a fresh password sign-in still works.
	env.signIn(t, env.ownerEmail, env.ownerPassword)
}

// TestSigningOutRevokesTheAdminGrant: the grant lives on the session row, so
// revoking the session must take it with it.
func TestSigningOutRevokesTheAdminGrant(t *testing.T) {
	env := newTestEnv(t)
	env.makePlatformAdmin(t, env.ownerEmail)
	session, csrf := env.signIn(t, env.ownerEmail, env.ownerPassword)
	env.authed(t, http.MethodPost, "/api/v1/admin/session",
		map[string]string{"password": env.ownerPassword}, session, csrf)

	env.authed(t, http.MethodPost, "/api/v1/auth/sign-out", nil, session, csrf)

	rec := env.authedGet(t, "/api/v1/admin/flags", session)
	assertErrorResponse(t, rec, http.StatusUnauthorized, "UNAUTHENTICATED")
}

// TestEveryAdminRequestIsAudited: the log is written from middleware, so even
// a plain read leaves a row.
func TestEveryAdminRequestIsAudited(t *testing.T) {
	env := newTestEnv(t)
	env.makePlatformAdmin(t, env.ownerEmail)
	session, csrf := env.signIn(t, env.ownerEmail, env.ownerPassword)
	env.authed(t, http.MethodPost, "/api/v1/admin/session",
		map[string]string{"password": env.ownerPassword}, session, csrf)

	before := env.auditRowCount(t)
	env.authedGet(t, "/api/v1/admin/flags", session)
	after := env.auditRowCount(t)

	if after != before+1 {
		t.Fatalf("audit rows went %d -> %d; a read must write exactly one row", before, after)
	}
}

// TestAdminAuditRowRecordsTheRealRequest guards what TestEveryAdminRequestIsAudited
// does not: that the row auditAdmin writes carries THIS request's own method
// and path, not merely that a row of some kind exists.
// TestAdminAuditRepoRecordsAndReadsBack next door proves the repository
// round-trips whatever Action, Target and Detail it is given -- it says
// nothing about what auditAdmin actually passes it. Without this test,
// auditAdmin could go back to writing Target: "" for every row (as it did
// for the field's entire life -- see LEARNING.md's admin-surface entries) and
// every other admin test, including the count-only one above, would stay
// green.
func TestAdminAuditRowRecordsTheRealRequest(t *testing.T) {
	env := newTestEnv(t)
	env.makePlatformAdmin(t, env.ownerEmail)
	session, csrf := env.signIn(t, env.ownerEmail, env.ownerPassword)
	env.authed(t, http.MethodPost, "/api/v1/admin/session",
		map[string]string{"password": env.ownerPassword}, session, csrf)

	env.authedGet(t, "/api/v1/admin/flags", session)

	entries, err := env.adminAudit.Recent(context.Background(), 1)
	if err != nil {
		t.Fatalf("Recent: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("Recent(1) returned %d entries, want 1", len(entries))
	}
	if entries[0].Action != "GET /api/v1/admin/flags" {
		t.Fatalf("Action = %q, want %q", entries[0].Action, "GET /api/v1/admin/flags")
	}
	if entries[0].Target != "/api/v1/admin/flags" {
		t.Fatalf("Target = %q, want %q", entries[0].Target, "/api/v1/admin/flags")
	}
}

// TestMeCarriesEveryDefinedFlag: every key is always present, so the frontend
// never has to decide what a missing key means.
func TestMeCarriesEveryDefinedFlag(t *testing.T) {
	env := newTestEnv(t)
	session, _ := env.signIn(t, env.ownerEmail, env.ownerPassword)

	rec := env.authedGet(t, "/api/v1/auth/me", session)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /auth/me = %d, want 200", rec.Code)
	}

	var body struct {
		IsPlatformAdmin bool            `json:"isPlatformAdmin"`
		Features        map[string]bool `json:"features"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode me: %v", err)
	}
	if body.IsPlatformAdmin {
		t.Fatal("an ordinary owner is not a platform admin")
	}
	for _, def := range domain.AllFlags() {
		if _, present := body.Features[string(def.Flag)]; !present {
			t.Fatalf("features is missing %q: %v", def.Flag, body.Features)
		}
	}
}

func TestMeMarksAPlatformAdmin(t *testing.T) {
	env := newTestEnv(t)
	env.makePlatformAdmin(t, env.ownerEmail)
	session, _ := env.signIn(t, env.ownerEmail, env.ownerPassword)

	rec := env.authedGet(t, "/api/v1/auth/me", session)
	var body struct {
		IsPlatformAdmin bool `json:"isPlatformAdmin"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode me: %v", err)
	}
	if !body.IsPlatformAdmin {
		t.Fatal("a platform admin's me bundle does not say so")
	}
}

// TestAdminCanToggleAFlagGlobally walks the happy path end to end: an admin
// turns a flag on, and the flag-gated route it was hiding behind opens.
func TestAdminCanToggleAFlagGlobally(t *testing.T) {
	env := newTestEnv(t)
	env.makePlatformAdmin(t, env.ownerEmail)
	session, csrf := env.signIn(t, env.ownerEmail, env.ownerPassword)
	env.authed(t, http.MethodPost, "/api/v1/admin/session",
		map[string]string{"password": env.ownerPassword}, session, csrf)

	rec := env.authed(t, http.MethodPut, "/api/v1/admin/flags/family_calendar",
		map[string]bool{"enabled": true}, session, csrf)
	if rec.Code != http.StatusOK {
		t.Fatalf("PUT flag = %d, body %s; want 200", rec.Code, rec.Body.String())
	}

	rec = env.authedGet(t, "/api/v1/family/calendar", session)
	if rec.Code != http.StatusOK {
		t.Fatalf("the gated route after enabling = %d, want 200", rec.Code)
	}
}

// TestAdminFlagWritesRefuseAnUnknownKey pins the fail-closed rule: a key
// domain.ParseFlag does not recognise is refused before anything is written,
// not silently accepted into an orphaned row.
func TestAdminFlagWritesRefuseAnUnknownKey(t *testing.T) {
	env := newTestEnv(t)
	env.makePlatformAdmin(t, env.ownerEmail)
	session, csrf := env.signIn(t, env.ownerEmail, env.ownerPassword)
	env.authed(t, http.MethodPost, "/api/v1/admin/session",
		map[string]string{"password": env.ownerPassword}, session, csrf)

	rec := env.authed(t, http.MethodPut, "/api/v1/admin/flags/not_a_flag",
		map[string]bool{"enabled": true}, session, csrf)
	assertErrorResponse(t, rec, http.StatusUnprocessableEntity, "UNKNOWN_FLAG")
}

// TestClearingAHouseholdOverrideIsNotTheSameAsTurningItOff pins the rule the
// whole DELETE route exists for: "no opinion" and "explicitly off" are
// different states. Setting a household override to false must make the
// gated route answer 404 (explicitly off); clearing that same override must
// make it answer 200 again, because with no override left the household falls
// back to the global true set above -- not to false, and not to a second
// 404 that would mean the DELETE quietly became another way to turn it off.
func TestClearingAHouseholdOverrideIsNotTheSameAsTurningItOff(t *testing.T) {
	env := newTestEnv(t)
	env.makePlatformAdmin(t, env.ownerEmail)
	session, csrf := env.signIn(t, env.ownerEmail, env.ownerPassword)
	env.authed(t, http.MethodPost, "/api/v1/admin/session",
		map[string]string{"password": env.ownerPassword}, session, csrf)

	env.authed(t, http.MethodPut, "/api/v1/admin/flags/family_calendar",
		map[string]bool{"enabled": true}, session, csrf)
	env.authed(t, http.MethodPut,
		"/api/v1/admin/flags/family_calendar/households/"+env.householdID,
		map[string]bool{"enabled": false}, session, csrf)

	rec := env.authedGet(t, "/api/v1/family/calendar", session)
	assertErrorResponse(t, rec, http.StatusNotFound, "NOT_FOUND")

	rec = env.authed(t, http.MethodDelete,
		"/api/v1/admin/flags/family_calendar/households/"+env.householdID, nil, session, csrf)
	if rec.Code != http.StatusOK {
		t.Fatalf("DELETE override = %d, want 200", rec.Code)
	}

	// With the override gone the household falls back to the global true.
	rec = env.authedGet(t, "/api/v1/family/calendar", session)
	if rec.Code != http.StatusOK {
		t.Fatalf("after clearing the override = %d, want 200", rec.Code)
	}
}
