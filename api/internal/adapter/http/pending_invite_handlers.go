package httpadapter

import (
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
)

// An owner's view of the invites their household has sent and nobody has
// accepted yet (partner-invite spec, milestone 1). Before this an invite was
// written and never read back: the modal closed and Settings showed no trace,
// so an owner could not tell a sent invite from a lost one. The public,
// pre-sign-in invite routes live in invite_handlers.go.

// inviteSummaryDTO is one row of GET /household/invites, wrapping
// usecase.InviteSummary. Named for that type rather than "pendingInviteDTO"
// because admin_directory_handlers.go already declares a pendingInviteDTO
// for the operator directory's own, differently-shaped view of an invite
// (no ID, no Capabilities) -- the same collision the usecase layer resolves
// the same way (InviteSummary vs PendingInvite; see ports.go). Email is
// always the real address: the route is owner-only, the same rule that lets
// only an owner see members' addresses (handleListMembers).
type inviteSummaryDTO struct {
	ID           string    `json:"id"`
	Name         string    `json:"name"`
	Email        string    `json:"email"`
	Role         string    `json:"role"`
	Capabilities []string  `json:"capabilities"`
	ExpiresAt    time.Time `json:"expiresAt"`
}

func handleListPendingInvites(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		scope, ok := requireScope(w, r)
		if !ok {
			return
		}
		invites, err := deps.Invites.ListPending(r.Context(), scope.HouseholdID)
		if err != nil {
			MapDomainError(w, r, err)
			return
		}
		out := make([]inviteSummaryDTO, 0, len(invites))
		for _, invite := range invites {
			out = append(out, inviteSummaryDTO{
				ID:           invite.ID,
				Name:         invite.Name,
				Email:        invite.Email,
				Role:         string(invite.Role),
				Capabilities: invite.Capabilities.Strings(),
				ExpiresAt:    invite.ExpiresAt,
			})
		}
		WriteJSON(w, http.StatusOK, out)
	}
}

// handleWithdrawInvite answers 204: the row is gone, so there is nothing to
// describe. Its guards live on the route (router.go): owner, CSRF, and a
// browser session.
func handleWithdrawInvite(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		scope, ok := requireScope(w, r)
		if !ok {
			return
		}
		if err := deps.Invites.Withdraw(r.Context(), scope.HouseholdID, chi.URLParam(r, "id")); err != nil {
			MapDomainError(w, r, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}
