package httpadapter

import (
	"net/http"

	"github.com/andreasoentoro/hearth/api/internal/domain"
)

// requireCapability answers 403 FORBIDDEN unless the caller's Scope carries
// cap. The accounts routes are its first users: before them it was defined and
// unwired, which made the promise that the server enforces capabilities
// independently of the UI vacuous.
//
// On the account write routes it is stacked with requireOwner, and today that
// is redundant -- domain.ValidateMembershipChange refuses an owner who does not
// hold every capability, so "an owner without money" is not a representable
// state. It is stacked anyway: the alternative is for these routes to depend on
// an invariant enforced in a different layer for a different reason, and if
// that invariant is ever relaxed every route leaning on it opens silently. One
// extra middleware call is a cheaper price than that coupling.
func requireCapability(cap domain.Capability) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			scope, ok := RequestScope(r)
			if !ok || !scope.Membership.Capabilities.Has(cap) {
				WriteError(w, http.StatusForbidden, "FORBIDDEN", "You do not have permission to do that.", nil)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
