package httpadapter

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5/middleware"
)

// TestRecovererWritesTheStandardEnvelopeWithARequestID pins the fix: a panic
// downstream must answer with the same error envelope every other failure
// path uses, request ID included, rather than chi's own middleware.Recoverer
// bare, bodyless 500 -- see recoverer's doc comment in
// middleware_recoverer.go.
func TestRecovererWritesTheStandardEnvelopeWithARequestID(t *testing.T) {
	panics := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		panic("boom")
	})

	handler := middleware.RequestID(recoverer(panics))

	req := httptest.NewRequest(http.MethodGet, "/anything", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusInternalServerError)
	}

	var body struct {
		Error struct {
			Code    string         `json:"code"`
			Message string         `json:"message"`
			Details map[string]any `json:"details"`
		} `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v (body = %s)", err, rec.Body.String())
	}
	if body.Error.Code != "INTERNAL" {
		t.Fatalf("code = %q, want INTERNAL", body.Error.Code)
	}
	reqID, ok := body.Error.Details["requestId"]
	if !ok {
		t.Fatal("details.requestId is missing")
	}
	if reqID == "" {
		t.Fatal("details.requestId is empty")
	}
}

// TestRecovererRepanicsErrAbortHandlerUnlogged mirrors
// middleware.Recoverer's own contract: http.ErrAbortHandler means net/http
// itself wants the connection aborted with no further writes, not an
// application panic recoverer should turn into a 500.
func TestRecovererRepanicsErrAbortHandlerUnlogged(t *testing.T) {
	panics := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		panic(http.ErrAbortHandler)
	})
	handler := recoverer(panics)

	defer func() {
		rec := recover()
		if rec != http.ErrAbortHandler {
			t.Fatalf("recovered value = %v, want http.ErrAbortHandler to have propagated", rec)
		}
	}()

	req := httptest.NewRequest(http.MethodGet, "/anything", nil)
	handler.ServeHTTP(httptest.NewRecorder(), req)
	t.Fatal("expected http.ErrAbortHandler to propagate out of ServeHTTP")
}
