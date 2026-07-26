// Package httpadapter is the HTTP interface layer. It translates requests into
// use-case calls and use-case results into responses. It contains no business
// rules. The package is named httpadapter rather than http so that handlers can
// still refer to the standard library.
package httpadapter

import (
	"encoding/json"
	"log/slog"
	"net/http"
)

type errorBody struct {
	Code    string         `json:"code"`
	Message string         `json:"message"`
	Details map[string]any `json:"details,omitempty"`
}

type errorEnvelope struct {
	Error errorBody `json:"error"`
}

// WriteJSON writes body as JSON with the given status.
func WriteJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if body == nil {
		return
	}
	if err := json.NewEncoder(w).Encode(body); err != nil {
		slog.Error("write json response", "error", err)
	}
}

// WriteError writes the single error envelope every failure response uses.
func WriteError(w http.ResponseWriter, status int, code, message string, details map[string]any) {
	WriteJSON(w, status, errorEnvelope{Error: errorBody{Code: code, Message: message, Details: details}})
}
