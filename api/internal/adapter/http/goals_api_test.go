package httpadapter_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// --- Task 8: goal routes ----------------------------------------------------

// goalDTO mirrors goal_handlers.go's wire shape for one goal card.
type goalDTOBody struct {
	ID                   string  `json:"id"`
	Name                 string  `json:"name"`
	TargetMinor          int64   `json:"targetMinor"`
	Currency             string  `json:"currency"`
	TargetMonth          *string `json:"targetMonth"`
	PlannedMonthlyMinor  int64   `json:"plannedMonthlyMinor"`
	ContributedMinor     int64   `json:"contributedMinor"`
	Percent              int     `json:"percent"`
	Status               string  `json:"status"`
	RequiredMonthlyMinor int64   `json:"requiredMonthlyMinor"`
	RequiredMonthlyOK    bool    `json:"requiredMonthlyOk"`
	ArchivedAt           *string `json:"archivedAt"`
}

type goalResponseBody struct {
	Goal goalDTOBody `json:"goal"`
}

type goalsListResponseBody struct {
	Currency string        `json:"currency"`
	Goals    []goalDTOBody `json:"goals"`
	Summary  struct {
		PlannedMonthlyTotalMinor int64 `json:"plannedMonthlyTotalMinor"`
		ActualThisMonthMinor     int64 `json:"actualThisMonthMinor"`
		OnTrackCount             int   `json:"onTrackCount"`
		DatedCount               int   `json:"datedCount"`
		NoDateCount              int   `json:"noDateCount"`
		ExcludedNoRate           int   `json:"excludedNoRate"`
		NextGoal                 *struct {
			ID          string  `json:"id"`
			Name        string  `json:"name"`
			TargetMonth *string `json:"targetMonth"`
		} `json:"nextGoal"`
	} `json:"summary"`
}

type contributionBody struct {
	ID                string  `json:"id"`
	AmountMinor       int64   `json:"amountMinor"`
	OccurredOn        string  `json:"occurredOn"`
	Note              string  `json:"note"`
	Source            string  `json:"source"`
	SourceBudgetMonth *string `json:"sourceBudgetMonth"`
}

type contributionResponseBody struct {
	Contribution contributionBody `json:"contribution"`
}

type contributionsListBody struct {
	Contributions []contributionBody `json:"contributions"`
}

func decodeGoalsList(t *testing.T, rec *httptest.ResponseRecorder) goalsListResponseBody {
	t.Helper()
	var body goalsListResponseBody
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode goals list: %v (body = %s)", err, rec.Body.String())
	}
	return body
}

func decodeGoal(t *testing.T, rec *httptest.ResponseRecorder) goalResponseBody {
	t.Helper()
	var body goalResponseBody
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode goal: %v (body = %s)", err, rec.Body.String())
	}
	return body
}

// mustCreateGoal POSTs body to /goals as the owner and fails the test
// immediately if the create itself did not succeed -- setup, not the
// assertion under test, the same shape mustCreateAccountID uses.
func (env *testEnv) mustCreateGoal(t *testing.T, session, csrf *http.Cookie, body map[string]any) goalResponseBody {
	t.Helper()
	rec := env.authed(t, http.MethodPost, "/api/v1/goals", body, session, csrf)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create goal: status = %d, body = %s", rec.Code, rec.Body.String())
	}
	return decodeGoal(t, rec)
}

// --- route-walk matrix ------------------------------------------------------

// TestGoalRoutesRequireMoneyAndOwner is TestBudgetRoutesRequireMoneyAndOwner's
// and TestCategoryWriteRoutesRequireMoneyAndOwner's shape applied to all eight
// goal routes: reads and writes alike sit behind CapMoney AND requireOwner,
// the same as transactions, categories and budgets -- goals are as much "the
// household's money" as a ledger row.
//
// wantOwner pins the exact status an owner receives, not merely "not
// 401/403": a route wired with a nil deps.Goals would pass both guards and
// panic into a 500, which a bare non-401/403 check would let slide by
// unnoticed (the same reasoning transactions_api_test.go's comment gives).
//
// zeroUUID stands in for "a member of another household": this suite has no
// second-household fixture to build a real cross-household id from
// (budget_api_test.go's TestBudgetSaveValidationErrors makes the same call),
// and GoalRepository.Get's own contract makes the two indistinguishable --
// both simply match no row scoped to this household.
func TestGoalRoutesRequireMoneyAndOwner(t *testing.T) {
	env := newTestEnv(t)
	zeroUUID := "00000000-0000-0000-0000-000000000000"

	routes := []struct {
		method, path string
		wantOwner    int
	}{
		{http.MethodGet, "/api/v1/goals", http.StatusOK},
		// GET .../contributions performs no existence check on the goal id
		// (GoalService.Contributions' own contract -- see
		// TestGoalContributionsListForUnknownGoalIsEmpty below for why that
		// is deliberate, not a gap this test should paper over), so an
		// owner reaching it with a made-up id still gets 200 with an empty
		// list -- which still proves the guards passed and the handler is
		// wired, the same role wantOwner plays on every other row here.
		{http.MethodGet, "/api/v1/goals/" + zeroUUID + "/contributions", http.StatusOK},
		{http.MethodPost, "/api/v1/goals", http.StatusBadRequest},
		{http.MethodPatch, "/api/v1/goals/" + zeroUUID, http.StatusBadRequest},
		{http.MethodPost, "/api/v1/goals/" + zeroUUID + "/archive", http.StatusNotFound},
		{http.MethodPost, "/api/v1/goals/" + zeroUUID + "/restore", http.StatusNotFound},
		{http.MethodPost, "/api/v1/goals/" + zeroUUID + "/contributions", http.StatusBadRequest},
		{http.MethodDelete, "/api/v1/goals/" + zeroUUID + "/contributions/" + zeroUUID, http.StatusNotFound},
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
				t.Fatalf("no money capability = %d, want 403 (body = %s)", rec.Code, rec.Body.String())
			}

			session, csrf = env.signIn(t, env.moneyLimitedEmail, env.moneyLimitedPassword)
			rec = requestRouteAs(t, env, route.method, route.path, session, csrf)
			if rec.Code != http.StatusForbidden {
				t.Fatalf("limited member holding money = %d, want 403 (body = %s)", rec.Code, rec.Body.String())
			}

			session, csrf = env.signIn(t, env.ownerEmail, env.ownerPassword)
			rec = requestRouteAs(t, env, route.method, route.path, session, csrf)
			if rec.Code != route.wantOwner {
				t.Fatalf("owner = %d, want %d (body = %s)", rec.Code, route.wantOwner, rec.Body.String())
			}
		})
	}
}

// TestGoalWriteRoutesRequireCSRF mirrors TestCategoryWriteRoutesRequireCSRF
// for the six mutating goal routes: no token at all, and a token that does
// not match the cookie, both refused by CSRF_INVALID specifically -- not
// merely a 403, which requireOwner above it in the guard stack would also
// produce.
func TestGoalWriteRoutesRequireCSRF(t *testing.T) {
	env := newTestEnv(t)
	session, csrf := env.signIn(t, env.ownerEmail, env.ownerPassword)
	zeroUUID := "00000000-0000-0000-0000-000000000000"

	routes := []struct{ method, path string }{
		{http.MethodPost, "/api/v1/goals"},
		{http.MethodPatch, "/api/v1/goals/" + zeroUUID},
		{http.MethodPost, "/api/v1/goals/" + zeroUUID + "/archive"},
		{http.MethodPost, "/api/v1/goals/" + zeroUUID + "/restore"},
		{http.MethodPost, "/api/v1/goals/" + zeroUUID + "/contributions"},
		{http.MethodDelete, "/api/v1/goals/" + zeroUUID + "/contributions/" + zeroUUID},
	}

	for _, route := range routes {
		t.Run(route.method+" "+route.path, func(t *testing.T) {
			req := httptest.NewRequest(route.method, route.path, nil)
			req.AddCookie(session)
			req.AddCookie(csrf)
			rec := httptest.NewRecorder()
			env.router.ServeHTTP(rec, req)
			assertErrorResponse(t, rec, http.StatusForbidden, "CSRF_INVALID")

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

// --- behaviour ---------------------------------------------------------

// TestGoalListEmptyState pins the brief's "never 204, never 404" contract: a
// household with no goals still answers 200 with an empty (not null) goals
// array and a fully-populated, all-zero summary.
func TestGoalListEmptyState(t *testing.T) {
	env := newTestEnv(t)
	session, _ := env.signIn(t, env.ownerEmail, env.ownerPassword)

	rec := env.authedGet(t, "/api/v1/goals", session)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body = %s)", rec.Code, rec.Body.String())
	}
	if !json.Valid(rec.Body.Bytes()) {
		t.Fatalf("body is not valid JSON: %s", rec.Body.String())
	}
	body := decodeGoalsList(t, rec)
	if body.Goals == nil {
		t.Fatal("goals = nil, want an empty slice ([] on the wire, not null)")
	}
	if len(body.Goals) != 0 {
		t.Fatalf("goals = %+v, want none", body.Goals)
	}
	if body.Currency != "SGD" {
		t.Fatalf("currency = %q, want SGD (the seeded household's primary)", body.Currency)
	}
	if body.Summary.OnTrackCount != 0 || body.Summary.DatedCount != 0 || body.Summary.NoDateCount != 0 {
		t.Fatalf("summary counts = %+v, want all zero", body.Summary)
	}
	if body.Summary.NextGoal != nil {
		t.Fatalf("nextGoal = %+v, want nil", body.Summary.NextGoal)
	}

	// Confirm the raw wire shape too: "goals": [] literally, not omitted or
	// null, which decodeGoalsList's nil check above cannot itself prove --
	// json.Unmarshal happily leaves a Go nil slice for both `[]` and a
	// missing key.
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(rec.Body.Bytes(), &raw); err != nil {
		t.Fatalf("decode raw: %v", err)
	}
	if string(raw["goals"]) != "[]" {
		t.Fatalf(`raw "goals" = %s, want literal []`, raw["goals"])
	}
}

// TestGoalListReflectsRealDerivedFigures proves toGoalDTO and
// toGoalsResponse carry real, non-zero derived figures through the wire --
// not merely their zero-value shape, which every other test in this file
// happens to exercise (either an error path, or a goal nobody has
// contributed to yet). GoalService.List computes contributedMinor, percent,
// status, requiredMonthlyMinor and the summary's two totals; a mapping bug
// here -- swapping PlannedMonthlyTotal and ActualThisMonth, or wiring
// Percent to the wrong field -- would leave every other test in this file
// green. It is also the only test that proves writeGoal's re-read (the
// whole reason it exists instead of answering with the write call's own
// return value) actually fetches something other than zeros.
//
// The target month and the contribution's date are both computed relative
// to time.Now() rather than hardcoded: the harness runs against the real
// wall clock (clock.System{}), so a literal future month would eventually
// stop being future, and MonthContributionTotals filters strictly to the
// current calendar month, so a hardcoded date would eventually land outside
// it and read back actualThisMonthMinor as 0.
func TestGoalListReflectsRealDerivedFigures(t *testing.T) {
	env := newTestEnv(t)
	session, csrf := env.signIn(t, env.ownerEmail, env.ownerPassword)

	now := time.Now().UTC()
	// Six calendar months out from the first of the current month -- day-of-
	// month neutral, the same normalize-then-AddDate shape
	// TestBudgetHistoryMonthsIsClamped's own "base" local uses, so this does
	// not misbehave on a day (like the 31st) a later month does not have.
	targetMonth := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC).AddDate(0, 6, 0)
	occurredOn := time.Date(now.Year(), now.Month(), 14, 0, 0, 0, 0, time.UTC).Format("2006-01-02")

	created := env.mustCreateGoal(t, session, csrf, map[string]any{
		"name": "Derived figures goal", "targetMinor": 600_000,
		"plannedMonthlyMinor": 100_000, "targetMonth": targetMonth.Format("2006-01"),
	})

	addRec := env.authed(t, http.MethodPost, "/api/v1/goals/"+created.Goal.ID+"/contributions",
		map[string]any{"amountMinor": 300_000, "occurredOn": occurredOn}, session, csrf)
	if addRec.Code != http.StatusCreated {
		t.Fatalf("add contribution: status = %d, body = %s", addRec.Code, addRec.Body.String())
	}

	rec := env.authedGet(t, "/api/v1/goals", session)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body = %s)", rec.Code, rec.Body.String())
	}
	list := decodeGoalsList(t, rec)

	var got *goalDTOBody
	for i := range list.Goals {
		if list.Goals[i].ID == created.Goal.ID {
			got = &list.Goals[i]
		}
	}
	if got == nil {
		t.Fatalf("goal %s missing from the list", created.Goal.ID)
	}

	// contributedMinor: exactly the one contribution. percent: 300000/600000
	// -> 50, GoalProgressPercent's own rounding rule (domain/goal.go).
	if got.ContributedMinor != 300_000 {
		t.Fatalf("contributedMinor = %d, want 300000", got.ContributedMinor)
	}
	if got.Percent != 50 {
		t.Fatalf("percent = %d, want 50", got.Percent)
	}
	// Status: 300000 remaining over 7 months-left (6 full months out, plus
	// the current one, MonthsLeftInclusive's own counting rule) needs
	// ceil(300000/7) = 42858/month, comfortably under the 100000 planned --
	// on_track, deterministically, regardless of which day of the month this
	// test happens to run on.
	if got.Status != "on_track" {
		t.Fatalf("status = %q, want on_track", got.Status)
	}
	if !got.RequiredMonthlyOK {
		t.Fatal("requiredMonthlyOk = false, want true (a dated, unachieved goal)")
	}
	if got.RequiredMonthlyMinor <= 0 {
		t.Fatalf("requiredMonthlyMinor = %d, want a real positive figure", got.RequiredMonthlyMinor)
	}

	if list.Summary.PlannedMonthlyTotalMinor != 100_000 {
		t.Fatalf("summary.plannedMonthlyTotalMinor = %d, want 100000", list.Summary.PlannedMonthlyTotalMinor)
	}
	if list.Summary.ActualThisMonthMinor != 300_000 {
		t.Fatalf("summary.actualThisMonthMinor = %d, want 300000 (the manual contribution, dated this month)",
			list.Summary.ActualThisMonthMinor)
	}
	if list.Summary.NextGoal == nil {
		t.Fatal("summary.nextGoal = nil, want the one dated goal in this household")
	} else if list.Summary.NextGoal.ID != created.Goal.ID {
		t.Fatalf("summary.nextGoal.id = %q, want %q", list.Summary.NextGoal.ID, created.Goal.ID)
	}
}

// TestGoalCreateDefaultsCurrencyToHouseholdPrimary is the brief's "POST
// /goals without currency stores the household's primary" case.
func TestGoalCreateDefaultsCurrencyToHouseholdPrimary(t *testing.T) {
	env := newTestEnv(t)
	session, csrf := env.signIn(t, env.ownerEmail, env.ownerPassword)

	created := env.mustCreateGoal(t, session, csrf, map[string]any{
		"name": "Japan 2027", "targetMinor": 1_000_000,
		"plannedMonthlyMinor": 55_000, "startingBalanceMinor": 0,
	})
	if created.Goal.Currency != "SGD" {
		t.Fatalf("currency = %q, want SGD (the household's primary, no currency was sent)", created.Goal.Currency)
	}
}

// TestGoalCreateInvalidCurrencyIs422 is the brief's "ZZZ -> 422" case:
// domain.ParseCurrency refuses an unknown code, and MapDomainError's existing
// ErrInvalidMoney case (shared with accounts and households) answers
// INVALID_CURRENCY.
func TestGoalCreateInvalidCurrencyIs422(t *testing.T) {
	env := newTestEnv(t)
	session, csrf := env.signIn(t, env.ownerEmail, env.ownerPassword)

	rec := env.authed(t, http.MethodPost, "/api/v1/goals", map[string]any{
		"name": "Bad currency", "targetMinor": 100_000, "currency": "ZZZ",
		"plannedMonthlyMinor": 10_000,
	}, session, csrf)
	assertErrorResponse(t, rec, http.StatusUnprocessableEntity, "INVALID_CURRENCY")
}

// TestGoalCreateInvalidTargetMonthIs400 pins the brief's INVALID_MONTH/400
// shape -- the same status budget_api_test.go's TestBudgetMalformedMonthIs400
// pins for the {month} path segment, here for a body field instead.
func TestGoalCreateInvalidTargetMonthIs400(t *testing.T) {
	env := newTestEnv(t)
	session, csrf := env.signIn(t, env.ownerEmail, env.ownerPassword)

	rec := env.authed(t, http.MethodPost, "/api/v1/goals", map[string]any{
		"name": "Bad month", "targetMinor": 100_000, "plannedMonthlyMinor": 10_000,
		"targetMonth": "2026-13",
	}, session, csrf)
	assertErrorResponse(t, rec, http.StatusBadRequest, "INVALID_MONTH")
}

// TestGoalCreateNullTargetMonthIsDateless is the brief's "targetMonth: null
// on create is accepted" case: a goal with no target date is a real, valid
// state (status "none"), not an error.
func TestGoalCreateNullTargetMonthIsDateless(t *testing.T) {
	env := newTestEnv(t)
	session, csrf := env.signIn(t, env.ownerEmail, env.ownerPassword)

	created := env.mustCreateGoal(t, session, csrf, map[string]any{
		"name": "No deadline", "targetMinor": 100_000, "plannedMonthlyMinor": 10_000,
		"targetMonth": nil,
	})
	if created.Goal.TargetMonth != nil {
		t.Fatalf("targetMonth = %v, want nil", created.Goal.TargetMonth)
	}
	if created.Goal.Status != "none" {
		t.Fatalf("status = %q, want none", created.Goal.Status)
	}
}

// TestGoalCreateDuplicateNameIs409 is the brief's "duplicate name -> 409
// GOAL_NAME_TAKEN" case against another LIVE goal: the details object carries
// no archived-goal hint, because there is no archived goal to restore.
func TestGoalCreateDuplicateNameIs409(t *testing.T) {
	env := newTestEnv(t)
	session, csrf := env.signIn(t, env.ownerEmail, env.ownerPassword)

	env.mustCreateGoal(t, session, csrf, map[string]any{
		"name": "Wedding fund", "targetMinor": 500_000, "plannedMonthlyMinor": 20_000,
	})

	rec := env.authed(t, http.MethodPost, "/api/v1/goals", map[string]any{
		"name": "Wedding fund", "targetMinor": 100_000, "plannedMonthlyMinor": 10_000,
	}, session, csrf)
	body := assertErrorResponse(t, rec, http.StatusConflict, "GOAL_NAME_TAKEN")
	if _, ok := body.Error.Details["archivedGoalId"]; ok {
		t.Fatalf("details = %+v, want no archivedGoalId -- the colliding goal is live, not archived", body.Error.Details)
	}
}

// TestGoalCreateNameHeldByArchivedGoalIs409WithRestoreHint is the brief's
// harder case: a name held by an ARCHIVED goal still 409s (an archived row
// still occupies its name, GoalRepository.Create's own contract), but the
// body names the archived goal's id so the New Goal modal can offer Restore
// instead of a dead end -- the categories gotcha this task's brief calls out.
func TestGoalCreateNameHeldByArchivedGoalIs409WithRestoreHint(t *testing.T) {
	env := newTestEnv(t)
	session, csrf := env.signIn(t, env.ownerEmail, env.ownerPassword)

	created := env.mustCreateGoal(t, session, csrf, map[string]any{
		"name": "Old car fund", "targetMinor": 500_000, "plannedMonthlyMinor": 20_000,
	})
	archiveRec := env.authed(t, http.MethodPost, "/api/v1/goals/"+created.Goal.ID+"/archive", nil, session, csrf)
	if archiveRec.Code != http.StatusOK {
		t.Fatalf("archive: status = %d, body = %s", archiveRec.Code, archiveRec.Body.String())
	}

	rec := env.authed(t, http.MethodPost, "/api/v1/goals", map[string]any{
		"name": "Old car fund", "targetMinor": 100_000, "plannedMonthlyMinor": 10_000,
	}, session, csrf)
	body := assertErrorResponse(t, rec, http.StatusConflict, "GOAL_NAME_TAKEN")
	gotID, ok := body.Error.Details["archivedGoalId"]
	if !ok {
		t.Fatalf("details = %+v, want archivedGoalId naming %s", body.Error.Details, created.Goal.ID)
	}
	if gotID != created.Goal.ID {
		t.Fatalf("archivedGoalId = %v, want %q", gotID, created.Goal.ID)
	}
}

// TestGoalPatchClearTargetMonth is the brief's PATCH clearTargetMonth case:
// a dated goal loses its date, and a follow-up GET shows both the null date
// and the resulting "none" status.
func TestGoalPatchClearTargetMonth(t *testing.T) {
	env := newTestEnv(t)
	session, csrf := env.signIn(t, env.ownerEmail, env.ownerPassword)

	created := env.mustCreateGoal(t, session, csrf, map[string]any{
		"name": "Dated goal", "targetMinor": 500_000, "plannedMonthlyMinor": 20_000,
		"targetMonth": "2027-06",
	})
	if created.Goal.TargetMonth == nil {
		t.Fatal("created goal targetMonth = nil, want 2027-06 (setup did not take)")
	}

	rec := env.authed(t, http.MethodPatch, "/api/v1/goals/"+created.Goal.ID,
		map[string]any{"clearTargetMonth": true}, session, csrf)
	if rec.Code != http.StatusOK {
		t.Fatalf("patch: status = %d, body = %s", rec.Code, rec.Body.String())
	}
	patched := decodeGoal(t, rec)
	if patched.Goal.TargetMonth != nil {
		t.Fatalf("patch response targetMonth = %v, want nil", patched.Goal.TargetMonth)
	}

	getRec := env.authedGet(t, "/api/v1/goals", session)
	list := decodeGoalsList(t, getRec)
	var found bool
	for _, g := range list.Goals {
		if g.ID != created.Goal.ID {
			continue
		}
		found = true
		if g.TargetMonth != nil {
			t.Fatalf("get targetMonth = %v, want nil", g.TargetMonth)
		}
		if g.Status != "none" {
			t.Fatalf("get status = %q, want none", g.Status)
		}
	}
	if !found {
		t.Fatalf("goal %s missing from the list after clearing its date", created.Goal.ID)
	}
}

// TestGoalUpdateCurrencyFieldMismatchIs422 closes the brief's first
// ErrGoalCurrencyImmutable path: GoalUpdate carries no currency field at all
// (type-enforced immutability inside the service), so the wire is the only
// place a caller could even attempt this, and this handler must refuse it
// rather than silently drop it. A matching currency (a defensive client
// that always echoes what it displayed) is not an error at all.
func TestGoalUpdateCurrencyFieldMismatchIs422(t *testing.T) {
	env := newTestEnv(t)
	session, csrf := env.signIn(t, env.ownerEmail, env.ownerPassword)

	created := env.mustCreateGoal(t, session, csrf, map[string]any{
		"name": "SGD goal", "targetMinor": 500_000, "plannedMonthlyMinor": 20_000,
	})

	rec := env.authed(t, http.MethodPatch, "/api/v1/goals/"+created.Goal.ID,
		map[string]any{"currency": "IDR"}, session, csrf)
	assertErrorResponse(t, rec, http.StatusUnprocessableEntity, "GOAL_CURRENCY_IMMUTABLE")

	// A matching currency is not a mismatch -- the request otherwise carries
	// a real edit (renaming) that must still take effect.
	rec = env.authed(t, http.MethodPatch, "/api/v1/goals/"+created.Goal.ID,
		map[string]any{"currency": "SGD", "name": "SGD goal renamed"}, session, csrf)
	if rec.Code != http.StatusOK {
		t.Fatalf("matching currency: status = %d, want 200 (body = %s)", rec.Code, rec.Body.String())
	}
	if decodeGoal(t, rec).Goal.Name != "SGD goal renamed" {
		t.Fatalf("name = %q, want the rename to have gone through", decodeGoal(t, rec).Goal.Name)
	}
}

// TestGoalArchiveOmitsFromListRestoreUndoesUnion is the brief's archive/
// restore case, applied to goals: the default list omits an archived goal,
// ?include_archived=true returns it ALONGSIDE the live ones (a union, not a
// filter swap -- the same AccountRepository.List contract), restore undoes
// it, and the summary counts live goals only in BOTH responses.
func TestGoalArchiveOmitsFromListRestoreUndoesUnion(t *testing.T) {
	env := newTestEnv(t)
	session, csrf := env.signIn(t, env.ownerEmail, env.ownerPassword)

	live := env.mustCreateGoal(t, session, csrf, map[string]any{
		"name": "Stays live", "targetMinor": 500_000, "plannedMonthlyMinor": 20_000,
	})
	toArchive := env.mustCreateGoal(t, session, csrf, map[string]any{
		"name": "Gets archived", "targetMinor": 300_000, "plannedMonthlyMinor": 15_000,
	})

	archiveRec := env.authed(t, http.MethodPost, "/api/v1/goals/"+toArchive.Goal.ID+"/archive", nil, session, csrf)
	if archiveRec.Code != http.StatusOK {
		t.Fatalf("archive: status = %d, body = %s", archiveRec.Code, archiveRec.Body.String())
	}
	archived := decodeGoal(t, archiveRec)
	if archived.Goal.ArchivedAt == nil {
		t.Fatal("archive response archivedAt = nil, want set")
	}

	// Default list: the archived goal is gone, the live one remains, and the
	// summary counts only the live goal.
	defaultRec := env.authedGet(t, "/api/v1/goals", session)
	defaultList := decodeGoalsList(t, defaultRec)
	assertGoalAbsent(t, defaultList.Goals, toArchive.Goal.ID)
	assertGoalPresent(t, defaultList.Goals, live.Goal.ID)
	if defaultList.Summary.NoDateCount != 1 {
		t.Fatalf("default list noDateCount = %d, want 1 (the live goal only)", defaultList.Summary.NoDateCount)
	}

	// include_archived=true: both goals appear together (the union), and the
	// summary STILL counts the live goal only.
	unionRec := env.authedGet(t, "/api/v1/goals?include_archived=true", session)
	unionList := decodeGoalsList(t, unionRec)
	assertGoalPresent(t, unionList.Goals, toArchive.Goal.ID)
	assertGoalPresent(t, unionList.Goals, live.Goal.ID)
	if unionList.Summary.NoDateCount != 1 {
		t.Fatalf("union list noDateCount = %d, want 1 (archived goal excluded from every count)", unionList.Summary.NoDateCount)
	}

	// Restore brings it back into the default list.
	restoreRec := env.authed(t, http.MethodPost, "/api/v1/goals/"+toArchive.Goal.ID+"/restore", nil, session, csrf)
	if restoreRec.Code != http.StatusOK {
		t.Fatalf("restore: status = %d, body = %s", restoreRec.Code, restoreRec.Body.String())
	}
	if restored := decodeGoal(t, restoreRec); restored.Goal.ArchivedAt != nil {
		t.Fatal("restore response archivedAt != nil, want nil")
	}
	afterRestoreRec := env.authedGet(t, "/api/v1/goals", session)
	afterRestoreList := decodeGoalsList(t, afterRestoreRec)
	assertGoalPresent(t, afterRestoreList.Goals, toArchive.Goal.ID)
	if afterRestoreList.Summary.NoDateCount != 2 {
		t.Fatalf("after restore noDateCount = %d, want 2 (both goals live again)", afterRestoreList.Summary.NoDateCount)
	}
}

func assertGoalPresent(t *testing.T, goals []goalDTOBody, id string) {
	t.Helper()
	for _, g := range goals {
		if g.ID == id {
			return
		}
	}
	t.Fatalf("goal %s missing from list", id)
}

func assertGoalAbsent(t *testing.T, goals []goalDTOBody, id string) {
	t.Helper()
	for _, g := range goals {
		if g.ID == id {
			t.Fatalf("goal %s still present in list, want absent", id)
		}
	}
}

// TestGoalContributionValidationErrors is the brief's contribution-write
// validation table: a zero amount, an archived goal, and a currency that
// does not match the goal's own -- the second ErrGoalCurrencyImmutable path,
// this time on POST .../contributions rather than PATCH. A matching currency
// and an absent one both succeed, proving the check does not over-refuse.
func TestGoalContributionValidationErrors(t *testing.T) {
	env := newTestEnv(t)
	session, csrf := env.signIn(t, env.ownerEmail, env.ownerPassword)

	goal := env.mustCreateGoal(t, session, csrf, map[string]any{
		"name": "Contribution target", "targetMinor": 500_000, "plannedMonthlyMinor": 20_000,
	})
	archivedGoal := env.mustCreateGoal(t, session, csrf, map[string]any{
		"name": "Archived target", "targetMinor": 500_000, "plannedMonthlyMinor": 20_000,
	})
	archiveRec := env.authed(t, http.MethodPost, "/api/v1/goals/"+archivedGoal.Goal.ID+"/archive", nil, session, csrf)
	if archiveRec.Code != http.StatusOK {
		t.Fatalf("archive: status = %d, body = %s", archiveRec.Code, archiveRec.Body.String())
	}

	t.Run("zero amount", func(t *testing.T) {
		rec := env.authed(t, http.MethodPost, "/api/v1/goals/"+goal.Goal.ID+"/contributions",
			map[string]any{"amountMinor": 0, "occurredOn": "2026-08-14"}, session, csrf)
		assertErrorResponse(t, rec, http.StatusUnprocessableEntity, "CONTRIBUTION_AMOUNT_ZERO")
	})

	t.Run("archived goal", func(t *testing.T) {
		rec := env.authed(t, http.MethodPost, "/api/v1/goals/"+archivedGoal.Goal.ID+"/contributions",
			map[string]any{"amountMinor": 50_000, "occurredOn": "2026-08-14"}, session, csrf)
		assertErrorResponse(t, rec, http.StatusUnprocessableEntity, "GOAL_ARCHIVED")
	})

	t.Run("mismatched currency", func(t *testing.T) {
		rec := env.authed(t, http.MethodPost, "/api/v1/goals/"+goal.Goal.ID+"/contributions",
			map[string]any{"amountMinor": 50_000, "occurredOn": "2026-08-14", "currency": "IDR"}, session, csrf)
		assertErrorResponse(t, rec, http.StatusUnprocessableEntity, "GOAL_CURRENCY_IMMUTABLE")
	})

	t.Run("matching currency succeeds", func(t *testing.T) {
		rec := env.authed(t, http.MethodPost, "/api/v1/goals/"+goal.Goal.ID+"/contributions",
			map[string]any{"amountMinor": 50_000, "occurredOn": "2026-08-14", "note": "August transfer", "currency": "SGD"},
			session, csrf)
		if rec.Code != http.StatusCreated {
			t.Fatalf("matching currency: status = %d, want 201 (body = %s)", rec.Code, rec.Body.String())
		}
		var body contributionResponseBody
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatalf("decode: %v (body = %s)", err, rec.Body.String())
		}
		if body.Contribution.AmountMinor != 50_000 {
			t.Fatalf("amountMinor = %d, want 50000", body.Contribution.AmountMinor)
		}
		if body.Contribution.Source != "manual" {
			t.Fatalf("source = %q, want manual", body.Contribution.Source)
		}
	})

	t.Run("absent currency succeeds", func(t *testing.T) {
		rec := env.authed(t, http.MethodPost, "/api/v1/goals/"+goal.Goal.ID+"/contributions",
			map[string]any{"amountMinor": 25_000, "occurredOn": "2026-08-20"}, session, csrf)
		if rec.Code != http.StatusCreated {
			t.Fatalf("absent currency: status = %d, want 201 (body = %s)", rec.Code, rec.Body.String())
		}
	})
}

// TestGoalContributionsListForUnknownGoalIsEmpty pins GoalService.
// Contributions' real, documented contract: it performs no existence check
// on the goal id (unlike AddContribution, which calls Get first) --
// ListContributions simply filters by household_id AND goal_id together and
// returns whatever matches, zero rows included. A made-up id and a real but
// contribution-less goal are answered identically, and neither leaks
// anything about the other: this is deliberate, not a gap Task 8 is meant to
// close.
func TestGoalContributionsListForUnknownGoalIsEmpty(t *testing.T) {
	env := newTestEnv(t)
	session, _ := env.signIn(t, env.ownerEmail, env.ownerPassword)
	zeroUUID := "00000000-0000-0000-0000-000000000000"

	rec := env.authedGet(t, "/api/v1/goals/"+zeroUUID+"/contributions", session)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body = %s)", rec.Code, rec.Body.String())
	}
	var body contributionsListBody
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v (body = %s)", err, rec.Body.String())
	}
	if len(body.Contributions) != 0 {
		t.Fatalf("contributions = %+v, want none", body.Contributions)
	}
}

// TestGoalDeleteContributionTwiceIsNotFound is the brief's "deleting it
// twice -> the not-found shape" case: the first delete succeeds with 204 and
// no body, the second finds nothing left to remove.
func TestGoalDeleteContributionTwiceIsNotFound(t *testing.T) {
	env := newTestEnv(t)
	session, csrf := env.signIn(t, env.ownerEmail, env.ownerPassword)

	goal := env.mustCreateGoal(t, session, csrf, map[string]any{
		"name": "Delete target", "targetMinor": 500_000, "plannedMonthlyMinor": 20_000,
	})
	addRec := env.authed(t, http.MethodPost, "/api/v1/goals/"+goal.Goal.ID+"/contributions",
		map[string]any{"amountMinor": 50_000, "occurredOn": "2026-08-14"}, session, csrf)
	if addRec.Code != http.StatusCreated {
		t.Fatalf("add contribution: status = %d, body = %s", addRec.Code, addRec.Body.String())
	}
	var added contributionResponseBody
	if err := json.Unmarshal(addRec.Body.Bytes(), &added); err != nil {
		t.Fatalf("decode: %v", err)
	}

	deletePath := "/api/v1/goals/" + goal.Goal.ID + "/contributions/" + added.Contribution.ID
	delRec := env.authed(t, http.MethodDelete, deletePath, nil, session, csrf)
	if delRec.Code != http.StatusNoContent {
		t.Fatalf("first delete: status = %d, want 204 (body = %s)", delRec.Code, delRec.Body.String())
	}
	if delRec.Body.Len() != 0 {
		t.Fatalf("first delete body = %q, want empty", delRec.Body.String())
	}

	delRec2 := env.authed(t, http.MethodDelete, deletePath, nil, session, csrf)
	assertErrorResponse(t, delRec2, http.StatusNotFound, "NOT_FOUND")
}

// TestGoalDeleteContributionCrossGoalIsNotFound is the brief's "a
// contribution belonging to a different goal of the same household -> the
// not-found shape" case: it proves DeleteContribution checks the (goalID,
// contributionID) PAIR, not the contribution id alone -- a bug here would
// let a caller delete any contribution in the household by guessing its id
// against the wrong goal's URL.
func TestGoalDeleteContributionCrossGoalIsNotFound(t *testing.T) {
	env := newTestEnv(t)
	session, csrf := env.signIn(t, env.ownerEmail, env.ownerPassword)

	goalA := env.mustCreateGoal(t, session, csrf, map[string]any{
		"name": "Goal A", "targetMinor": 500_000, "plannedMonthlyMinor": 20_000,
	})
	goalB := env.mustCreateGoal(t, session, csrf, map[string]any{
		"name": "Goal B", "targetMinor": 500_000, "plannedMonthlyMinor": 20_000,
	})
	addRec := env.authed(t, http.MethodPost, "/api/v1/goals/"+goalA.Goal.ID+"/contributions",
		map[string]any{"amountMinor": 50_000, "occurredOn": "2026-08-14"}, session, csrf)
	if addRec.Code != http.StatusCreated {
		t.Fatalf("add contribution: status = %d, body = %s", addRec.Code, addRec.Body.String())
	}
	var added contributionResponseBody
	if err := json.Unmarshal(addRec.Body.Bytes(), &added); err != nil {
		t.Fatalf("decode: %v", err)
	}

	wrongPath := "/api/v1/goals/" + goalB.Goal.ID + "/contributions/" + added.Contribution.ID
	rec := env.authed(t, http.MethodDelete, wrongPath, nil, session, csrf)
	assertErrorResponse(t, rec, http.StatusNotFound, "NOT_FOUND")

	// The contribution is still there through its real goal -- the failed
	// cross-goal attempt above did not remove it.
	listRec := env.authedGet(t, "/api/v1/goals/"+goalA.Goal.ID+"/contributions", session)
	var list contributionsListBody
	if err := json.Unmarshal(listRec.Body.Bytes(), &list); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(list.Contributions) != 1 || list.Contributions[0].ID != added.Contribution.ID {
		t.Fatalf("goal A contributions = %+v, want exactly the one added", list.Contributions)
	}
}
