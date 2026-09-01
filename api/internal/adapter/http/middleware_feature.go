package httpadapter

import (
	"net/http"

	"github.com/andreasoentoro/hearth/api/internal/domain"
)

// requireFeature answers 404 -- not 403 -- unless flag is on for this caller.
// On this install a disabled feature does not exist, and a 403 would confirm a
// route that is meant to be invisible.
//
// It handles the pre-auth routes itself. With no Scope on the request there is
// no household whose overrides could apply, so it resolves the global set
// alone. That fallback is why enforcement is one middleware rather than a
// middleware plus a helper public handlers remember to call: a hand-rolled
// check as a handler's first statement is the shape that gets forgotten on the
// next public route, and forgetting it fails open.
// It closes over deps rather than reaching for the request, the way every
// other middleware in this package does.
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
				// requirePlatformAdmin's doc comment in middleware_admin.go
				// for why a lookup failure must never be allowed to read as
				// domain.ErrNotFound's mapped 404. Here that 404 means "this
				// feature is hidden on this install," so routing a database
				// outage through MapDomainError would tell a would-be
				// signer-up that sign-up does not exist here, instead of
				// reporting a server fault.
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
