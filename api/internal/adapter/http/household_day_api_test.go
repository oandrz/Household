package httpadapter_test

import (
	"context"
	"net/http"
	"testing"
	"time"
)

// These tests pin "today" as the household's calendar day (ADR 12). They
// share one clock shape: 23:00 UTC, the hour at which a UTC server and a
// Singapore household disagree about the date.

// eveningClock is a clock stopped at the next 23:00 UTC at or after real now,
// and the UTC calendar day that instant falls on.
//
// Forward of real now, never behind it: session expiry is checked by
// Postgres's own now(), so a clock pinned to the past signs in and is then
// refused (see movableClock). At 23:00 UTC it is 07:00 the NEXT day in
// Singapore, which is the whole point: utcDay is the server's date and
// utcDay+1 is the household's.
func eveningClock() (clk *movableClock, utcDay time.Time) {
	now := time.Now().UTC()
	anchor := time.Date(now.Year(), now.Month(), now.Day(), 23, 0, 0, 0, time.UTC)
	if anchor.Before(now) {
		anchor = anchor.AddDate(0, 0, 1)
	}
	return &movableClock{now: anchor}, time.Date(anchor.Year(), anchor.Month(), anchor.Day(), 0, 0, 0, 0, time.UTC)
}

// setTimezone moves the household to another zone through the API, the way
// an owner does it in Settings.
func (env *testEnv) setTimezone(t *testing.T, session, csrf *http.Cookie, zone string) {
	t.Helper()
	rec := env.authed(t, http.MethodPatch, "/api/v1/household", map[string]any{"timezone": zone}, session, csrf)
	if rec.Code != http.StatusOK {
		t.Fatalf("set time zone to %s: status = %d, body = %s", zone, rec.Code, rec.Body.String())
	}
}

// The API refuses a zone it cannot load, so the only way one reaches the
// column is around the API: a hand-edited row, or zone data that changed
// under a stored name. Every date the household would then see is a guess.
// The request fails instead, for a session and for a token alike, because
// each has its own middleware and both must set the household's day.
func TestAHouseholdWhoseTimezoneCannotBeLoadedGetsA500NotAGuessedDate(t *testing.T) {
	env := newTestEnv(t)
	session, csrf := env.signIn(t, env.ownerEmail, env.ownerPassword)
	token := env.mustCreateToken(t, session, csrf, "script").Token

	if rec := env.authedGet(t, "/api/v1/auth/me", session); rec.Code != http.StatusOK {
		t.Fatalf("before the zone is broken, a session GET is %d, want 200", rec.Code)
	}
	if rec := env.bearer(t, http.MethodGet, "/api/v1/auth/me", nil, token); rec.Code != http.StatusOK {
		t.Fatalf("before the zone is broken, a token GET is %d, want 200", rec.Code)
	}

	if _, err := env.db.Pool().Exec(context.Background(),
		`UPDATE households SET timezone = 'Mars/Olympus_Mons' WHERE id = $1`, env.householdID); err != nil {
		t.Fatalf("break the stored zone: %v", err)
	}

	assertErrorResponse(t, env.authedGet(t, "/api/v1/auth/me", session), http.StatusInternalServerError, "INTERNAL")
	assertErrorResponse(t, env.bearer(t, http.MethodGet, "/api/v1/auth/me", nil, token), http.StatusInternalServerError, "INTERNAL")
}
