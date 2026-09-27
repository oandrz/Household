package httpadapter

import (
	"net/http"
	"time"
)

// parseTimeOrRefuse parses raw with layout, writes the caller's own error
// and returns false on failure. Status, code and message are parameters:
// bills/holdings answer 422 INVALID_DATE, months 400 INVALID_MONTH, and the
// frontend matches on those -- only the parse-then-refuse shape is shared.
func parseTimeOrRefuse(w http.ResponseWriter, raw, layout string, status int, code, message string) (time.Time, bool) {
	t, err := time.Parse(layout, raw)
	if err != nil {
		WriteError(w, status, code, message, nil)
		return time.Time{}, false
	}
	return t, true
}
