package httpadapter

import (
	"context"
	"net/http"

	"github.com/andreasoentoro/hearth/api/internal/domain"
	"github.com/andreasoentoro/hearth/api/internal/usecase"
)

// --- Shared response DTOs --------------------------------------------------
//
// These are shared by auth_handlers.go, invite_handlers.go, member_handlers.go
// and household_handlers.go -- all in package httpadapter -- rather than
// duplicated per file.

type userDTO struct {
	ID            string `json:"id"`
	Email         string `json:"email"`
	DisplayName   string `json:"displayName"`
	AvatarInitial string `json:"avatarInitial"`
}

func toUserDTO(u domain.User) userDTO {
	return userDTO{ID: u.ID, Email: u.Email, DisplayName: u.DisplayName, AvatarInitial: u.AvatarInitial}
}

type householdDTO struct {
	ID                    string `json:"id"`
	Name                  string `json:"name"`
	FamilyName            string `json:"familyName"`
	PrimaryCurrency       string `json:"primaryCurrency"`
	ShowSecondaryCurrency bool   `json:"showSecondaryCurrency"`
	SecondaryCurrency     string `json:"secondaryCurrency"`
	FXRateMode            string `json:"fxRateMode"`
}

func toHouseholdDTO(h domain.Household) householdDTO {
	return householdDTO{
		ID:                    h.ID,
		Name:                  h.Name,
		FamilyName:            h.FamilyName,
		PrimaryCurrency:       h.PrimaryCurrency,
		ShowSecondaryCurrency: h.ShowSecondaryCurrency,
		SecondaryCurrency:     h.SecondaryCurrency,
		FXRateMode:            h.FXRateMode,
	}
}

type membershipDTO struct {
	ID           string   `json:"id"`
	HouseholdID  string   `json:"householdId"`
	UserID       string   `json:"userId"`
	Role         string   `json:"role"`
	Capabilities []string `json:"capabilities"`
}

func toMembershipDTO(m domain.Membership) membershipDTO {
	return membershipDTO{
		ID:           m.ID,
		HouseholdID:  m.HouseholdID,
		UserID:       m.UserID,
		Role:         string(m.Role),
		Capabilities: m.Capabilities.Strings(),
	}
}

type spaceDTO struct {
	ID                 string `json:"id"`
	Key                string `json:"key"`
	Name               string `json:"name"`
	Visibility         string `json:"visibility"`
	Position           int    `json:"position"`
	IsBuiltin          bool   `json:"isBuiltin"`
	RequiredCapability string `json:"requiredCapability,omitempty"`
}

func toSpaceDTO(s domain.Space) spaceDTO {
	return spaceDTO{
		ID:                 s.ID,
		Key:                s.Key,
		Name:               s.Name,
		Visibility:         string(s.Visibility),
		Position:           s.Position,
		IsBuiltin:          s.IsBuiltin,
		RequiredCapability: string(s.RequiredCapability),
	}
}

// toSpaceDTOs always returns a non-nil, JSON-array-marshaling slice, even for
// zero spaces: a nil slice marshals to `null`, and the frontend's apiFetch
// treats an unparseable-as-expected ok body as a failure.
func toSpaceDTOs(spaces []domain.Space) []spaceDTO {
	out := make([]spaceDTO, 0, len(spaces))
	for _, s := range spaces {
		out = append(out, toSpaceDTO(s))
	}
	return out
}

// meResponseBody is GET /auth/me's body, and also what a successful sign-in,
// magic-link consumption, and invite acceptance answer with -- see
// completeSignIn below. The design deliberately returns everything the
// application shell needs in one response so the sidebar never waits on a
// request waterfall.
type meResponseBody struct {
	User         userDTO       `json:"user"`
	Household    householdDTO  `json:"household"`
	Membership   membershipDTO `json:"membership"`
	Capabilities []string      `json:"capabilities"`
	Spaces       []spaceDTO    `json:"spaces"`
}

// buildMeResponse assembles the GET /auth/me bundle for one caller. It is
// read-only, which is why every one of sign-in/magic-link-consume/invite-
// accept can call it before ever writing a cookie: an assembly failure here
// must never leave a live session cookie sitting next to a 500 response.
func buildMeResponse(ctx context.Context, deps Deps, userID, householdID string) (meResponseBody, error) {
	user, err := deps.Users.ByID(ctx, userID)
	if err != nil {
		return meResponseBody{}, err
	}
	membership, err := deps.Memberships.ByUser(ctx, userID)
	if err != nil {
		return meResponseBody{}, err
	}
	household, err := deps.Households.Get(ctx, householdID)
	if err != nil {
		return meResponseBody{}, err
	}
	spaces, err := deps.Households.Spaces(ctx, householdID, membership)
	if err != nil {
		return meResponseBody{}, err
	}
	return meResponseBody{
		User:         toUserDTO(user.User),
		Household:    toHouseholdDTO(household),
		Membership:   toMembershipDTO(membership),
		Capabilities: membership.Capabilities.Strings(),
		Spaces:       toSpaceDTOs(spaces),
	}, nil
}

// completeSignIn is the common tail of sign-in, magic-link consumption, and
// invite acceptance: all three produce a usecase.SignInResult, and all three
// must answer identically -- the me bundle, a session cookie and a CSRF
// cookie. The me bundle is assembled, and the CSRF token generated, before
// either cookie is written, so a failure at either step never leaves a live
// session cookie paired with an error response.
func completeSignIn(w http.ResponseWriter, r *http.Request, deps Deps, result usecase.SignInResult) {
	body, err := buildMeResponse(r.Context(), deps, result.UserID, result.HouseholdID)
	if err != nil {
		MapDomainError(w, r, err)
		return
	}
	csrfToken, _, err := deps.Tokens.NewToken()
	if err != nil {
		MapDomainError(w, r, err)
		return
	}
	setSessionCookie(w, deps, result.SessionToken, result.ExpiresAt)
	setCSRFCookie(w, deps, csrfToken, result.ExpiresAt)
	WriteJSON(w, http.StatusOK, body)
}

type signInRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

func handleSignIn(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req signInRequest
		if !decodeJSONBody(w, r, &req) {
			return
		}
		result, err := deps.Auth.SignIn(r.Context(), req.Email, req.Password)
		if err != nil {
			MapDomainError(w, r, err)
			return
		}
		completeSignIn(w, r, deps, result)
	}
}

type magicLinkRequest struct {
	Email string `json:"email"`
}

func handleRequestMagicLink(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req magicLinkRequest
		if !decodeJSONBody(w, r, &req) {
			return
		}
		// RequestMagicLink's own contract is "always nil" (see its doc
		// comment in usecase/auth.go) -- this still checks err so a future
		// change to that contract fails loudly here rather than being
		// silently ignored.
		if err := deps.Auth.RequestMagicLink(r.Context(), req.Email); err != nil {
			MapDomainError(w, r, err)
			return
		}
		WriteJSON(w, http.StatusAccepted, map[string]string{"status": "accepted"})
	}
}

type magicLinkConsumeRequest struct {
	Token string `json:"token"`
}

func handleConsumeMagicLink(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req magicLinkConsumeRequest
		if !decodeJSONBody(w, r, &req) {
			return
		}
		result, err := deps.Auth.ConsumeMagicLink(r.Context(), req.Token)
		if err != nil {
			MapDomainError(w, r, err)
			return
		}
		completeSignIn(w, r, deps, result)
	}
}

// handleSignOut sits behind requireSession, so the hearth_session cookie is
// already known to be present and to have resolved to a live session by the
// time this runs -- it is read again here (rather than threaded through
// Scope) because SignOut needs the raw token to hash, and Scope carries only
// the identity the middleware already resolved from it.
func handleSignOut(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		cookie, err := r.Cookie(sessionCookieName)
		if err == nil && cookie.Value != "" {
			if err := deps.Auth.SignOut(r.Context(), cookie.Value); err != nil {
				MapDomainError(w, r, err)
				return
			}
		}
		clearSessionCookie(w, deps)
		clearCSRFCookie(w, deps)
		w.WriteHeader(http.StatusNoContent)
	}
}

func handleMe(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		scope, ok := RequestScope(r)
		if !ok {
			WriteError(w, http.StatusUnauthorized, "UNAUTHENTICATED", "Sign in required.", nil)
			return
		}
		body, err := buildMeResponse(r.Context(), deps, scope.UserID, scope.HouseholdID)
		if err != nil {
			MapDomainError(w, r, err)
			return
		}
		WriteJSON(w, http.StatusOK, body)
	}
}
