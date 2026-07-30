package httpadapter_test

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestTransactionWriteRoutesRequireCSRF drives the three mutating
// transactions routes with no CSRF token at all, and with one that does not
// match the cookie.
//
// TestCSRFIsRequiredForMutatingRequests (auth_api_test.go) already proves
// requireCSRF works, using sign-out. It does not prove this route group is
// behind it: deleting
// `w.Use(requireCSRF)` from the transactions group in router.go left the
// entire suite green, because every other test reaches these routes through
// env.authed, which always supplies the token.
//
// Two details are load-bearing:
//
//   - The session is an owner's. requireCSRF sits *after*
//     requireCapability(CapMoney) and requireOwner in that group, so any
//     lesser caller is refused before it is ever reached -- and would pass
//     this test with the middleware deleted.
//   - The assertion is on the CSRF_INVALID code, not on 403 alone. A bare
//     status check would also stay green if requireOwner were what did the
//     refusing, which is the failure mode the point above describes.
func TestTransactionWriteRoutesRequireCSRF(t *testing.T) {
	env := newTestEnv(t)
	session, csrf := env.signIn(t, env.ownerEmail, env.ownerPassword)

	zeroUUID := "00000000-0000-0000-0000-000000000000"
	routes := []struct{ method, path string }{
		{http.MethodPost, "/api/v1/transactions"},
		{http.MethodPatch, "/api/v1/transactions/" + zeroUUID},
		{http.MethodDelete, "/api/v1/transactions/" + zeroUUID},
	}

	for _, route := range routes {
		t.Run(route.method+" "+route.path, func(t *testing.T) {
			// No X-CSRF-Token header at all.
			req := httptest.NewRequest(route.method, route.path, nil)
			req.AddCookie(session)
			req.AddCookie(csrf)
			rec := httptest.NewRecorder()
			env.router.ServeHTTP(rec, req)
			assertErrorResponse(t, rec, http.StatusForbidden, "CSRF_INVALID")

			// Header present, but not the cookie's value.
			req2 := httptest.NewRequest(route.method, route.path, nil)
			req2.AddCookie(session)
			req2.AddCookie(csrf)
			req2.Header.Set("X-CSRF-Token", "definitely-the-wrong-value")
			rec2 := httptest.NewRecorder()
			env.router.ServeHTTP(rec2, req2)
			assertErrorResponse(t, rec2, http.StatusForbidden, "CSRF_INVALID")
		})
	}
}

// --- Task 51: transactions and categories routes ---------------------------

// requestRouteAs issues route as the caller identified by session/csrf, using
// authedGet for reads (which carry no CSRF cookie or header at all -- GET is
// exempt) and authed for everything else. A single helper keeps the four
// caller shapes below hitting the guard chain the same way a browser would,
// rather than each caller shape improvising its own request construction.
func requestRouteAs(t *testing.T, env *testEnv, method, path string, session, csrf *http.Cookie) *httptest.ResponseRecorder {
	t.Helper()
	if method == http.MethodGet {
		return env.authedGet(t, path, session)
	}
	return env.authed(t, method, path, nil, session, csrf)
}

// TestTransactionRoutesRequireMoneyAndOwner is the test that proves decision
// 5 rather than assuming it: every transactions and categories route
// requires money AND owner, reads included -- unlike accounts, whose read is
// open to any money holder.
//
// A limited member's accounts view shows names with no amounts (accounts
// decision 5). Applied to a ledger, that is a table whose every figure is
// blank next to a "Spent this month" that must be absent rather than zero --
// a page that reads as broken. So for a limited member the money capability
// means "see which accounts this household has" and nothing further. The
// obvious "fix" is to make the two route groups consistent, which is why
// this test names the difference explicitly.
//
// The third caller shape -- env.moneyLimitedEmail, a limited member who DOES
// hold money -- is the one that actually separates this test from
// TestAccountsListRequiresTheMoneyCapability. env.limitedEmail alone would
// never exercise requireOwner at all: it fails at requireCapability first,
// and the walk would pass even if requireOwner were deleted from the group
// entirely.
//
// Known gap, checked and not fixable: this matrix cannot independently prove
// requireCapability(domain.CapMoney) is present on this route group. Doing
// that needs a caller who passes requireOwner but fails requireCapability --
// an owner without money -- and that state cannot be built at all, in this
// system, by any caller of this test. domain.ValidateMembershipChange's
// validateCapabilitiesForRole refuses it at the service layer, and the
// database's own owners_hold_all_capabilities CHECK constraint
// (migrations/00002_identity.sql) refuses it even for a raw
// MembershipRepo.Create that bypasses that service entirely -- confirmed
// empirically while writing this test: constructing that fixture failed
// with "violates check constraint owners_hold_all_capabilities" straight out
// of Postgres. Every caller shape this file can construct is therefore
// either an owner (who always holds money, so requireCapability never has
// anything to refuse) or a non-owner (who requireOwner already refuses
// regardless of requireCapability). Removing requireCapability from the
// transactions group was tried by hand and, as this reasoning predicts, left
// every case in this test green. This is the same "must not lean on an
// invariant enforced in another layer" risk router.go's comment names for
// requireOwner, just one layer further down: the guard is unfalsifiable
// today because the invariant holds in two independent places, not because
// the guard does nothing.
func TestTransactionRoutesRequireMoneyAndOwner(t *testing.T) {
	env := newTestEnv(t)

	zeroUUID := "00000000-0000-0000-0000-000000000000"
	// wantOwner is the exact status the owner must receive on each route, not
	// merely "not 401/403". A route wired with a nil-Deps service still fails
	// at neither guard and panics into a 500 inside the handler -- "not
	// 401/403" would let that slide by unnoticed, which is exactly how this
	// test's first draft passed while deps.Transactions and deps.Categories
	// were both nil in the test harness. Pinning the real value (200 for a
	// read against an empty ledger, 400 for a handler that rejects this nil
	// body before ever touching a service, 404 for an update/delete against
	// an id that does not exist) makes that failure mode loud instead of
	// silent.
	routes := []struct {
		method, path string
		wantOwner    int
	}{
		{http.MethodGet, "/api/v1/transactions", http.StatusOK},
		{http.MethodPost, "/api/v1/transactions", http.StatusBadRequest},
		{http.MethodPatch, "/api/v1/transactions/" + zeroUUID, http.StatusBadRequest},
		{http.MethodDelete, "/api/v1/transactions/" + zeroUUID, http.StatusNotFound},
		{http.MethodGet, "/api/v1/categories", http.StatusOK},
	}

	for _, route := range routes {
		t.Run(route.method+" "+route.path, func(t *testing.T) {
			// No session at all.
			rec := env.do(route.method, route.path, nil)
			if rec.Code != http.StatusUnauthorized {
				t.Fatalf("no session = %d, want 401 (body = %s)", rec.Code, rec.Body.String())
			}

			// Signed in, but without the money capability.
			session, csrf := env.signIn(t, env.limitedEmail, env.limitedPassword)
			rec = requestRouteAs(t, env, route.method, route.path, session, csrf)
			if rec.Code != http.StatusForbidden {
				t.Fatalf("no money capability = %d, want 403 (body = %s)", rec.Code, rec.Body.String())
			}

			// A limited member who DOES hold money. Refused anyway -- this is
			// the case that separates transactions from accounts.
			session, csrf = env.signIn(t, env.moneyLimitedEmail, env.moneyLimitedPassword)
			rec = requestRouteAs(t, env, route.method, route.path, session, csrf)
			if rec.Code != http.StatusForbidden {
				t.Fatalf("limited member holding money = %d, want 403 (body = %s)", rec.Code, rec.Body.String())
			}

			// An owner reaches the handler. The exact status pins that the
			// guards let them through AND that the service behind the
			// handler is actually wired -- see wantOwner's doc comment above
			// for the failure this catches that "not 401/403" would not.
			session, csrf = env.signIn(t, env.ownerEmail, env.ownerPassword)
			rec = requestRouteAs(t, env, route.method, route.path, session, csrf)
			if rec.Code != route.wantOwner {
				t.Fatalf("owner = %d, want %d (body = %s)", rec.Code, route.wantOwner, rec.Body.String())
			}
		})
	}
}
