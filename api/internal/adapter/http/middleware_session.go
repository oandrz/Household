package httpadapter

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/andreasoentoro/hearth/api/internal/domain"
)

// scopeKey is the request-context key requireSession stores a Scope under.
// It is an unexported type so no other package can collide with it.
type scopeKey struct{}

// Scope is the authenticated caller's identity, set by requireSession and
// read by every downstream middleware (requireCapability, requireOwner) and
// handler via RequestScope.
type Scope struct {
	UserID      string
	HouseholdID string
	Membership  domain.Membership
	// AuthVia says which credential spoke: a browser session cookie or a
	// personal API token. Most guards do not care -- both resolve to the
	// same membership -- but requireCSRF skips its check for a token and
	// requireCookieSession refuses one. Any switch over it must refuse by
	// default; see authVia's own comment.
	AuthVia authVia
	// Flags is this household's resolved answer for every flag this build
	// defines -- every key present, so a reader never has to interpret an
	// absence.
	Flags domain.FlagSet
}

// RequestScope reads the Scope requireSession placed on r's context. The
// bool is false for any request that never passed through requireSession.
func RequestScope(r *http.Request) (Scope, bool) {
	scope, ok := r.Context().Value(scopeKey{}).(Scope)
	return scope, ok
}

// requireScope is RequestScope plus the refusal, for handlers that have
// nothing sensible to do without a scope -- which is all of them.
//
// `scope, _ := RequestScope(r)` looks harmless but isn't: the discarded bool
// leaves an empty household id that every query below then trusts, answering
// a wrong-but-loud 404 on a list route or a silent 200-with-no-rows on a
// report route that tells an unauthenticated caller they own nothing. Fail
// closed on values you did not construct.
//
// The guard chain already makes this unreachable; it is the second lock, not
// the first, and it costs two lines a handler.
func requireScope(w http.ResponseWriter, r *http.Request) (Scope, bool) {
	scope, ok := RequestScope(r)
	if !ok {
		WriteError(w, http.StatusUnauthorized, "UNAUTHENTICATED", "Sign in required.", nil)
		return Scope{}, false
	}
	return scope, true
}

// authVia is the credential a request authenticated with. It is a named
// type rather than a bool so a third kind (a signed webhook, say) cannot be
// added without every switch over it being revisited: the zero value means
// "not set", and anything reading it must treat that as a refusal.
type authVia string

const (
	authViaSession authVia = "session"
	authViaToken   authVia = "token"
)

// bearerPrefix is the Authorization scheme a token arrives under. A header
// with any other scheme is a malformed credential and is refused; it never
// falls through to the cookie.
const bearerPrefix = "Bearer "

const sessionCookieName = "hearth_session"

// SessionTTL is how long a session lives from issue or extension: 30 days,
// per the design's global constraints. cmd/api/main.go hands this same value
// to both AuthDeps.SessionTTL and InviteDeps.SessionTTL, so a session from
// sign-in and one from accepting an invite expire on the same schedule;
// requireSession's own extension (sessionExtendThreshold) resets a session
// to that same horizon.
const SessionTTL = 30 * 24 * time.Hour

// sessionExtendThreshold governs how often an active session's expiry is
// actually rewritten -- extending on every request would mean a write per
// API call for the life of the session, exactly the write amplification a
// threshold exists to avoid. Expiry is pushed back out only once it has
// drifted within a day of lapsing, so a session used daily gets roughly one
// extension every 29 days, not one per request.
const sessionExtendThreshold = 24 * time.Hour

// sessionTouchInterval is how stale sessions.last_seen_at may be before a
// request refreshes it. One write per session-hour rather than one per
// request: the column answers "was this household active this week", which
// an hour's resolution serves completely, and this middleware runs on every
// authenticated request. Compare sessionExtendThreshold above, which bounds
// a different write for the same reason.
const sessionTouchInterval = time.Hour

// requireSession reads the hearth_session cookie, resolves it to a live
// session, loads the caller's membership, and stores both as a Scope on the
// request context. A missing or unresolvable cookie -- absent, unknown,
// expired, or revoked -- answers 401 UNAUTHENTICATED and never calls next.
//
// An Authorization header, when present, is the only credential considered:
// the request is handed to requireToken and the cookie is never read, so a
// bad token beside a good cookie is still a 401. A caller who sent a token
// meant to use it, and a silent fallback would hide a revoked or expired
// token behind a browser that happens to be signed in.
func requireSession(deps Deps) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Header.Get("Authorization") != "" {
				requireToken(deps, next).ServeHTTP(w, r)
				return
			}
			cookie, err := r.Cookie(sessionCookieName)
			if err != nil || cookie.Value == "" {
				WriteError(w, http.StatusUnauthorized, "UNAUTHENTICATED", "Sign in required.", nil)
				return
			}

			ctx := r.Context()
			hash := deps.Tokens.HashToken(cookie.Value)
			record, err := deps.Sessions.ByTokenHash(ctx, hash)
			if err != nil {
				WriteError(w, http.StatusUnauthorized, "UNAUTHENTICATED", "Sign in required.", nil)
				return
			}

			// ByUser cannot take a household scope (see
			// usecase.MembershipRepository.ByUser's doc comment), so the
			// result is cross-checked against the session's own HouseholdID
			// rather than trusted blindly. This defends against a future
			// multi-household user; the current schema's UNIQUE constraint
			// should never actually trigger it.
			membership, err := deps.Memberships.ByUser(ctx, record.UserID)
			if err != nil || membership.HouseholdID != record.HouseholdID {
				WriteError(w, http.StatusUnauthorized, "UNAUTHENTICATED", "Sign in required.", nil)
				return
			}

			now := deps.Clock.Now()
			if record.ExpiresAt.Sub(now) < sessionExtendThreshold {
				newExpiry := now.Add(SessionTTL)
				if err := deps.Sessions.Extend(ctx, hash, newExpiry); err != nil {
					// Best-effort: a failure here must not turn an
					// already-authenticated request into a 401. The session
					// keeps its existing (still-live, per the ByTokenHash
					// lookup above) expiry and gets another chance to extend
					// on the next request.
					slog.Warn("failed to extend session", "error", err)
				} else {
					// Extending the database row is only half of "extended on
					// use": the browser's hearth_session cookie was set once,
					// at sign-in, with a fixed Expires, and would still
					// discard it on that schedule no matter how actively the
					// session was used -- Extend above changed the row, not
					// the cookie. csrf_token has the same fixed-lifetime
					// problem. Both are re-issued here with the same token
					// values (only the expiry moves), so a browser still
					// actively presenting this session never loses it.
					setSessionCookie(w, deps, cookie.Value, newExpiry)
					if csrfCookie, err := r.Cookie(csrfCookieName); err == nil {
						setCSRFCookie(w, deps, csrfCookie.Value, newExpiry)
					}
				}
			}

			if record.LastSeenAt == nil || now.Sub(*record.LastSeenAt) >= sessionTouchInterval {
				if err := deps.Sessions.Touch(ctx, hash, now); err != nil {
					// Best-effort, exactly like Extend above: a usage
					// timestamp that could not be written must not fail an
					// authenticated request. The next request tries again.
					slog.Warn("failed to touch session", "error", err)
				}
			}

			// Flags are resolved per request, not cached. One box, few
			// households, and a cache that is stale for a minute after the
			// operator flips a switch is a worse defect than one indexed
			// query. A cache belongs here when a measurement asks for one.
			flags, err := deps.Admin.FlagsFor(ctx, record.HouseholdID)
			if err != nil {
				// logAndWriteInternal, not MapDomainError -- see
				// requirePlatformAdmin's comment in middleware_admin.go for
				// why a lookup failure must never read as domain.ErrNotFound's
				// 404. The stakes are larger here: 404 means "this feature is
				// hidden" on every authenticated route, not just the admin
				// subtree, so a flags lookup that produced ErrNotFound through
				// MapDomainError would tell every caller the whole product had
				// been switched off.
				logAndWriteInternal(w, r, err)
				return
			}

			scope := Scope{UserID: record.UserID, HouseholdID: record.HouseholdID, Membership: membership, Flags: flags, AuthVia: authViaSession}
			// The admin grant is put on the context from the same session
			// record the scope is built from, so the two can never disagree
			// about which session is speaking. It stays out of Scope
			// because it isn't part of the caller's household identity --
			// every downstream consumer of Scope (requireCapability,
			// requireOwner, every handler) would otherwise gain a field
			// that means nothing to it.
			ctx = withAdminGrant(context.WithValue(ctx, scopeKey{}, scope), record.AdminGrantExpiresAt)
			ctx = withSessionHash(ctx, hash)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// setSessionCookie writes the hearth_session cookie: HttpOnly always, Secure
// per deps.Secure (false only in development), SameSite=Lax, Path=/.
func setSessionCookie(w http.ResponseWriter, deps Deps, token string, expiresAt time.Time) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		Secure:   deps.Secure,
		SameSite: http.SameSiteLaxMode,
		Expires:  expiresAt,
	})
}

// clearSessionCookie expires the hearth_session cookie immediately, at
// sign-out.
func clearSessionCookie(w http.ResponseWriter, deps Deps) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		Secure:   deps.Secure,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   -1,
	})
}
