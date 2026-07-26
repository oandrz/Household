package httpadapter

import (
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

// toMemberViewDTO builds one row of the member list. revealEmail gates the
// User.Email field: when false (a limited caller), it is emptied rather than
// populated -- the field key still appears in the JSON (userDTO's Email has
// no `omitempty`), so the response shape is identical either way and the
// frontend never needs two types.
//
// An empty email here is deliberately ambiguous between "withheld because
// the caller isn't an owner" and "this member genuinely has no email" (a
// credential-less child's own record) -- see the doc comment on
// handleListMembers for why that ambiguity is the point, not a gap.
func toMemberViewDTO(v usecase.MemberView, revealEmail bool) memberViewDTO {
	user := toUserDTO(v.User)
	if !revealEmail {
		user.Email = ""
	}
	return memberViewDTO{
		ID:           v.Membership.ID,
		User:         user,
		Role:         string(v.Membership.Role),
		Capabilities: v.Membership.Capabilities.Strings(),
	}
}

// handleListMembers is reachable by any authenticated member -- names,
// roles and capabilities are needed household-wide (a future slice's
// calendar per-person filters, in particular) -- but email addresses are
// personal data belonging to the other members and the identifier the whole
// authentication surface is keyed on. Nothing in the design shows a child a
// parent's email address, so the list always contains every member (the
// household's full roster, not a filtered subset), and only an owner caller
// gets the User.Email field populated; a limited caller sees it emptied on
// every row via toMemberViewDTO's revealEmail parameter. This is a
// per-field redaction, deliberately not a per-row filter, so a limited
// caller still learns who is in the household and what they can do -- just
// not how to reach them by email.
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
		revealEmail := scope.Membership.Role == domain.RoleOwner
		out := make([]memberViewDTO, 0, len(views))
		for _, v := range views {
			out = append(out, toMemberViewDTO(v, revealEmail))
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
		if !decodeJSONBody(w, r, &req) {
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

// updateMemberRequest's fields are pointers for the same reason
// updateHouseholdRequest's and notificationPreferencesRequest's are (see
// household_handlers.go): a plain string/slice field cannot distinguish "the
// caller omitted this" from "the caller sent its zero value," so a request
// that only means to change capabilities would otherwise blank Role to ""
// and fail as an unknown role, and vice versa. Unlike those two, this one
// briefly shipped with value fields anyway -- Task 20's frontend had to work
// around it client-side by always sending both fields together. Fixed here
// the same way: absent means unchanged.
type updateMemberRequest struct {
	Role         *string   `json:"role"`
	Capabilities *[]string `json:"capabilities"`
}

// currentMembership finds membershipID's row in an already-loaded member
// list. Returns domain.ErrNotFound for a miss, matching what
// domain.ValidateMembershipChange itself returns for the same target --
// asking to patch a membership that doesn't exist should look identical
// either way.
func currentMembership(views []usecase.MemberView, membershipID string) (domain.Membership, error) {
	for _, v := range views {
		if v.Membership.ID == membershipID {
			return v.Membership, nil
		}
	}
	return domain.Membership{}, domain.ErrNotFound
}

// handleUpdateMember sits behind requireOwner. A successful update's normal
// body just echoes what was set; if the update itself succeeded but
// usecase.ErrSessionRevocationFailed comes back, that same body gets a
// warning field appended and the response stays 200 -- the mutation did
// happen, and reporting it as a failure would invite a pointless retry. This
// is checked here, before MapDomainError, because MapDomainError only ever
// sees the error, not this route's success body to append the warning to.
//
// This is a real PATCH: it reads the membership's current role and
// capabilities first (via deps.Members.List, the same read
// handleListMembers uses) and applies only the fields present in the
// request, exactly as handleUpdateHousehold does for /household. Role and
// capabilities interact through domain rules that only make sense evaluated
// together -- an owner must hold every capability, a limited member may
// never hold "marriage" -- so a role-only patch is validated against the
// membership's *existing* capabilities, and a capabilities-only patch is
// validated against its *existing* role, never against a half-populated
// candidate. usecase.MemberService.Update already runs the fully-resolved
// candidate through domain.ValidateMembershipChange internally; building
// that candidate here, before calling it, is what makes that validation
// correct rather than vacuous.
func handleUpdateMember(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		scope, ok := RequestScope(r)
		if !ok {
			WriteError(w, http.StatusUnauthorized, "UNAUTHENTICATED", "Sign in required.", nil)
			return
		}
		membershipID := chi.URLParam(r, "id")

		var req updateMemberRequest
		if !decodeJSONBody(w, r, &req) {
			return
		}

		views, err := deps.Members.List(r.Context(), scope.HouseholdID)
		if err != nil {
			MapDomainError(w, r, err)
			return
		}
		current, err := currentMembership(views, membershipID)
		if err != nil {
			MapDomainError(w, r, err)
			return
		}

		role := current.Role
		if req.Role != nil {
			role, err = domain.ParseRole(*req.Role)
			if err != nil {
				MapDomainError(w, r, err)
				return
			}
		}
		caps := current.Capabilities
		if req.Capabilities != nil {
			caps, err = domain.ParseCapabilities(*req.Capabilities)
			if err != nil {
				MapDomainError(w, r, err)
				return
			}
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
