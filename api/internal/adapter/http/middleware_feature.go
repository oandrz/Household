package httpadapter

import (
	"net/http"

	"github.com/andreasoentoro/hearth/api/internal/domain"
)

// requireFeature answers 404, not 403, unless flag is on for this caller: a
// disabled feature does not exist here, and a 403 would confirm a route
// meant to be invisible.
//
// With no Scope (the pre-auth routes) there is no household whose overrides
// apply, so it resolves the global flag set instead -- which is why this is
// one middleware rather than a helper handlers must remember to call: a
// hand-rolled check as a handler's first line is the shape that gets
// forgotten on the next route, and forgetting it fails open. It closes over
// deps, like every other middleware here, rather than reaching for the
// request.
func requireFeature(deps Deps, flag domain.Flag) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if scope, ok := RequestScope(r); ok {
				if !scope.Flags.Enabled(flag) {
					writeNotFound(w)
					return
				}
				next.ServeHTTP(w, r)
				return
			}

			flags, err := deps.Admin.GlobalFlags(r.Context())
			if err != nil {
				// logAndWriteInternal, not MapDomainError: see
				// requirePlatformAdmin's doc comment for why a lookup failure
				// must not read as ErrNotFound's mapped 404. Here that 404
				// means "hidden on this install," so a database outage
				// routed through MapDomainError would tell a would-be
				// signer-up that sign-up itself doesn't exist.
				logAndWriteInternal(w, r, err)
				return
			}
			if !flags.Enabled(flag) {
				writeNotFound(w)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
