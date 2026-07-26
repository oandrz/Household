package httpadapter

import (
	"log/slog"
	"net/http"
	"runtime/debug"

	"github.com/go-chi/chi/v5/middleware"
)

// recoverer replaces chi's own middleware.Recoverer, which -- on a recovered
// panic -- writes a bare `w.WriteHeader(http.StatusInternalServerError)` with
// no body at all: the one response on this API's entire surface that skips
// the standard error envelope every other failure path uses (see
// MapDomainError and logAndWriteInternal in errors.go). That is exactly
// backwards, because a panic is precisely the moment the spec's
// user-quotable request ID matters most -- there is no handler left to
// attach it, and the envelope is the only place a caller could ever see it.
//
// This still does what chi's version does -- log the panic value and a
// stack trace, and re-panic http.ErrAbortHandler unlogged and unhandled so
// the connection aborts exactly as net/http itself expects -- it just also
// writes the same INTERNAL envelope logAndWriteInternal does, with the
// request ID in `details`, instead of nothing.
func recoverer(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			rec := recover()
			if rec == nil {
				return
			}
			if rec == http.ErrAbortHandler {
				// Mirrors middleware.Recoverer: this value means net/http
				// itself wants the connection aborted with no further
				// writes and no log noise, not a real application panic.
				panic(rec)
			}

			reqID := middleware.GetReqID(r.Context())
			slog.Error("panic recovered",
				"panic", rec,
				"request_id", reqID,
				"stack", string(debug.Stack()),
			)

			// A hijacked or Upgrade connection has already left net/http's
			// normal response-writing path by this point; writing to it
			// here would either panic again or corrupt an already-open
			// connection, so this mirrors middleware.Recoverer's own guard.
			if r.Header.Get("Connection") == "Upgrade" {
				return
			}
			WriteError(w, http.StatusInternalServerError, "INTERNAL",
				"Something went wrong. Please try again, or quote this reference if it keeps happening.",
				map[string]any{"requestId": reqID})
		}()
		next.ServeHTTP(w, r)
	})
}
