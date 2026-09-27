package httpadapter

import (
	"net/http"

	"github.com/andreasoentoro/hearth/api/internal/domain"
)

// requireCapability answers 403 FORBIDDEN unless the caller's Scope carries
// cap, so the server enforces capabilities independently of the UI.
//
// On the account write routes it is stacked with requireOwner, which today
// makes it redundant -- domain.ValidateMembershipChange already refuses an
// owner missing any capability. It stays anyway: relying only on that
// invariant would couple these routes to a rule enforced in a different
// layer for a different reason, and one extra middleware call is cheaper
// than that coupling breaking silently if the invariant is ever relaxed.
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
