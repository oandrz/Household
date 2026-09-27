package httpadapter_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// --- vision routes and the guard -------------------------------------------

// visionMeasureBody, visionPillarBody, visionMilestoneBody, visionBody and
// visionResponseBody mirror vision_handlers.go's unexported DTOs field for
// field -- the same "local body struct in the _test package" shape
// retroDetailBody uses, since this file cannot import the unexported types
// directly.
type visionMeasureBody struct {
	Label     string `json:"label"`
	Kind      string `json:"kind"`
	HasFigure bool   `json:"hasFigure"`
	Current   int    `json:"current"`
	Target    int    `json:"target"`
	Percent   int    `json:"percent"`
	Met       bool   `json:"met"`
	GoalID    string `json:"goalId"`
	GoalName  string `json:"goalName"`
}

type visionPillarBody struct {
	Name        string              `json:"name"`
	Description string              `json:"description"`
	Measures    []visionMeasureBody `json:"measures"`
}

type visionMilestoneBody struct {
	Year  int    `json:"year"`
	Title string `json:"title"`
	Note  string `json:"note"`
}

type visionBody struct {
	Year        int                   `json:"year"`
	Theme       string                `json:"theme"`
	Description string                `json:"description"`
	Version     int                   `json:"version"`
	Pillars     []visionPillarBody    `json:"pillars"`
	Milestones  []visionMilestoneBody `json:"milestones"`
}

type visionResponseBody struct {
	Vision visionBody `json:"vision"`
}

func decodeVision(t *testing.T, rec *httptest.ResponseRecorder) visionResponseBody {
	t.Helper()
	var body visionResponseBody
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode vision response: %v (body = %s)", err, rec.Body.String())
	}
	return body
}

// TestGetVisionReturnsAnEmptyDocumentForAYearNeverSet pins
// VisionService.Get's own empty-state contract: never a 404, and version 0
// so the next save is read as a create.
func TestGetVisionReturnsAnEmptyDocumentForAYearNeverSet(t *testing.T) {
	env := newTestEnv(t)
	session, _ := env.signIn(t, env.ownerEmail, env.ownerPassword)

	rec := env.authedGet(t, "/api/v1/marriage/vision?year=2026", session)
	if rec.Code != http.StatusOK {
		t.Fatalf("want 200 for a year never set, never 404; got %d (body = %s)", rec.Code, rec.Body.String())
	}
	body := decodeVision(t, rec)
	if body.Vision.Version != 0 {
		t.Fatalf("an unset year must carry version 0, got %d", body.Vision.Version)
	}
}

// TestGetVisionAsALimitedMemberIsRefused pins the read guard on its own
// merits, the counterpart to TestPutVisionAsALimitedMemberIsRefused below --
// without it, a mutation moving the GET route out of the marriage guard
// group would go undetected.
func TestGetVisionAsALimitedMemberIsRefused(t *testing.T) {
	env := newTestEnv(t)
	session, _ := env.signIn(t, env.limitedEmail, env.limitedPassword)

	rec := env.authedGet(t, "/api/v1/marriage/vision?year=2026", session)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("want 403 for a member without the marriage capability, got %d (body = %s)", rec.Code, rec.Body.String())
	}
}

// TestGetVisionWithAYearOutOfRangeIs422 pins the bounds check
// parseVisionYear's own doc comment explains: a bare strconv.Atoi would let
// 65538 wrap to year 2 at the repository's int16 cast and silently read the
// wrong row, so the explicit range check runs first and refuses this with
// 422 instead.
func TestGetVisionWithAYearOutOfRangeIs422(t *testing.T) {
	env := newTestEnv(t)
	session, _ := env.signIn(t, env.ownerEmail, env.ownerPassword)

	rec := env.authedGet(t, "/api/v1/marriage/vision?year=65538", session)
	assertErrorResponse(t, rec, http.StatusUnprocessableEntity, "INVALID_YEAR")
}

// TestGetVisionForANeverSetYearCarriesLiteralEmptyArrays reads the raw wire
// bytes, not the decoded struct: encoding/json turns both "null" and "[]"
// into a nil slice of length 0, so only the raw bytes can prove the
// empty-vision branch serialises "[]" and never "null" -- what the
// frontend's apiFetch and zod schemas assume for every collection.
func TestGetVisionForANeverSetYearCarriesLiteralEmptyArrays(t *testing.T) {
	env := newTestEnv(t)
	session, _ := env.signIn(t, env.ownerEmail, env.ownerPassword)

	rec := env.authedGet(t, "/api/v1/marriage/vision?year=2099", session)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body = %s)", rec.Code, rec.Body.String())
	}
	raw := rec.Body.String()
	if !strings.Contains(raw, `"pillars":[]`) {
		t.Fatalf("wire body does not literally carry \"pillars\":[] -- got %s", raw)
	}
	if !strings.Contains(raw, `"milestones":[]`) {
		t.Fatalf("wire body does not literally carry \"milestones\":[] -- got %s", raw)
	}
}

// TestPutVisionWithoutACSRFTokenIsRefused applies
// TestMarriageWriteRoutesRequireCSRF's shape to the vision write route:
// session and csrf cookies are present but no X-CSRF-Token header is sent,
// so requireCSRF -- ahead of requireOwner and the handler -- must be what
// refuses this, not a missing-body 400 or a stale-version 409.
func TestPutVisionWithoutACSRFTokenIsRefused(t *testing.T) {
	env := newTestEnv(t)
	session, csrf := env.signIn(t, env.ownerEmail, env.ownerPassword)

	req := httptest.NewRequest(http.MethodPut, "/api/v1/marriage/vision/2026",
		strings.NewReader(`{"version":0,"theme":"Slow down together","description":"","pillars":[],"milestones":[]}`))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(session)
	req.AddCookie(csrf)
	// Deliberately no X-CSRF-Token header at all.
	rec := httptest.NewRecorder()
	env.router.ServeHTTP(rec, req)
	assertErrorResponse(t, rec, http.StatusForbidden, "CSRF_INVALID")
}

// TestPutVisionAsALimitedMemberIsRefused proves the marriage capability
// gate: env.limitedEmail holds calendar and chores only, and no membership
// can hold marriage without also being an owner
// (domain.ErrLimitedCannotHoldMarriage) -- the same reason
// TestMarriageRoutesRequireMarriageAndOwner needs no third caller shape.
func TestPutVisionAsALimitedMemberIsRefused(t *testing.T) {
	env := newTestEnv(t)
	session, csrf := env.signIn(t, env.limitedEmail, env.limitedPassword)

	rec := env.authed(t, http.MethodPut, "/api/v1/marriage/vision/2026",
		map[string]any{"version": 0, "theme": "T", "description": "", "pillars": []any{}, "milestones": []any{}},
		session, csrf)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("want 403 for a member without the marriage capability, got %d (body = %s)", rec.Code, rec.Body.String())
	}
}

// TestPutVisionWithAYearOutOfRangeIs422 is the save-path counterpart of
// TestGetVisionWithAYearOutOfRangeIs422: VisionService.Save validates its
// own year too, but the handler must not depend on that downstream check to
// catch a malformed URL segment, so this pins that the handler's own check
// runs first -- INVALID_YEAR, not the service's VISION_INVALID.
func TestPutVisionWithAYearOutOfRangeIs422(t *testing.T) {
	env := newTestEnv(t)
	session, csrf := env.signIn(t, env.ownerEmail, env.ownerPassword)

	rec := env.authed(t, http.MethodPut, "/api/v1/marriage/vision/65538",
		map[string]any{"version": 0, "theme": "T", "description": "", "pillars": []any{}, "milestones": []any{}},
		session, csrf)
	assertErrorResponse(t, rec, http.StatusUnprocessableEntity, "INVALID_YEAR")
}

// TestPutVisionWithAStaleVersionIsAConflict is the conflict the version
// column exists for: a second save carrying the same (now stale) version 0
// must answer 409 VISION_CHANGED, never a silent overwrite of the first.
func TestPutVisionWithAStaleVersionIsAConflict(t *testing.T) {
	env := newTestEnv(t)
	session, csrf := env.signIn(t, env.ownerEmail, env.ownerPassword)

	body := map[string]any{"version": 0, "theme": "Slow down together", "description": "", "pillars": []any{}, "milestones": []any{}}
	first := env.authed(t, http.MethodPut, "/api/v1/marriage/vision/2026", body, session, csrf)
	if first.Code != http.StatusOK {
		t.Fatalf("first save: want 200, got %d (body = %s)", first.Code, first.Body.String())
	}

	second := env.authed(t, http.MethodPut, "/api/v1/marriage/vision/2026",
		map[string]any{"version": 0, "theme": "Overwritten", "description": "", "pillars": []any{}, "milestones": []any{}},
		session, csrf)
	assertErrorResponse(t, second, http.StatusConflict, "VISION_CHANGED")
}

// TestPutVisionWithALinkedMeasureMissingAGoalIs422 pins errors.go's split of
// the domain's two measure-shape sentinels into two codes and messages: a
// household that switches a measure to "A savings goal" and saves without
// picking one has picked NEITHER, so it must never get
// VISION_MEASURE_INVALID's "not both" copy, which is wrong for this shape.
//
// VisionModal.tsx's own client-side check normally stops this request from
// leaving the browser, so nothing else in this codebase reaches
// MapDomainError's ErrVisionMeasureGoalRequired case -- only a test that
// calls the handler directly, bypassing that guard the way a stale
// frontend build or a non-browser caller would, can prove it is wired
// correctly.
func TestPutVisionWithALinkedMeasureMissingAGoalIs422(t *testing.T) {
	env := newTestEnv(t)
	session, csrf := env.signIn(t, env.ownerEmail, env.ownerPassword)

	body := map[string]any{
		"version":     0,
		"theme":       "Slow down together",
		"description": "",
		"pillars": []any{
			map[string]any{
				"name":        "Us before logistics",
				"description": "",
				"measures": []any{
					map[string]any{"label": "Emergency fund", "kind": "linked", "goalId": ""},
				},
			},
		},
		"milestones": []any{},
	}
	rec := env.authed(t, http.MethodPut, "/api/v1/marriage/vision/2026", body, session, csrf)
	assertErrorResponse(t, rec, http.StatusUnprocessableEntity, "VISION_MEASURE_GOAL_REQUIRED")
}

// TestPutVisionRoundTripsPillarsAndMilestones pins that a save's response
// carries back what was sent, composed with the service's own arithmetic (a
// 2-of-2 typed measure comes back Met). VisionService's compose logic is
// tested in usecase/vision_test.go; this proves the HTTP layer moves that
// shape onto the wire intact.
func TestPutVisionRoundTripsPillarsAndMilestones(t *testing.T) {
	env := newTestEnv(t)
	session, csrf := env.signIn(t, env.ownerEmail, env.ownerPassword)

	body := map[string]any{
		"version":     0,
		"theme":       "Slow down together",
		"description": "Fewer commitments, more presence.",
		"pillars": []any{
			map[string]any{
				"name":        "Us before logistics",
				"description": "Partners first.",
				"measures": []any{
					map[string]any{"label": "Date nights / month", "kind": "typed", "current": 2, "target": 2},
				},
			},
		},
		"milestones": []any{
			map[string]any{"year": 2027, "title": "Sabbatical", "note": "Indonesia"},
		},
	}
	rec := env.authed(t, http.MethodPut, "/api/v1/marriage/vision/2026", body, session, csrf)
	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", rec.Code, rec.Body.String())
	}
	got := decodeVision(t, rec)
	if got.Vision.Version != 1 {
		t.Fatalf("a create must come back at version 1, got %d", got.Vision.Version)
	}
	if len(got.Vision.Pillars) != 1 || len(got.Vision.Pillars[0].Measures) != 1 {
		t.Fatalf("pillars did not round-trip: %+v", got.Vision.Pillars)
	}
	if !got.Vision.Pillars[0].Measures[0].Met {
		t.Fatal("2 of 2 must come back met")
	}
	if len(got.Vision.Milestones) != 1 {
		t.Fatalf("milestones did not round-trip: %+v", got.Vision.Milestones)
	}
}

// TestGetVisionWithNoYearDefaultsToTheCurrentYear pins the branch
// handleGetVision takes when the caller names no year at all
// (VisionService.CurrentYear()'s own contract) -- the shape of a page's
// first request on load, which no test above this one exercises: every
// other test in this file sends an explicit ?year=.
func TestGetVisionWithNoYearDefaultsToTheCurrentYear(t *testing.T) {
	env := newTestEnv(t)
	session, _ := env.signIn(t, env.ownerEmail, env.ownerPassword)

	rec := env.authedGet(t, "/api/v1/marriage/vision", session)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body = %s)", rec.Code, rec.Body.String())
	}
	body := decodeVision(t, rec)
	wantYear := time.Now().Year()
	if body.Vision.Year != wantYear {
		t.Fatalf("year = %d, want %d (today's year, since no ?year was given)", body.Vision.Year, wantYear)
	}
}

// TestPutVisionRoundTripsALinkedMeasure closes a coverage gap: no test
// above links a measure to a real goal, so four wire fields the frontend's
// zod schema mirrors -- hasFigure, percent, goalId, goalName -- have never
// been observed populated.
//
// The goal is given one contribution bringing it to a distinctive 50%,
// deliberately not 0: Current is always 0 for a linked measure
// (toMeasureView, usecase/vision.go, never sets it), so a regression that
// swapped Percent for Current in toVisionDTO would still read 0 and pass
// undetected against an untouched goal -- only a genuinely non-zero,
// non-Current percent makes that mutation visible.
func TestPutVisionRoundTripsALinkedMeasure(t *testing.T) {
	env := newTestEnv(t)
	session, csrf := env.signIn(t, env.ownerEmail, env.ownerPassword)

	goal := env.mustCreateGoal(t, session, csrf, map[string]any{
		"name": "House deposit", "targetMinor": 1_000_000, "plannedMonthlyMinor": 50_000,
	})
	contribRec := env.authed(t, http.MethodPost, "/api/v1/goals/"+goal.Goal.ID+"/contributions",
		map[string]any{"amountMinor": 500_000, "occurredOn": time.Now().Format("2006-01-02")}, session, csrf)
	if contribRec.Code != http.StatusCreated {
		t.Fatalf("add contribution: status = %d, body = %s", contribRec.Code, contribRec.Body.String())
	}

	body := map[string]any{
		"version":     0,
		"theme":       "Building our nest",
		"description": "",
		"pillars": []any{
			map[string]any{
				"name":        "Money together",
				"description": "",
				"measures": []any{
					map[string]any{"label": "House deposit", "kind": "linked", "goalId": goal.Goal.ID},
				},
			},
		},
		"milestones": []any{},
	}
	rec := env.authed(t, http.MethodPut, "/api/v1/marriage/vision/2026", body, session, csrf)
	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", rec.Code, rec.Body.String())
	}
	got := decodeVision(t, rec)
	if len(got.Vision.Pillars) != 1 || len(got.Vision.Pillars[0].Measures) != 1 {
		t.Fatalf("pillars did not round-trip: %+v", got.Vision.Pillars)
	}
	measure := got.Vision.Pillars[0].Measures[0]
	if measure.Kind != "linked" {
		t.Fatalf("kind = %q, want \"linked\"", measure.Kind)
	}
	if !measure.HasFigure {
		t.Fatal("hasFigure = false, want true -- a resolved linked goal must render its percentage")
	}
	if measure.GoalID != goal.Goal.ID {
		t.Fatalf("goalId = %q, want %q", measure.GoalID, goal.Goal.ID)
	}
	if measure.GoalName != "House deposit" {
		t.Fatalf("goalName = %q, want %q", measure.GoalName, "House deposit")
	}
	if measure.Percent != 50 {
		t.Fatalf("percent = %d, want 50 (500,000 of a 1,000,000 target)", measure.Percent)
	}
	if measure.Current != 0 {
		t.Fatalf("current = %d, want 0 -- Current is typed-only and must not be read for a linked measure", measure.Current)
	}
}
