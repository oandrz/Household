package httpadapter

import (
	"context"
	"log/slog"
	"net/http"
)

// Pinger reports whether a backing store is reachable.
type Pinger interface {
	Ping(ctx context.Context) error
}

// handleHealthz answers liveness. It must never touch the database: an
// unreachable database is a readiness problem, not a reason to restart.
func handleHealthz(w http.ResponseWriter, _ *http.Request) {
	WriteJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func handleReadyz(p Pinger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if err := p.Ping(r.Context()); err != nil {
			slog.Warn("readiness check failed", "error", err)
			WriteError(w, http.StatusServiceUnavailable, "NOT_READY", "The service is not ready to accept traffic.", nil)
			return
		}
		WriteJSON(w, http.StatusOK, map[string]string{"status": "ready"})
	}
}
