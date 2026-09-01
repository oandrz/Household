package httpadapter

import (
	"net/http"
)

type adminSessionRequest struct {
	Password string `json:"password"`
}

// handleAdminSession is the re-authentication. It answers 204: there is
// nothing to tell the caller that they do not already know, and the grant
// lives on the session row rather than in the response.
//
// The grant is written against the token hash of the cookie this request
// arrived with, not against the user, so re-authenticating in one browser
// does not open the surface in another that happens to hold a second live
// session for the same operator.
func handleAdminSession(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		scope, ok := RequestScope(r)
		if !ok {
			WriteError(w, http.StatusUnauthorized, "UNAUTHENTICATED", "Sign in required.", nil)
			return
		}
		var req adminSessionRequest
		if !decodeJSONBody(w, r, &req) {
			return
		}
		if err := deps.AdminReauth.Verify(r.Context(), scope.UserID, req.Password); err != nil {
			MapDomainError(w, r, err)
			return
		}

		cookie, err := r.Cookie(sessionCookieName)
		if err != nil || cookie.Value == "" {
			WriteError(w, http.StatusUnauthorized, "UNAUTHENTICATED", "Sign in required.", nil)
			return
		}
		expiresAt := deps.Clock.Now().Add(adminGrantTTL)
		if err := deps.Sessions.GrantAdmin(r.Context(), deps.Tokens.HashToken(cookie.Value), &expiresAt); err != nil {
			MapDomainError(w, r, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

type flagOverrideDTO struct {
	HouseholdID   string `json:"householdId"`
	HouseholdName string `json:"householdName"`
	Enabled       bool   `json:"enabled"`
}

type flagDTO struct {
	Key           string            `json:"key"`
	Description   string            `json:"description"`
	Default       bool              `json:"default"`
	GlobalSet     bool              `json:"globalSet"`
	GlobalEnabled bool              `json:"globalEnabled"`
	Effective     bool              `json:"effective"`
	Orphaned      bool              `json:"orphaned"`
	Overrides     []flagOverrideDTO `json:"overrides"`
}

type flagsResponse struct {
	Flags []flagDTO `json:"flags"`
}

// handleListFlags is the admin surface's first read. Both slices are built
// with make rather than left nil, so an install with no flags and no
// overrides still serialises as [] rather than null -- the frontend's
// apiFetch cannot tell a null apart from a missing field.
func handleListFlags(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		overview, err := deps.Admin.Overview(r.Context())
		if err != nil {
			MapDomainError(w, r, err)
			return
		}
		body := flagsResponse{Flags: make([]flagDTO, 0, len(overview))}
		for _, row := range overview {
			dto := flagDTO{
				Key: row.Key, Description: row.Description, Default: row.Default,
				GlobalSet: row.GlobalSet, GlobalEnabled: row.GlobalEnabled,
				Effective: row.Effective, Orphaned: row.Orphaned,
				Overrides: make([]flagOverrideDTO, 0, len(row.Overrides)),
			}
			for _, o := range row.Overrides {
				dto.Overrides = append(dto.Overrides, flagOverrideDTO{
					HouseholdID: o.HouseholdID, HouseholdName: o.HouseholdName, Enabled: o.Enabled,
				})
			}
			body.Flags = append(body.Flags, dto)
		}
		WriteJSON(w, http.StatusOK, body)
	}
}
