package httpadapter

import (
	"errors"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/andreasoentoro/hearth/api/internal/domain"
	"github.com/andreasoentoro/hearth/api/internal/usecase"
)

// maxRetroRequestBodyBytes replaces the ordinary maxRequestBodyBytes for
// PATCH /retros/{month} only: its body carries three free-text fields (went
// well / was hard / notes) that this feature deliberately never caps
// (RetroUpdate's own doc comment in ports.go), and the design's own worked
// example already runs to nine bullets across two of those fields before
// JSON-escaping even doubles the cost of every newline. 8 KiB is generous
// for an honest ten-minute retro while still refusing anything absurd -- the
// same reasoning maxBudgetRequestBodyBytes (budget_handlers.go) already
// gives for its own override.
const maxRetroRequestBodyBytes = 8 * 1024

// retroActionDTO is one action a retro decided on. AssigneeMembershipIDs is
// always an array, never null, even when nobody is assigned yet -- the same
// "no null collections" convention goalDTO's own slices follow -- and
// CarriedFrom is "" rather than omitted when the action was not carried, so
// the frontend never has to distinguish an absent key from an empty one.
type retroActionDTO struct {
	ID                    string     `json:"id"`
	Body                  string     `json:"body"`
	DoneAt                *time.Time `json:"doneAt"`
	CarriedFrom           string     `json:"carriedFrom"` // "" when not carried
	AssigneeMembershipIDs []string   `json:"assigneeMembershipIds"`
}

// retroDTO is one month's retro, its own fields plus the actions it owns.
// Mood and CompletedAt are pointers because nil is itself the meaningful
// state (no mood picked yet; still a draft) -- RetroRecord's own doc comment.
type retroDTO struct {
	ID          string           `json:"id"`
	Month       string           `json:"month"` // "2026-07"
	Mood        *int             `json:"mood"`  // null when nobody has picked
	WentWell    string           `json:"wentWell"`
	WasHard     string           `json:"wasHard"`
	Notes       string           `json:"notes"`
	CompletedAt *time.Time       `json:"completedAt"` // null means draft
	Version     int              `json:"version"`
	Actions     []retroActionDTO `json:"actions"`
}

// retroSummaryDTO is one row of the Retros history list. Quote already
// carries RetroService.List's derived "first sentence of Notes" figure --
// this DTO does not recompute it.
type retroSummaryDTO struct {
	ID          string `json:"id"`
	Month       string `json:"month"`
	Mood        *int   `json:"mood"`
	ActionCount int    `json:"actionCount"`
	Quote       string `json:"quote"` // "" renders no quotation marks at all
	Finished    bool   `json:"finished"`
}

// moodPointDTO is one point on the twelve-month mood chart. Mood is nil for
// a gap -- MoodPoint.HasMood false -- and must never be read as 0, the same
// rule MoodPoint's own doc comment states: zero is a claim, not an absence.
type moodPointDTO struct {
	Month string `json:"month"`
	Mood  *int   `json:"mood"` // null is a gap, never 0
}

// retrosResponse is the whole Retros history screen: GET /retros' entire
// body.
type retrosResponse struct {
	Retros     []retroSummaryDTO `json:"retros"`
	Mood       []moodPointDTO    `json:"mood"`
	DoneCount  int               `json:"doneCount"`
	Since      *string           `json:"since"`      // "2025-08", or null
	StartMonth *string           `json:"startMonth"` // null when both months exist
}

// retroResponse is one month's detail screen: GET /retros/{month}'s entire
// body. CarryOver is a top-level sibling of Retro, not nested inside it --
// RetroView keeps the two apart because a carried-over action belongs to
// LAST month's retro, not this one.
type retroResponse struct {
	Retro     retroDTO         `json:"retro"`
	CarryOver []retroActionDTO `json:"carryOver"`
}

// retroWriteResponse is what POST /retros, PATCH /retros/{month} and POST
// /retros/{month}/complete all answer: the retro itself, nested under the
// same "retro" key retroResponse uses -- no top-level CarryOver, because that
// field is a detail-*screen* concept (retroResponse's own doc comment: "a
// carried-over action belongs to LAST month's retro, not this one"), not
// part of the retro resource these three writes return. PATCH and complete
// both fill Actions from a real read taken moments earlier in the same
// request (see handleSaveRetro and handleCompleteRetro), never from an
// empty placeholder: an inaccurate "actions": [] here would look like real
// data and could silently wipe out a client's already-loaded action list if
// it merged this response in naively.
type retroWriteResponse struct {
	Retro retroDTO `json:"retro"`
}

// saveRetroRequest is PATCH /retros/{month}'s body: a full replace of the
// retro's own fields, never a partial patch -- the modal always sends all
// three text fields plus mood plus the version it loaded (RetroService.Save
// always overwrites WentWell/WasHard/Notes unconditionally; there is no
// per-field "unchanged" sentinel the way updateGoalRequest's pointers give
// goals). Mood is *int so JSON null clears it, a real and legitimate state
// (RetroRecord.Mood's own doc comment) -- Go's zero value for int, 0, cannot
// be used for that because 0 is not a valid mood either.
type saveRetroRequest struct {
	Mood     *int   `json:"mood"`
	WentWell string `json:"wentWell"`
	WasHard  string `json:"wasHard"`
	Notes    string `json:"notes"`
	Version  int    `json:"version"`
}

// addRetroActionRequest is POST /retros/{month}/actions' body.
// AssigneeMembershipIDs and CarriedFrom are exactly RetroActionInput's own
// optional fields; neither is validated here beyond decoding -- Task 6's
// repository already refuses a malformed or foreign-household CarriedFrom
// as domain.ErrNotFound and treats a duplicate assignee as a no-op, so
// re-checking either in this handler would be a second, possibly-diverging
// copy of a rule that already lives at the boundary that owns it.
type addRetroActionRequest struct {
	Body                  string   `json:"body"`
	AssigneeMembershipIDs []string `json:"assigneeMembershipIds"`
	CarriedFrom           string   `json:"carriedFrom"`
}

// retroActionResponse is POST /retros/{month}/actions' whole body: the
// created action, nested the way contributionResponse (goal_handlers.go)
// nests a created contribution -- one sub-resource, not its parent retro
// re-fetched.
type retroActionResponse struct {
	Action retroActionDTO `json:"action"`
}

// setRetroActionDoneRequest is PATCH /retros/{month}/actions/{id}'s whole
// body: the tick, nothing else.
type setRetroActionDoneRequest struct {
	Done bool `json:"done"`
}

// retroActionTickResponse is PATCH /retros/{month}/actions/{id}'s whole
// body: only what the tick actually changed, deliberately NOT a
// retroActionDTO. RetroActionRepository.SetDone returns no record, only an
// error, and this handler never resolves {month} into a retro id (see
// handleSetRetroActionDone's own comment for why), so nothing here could
// fill Body, CarriedFrom or AssigneeMembershipIDs honestly. Shipping a
// retroActionDTO with those zeroed would look like real data and could
// silently wipe them out if a client ever merged this response into its
// action list the way it merges a real one -- naming a narrower type here
// stops that mistake at compile time on the frontend side (Task 9).
type retroActionTickResponse struct {
	ID     string     `json:"id"`
	DoneAt *time.Time `json:"doneAt"`
}

// handleListRetros serves the whole Retros history screen: every summary
// row, the twelve-month mood chart, the finished count and its "since"
// month, and which month (if any) is startable. RetroService.List does
// every derived figure; this handler only shapes the wire response.
func handleListRetros(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		scope, _ := RequestScope(r)

		view, err := deps.Retros.List(r.Context(), scope.HouseholdID, deps.Clock.Now())
		if err != nil {
			MapDomainError(w, r, err)
			return
		}
		WriteJSON(w, http.StatusOK, toRetrosResponse(view))
	}
}

// handleGetRetro serves one month's detail screen: the retro, its own
// actions, and the previous month's still-open actions as a carry-over
// offer. {month} is parsed with parseBudgetMonth -- the same parser
// budget_handlers.go already uses for the identical "YYYY-MM path segment"
// wire shape, so a malformed month answers the same INVALID_MONTH/400 there
// rather than a second, possibly-diverging parser living here.
//
// month is passed through to RetroService.Month un-normalised: the service
// normalises it (RetroService.Month's own doc comment), not this layer, so
// there is exactly one place that decides what "the first of the month,
// midnight UTC" means.
//
// A month with no retro comes back as domain.ErrNotFound, which
// MapDomainError turns into 404 -- the page reads that as "not started," an
// empty state, never as an error to show the caller.
func handleGetRetro(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		scope, _ := RequestScope(r)
		month, ok := parseBudgetMonth(w, r)
		if !ok {
			return
		}

		view, err := deps.Retros.Month(r.Context(), scope.HouseholdID, month)
		if err != nil {
			MapDomainError(w, r, err)
			return
		}

		carryOver := make([]retroActionDTO, 0, len(view.CarryOver))
		for _, a := range view.CarryOver {
			carryOver = append(carryOver, toRetroActionDTO(a))
		}

		WriteJSON(w, http.StatusOK, retroResponse{
			Retro:     toRetroDTO(view.Retro, view.Actions),
			CarryOver: carryOver,
		})
	}
}

// handleStartRetro creates the draft RetroService.Start picks -- the earlier
// of {this month, last month} that has none yet. It reads no body: the month
// comes entirely from the household's own state and the clock, never from
// the client, because a client-supplied month would let a stale tab file a
// retro against a month the "Start retro" button never actually offered it
// (domain.StartableMonth's own contract).
func handleStartRetro(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		scope, _ := RequestScope(r)

		created, err := deps.Retros.Start(r.Context(), scope.HouseholdID, deps.Clock.Now())
		if err != nil {
			// Start's own repository never wraps this in anything more
			// specific (RetroRepository.Create's own doc comment: a plain
			// domain.ErrAlreadyExists on the UNIQUE clash) -- unlike
			// CreateGoal or CreateSpace, which translate the same sentinel
			// into a route-specific one before MapDomainError ever sees it.
			// The intercept has to live here instead, one request early, the
			// same shape writeGoalNameConflict (goal_handlers.go) already
			// uses for its own sentinel. This is a real race, not a
			// theoretical one: two partners tapping "Start retro" at the
			// same instant both pass Start's own pre-check before only one
			// of the two concurrent inserts can win -- see
			// TestStartRetroRaceIs409RetroExists (marriage_api_test.go),
			// which builds that race with a repository double because no
			// sequential HTTP call can reach it.
			if errors.Is(err, domain.ErrAlreadyExists) {
				WriteError(w, http.StatusConflict, "RETRO_EXISTS",
					"Someone already started this month's retro. Reload to see it.", nil)
				return
			}
			MapDomainError(w, r, err)
			return
		}

		// A freshly created draft truly has no actions yet -- toRetroDTO(created, nil)
		// is accurate here, not a placeholder a caller has to know to ignore.
		WriteJSON(w, http.StatusCreated, retroWriteResponse{Retro: toRetroDTO(created, nil)})
	}
}

// handleSaveRetro replaces the retro's own fields (mood, the two textareas,
// notes) under the version guard decision 6 exists for: a stale version
// answers domain.ErrRetroChanged (409 RETRO_CHANGED), never a silent merge.
//
// RetroUpdate needs the retro's own id, which {month} alone does not carry.
// deps.Retros.Month resolves it, and its Actions come back to the response
// for free: Save touches only the retro's own columns, never retro_actions
// (RetroService.Save's own doc comment), so a read taken moments before Save
// runs is still accurate by the time this handler answers.
func handleSaveRetro(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		scope, _ := RequestScope(r)
		month, ok := parseBudgetMonth(w, r)
		if !ok {
			return
		}

		var req saveRetroRequest
		if !decodeJSONBodyLimit(w, r, &req, maxRetroRequestBodyBytes) {
			return
		}

		view, err := deps.Retros.Month(r.Context(), scope.HouseholdID, month)
		if err != nil {
			MapDomainError(w, r, err)
			return
		}

		updated, err := deps.Retros.Save(r.Context(), usecase.RetroUpdate{
			HouseholdID: scope.HouseholdID,
			RetroID:     view.Retro.ID,
			Month:       month,
			Mood:        req.Mood,
			WentWell:    req.WentWell,
			WasHard:     req.WasHard,
			Notes:       req.Notes,
			Version:     req.Version,
		})
		if err != nil {
			MapDomainError(w, r, err)
			return
		}

		WriteJSON(w, http.StatusOK, retroWriteResponse{Retro: toRetroDTO(updated, view.Actions)})
	}
}

// handleCompleteRetro finishes the retro: its own route, not a field on
// PATCH, so that saving a typo in the notes can never finish the retro as a
// side effect (the same reasoning archive already carries on accounts,
// categories, goals and bills). Idempotent -- RetroService.Finish's own doc
// comment -- so a double-submit or a retry after a dropped response is
// harmless.
//
// {month} is resolved to the retro's own id the same way handleSaveRetro
// resolves it, and for the same reason the Actions read taken during that
// resolve is still accurate afterwards: Finish only stamps completed_at, it
// never touches retro_actions.
func handleCompleteRetro(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		scope, _ := RequestScope(r)
		month, ok := parseBudgetMonth(w, r)
		if !ok {
			return
		}

		view, err := deps.Retros.Month(r.Context(), scope.HouseholdID, month)
		if err != nil {
			MapDomainError(w, r, err)
			return
		}

		completed, err := deps.Retros.Finish(r.Context(), scope.HouseholdID, view.Retro.ID, deps.Clock.Now())
		if err != nil {
			MapDomainError(w, r, err)
			return
		}

		WriteJSON(w, http.StatusOK, retroWriteResponse{Retro: toRetroDTO(completed, view.Actions)})
	}
}

// handleDiscardRetro removes a draft. {month} is resolved to the retro's own
// id the same way the two handlers above resolve it -- deps.Retros.Month's
// underlying ByMonth finds a FINISHED retro too, completed_at plays no part
// in that lookup, so a delete aimed at a finished retro still reaches
// DiscardDraft. What actually refuses it is RetroRepository.DeleteDraft's
// own `WHERE completed_at IS NULL` (its doc comment), which answers
// domain.ErrNotFound on that zero-row match -- the identical 404 a genuinely
// missing retro gets, which is the point: "there is no draft here" reads the
// same either way, with no separate "that retro is already finished" state
// for the client to handle.
func handleDiscardRetro(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		scope, _ := RequestScope(r)
		month, ok := parseBudgetMonth(w, r)
		if !ok {
			return
		}

		view, err := deps.Retros.Month(r.Context(), scope.HouseholdID, month)
		if err != nil {
			MapDomainError(w, r, err)
			return
		}

		if err := deps.Retros.DiscardDraft(r.Context(), scope.HouseholdID, view.Retro.ID); err != nil {
			MapDomainError(w, r, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

// handleAddRetroAction adds one action to the month's retro. {month} is
// resolved to the retro's own id the same way the write handlers above
// resolve it -- AddAction (like Save, Finish and DiscardDraft) needs the id,
// never the month, to identify which retro an action belongs to.
func handleAddRetroAction(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		scope, _ := RequestScope(r)
		month, ok := parseBudgetMonth(w, r)
		if !ok {
			return
		}

		var req addRetroActionRequest
		if !decodeJSONBody(w, r, &req) {
			return
		}

		view, err := deps.Retros.Month(r.Context(), scope.HouseholdID, month)
		if err != nil {
			MapDomainError(w, r, err)
			return
		}

		created, err := deps.Retros.AddAction(r.Context(), usecase.RetroActionInput{
			HouseholdID:           scope.HouseholdID,
			RetroID:               view.Retro.ID,
			Body:                  req.Body,
			AssigneeMembershipIDs: req.AssigneeMembershipIDs,
			CarriedFrom:           req.CarriedFrom,
		})
		if err != nil {
			MapDomainError(w, r, err)
			return
		}
		WriteJSON(w, http.StatusCreated, retroActionResponse{Action: toRetroActionDTO(created)})
	}
}

// handleSetRetroActionDone ticks or unticks one action. Unlike every write
// handler above, it does NOT resolve {month} into a retro id at all:
// RetroActionRepository.SetDone (like Remove, below) is scoped by household
// id and action id alone, the same household-plus-id scoping every other
// by-id route in this API already relies on. {month} sits in the URL only so
// the route reads like the screen -- the spec's own reasoning for addressing
// every retro route by month -- and is never cross-checked against which
// retro actually owns the action.
//
// That is a deliberate, narrow gap, not an oversight: an action id is
// already household-scoped and not guessable across households, so the
// worst a mismatched {month} in the URL can do is tick the caller's own,
// correctly-scoped action under a URL that mislabels which month it belongs
// to -- never reach another household's data, and never bypass
// requireCapability/requireOwner/requireCSRF, which run before this handler
// regardless of what {month} says.
func handleSetRetroActionDone(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		scope, _ := RequestScope(r)
		id := chi.URLParam(r, "id")

		var req setRetroActionDoneRequest
		if !decodeJSONBody(w, r, &req) {
			return
		}

		at := deps.Clock.Now()
		if err := deps.Retros.SetActionDone(r.Context(), scope.HouseholdID, id, req.Done, at); err != nil {
			MapDomainError(w, r, err)
			return
		}

		var doneAt *time.Time
		if req.Done {
			doneAt = &at
		}
		WriteJSON(w, http.StatusOK, retroActionTickResponse{ID: id, DoneAt: doneAt})
	}
}

// handleRemoveRetroAction deletes one action. Like handleSetRetroActionDone
// above, it never resolves {month} -- RetroActionRepository.Remove is scoped
// by household id and action id alone; see that handler's own comment for
// why that scoping is enough on its own.
func handleRemoveRetroAction(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		scope, _ := RequestScope(r)
		id := chi.URLParam(r, "id")

		if err := deps.Retros.RemoveAction(r.Context(), scope.HouseholdID, id); err != nil {
			MapDomainError(w, r, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

func toRetroActionDTO(a usecase.RetroActionRecord) retroActionDTO {
	assignees := a.AssigneeMembershipIDs
	if assignees == nil {
		assignees = []string{}
	}
	return retroActionDTO{
		ID:                    a.ID,
		Body:                  a.Body,
		DoneAt:                a.DoneAt,
		CarriedFrom:           a.CarriedFrom,
		AssigneeMembershipIDs: assignees,
	}
}

func toRetroDTO(rec usecase.RetroRecord, actions []usecase.RetroActionRecord) retroDTO {
	dtoActions := make([]retroActionDTO, 0, len(actions))
	for _, a := range actions {
		dtoActions = append(dtoActions, toRetroActionDTO(a))
	}
	return retroDTO{
		ID:          rec.ID,
		Month:       rec.Month.Format(monthLayout),
		Mood:        rec.Mood,
		WentWell:    rec.WentWell,
		WasHard:     rec.WasHard,
		Notes:       rec.Notes,
		CompletedAt: rec.CompletedAt,
		Version:     rec.Version,
		Actions:     dtoActions,
	}
}

func toRetroSummaryDTO(s usecase.RetroSummary) retroSummaryDTO {
	return retroSummaryDTO{
		ID:          s.Retro.ID,
		Month:       s.Retro.Month.Format(monthLayout),
		Mood:        s.Retro.Mood,
		ActionCount: s.ActionCount,
		Quote:       s.Quote,
		Finished:    s.Retro.CompletedAt != nil,
	}
}

func toMoodPointDTO(m usecase.MoodPoint) moodPointDTO {
	dto := moodPointDTO{Month: m.Month.Format(monthLayout)}
	if m.HasMood {
		mood := m.Mood
		dto.Mood = &mood
	}
	return dto
}

func toRetrosResponse(view usecase.RetrosView) retrosResponse {
	summaries := make([]retroSummaryDTO, 0, len(view.Summaries))
	for _, s := range view.Summaries {
		summaries = append(summaries, toRetroSummaryDTO(s))
	}
	mood := make([]moodPointDTO, 0, len(view.Mood))
	for _, m := range view.Mood {
		mood = append(mood, toMoodPointDTO(m))
	}

	out := retrosResponse{
		Retros:    summaries,
		Mood:      mood,
		DoneCount: view.DoneCount,
	}
	if view.Since != nil {
		s := view.Since.Format(monthLayout)
		out.Since = &s
	}
	if view.StartMonth != nil {
		s := view.StartMonth.Format(monthLayout)
		out.StartMonth = &s
	}
	return out
}
