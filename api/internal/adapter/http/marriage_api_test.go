package httpadapter_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

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

// retroActionBody, retroDetailBody, retroSummaryBody, moodPointBody and
// retrosListWithDataBody mirror retro_handlers.go's unexported DTOs field
// for field, the same "local body struct in the _test package" shape
// goals_api_test.go's goalDTOBody already uses -- this file cannot import
// the unexported types directly.
type retroActionBody struct {
	ID                    string     `json:"id"`
	Body                  string     `json:"body"`
	DoneAt                *time.Time `json:"doneAt"`
	CarriedFrom           string     `json:"carriedFrom"`
	AssigneeMembershipIDs []string   `json:"assigneeMembershipIds"`
}

type retroDetailBody struct {
	Retro struct {
		ID          string            `json:"id"`
		Month       string            `json:"month"`
		Mood        *int              `json:"mood"`
		WentWell    string            `json:"wentWell"`
		WasHard     string            `json:"wasHard"`
		Notes       string            `json:"notes"`
		CompletedAt *time.Time        `json:"completedAt"`
		Version     int               `json:"version"`
		Actions     []retroActionBody `json:"actions"`
	} `json:"retro"`
	CarryOver []retroActionBody `json:"carryOver"`
}

type retroSummaryBody struct {
	ID          string `json:"id"`
	Month       string `json:"month"`
	Mood        *int   `json:"mood"`
	ActionCount int    `json:"actionCount"`
	Quote       string `json:"quote"`
	Finished    bool   `json:"finished"`
}

type retrosListWithDataBody struct {
	Retros []retroSummaryBody `json:"retros"`
	Mood   []struct {
		Month string `json:"month"`
		Mood  *int   `json:"mood"`
	} `json:"mood"`
	DoneCount  int     `json:"doneCount"`
	Since      *string `json:"since"`
	StartMonth *string `json:"startMonth"`
}

// TestRetroWireShapeWithRealDataMatchesTheBrief seeds a real, finished retro
// with one unassigned action through usecase.RetroService directly (env.deps
// exposes it, the same "real service, no HTTP" setup shape budget_api_test.go
// and goals_api_test.go's own setup helpers use), then reads it back through
// both marriage routes and pins every non-null-only field this task's brief
// promises Task 9's zod schemas will mirror.
//
// The empty-state test above only exercises toRetrosResponse's all-null
// branch; every toXxxDTO converter this task wrote otherwise never ran
// against real data before this test existed. AssigneeMembershipIDs is the
// case that matters most: Task 6's retro_action_repo_test.go documents that
// the repository returns nil, never []string{}, for an unassigned action,
// and toRetroActionDTO's own nil-to-[]string{} normalisation had never
// executed until this test called it.
func TestRetroWireShapeWithRealDataMatchesTheBrief(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()
	today := time.Now().UTC()

	// domain.StartableMonth picks the EARLIER of {previous, current} that has
	// no retro yet; a fresh household has neither, so this returns the
	// PREVIOUS calendar month, not today's -- read it off the created record
	// rather than assuming which month landed.
	created, err := env.deps.Retros.Start(ctx, env.householdID, today)
	if err != nil {
		t.Fatalf("start retro: %v", err)
	}

	mood := 4
	saved, err := env.deps.Retros.Save(ctx, usecase.RetroUpdate{
		HouseholdID: env.householdID,
		RetroID:     created.ID,
		Month:       created.Month,
		Mood:        &mood,
		WentWell:    "We stuck to the grocery budget.",
		WasHard:     "Christine's parents visiting threw off the schedule.",
		Notes:       "We finally fixed the grocery budget. Next month: try meal prep.",
		Version:     created.Version,
	})
	if err != nil {
		t.Fatalf("save retro: %v", err)
	}

	action, err := env.deps.Retros.AddAction(ctx, usecase.RetroActionInput{
		HouseholdID: env.householdID,
		RetroID:     saved.ID,
		Body:        "Set up a shared grocery list",
		// Deliberately nil, not []string{}: the state Task 6 documented the
		// repository actually returns for an unassigned action.
		AssigneeMembershipIDs: nil,
	})
	if err != nil {
		t.Fatalf("add action: %v", err)
	}

	if _, err := env.deps.Retros.Finish(ctx, env.householdID, saved.ID, today); err != nil {
		t.Fatalf("finish retro: %v", err)
	}

	session, _ := env.signIn(t, env.ownerEmail, env.ownerPassword)

	monthPath := "/api/v1/retros/" + created.Month.Format("2006-01")
	getRec := env.authedGet(t, monthPath, session)
	if getRec.Code != http.StatusOK {
		t.Fatalf("GET %s: status = %d, body = %s", monthPath, getRec.Code, getRec.Body.String())
	}
	t.Logf("GET %s body: %s", monthPath, getRec.Body.String())

	var detail retroDetailBody
	if err := json.Unmarshal(getRec.Body.Bytes(), &detail); err != nil {
		t.Fatalf("decode retro detail: %v (body = %s)", err, getRec.Body.String())
	}

	if detail.Retro.Month != created.Month.Format("2006-01") {
		t.Fatalf("month = %q, want %q", detail.Retro.Month, created.Month.Format("2006-01"))
	}
	if detail.Retro.Mood == nil || *detail.Retro.Mood != 4 {
		t.Fatalf("mood = %v, want 4", detail.Retro.Mood)
	}
	if detail.Retro.CompletedAt == nil {
		t.Fatal("completedAt = null, want a timestamp (Finish was called)")
	}
	if len(detail.Retro.Actions) != 1 {
		t.Fatalf("actions has %d entries, want 1", len(detail.Retro.Actions))
	}
	got := detail.Retro.Actions[0]
	if got.ID != action.ID {
		t.Fatalf("action id = %q, want %q", got.ID, action.ID)
	}
	if got.DoneAt != nil {
		t.Fatalf("doneAt = %v, want null (never ticked)", got.DoneAt)
	}
	if got.CarriedFrom != "" {
		t.Fatalf("carriedFrom = %q, want \"\" (not carried)", got.CarriedFrom)
	}
	if got.AssigneeMembershipIDs == nil {
		t.Fatal("assigneeMembershipIds = null, want [] -- toRetroActionDTO must normalise the repository's nil")
	}
	if len(got.AssigneeMembershipIDs) != 0 {
		t.Fatalf("assigneeMembershipIds = %v, want an empty array", got.AssigneeMembershipIDs)
	}
	if len(detail.CarryOver) != 0 {
		t.Fatalf("carryOver has %d entries, want 0 (nothing open the month before)", len(detail.CarryOver))
	}

	listRec := env.authedGet(t, "/api/v1/retros", session)
	if listRec.Code != http.StatusOK {
		t.Fatalf("GET /retros: status = %d, body = %s", listRec.Code, listRec.Body.String())
	}
	t.Logf("GET /api/v1/retros body: %s", listRec.Body.String())

	var list retrosListWithDataBody
	if err := json.Unmarshal(listRec.Body.Bytes(), &list); err != nil {
		t.Fatalf("decode retros list: %v (body = %s)", err, listRec.Body.String())
	}

	if len(list.Retros) != 1 {
		t.Fatalf("retros has %d entries, want 1", len(list.Retros))
	}
	summary := list.Retros[0]
	if summary.Month != created.Month.Format("2006-01") {
		t.Fatalf("summary month = %q, want %q", summary.Month, created.Month.Format("2006-01"))
	}
	if !summary.Finished {
		t.Fatal("finished = false, want true")
	}
	if summary.ActionCount != 1 {
		t.Fatalf("actionCount = %d, want 1", summary.ActionCount)
	}
	wantQuote := "We finally fixed the grocery budget."
	if summary.Quote != wantQuote {
		t.Fatalf("quote = %q, want %q (RetroService.List's FirstSentence(Notes))", summary.Quote, wantQuote)
	}
	if list.DoneCount != 1 {
		t.Fatalf("doneCount = %d, want 1", list.DoneCount)
	}
	if list.Since == nil || *list.Since != created.Month.Format("2006-01") {
		t.Fatalf("since = %v, want %q", list.Since, created.Month.Format("2006-01"))
	}
	var moodPoints int
	for _, m := range list.Mood {
		if m.Mood != nil {
			moodPoints++
		}
	}
	if moodPoints != 1 {
		t.Fatalf("mood chart has %d non-null points, want exactly 1", moodPoints)
	}
}
