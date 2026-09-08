package httpadapter

import (
	"context"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/andreasoentoro/hearth/api/internal/domain"
)

// tokenTouchInterval is how stale api_tokens.last_used_at may be before a
// request refreshes it -- the same one-write-per-hour rule sessions use, for
// the same reason (middleware_session.go's sessionTouchInterval).
const tokenTouchInterval = time.Hour

// requireToken is requireSession's other half: the request carried an
// Authorization header, so this resolves it to a live personal API token and
// builds the same Scope a session would get, with AuthVia set to token.
//
// Every failure -- wrong scheme, wrong prefix, unknown, expired, revoked,
// membership gone -- answers the one 401 requireSession answers, so a probe
// learns nothing from the difference. No admin grant is placed on the
// context: that lives on a session row, so a token can never reach /admin.
func requireToken(deps Deps, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, ok := strings.CutPrefix(r.Header.Get("Authorization"), bearerPrefix)
		if !ok || !strings.HasPrefix(raw, domain.APITokenPrefix) {
			WriteError(w, http.StatusUnauthorized, "UNAUTHENTICATED", "Sign in required.", nil)
			return
		}
		ctx := r.Context()
		token, err := deps.APITokenRepo.ByTokenHash(ctx, deps.Tokens.HashToken(raw))
		if err != nil {
			WriteError(w, http.StatusUnauthorized, "UNAUTHENTICATED", "Sign in required.", nil)
			return
		}
		// The same cross-check requireSession makes: the membership found
		// by user must be the household the token names.
		membership, err := deps.Memberships.ByUser(ctx, token.UserID)
		if err != nil || membership.HouseholdID != token.HouseholdID {
			WriteError(w, http.StatusUnauthorized, "UNAUTHENTICATED", "Sign in required.", nil)
			return
		}

		now := deps.Clock.Now()
		if token.LastUsedAt == nil || now.Sub(*token.LastUsedAt) >= tokenTouchInterval {
			if err := deps.APITokenRepo.Touch(ctx, token.ID, now); err != nil {
				// Best-effort, exactly like a session touch: a timestamp
				// that could not be written must not fail the request.
				slog.Warn("failed to touch api token", "error", err)
			}
		}

		flags, err := deps.Admin.FlagsFor(ctx, token.HouseholdID)
		if err != nil {
			logAndWriteInternal(w, r, err)
			return
		}
		scope := Scope{UserID: token.UserID, HouseholdID: token.HouseholdID, Membership: membership, Flags: flags, AuthVia: authViaToken}
		next.ServeHTTP(w, r.WithContext(context.WithValue(ctx, scopeKey{}, scope)))
	})
}

// requireCookieSession refuses a request that authenticated with a token.
// It guards the two routes that mint and revoke tokens: a leaked token must
// not be able to make itself permanent, and revocation is a decision for
// the person, from a browser they signed in to. The refusing default is the
// point -- an unset AuthVia is not a session.
func requireCookieSession(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		scope, ok := RequestScope(r)
		if !ok {
			WriteError(w, http.StatusUnauthorized, "UNAUTHENTICATED", "Sign in required.", nil)
			return
		}
		switch scope.AuthVia {
		case authViaSession:
			next.ServeHTTP(w, r)
		default:
			WriteError(w, http.StatusForbidden, "SESSION_REQUIRED",
				"This action needs a signed-in browser session, not an API token.", nil)
		}
	})
}
