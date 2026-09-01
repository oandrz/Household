package httpadapter

import "net/http"

// handleListCalendarEvents is the Family calendar's first endpoint. There are
// no events yet -- the feature is ⬜ in docs/FEATURE_TRACKER.md -- so it
// answers an empty list. It exists now because dark-shipping needs a real
// route to prove itself against, and because a 2xx with no body would break
// apiFetch (see CLAUDE.md).
func handleListCalendarEvents(_ Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		WriteJSON(w, http.StatusOK, map[string]any{"events": []any{}})
	}
}
