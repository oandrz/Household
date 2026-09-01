package httpadapter_test

import (
	"net/http"
	"testing"
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
