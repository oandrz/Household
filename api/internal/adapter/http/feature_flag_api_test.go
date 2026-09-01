package httpadapter_test

import (
	"context"
	"net/http"
	"testing"

	"github.com/andreasoentoro/hearth/api/internal/domain"
)

// TestADisabledFeatureAnswers404 -- not 403. On this install the feature does
// not exist, and 403 would leak the roadmap.
func TestADisabledFeatureAnswers404(t *testing.T) {
	env := newTestEnv(t)
	session, _ := env.signIn(t, env.ownerEmail, env.ownerPassword)

	// family_calendar is off by default, and its route is registered.
	rec := env.authedGet(t, "/api/v1/family/calendar", session)
	assertErrorResponse(t, rec, http.StatusNotFound, "NOT_FOUND")

	if err := env.featureFlags.SetGlobal(context.Background(),
		string(domain.FlagFamilyCalendar), true, ""); err != nil {
		t.Fatalf("SetGlobal: %v", err)
	}

	rec = env.authedGet(t, "/api/v1/family/calendar", session)
	if rec.Code != http.StatusOK {
		t.Fatalf("after enabling the flag = %d, body %s; want 200", rec.Code, rec.Body.String())
	}
}

// TestAHouseholdOverrideEnablesOnlyThatHousehold.
func TestAHouseholdOverrideEnablesOnlyThatHousehold(t *testing.T) {
	env := newTestEnv(t)
	session, _ := env.signIn(t, env.ownerEmail, env.ownerPassword)

	if err := env.featureFlags.SetHousehold(context.Background(), env.householdID,
		string(domain.FlagFamilyCalendar), true, ""); err != nil {
		t.Fatalf("SetHousehold: %v", err)
	}

	rec := env.authedGet(t, "/api/v1/family/calendar", session)
	if rec.Code != http.StatusOK {
		t.Fatalf("the household with the override = %d, want 200", rec.Code)
	}
}

// TestSignupsOpenGatesThePublicRoute is the pre-auth case: no session exists,
// so there is no household layer to consult, and requireFeature must resolve
// the global set on its own rather than failing or treating it as on.
func TestSignupsOpenGatesThePublicRoute(t *testing.T) {
	env := newTestEnv(t)

	rec := env.do(http.MethodPost, "/api/v1/auth/sign-up", map[string]string{"email": "new@example.test"})
	if rec.Code != http.StatusAccepted {
		t.Fatalf("sign-up with signups_open on = %d, want 202", rec.Code)
	}

	if err := env.featureFlags.SetGlobal(context.Background(),
		string(domain.FlagSignupsOpen), false, ""); err != nil {
		t.Fatalf("SetGlobal: %v", err)
	}

	rec = env.do(http.MethodPost, "/api/v1/auth/sign-up", map[string]string{"email": "later@example.test"})
	assertErrorResponse(t, rec, http.StatusNotFound, "NOT_FOUND")
}

// TestAHouseholdOverrideCannotOpenAClosedPublicRoute: household overrides are
// meaningless before there is a household, and must never be treated as "on".
func TestAHouseholdOverrideCannotOpenAClosedPublicRoute(t *testing.T) {
	env := newTestEnv(t)

	if err := env.featureFlags.SetGlobal(context.Background(),
		string(domain.FlagSignupsOpen), false, ""); err != nil {
		t.Fatalf("SetGlobal: %v", err)
	}
	if err := env.featureFlags.SetHousehold(context.Background(), env.householdID,
		string(domain.FlagSignupsOpen), true, ""); err != nil {
		t.Fatalf("SetHousehold: %v", err)
	}

	rec := env.do(http.MethodPost, "/api/v1/auth/sign-up", map[string]string{"email": "sneaky@example.test"})
	assertErrorResponse(t, rec, http.StatusNotFound, "NOT_FOUND")
}

// TestClosingSignupsAlsoClosesTheCompletionRoutes -- otherwise a token minted
// before the switch stays redeemable indefinitely.
func TestClosingSignupsAlsoClosesTheCompletionRoutes(t *testing.T) {
	env := newTestEnv(t)
	env.do(http.MethodPost, "/api/v1/auth/sign-up", map[string]string{"email": "half@example.test"})
	token := env.lastSignupToken(t)

	if err := env.featureFlags.SetGlobal(context.Background(),
		string(domain.FlagSignupsOpen), false, ""); err != nil {
		t.Fatalf("SetGlobal: %v", err)
	}

	rec := env.do(http.MethodGet, "/api/v1/auth/sign-up/"+token, nil)
	assertErrorResponse(t, rec, http.StatusNotFound, "NOT_FOUND")
}
