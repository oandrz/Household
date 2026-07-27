package httpadapter

import (
	"net"
	"net/http"
	"sync"
	"time"
)

// ipRateLimiter is a fixed-window counter keyed by client IP, used to bound the
// unauthenticated sign-up endpoint.
//
// Sign-up is open to anyone -- a deliberate product decision -- and the
// per-address limit in SignupService is trivially bypassed by varying the
// address. That makes this the thing standing between the SMTP relay and a
// stranger with a loop, not a hardening nicety.
//
// IT IS IN-MEMORY AND THEREFORE PER-PROCESS. A second API replica doubles the
// effective limit, and a restart clears it. Anyone adding a replica must
// replace this with a shared counter -- a signup_attempts table indexed by
// (ip, at) is the obvious move, and SignupService's global daily ceiling
// already reads from the database for exactly this reason. It is in-memory here
// because there is one API container today and a table per request rejected is
// a worse trade at that scale.
type ipRateLimiter struct {
	mu      sync.Mutex
	counts  map[string]int
	window  time.Duration
	limit   int
	resetAt time.Time
	now     func() time.Time
}

// newIPRateLimiter does not call now itself. NewRouter builds the whole route
// tree -- including this limiter -- unconditionally, and some callers build a
// router with a deliberately incomplete Deps to exercise one unrelated route in
// isolation (health_test.go's Deps{Pinger: ...} for /healthz is exactly this);
// calling now() here would panic on a nil deps.Clock before a single request
// had been served. allow (below) seeds resetAt lazily on its own first call
// instead, which costs nothing: nothing can call allow before a real request
// reaches the sign-up route, and by then Clock is always set.
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
		// Whole-map reset rather than per-key expiry: it bounds memory without
		// a sweeper goroutine, and the imprecision at a window boundary does
		// not matter for a limit whose job is to stop a loop. resetAt.IsZero()
		// covers the very first call, when newIPRateLimiter left it unset.
		l.counts = map[string]int{}
		l.resetAt = now.Add(l.window)
	}

	if l.counts[ip] >= l.limit {
		return false
	}
	l.counts[ip]++
	return true
}

// clientIP prefers the address chi's middleware.RealIP has already resolved
// (it rewrites r.RemoteAddr from X-Forwarded-For, and the router installs it),
// falling back to the raw RemoteAddr. The port is stripped so repeat requests
// from one client, which arrive on different ephemeral ports, count together --
// forgetting that makes the limiter count nothing at all.
func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// rateLimitByIP answers 429 when an IP is over its window. This is the one
// place in the sign-up flow that may answer something other than 202, and that
// is safe: the limit is keyed by IP, not by address, so what it reveals is "you
// have sent a lot of requests" -- something the caller already knows -- and
// never anything about whether any particular address is registered.
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
