package httpadapter_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/andreasoentoro/hearth/api/internal/domain"
	"github.com/andreasoentoro/hearth/api/internal/usecase"
)

// --- Task 7: marriage read routes and the guard -----------------------------

// TestMarriageRoutesRequireMarriageAndOwner walks every marriage route
// against every member state, the same shape TestGoalRoutesRequireMoneyAndOwner
// and TestBudgetRoutesRequireMoneyAndOwner already use for money.
//
// There is no third caller shape here the way moneyLimitedEmail lets the
// money matrices separate requireCapability from requireOwner: a limited
// member cannot legitimately hold marriage at all
// (domain.ErrLimitedCannotHoldMarriage), so no real sign-in can ever reach
// requireOwner with requireCapability already satisfied.
// TestMarriageRouteRejectsALimitedMemberHoldingMarriage below is what proves
// requireOwner on its own merits instead, using a fixture no real write path
// can build.
func TestMarriageRoutesRequireMarriageAndOwner(t *testing.T) {
	env := newTestEnv(t)

	routes := []struct {
		method, path string
		wantOwner    int
	}{
		{http.MethodGet, "/api/v1/retros", http.StatusOK},
		{http.MethodGet, "/api/v1/retros/2026-07", http.StatusNotFound},
	}

	for _, route := range routes {
		t.Run(route.method+" "+route.path, func(t *testing.T) {
			rec := env.do(route.method, route.path, nil)
			if rec.Code != http.StatusUnauthorized {
				t.Fatalf("no session = %d, want 401 (body = %s)", rec.Code, rec.Body.String())
			}

			session, csrf := env.signIn(t, env.limitedEmail, env.limitedPassword)
			rec = requestRouteAs(t, env, route.method, route.path, session, csrf)
			if rec.Code != http.StatusForbidden {
				t.Fatalf("limited member = %d, want 403 (body = %s)", rec.Code, rec.Body.String())
			}

			session, csrf = env.signIn(t, env.ownerEmail, env.ownerPassword)
			rec = requestRouteAs(t, env, route.method, route.path, session, csrf)
			if rec.Code != route.wantOwner {
				t.Fatalf("owner = %d, want %d (body = %s)", rec.Code, route.wantOwner, rec.Body.String())
			}
		})
	}
}

// TestGetRetroRejectsAMalformedMonth pins the parseBudgetMonth contract this
// route reuses: an unparsable {month} answers 400 INVALID_MONTH before the
// service is ever called, the same shape TestBudgetMalformedMonthIs400 pins
// for budgets.
func TestGetRetroRejectsAMalformedMonth(t *testing.T) {
	env := newTestEnv(t)
	session, _ := env.signIn(t, env.ownerEmail, env.ownerPassword)

	rec := env.authedGet(t, "/api/v1/retros/July", session)
	assertErrorResponse(t, rec, http.StatusBadRequest, "INVALID_MONTH")
}

// TestGetRetroForAnEmptyMonthIs404 pins the brief's "not started, not an
// error" contract: a well-formed month with no retro row answers 404, which
// the page reads as an empty state.
func TestGetRetroForAnEmptyMonthIs404(t *testing.T) {
	env := newTestEnv(t)
	session, _ := env.signIn(t, env.ownerEmail, env.ownerPassword)

	rec := env.authedGet(t, "/api/v1/retros/2001-01", session)
	assertErrorResponse(t, rec, http.StatusNotFound, "NOT_FOUND")
}

// membershipDouble is a usecase.MembershipRepository stub answering every
// ByUser lookup with a fixed membership, regardless of which user asks.
// Every other method delegates to the embedded real repository, unused by
// either test below -- only ByUser is what requireSession
// (middleware_session.go) ever calls to populate Scope.Membership.
type membershipDouble struct {
	usecase.MembershipRepository
	membership domain.Membership
}

func (m membershipDouble) ByUser(context.Context, string) (domain.Membership, error) {
	return m.membership, nil
}

// TestMarriageRouteRejectsALimitedMemberHoldingMarriage is the mutation
// check the task brief calls for by name: prove requireOwner refuses on its
// own, not merely because no state reaching it today happens to also fail
// requireCapability.
//
// The brief's own wording assumes one gate beneath the HTTP layer
// (domain.ValidateMembershipChange, called only from usecase/member.go) and
// says to build the state "directly at the repository level" to get past
// it. Trying exactly that -- calling MembershipRepository.Create with
// {Role: RoleLimited, Capabilities: {CapMarriage}} -- surfaces a second gate
// this task did not expect: migrations/00002_identity.sql's
// limited_members_have_no_marriage CHECK constraint refuses the INSERT
// outright (postgres.MembershipRepo.Create's own doc comment names this as
// deliberate, "the second gate for exactly this reason"). A real household
// cannot reach this state through any write path this codebase has, service
// or repository -- domain.NewMembership, MemberService, and Postgres itself
// all refuse it independently.
//
// So this test builds the state one seam further out: a
// usecase.MembershipRepository double, substituted only for this one
// request via env.routerWithMemberships, standing in for "the day one of
// those three gates is relaxed and the caller reaches this router with
// scope.Membership already holding {RoleLimited, CapMarriage}." That is the
// literal scenario the money group's comment in router.go warns about, and
// proving it here is what makes m.Use(requireOwner) in the marriage group
// a tested line instead of a defensive one nobody would notice going quiet.
//
// The session itself is a real one -- env.limitedEmail, signed in through
// env.router exactly as a browser would -- so only Scope.Membership is
// doctored; Scope.UserID and Scope.HouseholdID still come from a real,
// live session row shared by both routers (same deps.Sessions).
// HouseholdID on the doctored membership must still match
// env.householdID: requireSession cross-checks
// membership.HouseholdID != record.HouseholdID and answers 401 on a
// mismatch, which would look like the guard was never reached rather than
// like it refused.
//
// requireCapability and requireOwner both answer the same FORBIDDEN code,
// so the response alone cannot say which one refused -- what isolates
// requireOwner here is the fixture itself: it HOLDS marriage, so
// requireCapability has nothing left to refuse, and only requireOwner can
// be the one answering 403.
func TestMarriageRouteRejectsALimitedMemberHoldingMarriage(t *testing.T) {
	env := newTestEnv(t)
	session, _ := env.signIn(t, env.limitedEmail, env.limitedPassword)

	router := env.routerWithMemberships(membershipDouble{
		MembershipRepository: env.deps.Memberships,
		membership: domain.Membership{
			HouseholdID:  env.householdID,
			UserID:       "irrelevant-scope-userid-comes-from-the-real-session",
			Role:         domain.RoleLimited,
			Capabilities: domain.Capabilities{domain.CapMarriage},
		},
	})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/retros", nil)
	req.AddCookie(session)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403 (body = %s)", rec.Code, rec.Body.String())
	}
	body := decodeError(t, rec)
	if body.Error.Code != "FORBIDDEN" {
		t.Fatalf("error code = %q, want FORBIDDEN (body = %s)", body.Error.Code, rec.Body.String())
	}
}

// TestMarriageRouteRejectsAnOwnerFixtureMissingMarriage is
// TestMarriageRouteRejectsALimitedMemberHoldingMarriage's mirror image,
// pinning requireCapability the same way that test pins requireOwner. No
// real owner can lack any capability -- migrations/00002_identity.sql's
// owners_hold_all_capabilities CHECK constraint is the same kind of second
// gate limited_members_have_no_marriage is for the other test -- so this
// uses the identical membershipDouble seam to build the state.
func TestMarriageRouteRejectsAnOwnerFixtureMissingMarriage(t *testing.T) {
	env := newTestEnv(t)
	session, _ := env.signIn(t, env.ownerEmail, env.ownerPassword)

	router := env.routerWithMemberships(membershipDouble{
		MembershipRepository: env.deps.Memberships,
		membership: domain.Membership{
			HouseholdID:  env.householdID,
			UserID:       "irrelevant-scope-userid-comes-from-the-real-session",
			Role:         domain.RoleOwner,
			Capabilities: domain.Capabilities{domain.CapCalendar, domain.CapChores, domain.CapMoney},
		},
	})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/retros", nil)
	req.AddCookie(session)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	assertErrorResponse(t, rec, http.StatusForbidden, "FORBIDDEN")
}

// TestRetrosListEmptyStateHasTheDocumentedShape is a light wire-level pin
// that the owner's 200 actually decodes to the JSON shape this task's brief
// promises Task 9's zod schemas will mirror field for field -- not a
// behavioural test of RetroService.List's arithmetic, which belongs to
// usecase/retro_test.go.
func TestRetrosListEmptyStateHasTheDocumentedShape(t *testing.T) {
	env := newTestEnv(t)
	session, _ := env.signIn(t, env.ownerEmail, env.ownerPassword)

	rec := env.authedGet(t, "/api/v1/retros", session)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body = %s)", rec.Code, rec.Body.String())
	}

	var body struct {
		Retros []json.RawMessage `json:"retros"`
		Mood   []struct {
			Month string `json:"month"`
			Mood  *int   `json:"mood"`
		} `json:"mood"`
		DoneCount  int     `json:"doneCount"`
		Since      *string `json:"since"`
		StartMonth *string `json:"startMonth"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v (body = %s)", err, rec.Body.String())
	}

	if body.Retros == nil {
		t.Fatal("retros = null, want an empty array")
	}
	if len(body.Mood) != 12 {
		t.Fatalf("mood has %d points, want 12", len(body.Mood))
	}
	if body.DoneCount != 0 {
		t.Fatalf("doneCount = %d, want 0 (a fresh household has no finished retros)", body.DoneCount)
	}
	if body.Since != nil {
		t.Fatalf("since = %v, want null", *body.Since)
	}
}
