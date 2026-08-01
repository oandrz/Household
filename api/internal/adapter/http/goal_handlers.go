package httpadapter

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/andreasoentoro/hearth/api/internal/domain"
	"github.com/andreasoentoro/hearth/api/internal/usecase"
)

// goalDTO is one card on the Goals screen: the stored goal plus every
// derived figure GoalService.List computes (contributed, percent, status,
// required monthly). It is also the shape every write route answers, inside
// {"goal": ...} -- see writeGoal for why a write's own return value is never
// enough to build one of these on its own.
type goalDTO struct {
	ID                   string     `json:"id"`
	Name                 string     `json:"name"`
	TargetMinor          int64      `json:"targetMinor"`
	Currency             string     `json:"currency"`
	TargetMonth          *string    `json:"targetMonth"`
	PlannedMonthlyMinor  int64      `json:"plannedMonthlyMinor"`
	ContributedMinor     int64      `json:"contributedMinor"`
	Percent              int        `json:"percent"`
	Status               string     `json:"status"`
	RequiredMonthlyMinor int64      `json:"requiredMonthlyMinor"`
	RequiredMonthlyOK    bool       `json:"requiredMonthlyOk"`
	ArchivedAt           *time.Time `json:"archivedAt"`
}

type goalResponse struct {
	Goal goalDTO `json:"goal"`
}

type nextGoalDTO struct {
	ID          string  `json:"id"`
	Name        string  `json:"name"`
	TargetMonth *string `json:"targetMonth"`
}

// goalsSummaryDTO is the page header and the Monthly contributions card. It
// counts live goals only -- an archived goal is in no count, whether or not
// ?include_archived=true put it in the goals list alongside them
// (GoalService.List's own doc comment).
type goalsSummaryDTO struct {
	PlannedMonthlyTotalMinor int64        `json:"plannedMonthlyTotalMinor"`
	ActualThisMonthMinor     int64        `json:"actualThisMonthMinor"`
	OnTrackCount             int          `json:"onTrackCount"`
	DatedCount               int          `json:"datedCount"`
	NoDateCount              int          `json:"noDateCount"`
	ExcludedNoRate           int          `json:"excludedNoRate"`
	NextGoal                 *nextGoalDTO `json:"nextGoal"`
}

// goalsResponse is GET /goals' whole body. Currency is the household's
// primary (GoalsSummary.Currency), not any one goal's own -- a card can be in
// a different currency, but the page-level figures below it are always in
// this one.
type goalsResponse struct {
	Currency string          `json:"currency"`
	Goals    []goalDTO       `json:"goals"`
	Summary  goalsSummaryDTO `json:"summary"`
}

type contributionDTO struct {
	ID                string  `json:"id"`
	AmountMinor       int64   `json:"amountMinor"`
	OccurredOn        string  `json:"occurredOn"`
	Note              string  `json:"note"`
	Source            string  `json:"source"`
	SourceBudgetMonth *string `json:"sourceBudgetMonth"`
}

type contributionResponse struct {
	Contribution contributionDTO `json:"contribution"`
}

type contributionsResponse struct {
	Contributions []contributionDTO `json:"contributions"`
}

// createGoalRequest is POST /goals' body. Currency is a plain string, not a
// pointer: "" and an omitted key decode identically, and both mean the same
// thing here -- fill in the household's primary, done by the handler so the
// service never has to guess (GoalService.Create's own doc comment: it
// refuses an empty currency rather than defaulting one).
type createGoalRequest struct {
	Name                 string  `json:"name"`
	TargetMinor          int64   `json:"targetMinor"`
	Currency             string  `json:"currency"`
	TargetMonth          *string `json:"targetMonth"`
	PlannedMonthlyMinor  int64   `json:"plannedMonthlyMinor"`
	StartingBalanceMinor int64   `json:"startingBalanceMinor"`
}

// updateGoalRequest is PATCH /goals/{id}'s body. Every field but
// ClearTargetMonth is a pointer so an absent key round-trips as "unchanged"
// (usecase.GoalUpdate's own convention) rather than a stored zero.
//
// Currency exists on this struct even though usecase.GoalUpdate has no field
// for it at all: GoalUpdate's own doc comment explains that its absence is
// what makes currency immutability type-enforced one layer down, but a
// caller can still put a "currency" key in the JSON body, and this handler
// is the only place left that can see it and refuse it (domain.
// ErrGoalCurrencyImmutable) rather than silently dropping it on the floor.
type updateGoalRequest struct {
	Name                *string `json:"name"`
	TargetMinor         *int64  `json:"targetMinor"`
	Currency            *string `json:"currency"`
	TargetMonth         *string `json:"targetMonth"`
	ClearTargetMonth    bool    `json:"clearTargetMonth"`
	PlannedMonthlyMinor *int64  `json:"plannedMonthlyMinor"`
}

// addContributionRequest is POST /goals/{id}/contributions' body. Currency is
// optional and exists only to be checked -- a contribution has no currency of
// its own, it is always written in its goal's (usecase.NewContribution's own
// doc comment) -- so this field never reaches the service at all; it is
// compared against the goal's stored currency in the handler and either
// matches (accepted) or does not (domain.ErrGoalCurrencyImmutable), the same
// "never silently drop a value the caller did not construct" rule the PATCH
// currency field above closes.
type addContributionRequest struct {
	AmountMinor int64   `json:"amountMinor"`
	OccurredOn  string  `json:"occurredOn"`
	Note        string  `json:"note"`
	Currency    *string `json:"currency"`
}

// parseGoalMonth parses a "YYYY-MM" body field, the same layout and the same
// contract as parseBudgetMonth's path-segment version: INVALID_MONTH/400 on
// failure, written directly since neither caller has anything else useful to
// add.
func parseGoalMonth(w http.ResponseWriter, raw string) (time.Time, bool) {
	t, err := time.Parse(monthLayout, raw)
	if err != nil {
		WriteError(w, http.StatusBadRequest, "INVALID_MONTH", "That month could not be read. Use YYYY-MM.", nil)
		return time.Time{}, false
	}
	return t, true
}

// handleListGoals serves the whole Goals screen: every card (live only by
// default, live-and-archived together when include_archived=true -- a union,
// not a filter swap) plus the page summary, which counts live goals only
// either way. include_archived is spelled the way account_handlers.go
// already spells the same parameter on GET /accounts, so the two query
// strings agree.
func handleListGoals(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		scope, _ := RequestScope(r)
		includeArchived := r.URL.Query().Get("include_archived") == "true"

		view, err := deps.Goals.List(r.Context(), scope.HouseholdID, includeArchived, deps.Clock.Now())
		if err != nil {
			MapDomainError(w, r, err)
			return
		}
		WriteJSON(w, http.StatusOK, toGoalsResponse(view))
	}
}

// handleListGoalContributions serves one goal's contribution history.
//
// It performs no existence check of its own on the goal id --
// GoalService.Contributions' own contract does none either (unlike
// AddContribution, which reads the goal first to check it is not archived).
// GoalRepository.ListContributions filters by household_id AND goal_id
// together, so a made-up or foreign-household id simply matches zero rows;
// nothing leaks either way, and a goal with no contributions yet is
// indistinguishable from one that does not exist, which is fine here because
// there is nothing sensitive an empty list could disclose. Task 8's own
// TestGoalContributionsListForUnknownGoalIsEmpty pins this deliberately
// rather than adding a lookup this route does not otherwise need.
func handleListGoalContributions(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		scope, _ := RequestScope(r)
		id := chi.URLParam(r, "id")

		contributions, err := deps.Goals.Contributions(r.Context(), scope.HouseholdID, id)
		if err != nil {
			MapDomainError(w, r, err)
			return
		}
		out := contributionsResponse{Contributions: make([]contributionDTO, 0, len(contributions))}
		for _, c := range contributions {
			out.Contributions = append(out.Contributions, toContributionDTO(c))
		}
		WriteJSON(w, http.StatusOK, out)
	}
}

// handleCreateGoal adds one goal. A currency the caller did not send is
// filled in from the household's primary here, before the service ever sees
// the request -- GoalService.Create refuses an empty currency rather than
// guessing one, by design.
func handleCreateGoal(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		scope, _ := RequestScope(r)
		var req createGoalRequest
		if !decodeJSONBody(w, r, &req) {
			return
		}
		// Read once and thread through: createdOn (Create) and the re-read
		// below (writeGoal) must agree on "now" within this one request, the
		// same single-read convention handleBudgetHistory's own "today" local
		// follows.
		today := deps.Clock.Now()

		currency := strings.TrimSpace(req.Currency)
		if currency == "" {
			household, err := deps.Households.Get(r.Context(), scope.HouseholdID)
			if err != nil {
				MapDomainError(w, r, err)
				return
			}
			currency = household.PrimaryCurrency
		}

		var targetMonth *time.Time
		if req.TargetMonth != nil {
			m, ok := parseGoalMonth(w, *req.TargetMonth)
			if !ok {
				return
			}
			targetMonth = &m
		}

		created, err := deps.Goals.Create(r.Context(), usecase.NewGoal{
			HouseholdID:          scope.HouseholdID,
			Name:                 req.Name,
			TargetMinor:          req.TargetMinor,
			Currency:             currency,
			TargetMonth:          targetMonth,
			PlannedMonthlyMinor:  req.PlannedMonthlyMinor,
			StartingBalanceMinor: req.StartingBalanceMinor,
		}, today)
		if err != nil {
			writeGoalNameConflict(w, r, deps, scope.HouseholdID, req.Name, today, err)
			return
		}
		writeGoal(w, r, deps, scope.HouseholdID, created.ID, today, http.StatusCreated)
	}
}

// handleUpdateGoal applies a patch. When the body carries a currency field,
// it must match the goal's own or the whole request is refused before
// anything is written -- see updateGoalRequest's own comment for why that
// check has to live here rather than in usecase.GoalUpdate.
func handleUpdateGoal(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		scope, _ := RequestScope(r)
		var req updateGoalRequest
		if !decodeJSONBody(w, r, &req) {
			return
		}
		id := chi.URLParam(r, "id")
		today := deps.Clock.Now()

		if req.Currency != nil {
			views, err := listGoalViews(r.Context(), deps, scope.HouseholdID, today)
			if err != nil {
				MapDomainError(w, r, err)
				return
			}
			current, found := findGoalViewByID(views, id)
			if !found {
				WriteError(w, http.StatusNotFound, "NOT_FOUND", "That could not be found.", nil)
				return
			}
			wireCurrency := strings.ToUpper(strings.TrimSpace(*req.Currency))
			if wireCurrency != current.Goal.Target.Currency {
				MapDomainError(w, r, domain.ErrGoalCurrencyImmutable)
				return
			}
		}

		var targetMonth *time.Time
		if !req.ClearTargetMonth && req.TargetMonth != nil {
			m, ok := parseGoalMonth(w, *req.TargetMonth)
			if !ok {
				return
			}
			targetMonth = &m
		}

		patch := usecase.GoalUpdate{
			Name:                req.Name,
			TargetMinor:         req.TargetMinor,
			TargetMonth:         targetMonth,
			ClearTargetMonth:    req.ClearTargetMonth,
			PlannedMonthlyMinor: req.PlannedMonthlyMinor,
		}

		attemptedName := ""
		if req.Name != nil {
			attemptedName = *req.Name
		}
		if _, err := deps.Goals.Update(r.Context(), scope.HouseholdID, id, patch); err != nil {
			writeGoalNameConflict(w, r, deps, scope.HouseholdID, attemptedName, today, err)
			return
		}
		writeGoal(w, r, deps, scope.HouseholdID, id, today, http.StatusOK)
	}
}

func handleArchiveGoal(deps Deps) http.HandlerFunc { return setGoalArchived(deps, true) }
func handleRestoreGoal(deps Deps) http.HandlerFunc { return setGoalArchived(deps, false) }

// setGoalArchived backs both the archive and the restore route -- the same
// "one function, not two near-identical ones" shape account_handlers.go's
// setArchived and category_handlers.go's setCategoryArchived both use.
// Archive and restore are their own routes rather than a field on PATCH: an
// ordinary rename that happened to include it would archive the goal as a
// side effect of saving a name, the reason router.go's own comment gives for
// accounts and categories.
func setGoalArchived(deps Deps, archived bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		scope, _ := RequestScope(r)
		id := chi.URLParam(r, "id")
		today := deps.Clock.Now()
		if _, err := deps.Goals.SetArchived(r.Context(), scope.HouseholdID, id, archived, today); err != nil {
			MapDomainError(w, r, err)
			return
		}
		writeGoal(w, r, deps, scope.HouseholdID, id, today, http.StatusOK)
	}
}

// handleAddGoalContribution records one manual contribution.
func handleAddGoalContribution(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		scope, _ := RequestScope(r)
		var req addContributionRequest
		if !decodeJSONBody(w, r, &req) {
			return
		}
		goalID := chi.URLParam(r, "id")

		if req.Currency != nil {
			views, err := listGoalViews(r.Context(), deps, scope.HouseholdID, deps.Clock.Now())
			if err != nil {
				MapDomainError(w, r, err)
				return
			}
			current, found := findGoalViewByID(views, goalID)
			if !found {
				WriteError(w, http.StatusNotFound, "NOT_FOUND", "That could not be found.", nil)
				return
			}
			wireCurrency := strings.ToUpper(strings.TrimSpace(*req.Currency))
			if wireCurrency != current.Goal.Target.Currency {
				MapDomainError(w, r, domain.ErrGoalCurrencyImmutable)
				return
			}
		}

		occurredOn, err := time.Parse(occurredOnLayout, req.OccurredOn)
		if err != nil {
			WriteError(w, http.StatusUnprocessableEntity, "INVALID_DATE",
				"That date could not be read. Use YYYY-MM-DD.", nil)
			return
		}

		created, err := deps.Goals.AddContribution(r.Context(), usecase.NewContribution{
			HouseholdID: scope.HouseholdID,
			GoalID:      goalID,
			AmountMinor: req.AmountMinor,
			OccurredOn:  occurredOn,
			Note:        req.Note,
		})
		if err != nil {
			MapDomainError(w, r, err)
			return
		}
		WriteJSON(w, http.StatusCreated, contributionResponse{Contribution: toContributionDTO(created)})
	}
}

// handleDeleteGoalContribution removes one contribution. 204 with no body is
// the one status on this API allowed to carry none -- apiFetch does not try
// to parse it (the same contract handleDeleteTransaction documents).
// DeleteContribution scopes its delete by household_id AND goal_id AND
// contribution id together (GoalRepository.DeleteContribution's own
// contract), so a contribution that belongs to a different goal of this same
// household -- not just a foreign household -- answers the same 404: the id
// pair is what is checked, never the contribution id alone.
func handleDeleteGoalContribution(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		scope, _ := RequestScope(r)
		goalID := chi.URLParam(r, "id")
		contributionID := chi.URLParam(r, "contributionId")
		if err := deps.Goals.DeleteContribution(r.Context(), scope.HouseholdID, goalID, contributionID); err != nil {
			MapDomainError(w, r, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

// listGoalViews is GoalService.List's live-and-archived union, stripped down
// to the slice callers below actually want. GoalService exposes no
// single-goal getter (it never needed one before this task: List already
// composes everything the screen shows in one call), so this is the one
// lookup every helper in this file that needs to inspect a specific existing
// goal -- the currency pre-checks, the archived-name conflict lookup, and
// writeGoal's re-read -- shares, rather than three call sites each reaching
// for List on their own.
func listGoalViews(ctx context.Context, deps Deps, householdID string, today time.Time) ([]usecase.GoalView, error) {
	view, err := deps.Goals.List(ctx, householdID, true, today)
	if err != nil {
		return nil, err
	}
	return view.Goals, nil
}

func findGoalViewByID(views []usecase.GoalView, id string) (usecase.GoalView, bool) {
	for _, v := range views {
		if v.Goal.ID == id {
			return v, true
		}
	}
	return usecase.GoalView{}, false
}

// findArchivedGoalByName is writeGoalNameConflict's lookup: among every goal
// this collision could be against, the one whose exact (trimmed) name
// matches and which is archived -- the row a Restore action could actually
// bring back.
func findArchivedGoalByName(views []usecase.GoalView, name string) (usecase.GoalView, bool) {
	trimmed := strings.TrimSpace(name)
	for _, v := range views {
		if v.Goal.Name == trimmed && v.Goal.IsArchived() {
			return v, true
		}
	}
	return usecase.GoalView{}, false
}

// writeGoal re-reads the goal through listGoalViews so every write response
// carries the derived figures (contributed, percent, status, required
// monthly) that Create/Update/SetArchived's own return values do not --
// GoalService.List is what computes them, and a handler computing them
// itself would be exactly the arithmetic this layer is not allowed to do.
// The same "re-read after write" shape writeAccount and writeTransaction
// already use, for the same reason: the write call's own return value is
// never quite enough to answer with.
//
// includeArchived is always true here (via listGoalViews), which is what
// lets archive and restore re-read successfully -- a re-read that filtered
// archived goals out would 404 its own just-completed archive.
func writeGoal(w http.ResponseWriter, r *http.Request, deps Deps, householdID, goalID string, today time.Time, status int) {
	views, err := listGoalViews(r.Context(), deps, householdID, today)
	if err != nil {
		MapDomainError(w, r, err)
		return
	}
	view, found := findGoalViewByID(views, goalID)
	if !found {
		// The write that got us here (Create/Update/SetArchived) already
		// succeeded and already checked existence on the way in -- a miss on
		// this immediate re-read is not a client mistake to explain with 404,
		// it is this handler's own invariant broken, and the generic
		// logged-500 path is what says so rather than reporting a successful
		// write as "not found."
		logAndWriteInternal(w, r, fmt.Errorf("goal %s not found on the re-read immediately after a successful write", goalID))
		return
	}
	WriteJSON(w, status, goalResponse{Goal: toGoalDTO(view)})
}

// writeGoalNameConflict answers a Create or Update failure. Every error but
// ErrGoalNameTaken passes straight through to MapDomainError unchanged. For
// ErrGoalNameTaken specifically, it looks for an ARCHIVED goal holding the
// attempted name and, if it finds one, answers 409 with that goal's id in
// details so the New/Edit modal can offer Restore instead of a dead end --
// the categories gotcha this task's brief names. A live-goal collision, or a
// failure of the lookup itself, falls back to MapDomainError's own
// GOAL_NAME_TAKEN case: a failed best-effort enhancement must never mask the
// real 409 the create or update already failed with.
func writeGoalNameConflict(w http.ResponseWriter, r *http.Request, deps Deps, householdID, attemptedName string, today time.Time, err error) {
	if !errors.Is(err, domain.ErrGoalNameTaken) {
		MapDomainError(w, r, err)
		return
	}
	if views, listErr := listGoalViews(r.Context(), deps, householdID, today); listErr == nil {
		if archived, ok := findArchivedGoalByName(views, attemptedName); ok {
			WriteError(w, http.StatusConflict, "GOAL_NAME_TAKEN",
				fmt.Sprintf("%q is the name of an archived goal. Restore it, or choose a different name.",
					strings.TrimSpace(attemptedName)),
				map[string]any{"archivedGoalId": archived.Goal.ID})
			return
		}
	}
	MapDomainError(w, r, err)
}

func toGoalDTO(v usecase.GoalView) goalDTO {
	dto := goalDTO{
		ID:                   v.Goal.ID,
		Name:                 v.Goal.Name,
		TargetMinor:          v.Goal.Target.Amount,
		Currency:             v.Goal.Target.Currency,
		PlannedMonthlyMinor:  v.Goal.PlannedMonthly.Amount,
		ContributedMinor:     v.Contributed.Amount,
		Percent:              v.Percent,
		Status:               string(v.Status),
		RequiredMonthlyMinor: v.RequiredMonthly.Amount,
		RequiredMonthlyOK:    v.RequiredMonthlyOK,
		ArchivedAt:           v.Goal.ArchivedAt,
	}
	if v.Goal.TargetMonth != nil {
		m := v.Goal.TargetMonth.Format(monthLayout)
		dto.TargetMonth = &m
	}
	return dto
}

func toGoalsResponse(view usecase.GoalsView) goalsResponse {
	out := goalsResponse{
		Currency: view.Summary.Currency,
		Goals:    make([]goalDTO, 0, len(view.Goals)),
		Summary: goalsSummaryDTO{
			PlannedMonthlyTotalMinor: view.Summary.PlannedMonthlyTotal.Amount,
			ActualThisMonthMinor:     view.Summary.ActualThisMonth.Amount,
			OnTrackCount:             view.Summary.OnTrackCount,
			DatedCount:               view.Summary.DatedCount,
			NoDateCount:              view.Summary.NoDateCount,
			ExcludedNoRate:           view.Summary.ExcludedNoRate,
		},
	}
	for _, g := range view.Goals {
		out.Goals = append(out.Goals, toGoalDTO(g))
	}
	if view.Summary.NextGoalID != "" {
		next := &nextGoalDTO{ID: view.Summary.NextGoalID, Name: view.Summary.NextGoalName}
		if view.Summary.NextGoalMonth != nil {
			m := view.Summary.NextGoalMonth.Format(monthLayout)
			next.TargetMonth = &m
		}
		out.Summary.NextGoal = next
	}
	return out
}

func toContributionDTO(c domain.GoalContribution) contributionDTO {
	dto := contributionDTO{
		ID:          c.ID,
		AmountMinor: c.Amount.Amount,
		OccurredOn:  c.OccurredOn.Format(occurredOnLayout),
		Note:        c.Note,
		Source:      string(c.Source),
	}
	if c.SourceBudgetMonth != nil {
		m := c.SourceBudgetMonth.Format(monthLayout)
		dto.SourceBudgetMonth = &m
	}
	return dto
}
