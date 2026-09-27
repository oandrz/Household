package httpadapter

import (
	"log/slog"
	"net/http"
	"runtime/debug"

	"github.com/go-chi/chi/v5/middleware"
)

// recoverer replaces chi's own middleware.Recoverer, which writes a bare
// 500 with no body on a recovered panic -- the one response on this API
// that skips the standard error envelope every other failure path uses
// (MapDomainError, logAndWriteInternal in errors.go). That is backwards: a
// panic is exactly when the request ID matters most, since no handler is
// left to attach it and the envelope is the only place a caller could see
// it.
//
// This still does what chi's version does -- logs the panic value and a
// stack trace, and re-panics http.ErrAbortHandler unlogged so the
// connection aborts as net/http expects -- it just also writes the same
// INTERNAL envelope logAndWriteInternal does, with the request ID in
// `details`, instead of nothing.
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
			// normal response-writing path; writing to it here would
			// either panic again or corrupt an open connection, so this
			// mirrors middleware.Recoverer's own guard.
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
