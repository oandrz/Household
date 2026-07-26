package httpadapter_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	httpadapter "github.com/andreasoentoro/hearth/api/internal/adapter/http"
)

type stubPinger struct{ err error }

func (s stubPinger) Ping(context.Context) error { return s.err }

func TestHealthzIgnoresTheDatabase(t *testing.T) {
	r := httpadapter.NewRouter(httpadapter.Deps{Pinger: stubPinger{err: errors.New("database is down")}})

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/healthz", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var body map[string]string
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body["status"] != "ok" {
		t.Fatalf("body = %v", body)
	}
}

func TestReadyzFailsWhenTheDatabaseIsUnreachable(t *testing.T) {
	r := httpadapter.NewRouter(httpadapter.Deps{Pinger: stubPinger{err: errors.New("database is down")}})

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/readyz", nil))

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", rec.Code)
	}
}

func TestReadyzSucceedsWhenTheDatabaseAnswers(t *testing.T) {
	r := httpadapter.NewRouter(httpadapter.Deps{Pinger: stubPinger{}})

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/readyz", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
}

func TestUnknownRouteReturnsTheErrorEnvelope(t *testing.T) {
	r := httpadapter.NewRouter(httpadapter.Deps{Pinger: stubPinger{}})

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/nope", nil))

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
	var body struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Error.Code != "NOT_FOUND" {
		t.Fatalf("error code = %q, want NOT_FOUND", body.Error.Code)
	}
}
