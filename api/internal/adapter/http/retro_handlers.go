package httpadapter

import (
	"net/http"
	"time"

	"github.com/andreasoentoro/hearth/api/internal/usecase"
)

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
