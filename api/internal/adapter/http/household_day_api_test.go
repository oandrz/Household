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

// QA ISSUE-002. At 07:00 in Singapore the holding forms default to today's
// date, and the server, still on yesterday in UTC, answered "That date is in
// the future." The date a household may record a fact on is its own today.
func TestAHoldingFactDatedOnTheHouseholdsTodayIsAccepted(t *testing.T) {
	clk, utcDay := eveningClock()
	env := newTestEnvWithClock(t, clk)
	session, csrf := env.signIn(t, env.ownerEmail, env.ownerPassword)
	account := newHoldingAccount(t, env, session, csrf, "Brokerage", "investment")
	holding := newHolding(t, env, session, csrf, account, "D05")
	base := "/api/v1/holdings/" + holding

	// One day ahead of the server's UTC date: today for a household in
	// Singapore, tomorrow for one on UTC.
	singaporeToday := utcDay.AddDate(0, 0, 1).Format("2006-01-02")
	singaporeTomorrow := utcDay.AddDate(0, 0, 2).Format("2006-01-02")

	// The three writes a holding takes, each of which carries a date.
	writes := []struct {
		name     string
		path     string
		body     func(date string) map[string]any
		accepted int
	}{
		{"a purchase", base + "/events", func(date string) map[string]any {
			return map[string]any{"kind": "acquisition", "quantity": "10", "amountMinor": 30_000, "occurredOn": date}
		}, http.StatusCreated},
		{"a price", base + "/valuations", func(date string) map[string]any {
			return map[string]any{"unitPriceMinor": 3_100, "asOf": date}
		}, http.StatusOK},
		{"a dividend", base + "/income", func(date string) map[string]any {
			return map[string]any{"kind": "income", "amountMinor": 4_500, "receivedOn": date}
		}, http.StatusCreated},
	}

	// The control, and the reason this test can fail: the household is still
	// on UTC here, so the very date accepted below is tomorrow and refused.
	// Without it, a server that accepted any date at all would pass.
	for _, w := range writes {
		t.Run(w.name+" dated the server's tomorrow is refused while the household is on UTC", func(t *testing.T) {
			rec := env.authed(t, http.MethodPost, w.path, w.body(singaporeToday), session, csrf)
			assertErrorResponse(t, rec, http.StatusUnprocessableEntity, "INVALID_DATE")
		})
	}

	env.setTimezone(t, session, csrf, "Asia/Singapore")

	for _, w := range writes {
		t.Run(w.name+" dated the household's today is accepted once it is on Singapore", func(t *testing.T) {
			rec := env.authed(t, http.MethodPost, w.path, w.body(singaporeToday), session, csrf)
			if rec.Code != w.accepted {
				t.Fatalf("status = %d, want %d (body = %s)", rec.Code, w.accepted, rec.Body.String())
			}
		})
		t.Run(w.name+" dated the household's tomorrow is still refused", func(t *testing.T) {
			rec := env.authed(t, http.MethodPost, w.path, w.body(singaporeTomorrow), session, csrf)
			assertErrorResponse(t, rec, http.StatusUnprocessableEntity, "INVALID_DATE")
		})
	}

	// A token has its own middleware. hearthctl must get the household's day
	// too, not the server's.
	t.Run("a token gets the household's today as well", func(t *testing.T) {
		token := env.mustCreateToken(t, session, csrf, "script").Token
		rec := env.bearer(t, http.MethodPost, base+"/valuations",
			map[string]any{"unitPriceMinor": 3_200, "asOf": singaporeToday}, token)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200 (body = %s)", rec.Code, rec.Body.String())
		}
	})
}
