package httpadapter

import (
	"crypto/subtle"
	"net/http"
	"time"

	"github.com/andreasoentoro/hearth/api/internal/domain"
)

const csrfCookieName = "csrf_token"
const csrfHeaderName = "X-CSRF-Token"

// requireCSRF implements the double-submit cookie check. GET, HEAD and
// OPTIONS are read-only and skip it entirely; every other method must carry
// a csrf_token cookie whose value matches the X-CSRF-Token header, compared
// with subtle.ConstantTimeCompare. A missing cookie, a missing header, or a
// mismatch all answer 403 CSRF_INVALID identically -- there is nothing a
// caller should learn from telling the three apart.
func requireCSRF(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet, http.MethodHead, http.MethodOptions:
			next.ServeHTTP(w, r)
			return
		}

		header := r.Header.Get(csrfHeaderName)
		cookie, err := r.Cookie(csrfCookieName)
		if err != nil || cookie.Value == "" || header == "" ||
			subtle.ConstantTimeCompare([]byte(cookie.Value), []byte(header)) != 1 {
			WriteError(w, http.StatusForbidden, "CSRF_INVALID", "The CSRF token is missing or does not match.", nil)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// setCSRFCookie writes the csrf_token cookie: deliberately not HttpOnly, so
// the frontend's JavaScript can read it and echo it back in the
// X-CSRF-Token header on every mutating request.
func setCSRFCookie(w http.ResponseWriter, deps Deps, token string, expiresAt time.Time) {
	http.SetCookie(w, &http.Cookie{
		Name:     csrfCookieName,
		Value:    token,
		Path:     "/",
		HttpOnly: false,
		Secure:   deps.Secure,
		SameSite: http.SameSiteLaxMode,
		Expires:  expiresAt,
	})
}

// clearCSRFCookie expires the csrf_token cookie immediately, at sign-out.
func clearCSRFCookie(w http.ResponseWriter, deps Deps) {
	http.SetCookie(w, &http.Cookie{
		Name:     csrfCookieName,
		Value:    "",
		Path:     "/",
		HttpOnly: false,
		Secure:   deps.Secure,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   -1,
	})
}

// requireOwner answers 403 FORBIDDEN unless the caller's Scope carries
// domain.RoleOwner. It gates household member mutations and space creation.
func requireOwner(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		scope, ok := RequestScope(r)
		if !ok || scope.Membership.Role != domain.RoleOwner {
			WriteError(w, http.StatusForbidden, "FORBIDDEN", "Only an owner may do that.", nil)
			return
		}
		next.ServeHTTP(w, r)
	})
}
