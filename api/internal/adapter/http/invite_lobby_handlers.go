package httpadapter

import (
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
)

// The owner's side of the invite lobby: getting a fresh link (which is also
// "Not them") and letting the knocker in. The list and withdraw routes live
// in pending_invite_handlers.go; the public, pre-sign-in routes live in
// invite_handlers.go.

type inviteLinkDTO struct {
	Link      string    `json:"link"`
	ExpiresAt time.Time `json:"expiresAt"`
}

// handleNewInviteLink serves both "Get a new link" and "Not them": the
// effects are identical (InviteService.NewLink's own doc comment), so
// there is one route. Its guards live on the route (router.go): owner,
// CSRF, and a browser session -- the same three that guard invite create
// and withdraw.
func handleNewInviteLink(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		scope, ok := requireScope(w, r)
		if !ok {
			return
		}
		link, err := deps.Invites.NewLink(r.Context(), scope.HouseholdID, chi.URLParam(r, "id"))
		if err != nil {
			MapDomainError(w, r, err)
			return
		}
		WriteJSON(w, http.StatusOK, inviteLinkDTO{Link: link.URL, ExpiresAt: link.ExpiresAt})
	}
}
