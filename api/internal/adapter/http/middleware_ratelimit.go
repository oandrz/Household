package httpadapter

import (
	"net"
	"net/http"
	"sync"
	"time"
)

// ipRateLimiter is a fixed-window counter keyed by client IP. Each
// unauthenticated endpoint that costs something -- sign-up and its
// completion, sign-in, magic-link request, Telegram start, accepting an
// invite -- holds its own instance (router.go); the reasoning below is
// written for sign-up and applies to all of them.
//
// Sign-up is open to anyone by design, and SignupService's per-address
// limit is trivially bypassed by varying the address, so this is what
// actually stands between the SMTP relay and a stranger with a loop.
//
// IT IS IN-MEMORY AND THEREFORE PER-PROCESS: a second replica doubles the
// effective limit, and a restart clears it. Replacing it with a shared
// counter (a signup_attempts table keyed by (ip, at), the way
// SignupService's daily ceiling already works) is the fix once there is
// more than one API container; in-memory is the right trade only at
// today's scale of one.
type ipRateLimiter struct {
	mu      sync.Mutex
	counts  map[string]int
	window  time.Duration
	limit   int
	resetAt time.Time
	now     func() time.Time
}

// newIPRateLimiter does not call now itself: NewRouter builds the whole
// route tree, including this limiter, unconditionally, and some callers
// build a router with a deliberately incomplete Deps to exercise one
// unrelated route in isolation (health_test.go's Deps{Pinger: ...} for
// /healthz is exactly this) -- calling now() here would panic on a nil
// deps.Clock before a single request had been served. allow (below) seeds
// resetAt lazily on its own first call instead, which costs nothing:
// nothing can call allow before a real request reaches the sign-up route,
// by which point Clock is always set.
func newIPRateLimiter(limit int, window time.Duration, now func() time.Time) *ipRateLimiter {
	return &ipRateLimiter{
		counts: map[string]int{},
		window: window,
		limit:  limit,
		now:    now,
	}
}

// allow reports whether this IP may proceed, and counts the request.
func (l *ipRateLimiter) allow(ip string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()

	if now := l.now(); l.resetAt.IsZero() || !now.Before(l.resetAt) {
		// Whole-map reset, not per-key expiry: it bounds memory with no
		// sweeper goroutine, and the imprecision at a window boundary
		// doesn't matter for a limit whose job is only to stop a loop.
		// resetAt.IsZero() covers the very first call, before
		// newIPRateLimiter has ever set it.
		l.counts = map[string]int{}
		l.resetAt = now.Add(l.window)
	}

	if l.counts[ip] >= l.limit {
		return false
	}
	l.counts[ip]++
	return true
}

// signInAttemptsPerIPPerWindow is generous next to the per-household
// lockout (3 failures), because one address can be a shared office or
// carrier exit. Its job is not to stop guessing -- the lockout does that --
// but to stop one client running argon2 as fast as it can send requests;
// crypto.Argon2Hasher's slots and queue cap whatever gets past it.
const (
	signInAttemptsPerIPPerWindow = 20
	signInLimitWindow            = 15 * time.Minute
)

// signUpCompletionsPerIPPerWindow and inviteAcceptsPerIPPerWindow limit the
// two public routes that turn a new password into a hash. A link from a
// mail is not a limit on its own: until the first request finishes, every
// request carrying the same link gets as far as the hasher.
//
// The numbers are sign-in's, for sign-in's reason: one address can be a
// whole home or office, a real person sends a handful of these, and the
// job is only to stop one client running argon2 in a loop. Only someone on
// the same address can use an allowance up.
//
// TestHashRouteLimitsLeaveTheHasherRoomForEveryoneElse ties these to the
// hasher's size; raise one and that test says what it costs.
const (
	signUpCompletionsPerIPPerWindow = 20
	inviteAcceptsPerIPPerWindow     = 20
	passwordSetupLimitWindow        = 15 * time.Minute
)

// passwordChangesPerIPPerWindow limits POST /auth/password. A caller who
// knows the password could otherwise repeat a check and a hash at the
// household allowance's 10 a second; the per-person guess counter only
// stops wrong guesses.
const passwordChangesPerIPPerWindow = 10

// magicLinkRequestsPerIPPerHour matches the reasoning of sign-up's limit: the
// per-address limit in AuthService is trivially bypassed by varying the
// address, so this is what stands between the mail relay and a loop.
const magicLinkRequestsPerIPPerHour = 10

// telegramStartsPerIPPerHour is more generous than the sign-up limit because
// this endpoint mails nothing and costs one small row -- but it is limited
// all the same, since it is reachable without a session.
const telegramStartsPerIPPerHour = 20

// clientIP is the address the per-IP limiters and the admin audit log use:
// r.RemoteAddr, already replaced with nginx's X-Real-IP by trustedProxyRealIP
// when, and only when, the request came from a trusted proxy. The port is
// stripped so repeat requests from one client -- which arrive on different
// ephemeral ports -- count together; forgetting that would make the
// limiter count nothing at all.
func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// rateLimitByIP answers 429 when an IP is over its window -- the one place
// in the sign-up flow that may answer something other than 202. That is
// safe: keyed by IP, not by address, so all it reveals is "you have sent a
// lot of requests," something the caller already knows, never whether any
// particular address is registered.
func rateLimitByIP(l *ipRateLimiter) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !l.allow(clientIP(r)) {
				WriteError(w, http.StatusTooManyRequests, "RATE_LIMITED",
					"Too many requests. Try again later.", nil)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
