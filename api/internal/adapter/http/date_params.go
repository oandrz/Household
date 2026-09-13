package httpadapter

import (
	"net/http"
	"time"
)

// parseTimeOrRefuse parses raw with layout, or writes the caller's own error
// and reports false. Each screen keeps its own status, code and message --
// bills and holdings answer 422 INVALID_DATE, months answer 400 INVALID_MONTH,
// and the frontend matches on those -- so they are parameters here rather
// than one shared wording. Only the parse-then-refuse shape is shared.
func parseTimeOrRefuse(w http.ResponseWriter, raw, layout string, status int, code, message string) (time.Time, bool) {
	t, err := time.Parse(layout, raw)
	if err != nil {
		WriteError(w, status, code, message, nil)
		return time.Time{}, false
	}
	return t, true
}
