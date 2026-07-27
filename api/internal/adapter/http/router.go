package httpadapter

import (
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/andreasoentoro/hearth/api/internal/usecase"
)

// signUpRequestsPerIPPerHour bounds one client's sign-up requests. The
// per-address limit in SignupService is bypassed by varying the address; this is
// what actually bounds outbound mail. 10 is generous for a household setting
// itself up (one request, maybe a couple of retries) and far below what a loop
// needs to be useful.
const signUpRequestsPerIPPerHour = 10

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
	Signups     *usecase.SignupService
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
	// recoverer, not middleware.Recoverer -- see its own doc comment in
	// middleware_recoverer.go for why chi's version (a bare, bodyless 500)
	// is wrong for this API.
	r.Use(recoverer)

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

			// Public: no session exists yet, and no CSRF cookie either.
			//
			// The per-IP limiter wraps only the request endpoint. The preview
			// and complete endpoints need a token that was mailed to a real
			// address, so they are not a path to unbounded mail; the request
			// endpoint is.
			auth.Group(func(su chi.Router) {
				// deps.Clock.Now, not a bound method value: forming the method
				// value would read deps.Clock immediately, while NewRouter is
				// still building the route tree -- and some callers (health_test.go's
				// Deps{Pinger: ...}) build a router with every other field left
				// zero, deliberately, because they only exercise /healthz. A closure
				// defers that read to request time, when every route's Deps is
				// actually complete.
				now := func() time.Time { return deps.Clock.Now() }
				su.Use(rateLimitByIP(newIPRateLimiter(signUpRequestsPerIPPerHour, time.Hour, now)))
				su.Post("/sign-up", handleSignUp(deps))
			})
			auth.Get("/sign-up/{token}", handleSignUpPreview(deps))
			auth.Post("/sign-up/{token}/complete", handleCompleteSignUp(deps))

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

		// Public: the sign-up form reads this before any session exists, and
		// Settings reads it after one. One list, served rather than duplicated
		// in the frontend.
		api.Get("/currencies", handleListCurrencies())

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
