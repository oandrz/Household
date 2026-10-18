package httpadapter

import "net/http"

// maxChangePasswordBodyBytes fits two passwords at their 256-byte ceiling
// even when JSON escaping doubles them; maxRequestBodyBytes would refuse a
// legitimate one.
const maxChangePasswordBodyBytes = 2048

type changePasswordRequest struct {
	CurrentPassword string `json:"currentPassword"`
	NewPassword     string `json:"newPassword"`
}

// handleChangePassword answers with a fresh session for this browser, the
// way sign-in does: the old session was revoked with every other.
func handleChangePassword(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		scope, ok := RequestScope(r)
		if !ok {
			WriteError(w, http.StatusUnauthorized, "UNAUTHENTICATED", "Sign in required.", nil)
			return
		}
		var req changePasswordRequest
		if !decodeJSONBodyLimit(w, r, &req, maxChangePasswordBodyBytes) {
			return
		}
		result, err := deps.AccessReset.ChangePassword(r.Context(), scope.UserID, scope.HouseholdID,
			req.CurrentPassword, req.NewPassword)
		if err != nil {
			MapDomainError(w, r, err)
			return
		}
		completeSignIn(w, r, deps, result)
	}
}

// handleSignOutEverywhere clears this browser's cookies only after the
// cut-off committed; on failure nothing was revoked, so the person stays
// signed in to try again.
func handleSignOutEverywhere(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		scope, ok := RequestScope(r)
		if !ok {
			WriteError(w, http.StatusUnauthorized, "UNAUTHENTICATED", "Sign in required.", nil)
			return
		}
		if err := deps.AccessReset.SignOutEverywhere(r.Context(), scope.UserID); err != nil {
			MapDomainError(w, r, err)
			return
		}
		clearSessionCookie(w, deps)
		clearCSRFCookie(w, deps)
		w.WriteHeader(http.StatusNoContent)
	}
}
