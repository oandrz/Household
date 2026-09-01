package httpadapter_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

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
func TestAdminLookupFailureIs500NotHidden(t *testing.T) {
	env := newTestEnv(t)
	env.makePlatformAdmin(t, env.ownerEmail)
	session, _ := env.signIn(t, env.ownerEmail, env.ownerPassword)

	d := env.deps
	d.Admin = usecase.NewAdminService(usecase.AdminDeps{
		Admins: failingPlatformAdmins{},
		Flags:  env.featureFlags,
		Audit:  env.adminAudit,
		Clock:  env.deps.Clock,
	})
	router := httpadapter.NewRouter(d)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/flags", nil)
	req.AddCookie(session)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	assertErrorResponse(t, rec, http.StatusInternalServerError, "INTERNAL")
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
