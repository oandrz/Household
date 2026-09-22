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

// admitResultDTO carries the new member and whether their sign-in link
// actually went out. There is no code field anywhere on this route's
// request: the four digits are compared by eye and accepted by no endpoint
// (spec decision 3) -- TestTheAdmitRequestHasNoCodeField pins that.
type admitResultDTO struct {
	Member     admittedMemberDTO `json:"member"`
	SignInSent bool              `json:"signInSent"`
}

// admittedMemberDTO mirrors memberViewDTO's shape (member_handlers.go): ID
// is the membership id, the same identifier the member list and every other
// member-shaped response use.
type admittedMemberDTO struct {
	ID           string   `json:"id"`
	Name         string   `json:"name"`
	Role         string   `json:"role"`
	Capabilities []string `json:"capabilities"`
}

// handleAdmitInvite reads no body at all -- there is nothing for a request
// to carry. Its guards live on the route (router.go): owner, CSRF and a
// browser session, the same three handleNewInviteLink's own comment
// describes, because the deciding click must sit where a stolen link
// cannot reach (spec decision 4).
func handleAdmitInvite(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		scope, ok := requireScope(w, r)
		if !ok {
			return
		}
		member, err := deps.Invites.Admit(r.Context(), scope.HouseholdID, chi.URLParam(r, "id"))
		if err != nil {
			MapDomainError(w, r, err)
			return
		}
		WriteJSON(w, http.StatusOK, admitResultDTO{
			Member: admittedMemberDTO{
				ID:           member.MembershipID,
				Name:         member.Name,
				Role:         string(member.Role),
				Capabilities: member.Capabilities.Strings(),
			},
			SignInSent: member.SignInSent,
		})
	}
}
