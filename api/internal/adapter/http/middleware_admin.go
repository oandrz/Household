package httpadapter

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5/middleware"

	"github.com/andreasoentoro/hearth/api/internal/usecase"
)

// adminGrantTTL is how long one re-authentication opens the admin surface
// for. It does not extend with activity, unlike the session itself
// (sessionExtendThreshold in middleware_session.go): a long admin session is
// re-authenticated, not renewed silently.
const adminGrantTTL = 30 * time.Minute

// requirePlatformAdmin answers 404, not 403, to a caller with no
// platform_admins row -- a 403 would confirm /admin exists, so a non-admin
// must see it as a routing miss.
//
// A lookup *failure* answers 500, not 404, via logAndWriteInternal:
// IsPlatformAdmin already turns domain.ErrNotFound into a plain false,
// so any error here is a real failure. Don't route it through
// MapDomainError -- its ErrNotFound row answers 404, so a future wrapped
// sentinel could turn an outage into a clean "not an admin" and silently
// lock the operator out. No error from this call should teach a caller
// anything.
//
// The 404 hides the surface only from authenticated non-admins.
// requireSession runs first, so an unauthenticated caller gets 401 instead
// (TestEveryProtectedRouteRejectsAnUnauthenticatedCaller requires this for
// every route) -- accepted, because WHO holds /admin is the secret, not
// whether it exists; GET /auth/me exposes isPlatformAdmin to every caller
// anyway. What the 404 buys: a signed-in member poking at the API learns
// nothing, and nobody can map the subtree by watching which paths answer
// differently.
//
// Being an admin is not enough: the request must also carry a browser
// session (ADR 7 rule 3) -- otherwise an admin's own personal API token
// would pass and reach the re-auth password check behind this guard.
// Anything whose AuthVia is not exactly authViaSession gets the same
// non-admin 404, before the lookup, so it leaves no trace behind the
// guard. Don't narrow this to "is a token": an unset AuthVia (see
// authVia's doc comment) and any future credential kind must be refused
// too until someone decides they belong here.
func requirePlatformAdmin(deps Deps) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			scope, ok := RequestScope(r)
			if !ok {
				WriteError(w, http.StatusUnauthorized, "UNAUTHENTICATED", "Sign in required.", nil)
				return
			}
			if scope.AuthVia != authViaSession {
				writeNotFound(w)
				return
			}
			isAdmin, err := deps.Admin.IsPlatformAdmin(r.Context(), scope.UserID)
			if err != nil {
				logAndWriteInternal(w, r, err)
				return
			}
			if !isAdmin {
				writeNotFound(w)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// auditAdmin writes one admin_audit_log row per request that reaches it,
// reads included. It is middleware, not a per-handler call, because a
// forgotten call is a handler bug and middleware cannot forget.
//
// The row is written before the handler runs, so a panic or timeout still
// leaves a trace. Target is the request path, never a body, a password or
// a row value. Detail never carries route parameters -- chi only
// populates those once routing has matched, which happens after this
// middleware chain runs -- but the request path already holds every value
// they would carry; Detail instead carries the raw query string when
// present, or an empty object.
//
// requireCSRF runs inside this middleware, not outside it, so a request it
// refuses still leaves its audit row: a forgery aimed at a real admin is
// exactly what this log exists to catch, and refusing it silently would
// hide the attack. See router.go's /admin subtree for the guard order.
func auditAdmin(deps Deps) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			scope, ok := RequestScope(r)
			if !ok {
				WriteError(w, http.StatusUnauthorized, "UNAUTHENTICATED", "Sign in required.", nil)
				return
			}

			// Only the query string is available this early, before
			// routing; a URL with none leaves the row {}.
			detail := map[string]any{}
			if r.URL.RawQuery != "" {
				detail["query"] = r.URL.RawQuery
			}

			if err := deps.Admin.RecordAudit(r.Context(), usecase.AdminAuditEntry{
				ActorUserID: scope.UserID,
				Action:      r.Method + " " + r.URL.Path,
				Target:      r.URL.Path,
				Detail:      detail,
				// clientIP: the TCP peer, or nginx's X-Real-IP when that peer
				// is inside TRUSTED_PROXY_CIDRS (trustedProxyRealIP), so this
				// column is only as trustworthy as that list. In production the
				// list is the hearth Docker network; nginx overwrites X-Real-IP
				// with what its own real_ip module resolved (X-Forwarded-For
				// trusted only from 172.28.0.0/16, real_ip_recursive off),
				// though a container on that network could still claim any
				// address, which nginx.conf's own comment accepts. Outside the
				// list, this is the address that actually connected, never a
				// header.
				IP: clientIP(r),
				At: deps.Clock.Now(),
			}); err != nil {
				// An unwritable audit log closes the surface, rather than
				// serving the request with auditing silently off -- exactly
				// the state this table exists to make impossible.
				slog.ErrorContext(r.Context(), "admin audit write failed",
					"request_id", middleware.GetReqID(r.Context()), "error", err)
				WriteError(w, http.StatusServiceUnavailable, "AUDIT_UNAVAILABLE",
					"The admin surface is closed because its audit log cannot be written.", nil)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// requireAdminGrant refuses until the caller has re-entered their password
// within adminGrantTTL. Its 401 carries ADMIN_REAUTH_REQUIRED rather than
// UNAUTHENTICATED so the frontend can show a password prompt instead of
// bouncing the operator all the way out to sign-in.
func requireAdminGrant(deps Deps) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			grant, ok := RequestAdminGrant(r)
			if !ok || grant == nil || !grant.After(deps.Clock.Now()) {
				WriteError(w, http.StatusUnauthorized, "ADMIN_REAUTH_REQUIRED",
					"Confirm your password to open the admin surface.", nil)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// adminGrantKey is the request-context key requireSession stores the session
// row's admin grant under. Unexported so no other package can collide.
type adminGrantKey struct{}

// RequestAdminGrant reads the grant expiry requireSession placed on r. The
// bool is false for any request that never passed through requireSession.
func RequestAdminGrant(r *http.Request) (*time.Time, bool) {
	grant, ok := r.Context().Value(adminGrantKey{}).(*time.Time)
	return grant, ok
}

func withAdminGrant(ctx context.Context, expiresAt *time.Time) context.Context {
	return context.WithValue(ctx, adminGrantKey{}, expiresAt)
}

// sessionHashKey is the request-context key requireSession stores the
// authenticated session's token hash under. Unexported, like adminGrantKey.
type sessionHashKey struct{}

// requestSessionHash reads the token hash of the session this request
// authenticated with. The bool is false for anything that did not
// authenticate with a session cookie, a personal API token included, so a
// caller needing "the session speaking" is never handed a cookie the
// request merely carried alongside some other credential (a cookie sent
// beside a token can never be the one granted).
func requestSessionHash(r *http.Request) ([]byte, bool) {
	hash, ok := r.Context().Value(sessionHashKey{}).([]byte)
	return hash, ok && len(hash) > 0
}

func withSessionHash(ctx context.Context, hash []byte) context.Context {
	return context.WithValue(ctx, sessionHashKey{}, hash)
}

// writeNotFound answers exactly what the router's own NotFound handler does,
// so a hidden admin route and a genuinely absent one are byte-identical.
func writeNotFound(w http.ResponseWriter) {
	WriteError(w, http.StatusNotFound, "NOT_FOUND", "That endpoint does not exist.", nil)
}
