package httpadapter

import "net/http"

// handleListCalendarEvents is the Family calendar's first endpoint. No
// events exist yet (⬜ in FEATURE_TRACKER.md), so it answers an empty list --
// the route exists now so dark-shipping has something real to hit, and
// returns a body because a 2xx with none breaks apiFetch (CLAUDE.md).
func handleListCalendarEvents(_ Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		WriteJSON(w, http.StatusOK, map[string]any{"events": []any{}})
	}
}
