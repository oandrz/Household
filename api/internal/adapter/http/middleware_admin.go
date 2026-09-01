package httpadapter

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5/middleware"

	"github.com/andreasoentoro/hearth/api/internal/usecase"
)

// adminGrantTTL is how long one re-authentication opens the admin surface for.
// It is deliberately not extended by activity, unlike the session itself (see
// sessionExtendThreshold in middleware_session.go): a long admin session is
// re-authenticated, not renewed silently.
const adminGrantTTL = 30 * time.Minute

// requirePlatformAdmin answers 404 -- not 403 -- to a caller with no
// platform_admins row. A 403 would confirm both that /admin exists and that
// this path is the right one; to everyone else the whole surface must look
// like a typo.
//
// A lookup *failure* is a 500, not a 404. "The database is down" must not read
// as a clean "you are not an admin", or an outage would silently lock the
// operator out with a message saying the page does not exist. That is why the
// error branch below calls logAndWriteInternal directly rather than
// MapDomainError: AdminService.IsPlatformAdmin has already consumed
// domain.ErrNotFound into a plain false (see its own doc comment), so every
// non-nil error it can return is a lookup failure -- but MapDomainError's
// table contains a domain.ErrNotFound case answering 404, and routing through
// it would leave a future wrapped-sentinel path free to turn an outage into
// exactly the clean "no" this comment forbids. There is no error from this
// call a caller should ever learn anything from.
//
// The limit of the 404, stated so nobody rediscovers it as a bug: it hides
// the surface from AUTHENTICATED non-admins only. An unauthenticated caller
// gets 401, not 404, because requireSession necessarily runs first --
// TestEveryProtectedRouteRejectsAnUnauthenticatedCaller requires exactly
// that of every route here -- so a stranger with no credentials can already
// tell /admin/flags from an unrouted path. That is accepted, not overlooked:
// the existence of an admin surface is not the secret. WHO holds it is, and
// a later task puts isPlatformAdmin into GET /auth/me for every caller
// anyway. What this guard buys is that a signed-in household member poking
// at the API learns nothing, and that nobody can enumerate the subtree's
// shape by watching which paths answer differently.
func requirePlatformAdmin(deps Deps) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			scope, ok := RequestScope(r)
			if !ok {
				WriteError(w, http.StatusUnauthorized, "UNAUTHENTICATED", "Sign in required.", nil)
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
// reads included. It is middleware rather than a call in each handler because
// a handler that forgets is the failure mode, and middleware cannot forget.
//
// The row is written before the handler runs, so a handler that panics or
// times out still leaves a trace of the attempt. Target is the full request
// path, never a body, a password or a row value: chi only populates a
// route's URL parameters (the {key}, {householdID} kind) once its own
// tree.FindRoute has matched, which happens inside routeHTTP -- the last
// step of this subtree's middleware chain, run after requirePlatformAdmin,
// this middleware and requireCSRF have all already executed. A row written
// here, before the handler runs, therefore cannot carry those parameters;
// the path itself already contains every value they would have held, so
// Detail is left an empty object rather than populated with data that isn't
// there yet.
//
// requireCSRF runs inside this middleware, not outside it, so a request
// refused for a missing or mismatched CSRF token has already left its row.
// That is the point rather than a side effect: a cross-site forgery aimed at
// a real platform admin is precisely what admin_audit_log exists to make
// visible, and refusing one silently would hide the one attack the log is
// for. See router.go's /admin subtree for why the guards sit in that order.
func auditAdmin(deps Deps) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			scope, ok := RequestScope(r)
			if !ok {
				WriteError(w, http.StatusUnauthorized, "UNAUTHENTICATED", "Sign in required.", nil)
				return
			}

			if err := deps.Admin.RecordAudit(r.Context(), usecase.AdminAuditEntry{
				ActorUserID: scope.UserID,
				Action:      r.Method + " " + r.URL.Path,
				Target:      r.URL.Path,
				Detail:      map[string]any{},
				// r.RemoteAddr here is middleware.RealIP's rewrite of it, read
				// from headers -- so this column is only ever as trustworthy as
				// the proxy in front of the service. web/nginx.conf is what
				// makes it trustworthy today, and says so at length: it blanks
				// any client-supplied True-Client-IP and sets X-Real-IP from
				// $remote_addr (chi takes the first non-empty of True-Client-IP,
				// X-Real-IP, X-Forwarded-For, with no trusted-proxy list of its
				// own), while nginx's real_ip module trusts X-Forwarded-For only
				// from 172.28.0.0/16 with real_ip_recursive off. Run this
				// service with anything else in front of it, or with nothing,
				// and the attacker chooses what this column says.
				IP: r.RemoteAddr,
				At: deps.Clock.Now(),
			}); err != nil {
				// An unwritable audit log closes the surface. The alternative
				// -- serve the request and log a warning -- is an admin
				// surface that works fine with auditing silently off, which is
				// the exact state this table exists to make impossible.
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

// writeNotFound answers exactly what the router's own NotFound handler does,
// so a hidden admin route and a genuinely absent one are byte-identical.
func writeNotFound(w http.ResponseWriter) {
	WriteError(w, http.StatusNotFound, "NOT_FOUND", "That endpoint does not exist.", nil)
}
