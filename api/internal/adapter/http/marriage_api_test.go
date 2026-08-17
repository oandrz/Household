package httpadapter_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	httpadapter "github.com/andreasoentoro/hearth/api/internal/adapter/http"
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
		// 2001-01, not any month near "today": TestRetroWireShapeWithRealDataMatchesTheBrief
		// seeds a real retro for the household's startable month (today's
		// previous month on a fresh household), and this route-walk's own
		// household is a separate newTestEnv/container each run -- but a
		// fixed near-today literal here would silently start meaning
		// something else the day any fixture shares a household, the same
		// "looks exhaustive, isn't" failure shape this task's mutation check
		// exists to catch elsewhere. 2001-01 (TestGetRetroForAnEmptyMonthIs404's
		// own literal) can never collide with a startable month.
		{http.MethodGet, "/api/v1/retros/2001-01", http.StatusNotFound},
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
	ID              string `json:"id"`
	Month           string `json:"month"`
	Mood            *int   `json:"mood"`
	ActionCount     int    `json:"actionCount"`
	OpenActionCount int    `json:"openActionCount"`
	Quote           string `json:"quote"`
	Finished        bool   `json:"finished"`
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
	// The one action on this retro was never ticked (asserted above:
	// detail.Retro.Actions[0].DoneAt == nil), so the open count equals the
	// total here -- this is what proves the wire actually carries the key
	// "openActionCount" (Task 9's zod schema mirrors this name exactly), not
	// just that the Go struct compiles.
	if summary.OpenActionCount != 1 {
		t.Fatalf("openActionCount = %d, want 1", summary.OpenActionCount)
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

// --- Task 8: marriage write routes and the 409 the screen can explain ------

// retroWriteBody mirrors retroWriteResponse (retro_handlers.go) field for
// field, the same "local body struct in the _test package" shape
// retroDetailBody above already uses for retroResponse -- this file cannot
// import the unexported type directly.
type retroWriteBody struct {
	Retro struct {
		ID      string `json:"id"`
		Month   string `json:"month"`
		Version int    `json:"version"`
	} `json:"retro"`
}

// mustStartRetro is test setup, not an assertion in itself: it POSTs /retros
// as whichever caller is passed in and fails the test immediately if that
// didn't succeed, so a broken start surfaces at the setup line rather than
// as a confusing failure in whichever test goes on to use the month, id or
// version it returns -- mustCreateAccount's own shape (api_test.go), applied
// to retros.
func mustStartRetro(t *testing.T, env *testEnv, session, csrf *http.Cookie) retroWriteBody {
	t.Helper()
	rec := env.authed(t, http.MethodPost, "/api/v1/retros", nil, session, csrf)
	if rec.Code != http.StatusCreated {
		t.Fatalf("start retro: status = %d, want 201 (body = %s)", rec.Code, rec.Body.String())
	}
	var body retroWriteBody
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode start-retro response: %v (body = %s)", err, rec.Body.String())
	}
	return body
}

// TestMarriageWriteRoutesRequireCSRF walks every mutating retro route with no
// CSRF token at all, and with one that does not match the cookie --
// TestTransactionWriteRoutesRequireCSRF's own shape (transactions_api_test.go),
// applied here for the identical two reasons its own comment gives:
// requireOwner sits ahead of requireCSRF in this group and answers the same
// 403, so a bare status check would stay green even with `m.Use(requireCSRF)`
// deleted from the marriage write sub-group in router.go; the CODE, not just
// the status, is what actually proves which guard refused. All seven writes
// are walked, not the five the task brief's own sketch named -- the tick and
// delete routes under /actions/{id} are exactly as mutating as the other
// five and deserve the identical proof.
//
// Every {id} below is a well-formed but non-existent UUID, and every {month}
// a plausible literal that names no real retro -- CSRF is checked before any
// lookup runs, so neither has to resolve to a real row for this test to be
// valid, the same reasoning TestTransactionWriteRoutesRequireCSRF's own
// zeroUUID relies on. That is also why hardcoding a month is safe only here:
// contrast TestPatchRetroWithAStaleVersionIs409RetroChanged below, which
// reads its month off a real created retro because its own handler runs
// past the CSRF gate and needs a row that actually exists.
func TestMarriageWriteRoutesRequireCSRF(t *testing.T) {
	env := newTestEnv(t)
	session, csrf := env.signIn(t, env.ownerEmail, env.ownerPassword)

	zeroUUID := "00000000-0000-0000-0000-000000000000"
	routes := []struct{ method, path string }{
		{http.MethodPost, "/api/v1/retros"},
		{http.MethodPatch, "/api/v1/retros/2026-07"},
		{http.MethodPost, "/api/v1/retros/2026-07/complete"},
		{http.MethodDelete, "/api/v1/retros/2026-07"},
		{http.MethodPost, "/api/v1/retros/2026-07/actions"},
		{http.MethodPatch, "/api/v1/retros/2026-07/actions/" + zeroUUID},
		{http.MethodDelete, "/api/v1/retros/2026-07/actions/" + zeroUUID},
	}

	for _, route := range routes {
		t.Run(route.method+" "+route.path, func(t *testing.T) {
			// No X-CSRF-Token header at all.
			req := httptest.NewRequest(route.method, route.path, nil)
			req.AddCookie(session)
			req.AddCookie(csrf)
			rec := httptest.NewRecorder()
			env.router.ServeHTTP(rec, req)
			assertErrorResponse(t, rec, http.StatusForbidden, "CSRF_INVALID")

			// Header present, but not the cookie's value.
			req2 := httptest.NewRequest(route.method, route.path, nil)
			req2.AddCookie(session)
			req2.AddCookie(csrf)
			req2.Header.Set("X-CSRF-Token", "definitely-the-wrong-value")
			rec2 := httptest.NewRecorder()
			env.router.ServeHTTP(rec2, req2)
			assertErrorResponse(t, rec2, http.StatusForbidden, "CSRF_INVALID")
		})
	}
}

// TestPatchRetroWithAStaleVersionIs409RetroChanged is the conflict the whole
// version column exists for, answered as a 409 with a code the frontend can
// branch on -- not a 500, and not a silent merge.
func TestPatchRetroWithAStaleVersionIs409RetroChanged(t *testing.T) {
	env := newTestEnv(t)
	session, csrf := env.signIn(t, env.ownerEmail, env.ownerPassword)
	created := mustStartRetro(t, env, session, csrf)

	path := "/api/v1/retros/" + created.Retro.Month
	body := map[string]any{"mood": 4, "wentWell": "mine", "wasHard": "", "notes": "", "version": created.Retro.Version}

	rec := env.authed(t, http.MethodPatch, path, body, session, csrf)
	if rec.Code != http.StatusOK {
		t.Fatalf("first save: status = %d, want 200 (body = %s)", rec.Code, rec.Body.String())
	}

	// Same (now stale) version again -- as if the other partner had already
	// saved and this tab never reloaded.
	rec = env.authed(t, http.MethodPatch, path, body, session, csrf)
	assertErrorResponse(t, rec, http.StatusConflict, "RETRO_CHANGED")
}

// TestPatchRetroReturnsTheIncrementedVersion pins the round-trip the brief's
// own "PATCH returns the retro including its new version" requirement
// exists for. Nothing else in this file reads `version` back out of a PATCH
// response at all: swapping handleSaveRetro's `updated` (Save's own return
// value) for the pre-write `view.Retro` at retro_handlers.go:307 would leave
// the status, TestPatchRetroWithAStaleVersionIs409RetroChanged and every
// JSON-parseability check in this file green, while a client trusting this
// field would send the stale version right back and get a spurious 409
// RETRO_CHANGED on every second save -- the single most confusing failure
// this feature could ship, since nothing about the save itself would have
// been wrong. The second PATCH below is what actually proves the round-trip
// rather than merely computing the expected number: it sends exactly the
// version the first response returned and requires that to succeed, which
// is precisely what a stale echo would break.
func TestPatchRetroReturnsTheIncrementedVersion(t *testing.T) {
	env := newTestEnv(t)
	session, csrf := env.signIn(t, env.ownerEmail, env.ownerPassword)
	created := mustStartRetro(t, env, session, csrf)
	path := "/api/v1/retros/" + created.Retro.Month

	rec := env.authed(t, http.MethodPatch, path,
		map[string]any{"mood": 4, "wentWell": "first", "wasHard": "", "notes": "", "version": created.Retro.Version},
		session, csrf)
	if rec.Code != http.StatusOK {
		t.Fatalf("first save: status = %d, want 200 (body = %s)", rec.Code, rec.Body.String())
	}
	var first retroWriteBody
	if err := json.Unmarshal(rec.Body.Bytes(), &first); err != nil {
		t.Fatalf("decode first save response: %v (body = %s)", err, rec.Body.String())
	}
	if first.Retro.Version != created.Retro.Version+1 {
		t.Fatalf("version = %d, want %d (exactly one higher than what was sent)",
			first.Retro.Version, created.Retro.Version+1)
	}

	// Send exactly the version the response above just returned. If the
	// handler had echoed a stale (pre-write) version instead, this would
	// fail with 409 RETRO_CHANGED even though nothing actually conflicted --
	// which is the concrete, frontend-visible symptom this test exists to
	// catch.
	rec = env.authed(t, http.MethodPatch, path,
		map[string]any{"mood": 3, "wentWell": "second", "wasHard": "", "notes": "", "version": first.Retro.Version},
		session, csrf)
	if rec.Code != http.StatusOK {
		t.Fatalf("second save using the version the first response returned: status = %d, want 200 (body = %s)",
			rec.Code, rec.Body.String())
	}
}

// TestPostRetroThirdTimeIsNothingToStart pins domain.ErrRetroNothingToStart's
// mapping. domain.StartableMonth never offers a month that already has a
// retro (spec decision 5), so a fresh household's first two POSTs each claim
// one of its two free candidate months and both succeed -- the double-click
// race TestStartRetroRaceIs409RetroExists below pins is NOT reachable by two
// sequential POSTs, only by two Create calls racing the SAME free month,
// which no sequential HTTP test can construct. It takes a third POST, once
// both candidates are taken, to reach domain.ErrRetroNothingToStart at all.
func TestPostRetroThirdTimeIsNothingToStart(t *testing.T) {
	env := newTestEnv(t)
	session, csrf := env.signIn(t, env.ownerEmail, env.ownerPassword)

	mustStartRetro(t, env, session, csrf)
	mustStartRetro(t, env, session, csrf)

	rec := env.authed(t, http.MethodPost, "/api/v1/retros", nil, session, csrf)
	assertErrorResponse(t, rec, http.StatusConflict, "RETRO_NOTHING_TO_START")
}

// alwaysExistsRetroRepo simulates the one race Start is actually exposed to:
// two partners tapping "Start retro" at the same instant, both passing the
// pre-check (ByMonth finds nothing yet for either candidate month) before
// only one of the two concurrent Create calls can win the underlying UNIQUE
// (household_id, month) constraint. No sequential HTTP call can build this
// state -- domain.StartableMonth always hands a fresh household its two free
// months in turn, one per POST (TestPostRetroThirdTimeIsNothingToStart above
// pins the two-already-taken case instead) -- so this is a
// usecase.RetroRepository double standing in for the concurrent write
// itself, the same seam membershipDouble above uses for a state no real
// write path can produce.
type alwaysExistsRetroRepo struct{}

func (alwaysExistsRetroRepo) Create(context.Context, string, time.Time) (usecase.RetroRecord, error) {
	return usecase.RetroRecord{}, domain.ErrAlreadyExists
}

func (alwaysExistsRetroRepo) ByMonth(context.Context, string, time.Time) (usecase.RetroRecord, error) {
	// Start's own pre-check (retroExists, usecase/retro.go) must see "no
	// retro yet" for both candidate months, or it would resolve straight to
	// domain.ErrRetroNothingToStart before Create is ever called --
	// ErrNotFound is what retroExists reads as "free."
	return usecase.RetroRecord{}, domain.ErrNotFound
}

func (alwaysExistsRetroRepo) List(context.Context, string) ([]usecase.RetroSummary, error) {
	return nil, nil
}

func (alwaysExistsRetroRepo) Update(context.Context, usecase.RetroUpdate) (usecase.RetroRecord, error) {
	return usecase.RetroRecord{}, domain.ErrNotFound
}

func (alwaysExistsRetroRepo) Complete(context.Context, string, string, time.Time) (usecase.RetroRecord, error) {
	return usecase.RetroRecord{}, domain.ErrNotFound
}

func (alwaysExistsRetroRepo) DeleteDraft(context.Context, string, string) error {
	return domain.ErrNotFound
}

// TestStartRetroRaceIs409RetroExists pins the race the task brief calls out
// by name: a prior task's review flagged that RetroService.Start returning
// domain.ErrAlreadyExists was undocumented and could plausibly fall through
// to a 500. It must answer 409 RETRO_EXISTS instead -- see
// alwaysExistsRetroRepo's own comment for why a repository double, rather
// than two real requests, is what it takes to build the state at all.
func TestStartRetroRaceIs409RetroExists(t *testing.T) {
	env := newTestEnv(t)
	session, csrf := env.signIn(t, env.ownerEmail, env.ownerPassword)

	// A router built on a copy of env.deps with only Retros swapped -- the
	// same shape env.routerWithMemberships uses for Memberships, applied to
	// the one other port a test in this file needs to substitute.
	d := env.deps
	d.Retros = usecase.NewRetroService(alwaysExistsRetroRepo{}, nil)
	router := httpadapter.NewRouter(d)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/retros", nil)
	req.AddCookie(session)
	req.AddCookie(csrf)
	req.Header.Set("X-CSRF-Token", csrf.Value)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	assertErrorResponse(t, rec, http.StatusConflict, "RETRO_EXISTS")
}

// TestDeleteAFinishedRetroIs404 pins that a finished retro cannot be
// deleted, and that the refusal is the same 404 a missing one gets -- the
// state on offer is "there is no draft here," not "that retro is finished."
func TestDeleteAFinishedRetroIs404(t *testing.T) {
	env := newTestEnv(t)
	session, csrf := env.signIn(t, env.ownerEmail, env.ownerPassword)
	created := mustStartRetro(t, env, session, csrf)

	path := "/api/v1/retros/" + created.Retro.Month
	rec := env.authed(t, http.MethodPost, path+"/complete", nil, session, csrf)
	if rec.Code != http.StatusOK {
		t.Fatalf("complete: status = %d, want 200 (body = %s)", rec.Code, rec.Body.String())
	}

	rec = env.authed(t, http.MethodDelete, path, nil, session, csrf)
	assertErrorResponse(t, rec, http.StatusNotFound, "NOT_FOUND")
}

// TestPatchRetroWithAnOutOfRangeMoodIs400InvalidMood pins domain.ErrInvalidMood's
// mapping over HTTP -- a row the task brief's own error table names, and
// nothing in this file before this test ever sent a mood outside 1..5
// through the route: a wrong status or a typo'd code string here would ship
// silently, and the frontend branches on this exact code.
func TestPatchRetroWithAnOutOfRangeMoodIs400InvalidMood(t *testing.T) {
	env := newTestEnv(t)
	session, csrf := env.signIn(t, env.ownerEmail, env.ownerPassword)
	created := mustStartRetro(t, env, session, csrf)

	path := "/api/v1/retros/" + created.Retro.Month
	rec := env.authed(t, http.MethodPatch, path,
		map[string]any{"mood": 7, "wentWell": "", "wasHard": "", "notes": "", "version": created.Retro.Version},
		session, csrf)
	assertErrorResponse(t, rec, http.StatusBadRequest, "INVALID_MOOD")
}

// TestAddRetroActionWithABlankBodyIs400 pins domain.ErrRetroActionBodyRequired's
// mapping over HTTP -- the brief's error table's other 400 row, likewise
// never exercised through the route before this test. A whitespace-only
// body, not a literal empty string, so this also proves AddAction's own
// trim-before-check ordering (RetroService.AddAction's doc comment) reaches
// the same refusal, not just the empty-string case.
func TestAddRetroActionWithABlankBodyIs400(t *testing.T) {
	env := newTestEnv(t)
	session, csrf := env.signIn(t, env.ownerEmail, env.ownerPassword)
	created := mustStartRetro(t, env, session, csrf)

	path := "/api/v1/retros/" + created.Retro.Month
	rec := env.authed(t, http.MethodPost, path+"/actions", map[string]any{"body": "   "}, session, csrf)
	assertErrorResponse(t, rec, http.StatusBadRequest, "RETRO_ACTION_BODY_REQUIRED")
}

// assertParseableJSONBody is TestEveryRetroWriteAnswersJSONExceptDelete's own
// check: the status is exactly wantStatus, AND the body parses as JSON --
// matching what apiFetch (web/src/lib/apiFetch.ts) actually does on an ok
// response. Both halves are load-bearing, checked here rather than left to
// each call site: a handler that resolved the wrong {id} (a broken
// chi.URLParam key, say) still answers a *parseable* JSON body -- it is just
// MapDomainError's own 4xx error envelope instead of the success shape the
// route is supposed to return. Checking parseability alone would call that
// a pass; checking the status here is what actually proves the write
// succeeded, not merely that whatever came back could be decoded.
func assertParseableJSONBody(t *testing.T, rec *httptest.ResponseRecorder, wantStatus int) {
	t.Helper()
	if rec.Code != wantStatus {
		t.Fatalf("status = %d, want %d (body = %s)", rec.Code, wantStatus, rec.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("response is not parseable JSON: %v (status = %d, body = %s)", err, rec.Code, rec.Body.String())
	}
}

// TestEveryRetroWriteAnswersJSONExceptDelete is 204's own boundary: every
// write in this group answers its OWN expected 2xx with a parseable JSON
// body except DELETE, which answers 204 with none -- apiFetch throws on an
// ok response it cannot parse, so this is not a stylistic nicety, it is what
// keeps the frontend from breaking on its own success path. The status is
// checked at every step, not just the shape of what came back: a route that
// resolved the wrong id would still answer a parseable JSON body (
// MapDomainError's own 4xx envelope), so parseability alone cannot tell a
// real success from a failure that merely decodes -- assertParseableJSONBody
// checks both, and the tick step additionally decodes doneAt to prove the
// write actually happened, not just that *a* 200 came back.
func TestEveryRetroWriteAnswersJSONExceptDelete(t *testing.T) {
	env := newTestEnv(t)
	session, csrf := env.signIn(t, env.ownerEmail, env.ownerPassword)

	created := mustStartRetro(t, env, session, csrf)
	path := "/api/v1/retros/" + created.Retro.Month

	// PATCH /retros/{month}
	assertParseableJSONBody(t, env.authed(t, http.MethodPatch, path,
		map[string]any{"mood": 3, "wentWell": "", "wasHard": "", "notes": "", "version": created.Retro.Version},
		session, csrf), http.StatusOK)

	// POST /retros/{month}/actions
	actionRec := env.authed(t, http.MethodPost, path+"/actions", map[string]any{"body": "Do a thing"}, session, csrf)
	assertParseableJSONBody(t, actionRec, http.StatusCreated)
	var action struct {
		Action struct {
			ID string `json:"id"`
		} `json:"action"`
	}
	if err := json.Unmarshal(actionRec.Body.Bytes(), &action); err != nil {
		t.Fatalf("decode add-action response: %v (body = %s)", err, actionRec.Body.String())
	}

	// PATCH /retros/{month}/actions/{id} -- the JSON-and-status check alone
	// cannot tell "the tick actually landed" from "the id lookup silently
	// resolved to nothing and MapDomainError's own error envelope happened
	// to parse", so decode the body and require doneAt to actually be set.
	tickRec := env.authed(t, http.MethodPatch, path+"/actions/"+action.Action.ID,
		map[string]any{"done": true}, session, csrf)
	assertParseableJSONBody(t, tickRec, http.StatusOK)
	var tick struct {
		ID     string     `json:"id"`
		DoneAt *time.Time `json:"doneAt"`
	}
	if err := json.Unmarshal(tickRec.Body.Bytes(), &tick); err != nil {
		t.Fatalf("decode tick response: %v (body = %s)", err, tickRec.Body.String())
	}
	if tick.ID != action.Action.ID {
		t.Fatalf("tick id = %q, want %q", tick.ID, action.Action.ID)
	}
	if tick.DoneAt == nil {
		t.Fatal("doneAt = null, want a timestamp -- done:true was sent")
	}

	// DELETE /retros/{month}/actions/{id} -- the one write that must NOT
	// carry a body.
	rec := env.authed(t, http.MethodDelete, path+"/actions/"+action.Action.ID, nil, session, csrf)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("delete action: status = %d, want 204 (body = %s)", rec.Code, rec.Body.String())
	}
	if rec.Body.Len() != 0 {
		t.Fatalf("delete action: body = %q, want empty", rec.Body.String())
	}

	// POST /retros/{month}/complete
	assertParseableJSONBody(t, env.authed(t, http.MethodPost, path+"/complete", nil, session, csrf), http.StatusOK)

	// POST /retros -- the first retro is now finished and cannot be
	// restarted, but this household still has one free candidate month
	// (domain.StartableMonth), so a second POST both proves POST /retros'
	// own 201 carries a body and gives DELETE /retros/{month} below a draft
	// that still exists to discard.
	second := mustStartRetro(t, env, session, csrf) // mustStartRetro already asserts 201 + a decodable body

	// DELETE /retros/{month}
	rec = env.authed(t, http.MethodDelete, "/api/v1/retros/"+second.Retro.Month, nil, session, csrf)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("delete draft: status = %d, want 204 (body = %s)", rec.Code, rec.Body.String())
	}
	if rec.Body.Len() != 0 {
		t.Fatalf("delete draft: body = %q, want empty", rec.Body.String())
	}
}
