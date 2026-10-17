package httpadapter

import (
	"context"
	"net/http"
	"time"
)

// RequestDeadline bounds one request inside the API, counted from arrival,
// so time spent waiting in a household's line counts too. The slowest
// normal request is a sign-in waiting in the password hasher's queue; see
// crypto.WaitersPerSlot. It sits inside a chain that must fire innermost
// first (statement timeout < this < the server's WriteTimeout < nginx's
// proxy_read_timeout), so a slow request gets this API's JSON answer, never
// nginx's HTML one.
const RequestDeadline = 15 * time.Second

func withRequestDeadline(d time.Duration) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx, cancel := context.WithTimeout(r.Context(), d)
			defer cancel()
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}
