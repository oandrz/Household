package httpadapter_test

import (
	"net/http"
	"regexp"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
)

// routeParam matches one chi path parameter, such as {id} or {month}.
var routeParam = regexp.MustCompile(`\{[^}]+\}`)

// fillRouteParams turns a route pattern into a path the router will match,
// by putting the same placeholder id in every parameter. The value never
// has to be real: the walks that use this assert on a guard's refusal, and
// every guard answers before a handler reads its parameters.
func fillRouteParams(route string) string {
	return routeParam.ReplaceAllString(route, "00000000-0000-0000-0000-000000000000")
}

// TestReadRoutesRejectALimitedMember is the read-side sibling of
// TestOwnerOnlyRoutesRejectALimitedMember, which skips GET. It walks the
// live router and sends every GET as a limited member with a valid session,
// asserting 403 FORBIDDEN unless the route is on the allowlist below.
//
// A read route registered outside its guarded group simply answers 200 and
// fails here. Without this walk it would pass every test: authorisation
// exists only at the edge, so a misplaced route has no second line of
// defence.
//
// It runs as two limited members because the money routes are guarded
// twice. A read placed in the "Money, any member" group is refused for a
// member without Money and allowed for one with it, so either member alone
// would miss a route that lost only one of its two guards.
func TestReadRoutesRejectALimitedMember(t *testing.T) {
	env := newTestEnv(t)

	// Every entry is a read that is correctly open to a limited member,
	// with the reason recorded beside it. Adding a route here is a decision
	// that a child in the household may read it.
	anyMember := map[string]bool{
		// Public, pre-auth: the sign-up form reads both before any session
		// exists, so there is no caller to refuse.
		"GET /api/v1/auth/sign-up/{token}": true,
		"GET /api/v1/currencies":           true,
		// The caller's own identity and their own credentials. Both are
		// user-scoped: neither shows another member's rows.
		"GET /api/v1/auth/me":                 true,
		"GET /api/v1/auth/tokens":             true,
		"GET /api/v1/auth/telegram":           true,
		"GET /api/v1/auth/telegram/link/{id}": true,
		// Needed household-wide: names, roles and spaces feed pickers on
		// every screen. The member list empties each email for a limited
		// caller (TestMemberListWithholdsEmailsFromALimitedMember).
		"GET /api/v1/household":         true,
		"GET /api/v1/household/members": true,
		"GET /api/v1/spaces":            true,
		// The handler narrows a limited member to their own tokens and chat
		// (TestALimitedMemberSeesOnlyTheirOwnTokensAndChat).
		"GET /api/v1/household/access": true,
		// Which nudges the household has switched on. It carries no money
		// figure and no personal data.
		"GET /api/v1/notification-preferences": true,
		// The family calendar is for every member once its flag is on.
		"GET /api/v1/family/calendar": true,
	}

	callers := []struct {
		name     string
		email    string
		password string
		// alsoAllowed is what this caller may read beyond anyMember.
		alsoAllowed map[string]bool
		// wantChecked is this walk's own re-measured count of refused
		// routes. Its only job is catching a walk that stopped enumerating
		// routes and so asserts nothing. Set it to what the walk reports
		// whenever a guarded read is added; never guess.
		wantChecked int
	}{
		{
			name:  "without Money",
			email: env.limitedEmail, password: env.limitedPassword,
			wantChecked: 18,
		},
		{
			name:  "with Money",
			email: env.moneyLimitedEmail, password: env.moneyLimitedPassword,
			// A limited member holding Money reads the account list with
			// every balance removed (TestAccountsAreRedactedForALimitedMember).
			alsoAllowed: map[string]bool{"GET /api/v1/accounts": true},
			wantChecked: 17,
		},
	}

	routes, ok := env.router.(chi.Routes)
	if !ok {
		t.Fatal("router does not implement chi.Routes")
	}

	for _, caller := range callers {
		t.Run(caller.name, func(t *testing.T) {
			session, _ := env.signIn(t, caller.email, caller.password)

			// seen records every allowlist entry the walk met, so an entry
			// naming a route that no longer exists fails below instead of
			// sitting here as a permission nobody reviews.
			seen := map[string]bool{}
			checked, adminChecked := 0, 0

			err := chi.Walk(routes, func(method, route string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
				if method != http.MethodGet {
					return nil
				}
				if !strings.HasPrefix(route, "/api/v1") {
					return nil // /healthz, /readyz
				}
				key := method + " " + route
				if invitePreAuthRoutes[key] {
					return nil // pre-auth by design -- see invitePreAuthRoutes' doc comment
				}
				if anyMember[key] || caller.alsoAllowed[key] {
					seen[key] = true
					return nil
				}

				rec := env.authedGet(t, fillRouteParams(route), session)

				// The admin subtree is gated on a platform_admins row, not on
				// the household role, and refuses with 404 so the surface
				// looks like a typo to a household member.
				if strings.HasPrefix(route, "/api/v1/admin") {
					if rec.Code != http.StatusNotFound {
						t.Errorf("%s: status = %d, want 404 (body = %s)", key, rec.Code, rec.Body.String())
					} else if body := decodeError(t, rec); body.Error.Code != "NOT_FOUND" {
						t.Errorf("%s: error code = %q, want NOT_FOUND", key, body.Error.Code)
					}
					adminChecked++
					return nil
				}

				if rec.Code != http.StatusForbidden {
					t.Errorf("%s: status = %d, want 403 -- a limited member can read this route; "+
						"guard it, or add it to the allowlist with the reason (body = %s)",
						key, rec.Code, rec.Body.String())
				} else if body := decodeError(t, rec); body.Error.Code != "FORBIDDEN" {
					t.Errorf("%s: error code = %q, want FORBIDDEN", key, body.Error.Code)
				}
				checked++
				return nil
			})
			if err != nil {
				t.Fatalf("chi.Walk: %v", err)
			}

			for key := range anyMember {
				if !seen[key] {
					t.Errorf("allowlist entry %q matches no route -- remove it", key)
				}
			}
			for key := range caller.alsoAllowed {
				if !seen[key] {
					t.Errorf("allowlist entry %q matches no route -- remove it", key)
				}
			}

			t.Logf("checked %d guarded read routes, %d admin read routes", checked, adminChecked)
			if checked < caller.wantChecked {
				t.Fatalf("checked %d guarded read routes, want at least %d -- "+
					"the walk may not be enumerating routes correctly", checked, caller.wantChecked)
			}
			// 7: the flag list, two household reads, two mail reads and two
			// database reads.
			if adminChecked < 7 {
				t.Fatalf("checked %d admin read routes, want at least 7 -- "+
					"the walk is no longer reaching the admin subtree", adminChecked)
			}
		})
	}
}
