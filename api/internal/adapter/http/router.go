package httpadapter

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
)

// Deps carries everything the HTTP layer needs. Handlers receive their
// collaborators through this struct rather than reaching for globals.
type Deps struct {
	Pinger Pinger
}

func NewRouter(deps Deps) http.Handler {
	r := chi.NewRouter()

	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(middleware.Recoverer)

	r.NotFound(func(w http.ResponseWriter, _ *http.Request) {
		WriteError(w, http.StatusNotFound, "NOT_FOUND", "That endpoint does not exist.", nil)
	})
	r.MethodNotAllowed(func(w http.ResponseWriter, _ *http.Request) {
		WriteError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "That method is not allowed here.", nil)
	})

	r.Get("/healthz", handleHealthz)
	r.Get("/readyz", handleReadyz(deps.Pinger))

	return r
}
