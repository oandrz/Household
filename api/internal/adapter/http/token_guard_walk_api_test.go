package httpadapter_test

import (
	"net/http"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
)

// TestAccessRoutesRefuseAnAPIToken walks every route under /auth and
// /household -- the two places where a way into the household is created,
// changed or removed -- and calls each with an owner's personal API token.
// It asserts 403 SESSION_REQUIRED unless the route is on the allowlist
// below.
//
// The rule it pins is ADR 7's: a leaked token must not be able to make
// itself permanent or change who can get in. The tests beside each feature
// pin the routes known today; this walk is what catches the next one, added
// under either prefix without requireCookieSession.
//
// The caller is an owner on purpose. A limited member's token would be
// refused by requireOwner first, and the walk would pass without ever
// reaching the guard it is named after.
//
// Its blind spot: a route that changes access but lives outside these two
// prefixes is not walked. Put such a route under one of them, or widen the
// prefix check below.
func TestAccessRoutesRefuseAnAPIToken(t *testing.T) {
	env := newTestEnv(t)
	session, csrf := env.signIn(t, env.ownerEmail, env.ownerPassword)
	tok := env.mustCreateToken(t, session, csrf, "token-guard-walk")

	// Every entry is a route a token may correctly reach, with the reason
	// recorded beside it. Adding a route here is a decision that a leaked
	// token calling it cannot widen or keep its own access.
	tokenAllowed := map[string]bool{
		// Public, pre-auth: no credential is read on these at all, so a
		// token neither helps nor is refused.
		"POST /api/v1/auth/sign-in":                  true,
		"POST /api/v1/auth/magic-link":               true,
		"POST /api/v1/auth/magic-link/consume":       true,
		"POST /api/v1/auth/sign-up":                  true,
		"GET /api/v1/auth/sign-up/{token}":           true,
		"POST /api/v1/auth/sign-up/{token}/complete": true,
		"POST /api/v1/auth/telegram/start":           true,
		// Reads that show no secret: who the token belongs to, and the
		// token list by prefix only (the raw value is shown once, at mint).
		"GET /api/v1/auth/me":     true,
		"GET /api/v1/auth/tokens": true,
		// The household and who is in it. A command-line client needs both
		// to do anything useful, and neither changes access.
		"GET /api/v1/household":         true,
		"GET /api/v1/household/members": true,
		// Pending invites, owner-only. The list carries no invite link.
		"GET /api/v1/household/invites": true,
		// Household settings: its name, currency and time zone. Nothing
		// here decides who can sign in.
		"PATCH /api/v1/household": true,
	}

	routes, ok := env.router.(chi.Routes)
	if !ok {
		t.Fatal("router does not implement chi.Routes")
	}

	seen := map[string]bool{}
	checked := 0
	err := chi.Walk(routes, func(method, route string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		if !strings.HasPrefix(route, "/api/v1/auth/") &&
			route != "/api/v1/household" &&
			!strings.HasPrefix(route, "/api/v1/household/") {
			return nil
		}
		key := method + " " + route
		if tokenAllowed[key] {
			seen[key] = true
			return nil
		}

		var body any
		if method != http.MethodGet {
			body = map[string]any{}
		}
		rec := env.bearer(t, method, fillRouteParams(route), body, tok.Token)

		if rec.Code != http.StatusForbidden {
			t.Errorf("%s: status = %d, want 403 -- an API token can reach this route; "+
				"guard it with requireCookieSession, or add it to the allowlist with the reason (body = %s)",
				key, rec.Code, rec.Body.String())
		} else if got := decodeError(t, rec); got.Error.Code != "SESSION_REQUIRED" {
			t.Errorf("%s: error code = %q, want SESSION_REQUIRED", key, got.Error.Code)
		}
		checked++
		return nil
	})
	if err != nil {
		t.Fatalf("chi.Walk: %v", err)
	}

	for key := range tokenAllowed {
		if !seen[key] {
			t.Errorf("allowlist entry %q matches no route -- remove it", key)
		}
	}

	t.Logf("checked %d cookie-only routes", checked)
	// 15 is this walk's own re-measured count. Its only job is catching a
	// walk that stopped enumerating routes and so asserts nothing. Set it to
	// what the walk reports whenever a cookie-only route is added; never
	// guess.
	if checked < 15 {
		t.Fatalf("checked %d cookie-only routes, want at least 15 -- "+
			"the walk may not be enumerating routes correctly", checked)
	}
}
