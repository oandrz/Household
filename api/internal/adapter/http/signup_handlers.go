package httpadapter

import (
	"net/http"

	"github.com/go-chi/chi/v5"
)

type signUpRequest struct {
	Email string `json:"email"`
}

// handleSignUp always answers 202 with the same body. SignupService.Request's
// contract is "always nil" (see its doc comment); this still checks err so a
// future change to that contract fails loudly here instead of being swallowed.
func handleSignUp(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req signUpRequest
		if !decodeJSONBody(w, r, &req) {
			return
		}
		if err := deps.Signups.Request(r.Context(), req.Email); err != nil {
			MapDomainError(w, r, err)
			return
		}
		// Byte-identical to handleRequestMagicLink's answer, deliberately.
		WriteJSON(w, http.StatusAccepted, map[string]string{"status": "accepted"})
	}
}

// handleSignUpPreview reads no body -- the token is in the path -- so it does
// not call decodeJSONBody, exactly as handleInvitePreview does not.
//
// CONTROLLER RULING R3: the body carries channel alongside email, mirroring
// usecase.SignupPreview -- see that type's own doc comment for why the
// create-household screen needs to know which identity the token proved. A
// plain map, not a struct with `json:"email,omitempty"`, is deliberate: a
// Telegram sign-up's Email is "", and omitempty on a struct field would drop
// the key entirely rather than send it empty. json.Marshal of a map never
// omits a key for an empty value, so this shape cannot make that mistake.
func handleSignUpPreview(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		preview, err := deps.Signups.Preview(r.Context(), chi.URLParam(r, "token"))
		if err != nil {
			MapDomainError(w, r, err)
			return
		}
		WriteJSON(w, http.StatusOK, map[string]string{"email": preview.Email, "channel": preview.Channel})
	}
}

type completeSignUpRequest struct {
	HouseholdName   string `json:"householdName"`
	DisplayName     string `json:"displayName"`
	PrimaryCurrency string `json:"primaryCurrency"`
	Password        string `json:"password"`
}

// handleCompleteSignUp provisions the household and signs the new owner in
// through completeSignIn -- the same tail sign-in, magic-link consumption and
// invite acceptance use, so all four answer with the identical me bundle and
// the identical pair of cookies.
func handleCompleteSignUp(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req completeSignUpRequest
		if !decodeJSONBody(w, r, &req) {
			return
		}
		result, err := deps.Signups.Complete(r.Context(), chi.URLParam(r, "token"),
			req.HouseholdName, req.DisplayName, req.PrimaryCurrency, req.Password)
		if err != nil {
			MapDomainError(w, r, err)
			return
		}
		completeSignIn(w, r, deps, result)
	}
}
