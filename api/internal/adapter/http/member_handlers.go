package httpadapter

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/andreasoentoro/hearth/api/internal/domain"
	"github.com/andreasoentoro/hearth/api/internal/usecase"
)

type memberViewDTO struct {
	ID           string   `json:"id"`
	User         userDTO  `json:"user"`
	Role         string   `json:"role"`
	Capabilities []string `json:"capabilities"`
}

func toMemberViewDTO(v usecase.MemberView) memberViewDTO {
	return memberViewDTO{
		ID:           v.Membership.ID,
		User:         toUserDTO(v.User),
		Role:         string(v.Membership.Role),
		Capabilities: v.Membership.Capabilities.Strings(),
	}
}

func handleListMembers(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		scope, ok := RequestScope(r)
		if !ok {
			WriteError(w, http.StatusUnauthorized, "UNAUTHENTICATED", "Sign in required.", nil)
			return
		}
		views, err := deps.Members.List(r.Context(), scope.HouseholdID)
		if err != nil {
			MapDomainError(w, r, err)
			return
		}
		out := make([]memberViewDTO, 0, len(views))
		for _, v := range views {
			out = append(out, toMemberViewDTO(v))
		}
		WriteJSON(w, http.StatusOK, out)
	}
}

type inviteMemberRequest struct {
	Name         string   `json:"name"`
	Email        string   `json:"email"`
	Role         string   `json:"role"`
	Capabilities []string `json:"capabilities"`
}

// handleInviteMember sits behind requireOwner: only an owner may add a
// member. It parses role and capabilities itself (rather than handing raw
// strings to the service) so a malformed value is reported through the same
// MapDomainError table (INVALID_ROLE / INVALID_CAPABILITIES) that a value
// domain.NewMembership itself rejects would be.
func handleInviteMember(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		scope, ok := RequestScope(r)
		if !ok {
			WriteError(w, http.StatusUnauthorized, "UNAUTHENTICATED", "Sign in required.", nil)
			return
		}
		var req inviteMemberRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			WriteError(w, http.StatusBadRequest, "INVALID_BODY", "The request body could not be parsed.", nil)
			return
		}
		role, err := domain.ParseRole(req.Role)
		if err != nil {
			MapDomainError(w, r, err)
			return
		}
		caps, err := domain.ParseCapabilities(req.Capabilities)
		if err != nil {
			MapDomainError(w, r, err)
			return
		}
		if err := deps.Invites.Create(r.Context(), scope.HouseholdID, scope.UserID, req.Name, req.Email, role, caps); err != nil {
			MapDomainError(w, r, err)
			return
		}
		WriteJSON(w, http.StatusCreated, map[string]string{"status": "invited"})
	}
}

type updateMemberRequest struct {
	Role         string   `json:"role"`
	Capabilities []string `json:"capabilities"`
}

// handleUpdateMember sits behind requireOwner. A successful update's normal
// body just echoes what was set; if the update itself succeeded but
// usecase.ErrSessionRevocationFailed comes back, that same body gets a
// warning field appended and the response stays 200 -- the mutation did
// happen, and reporting it as a failure would invite a pointless retry. This
// is checked here, before MapDomainError, because MapDomainError only ever
// sees the error, not this route's success body to append the warning to.
func handleUpdateMember(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		scope, ok := RequestScope(r)
		if !ok {
			WriteError(w, http.StatusUnauthorized, "UNAUTHENTICATED", "Sign in required.", nil)
			return
		}
		membershipID := chi.URLParam(r, "id")

		var req updateMemberRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			WriteError(w, http.StatusBadRequest, "INVALID_BODY", "The request body could not be parsed.", nil)
			return
		}
		role, err := domain.ParseRole(req.Role)
		if err != nil {
			MapDomainError(w, r, err)
			return
		}
		caps, err := domain.ParseCapabilities(req.Capabilities)
		if err != nil {
			MapDomainError(w, r, err)
			return
		}

		body := map[string]any{"id": membershipID, "role": string(role), "capabilities": caps.Strings()}
		if err := deps.Members.Update(r.Context(), scope.HouseholdID, membershipID, role, caps); err != nil {
			if errors.Is(err, usecase.ErrSessionRevocationFailed) {
				body["warning"] = sessionRevocationWarning
				WriteJSON(w, http.StatusOK, body)
				return
			}
			MapDomainError(w, r, err)
			return
		}
		WriteJSON(w, http.StatusOK, body)
	}
}

// handleRemoveMember sits behind requireOwner. Its ordinary success response
// is 204 with no body, per the API spec; the one exception is the same
// ErrSessionRevocationFailed case handleUpdateMember has, which must carry a
// warning and therefore cannot be a bodyless 204 -- so that case alone
// answers 200 with a small JSON body instead.
func handleRemoveMember(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		scope, ok := RequestScope(r)
		if !ok {
			WriteError(w, http.StatusUnauthorized, "UNAUTHENTICATED", "Sign in required.", nil)
			return
		}
		membershipID := chi.URLParam(r, "id")

		if err := deps.Members.Remove(r.Context(), scope.HouseholdID, membershipID); err != nil {
			if errors.Is(err, usecase.ErrSessionRevocationFailed) {
				WriteJSON(w, http.StatusOK, map[string]any{"status": "removed", "warning": sessionRevocationWarning})
				return
			}
			MapDomainError(w, r, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}
