package httpadapter

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
)

// invitePreviewResponse is GET /invites/:token's body: enough for the
// pre-auth invite screen to render "Christine invited you to join the
// Oentoro household as co-owner" without exposing anything else about the
// invite or the household.
type invitePreviewResponse struct {
	HouseholdName string   `json:"householdName"`
	InviterName   string   `json:"inviterName"`
	Name          string   `json:"name"`
	Role          string   `json:"role"`
	Capabilities  []string `json:"capabilities"`
}

func handleInvitePreview(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		token := chi.URLParam(r, "token")
		preview, err := deps.Invites.Preview(r.Context(), token)
		if err != nil {
			MapDomainError(w, r, err)
			return
		}
		WriteJSON(w, http.StatusOK, invitePreviewResponse{
			HouseholdName: preview.FamilyName,
			InviterName:   preview.InviterName,
			Name:          preview.Name,
			Role:          string(preview.Role),
			Capabilities:  preview.Capabilities.Strings(),
		})
	}
}

type acceptInviteRequest struct {
	Password    string `json:"password"`
	DisplayName string `json:"displayName"`
}

// handleAcceptInvite is public -- reached before the caller has any session --
// and, like sign-in and magic-link consumption, ends in completeSignIn: a
// successful acceptance signs the new member in exactly as those two paths
// do.
func handleAcceptInvite(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		token := chi.URLParam(r, "token")
		var req acceptInviteRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			WriteError(w, http.StatusBadRequest, "INVALID_BODY", "The request body could not be parsed.", nil)
			return
		}
		result, err := deps.Invites.Accept(r.Context(), token, req.Password, req.DisplayName)
		if err != nil {
			MapDomainError(w, r, err)
			return
		}
		completeSignIn(w, r, deps, result)
	}
}
