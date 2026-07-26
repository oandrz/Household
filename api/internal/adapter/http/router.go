package httpadapter

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/andreasoentoro/hearth/api/internal/usecase"
)

// Deps carries everything the HTTP layer needs. Handlers receive their
// collaborators through this struct rather than reaching for globals.
//
// Users and Memberships are raw repository ports, not routed through a
// service, because no Task 12-15 service exposes what this layer needs from
// them directly: GET /auth/me assembles its bundle from the user's own
// profile (Users.ByID) and the session middleware resolves a caller's
// Membership (Memberships.ByUser) to populate Scope. Every other Deps field
// mirrors what the task brief names.
type Deps struct {
	Pinger      Pinger
	Auth        *usecase.AuthService
	Invites     *usecase.InviteService
	Members     *usecase.MemberService
	Households  *usecase.HouseholdService
	Users       usecase.UserRepository
	Memberships usecase.MembershipRepository
	Sessions    usecase.SessionRepository
	Tokens      usecase.TokenGenerator
	Clock       usecase.Clock
	// Secure controls the Secure flag on the session and CSRF cookies. It is
	// !cfg.IsDevelopment(): false only in development, so cookies still work
	// over plain http on localhost.
	Secure bool
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

	r.Route("/api/v1", func(api chi.Router) {
		api.Route("/auth", func(auth chi.Router) {
			// Public: no session exists yet.
			auth.Post("/sign-in", handleSignIn(deps))
			auth.Post("/magic-link", handleRequestMagicLink(deps))
			auth.Post("/magic-link/consume", handleConsumeMagicLink(deps))

			auth.Group(func(g chi.Router) {
				g.Use(requireSession(deps))
				g.Get("/me", handleMe(deps))

				g.Group(func(m chi.Router) {
					m.Use(requireCSRF)
					m.Post("/sign-out", handleSignOut(deps))
				})
			})
		})

		// Public: reached before the caller has any session at all.
		api.Get("/invites/{token}", handleInvitePreview(deps))
		api.Post("/invites/{token}/accept", handleAcceptInvite(deps))

		api.Group(func(g chi.Router) {
			g.Use(requireSession(deps))

			g.Get("/household", handleGetHousehold(deps))
			g.Get("/household/members", handleListMembers(deps))
			g.Get("/spaces", handleListSpaces(deps))
			g.Get("/notification-preferences", handleGetNotificationPreferences(deps))

			g.Group(func(m chi.Router) {
				m.Use(requireCSRF)

				m.Group(func(o chi.Router) {
					o.Use(requireOwner)
					o.Patch("/household", handleUpdateHousehold(deps))
					o.Patch("/notification-preferences", handleUpdateNotificationPreferences(deps))
					o.Post("/household/members/invite", handleInviteMember(deps))
					o.Patch("/household/members/{id}", handleUpdateMember(deps))
					o.Delete("/household/members/{id}", handleRemoveMember(deps))
					o.Post("/spaces", handleCreateSpace(deps))
				})
			})
		})
	})

	return r
}
