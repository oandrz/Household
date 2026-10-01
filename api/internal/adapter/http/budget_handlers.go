package httpadapter

import (
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/andreasoentoro/hearth/api/internal/domain"
	"github.com/andreasoentoro/hearth/api/internal/usecase"
)

// maxBudgetRequestBodyBytes replaces maxRequestBodyBytes for PUT
// /budgets/{month} only: that request full-replaces the household's whole
// category list, which is unbounded elsewhere in this codebase (see
// decodeJSONBodyLimit in errors.go). 16 KiB holds several hundred lines --
// far more than realistic, but still refuses anything absurd.
const maxBudgetRequestBodyBytes = 16 * 1024

// defaultHistoryMonths, minHistoryMonths and maxHistoryMonths bound the
// `months` query parameter on GET /budgets/history: current month plus
// this many closed months back (BudgetService.History's doc comment). 24
// is two years -- generous for the History modal's avg-spend cards, short
// of an unbounded scan.
const (
	defaultHistoryMonths = 6
	minHistoryMonths     = 1
	maxHistoryMonths     = 24
)

// budgetLineDTO is both the wire shape of one PUT line and one GET line --
// {categoryId, capMinor} reads and writes identically, so one type serves
// both directions rather than two structs that would drift apart.
type budgetLineDTO struct {
	CategoryID string `json:"categoryId"`
	CapMinor   int64  `json:"capMinor"`
}

// budgetDTO is the "budget" field of both the month response (where it is a
// pointer, nil for the never-budgeted empty state) and the PUT response
// (where it is always populated -- a save cannot fail to produce one).
type budgetDTO struct {
	ExpectedIncomeMinor *int64          `json:"expectedIncomeMinor"`
	Lines               []budgetLineDTO `json:"lines"`
}

type budgetCategoryDTO struct {
	CategoryID string `json:"categoryId"`
	Name       string `json:"name"`
	Archived   bool   `json:"archived"`
	CapMinor   int64  `json:"capMinor"`
	SpentMinor int64  `json:"spentMinor"`
	Over       bool   `json:"over"`
}

type budgetPersonDTO struct {
	MembershipID string `json:"membershipId"`
	Name         string `json:"name"`
	SpentMinor   int64  `json:"spentMinor"`
}

// budgetMonthResponse is GET /budgets/{month}'s whole body. Budgeted, Spent
// and every figure below it are always real, even in the empty state
// (Budget nil), because usecase.BudgetMonthView computes them regardless --
// "the screen shows what was spent even before caps exist".
type budgetMonthResponse struct {
	Currency   string              `json:"currency"`
	Month      string              `json:"month"`
	Budget     *budgetDTO          `json:"budget"`
	Categories []budgetCategoryDTO `json:"categories"`

	BudgetedMinor  int64 `json:"budgetedMinor"`
	SpentMinor     int64 `json:"spentMinor"`
	RemainingMinor int64 `json:"remainingMinor"`
	PercentUsed    int   `json:"percentUsed"`
	PercentOK      bool  `json:"percentOk"`

	DaysLeft       int   `json:"daysLeft"`
	DailyPaceMinor int64 `json:"dailyPaceMinor"`
	DailyPaceOK    bool  `json:"dailyPaceOk"`

	ByPerson []budgetPersonDTO `json:"byPerson"`

	// ExcludedNoRate is a count, unlike monthSummaryDTO's list of the same
	// name: the Budget screen has no ledger rows for individual excluded
	// transactions, only the "N transactions excluded" note the design wants.
	ExcludedNoRate int `json:"excludedNoRate"`
	OverCount      int `json:"overCount"`

	// RolledOverAt and RolloverGoalID tell the Budget screen a month was
	// already rolled over, without a second request: both nil until POST
	// .../rollover succeeds, then populated together -- never one without
	// the other (domain.Budget's own doc comment). RolloverGoalID is
	// *string, not string, so an empty wire value is JSON null, not an
	// empty-string id -- the same convention as account_handlers.go's
	// ownerMembershipId.
	RolledOverAt   *time.Time `json:"rolledOverAt"`
	RolloverGoalID *string    `json:"rolloverGoalId"`

	// RolloverAmountMinor is the amount RollOver actually wrote, for the
	// frontend's "moved into X" sentence -- never RemainingMinor, which is
	// recomputed on every GET and can disagree with what was actually moved
	// (usecase.BudgetMonthView's comment names the failure this closes). nil
	// until a rollover happens, alongside RolledOverAt/RolloverGoalID, then
	// fixed.
	RolloverAmountMinor *int64 `json:"rolloverAmountMinor"`
}

type putBudgetResponse struct {
	Budget budgetDTO `json:"budget"`
}

type budgetHistoryMonthDTO struct {
	Month         string `json:"month"`
	BudgetedMinor int64  `json:"budgetedMinor"`
	SpentMinor    int64  `json:"spentMinor"`
	Closed        bool   `json:"closed"`
}

type budgetHistoryResponse struct {
	Months []budgetHistoryMonthDTO `json:"months"`
}

// saveBudgetRequest is PUT's body. ExpectedIncomeMinor is a pointer so an
// omitted field decodes to nil and round-trips as "not provided" rather than
// a stored zero -- the same convention BudgetService.Save documents.
type saveBudgetRequest struct {
	ExpectedIncomeMinor *int64          `json:"expectedIncomeMinor"`
	Lines               []budgetLineDTO `json:"lines"`
}

// rolloverBudgetRequest is POST .../rollover's whole body: which goal
// receives the month's unspent money. Every refusal (month open, nothing
// unspent, archived/wrong-currency goal, already rolled over) is
// BudgetService.RollOver's own job -- this handler only decodes and maps.
type rolloverBudgetRequest struct {
	GoalID string `json:"goalId"`
}

// parseBudgetMonth reads the {month} path segment. monthLayout ("2006-01")
// is transaction_handlers.go's constant, reused rather than redeclared --
// both routes take the same wire shape for a month.
func parseBudgetMonth(w http.ResponseWriter, r *http.Request) (time.Time, bool) {
	return parseTimeOrRefuse(w, chi.URLParam(r, "month"), monthLayout,
		http.StatusBadRequest, "INVALID_MONTH", "That month could not be read. Use YYYY-MM.")
}

// handleGetBudgetMonth serves the whole Budget screen for one month: the
// saved budget (or null, the empty state) alongside real spend figures
// either way.
func handleGetBudgetMonth(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		scope, _ := RequestScope(r)
		month, ok := parseBudgetMonth(w, r)
		if !ok {
			return
		}

		view, err := deps.Budgets.Month(r.Context(), scope.HouseholdID, month, scope.Today)
		if err != nil {
			MapDomainError(w, r, err)
			return
		}
		WriteJSON(w, http.StatusOK, toBudgetMonthResponse(view))
	}
}

// handlePutBudgetMonth saves a whole month's budget (full replace) and
// answers with what was actually stored -- the same shape
// writeTransaction/writeAccount use, except Save's own return value
// already carries everything the response needs, so there's no second
// read.
func handlePutBudgetMonth(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		scope, _ := RequestScope(r)
		month, ok := parseBudgetMonth(w, r)
		if !ok {
			return
		}

		var req saveBudgetRequest
		if !decodeJSONBodyLimit(w, r, &req, maxBudgetRequestBodyBytes) {
			return
		}

		lines := make([]usecase.BudgetLineInput, 0, len(req.Lines))
		for _, l := range req.Lines {
			lines = append(lines, usecase.BudgetLineInput{CategoryID: l.CategoryID, CapMinor: l.CapMinor})
		}

		budget, err := deps.Budgets.Save(r.Context(), scope.HouseholdID, month, req.ExpectedIncomeMinor, lines)
		if err != nil {
			MapDomainError(w, r, err)
			return
		}
		WriteJSON(w, http.StatusOK, putBudgetResponse{Budget: toBudgetDTO(budget)})
	}
}

// handleRolloverBudgetMonth moves a closed month's unspent budget into a
// goal, as one contribution. It answers 200, not 201, since the route
// creates no resource the client asked for by name (unlike POST
// /goals/{id}/contributions, which does and answers 201) -- the
// contribution here is a side effect, the same reason PUT /budgets/{month}
// answers 200.
//
// The response reuses goal_handlers.go's
// contributionResponse/contributionDTO rather than a second copy of the
// same shape -- a rollover's contribution is identical to a manual one,
// rendered the same way by the frontend.
//
// Every refusal is BudgetService.RollOver's own job, mapped by
// MapDomainError; this handler only decodes and calls.
func handleRolloverBudgetMonth(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		scope, _ := RequestScope(r)
		month, ok := parseBudgetMonth(w, r)
		if !ok {
			return
		}

		var req rolloverBudgetRequest
		if !decodeJSONBody(w, r, &req) {
			return
		}

		contribution, err := deps.Budgets.RollOver(r.Context(), scope.HouseholdID, month, req.GoalID, scope.Today)
		if err != nil {
			MapDomainError(w, r, err)
			return
		}
		WriteJSON(w, http.StatusOK, contributionResponse{Contribution: toContributionDTO(contribution)})
	}
}

// handleBudgetHistory serves the History modal's table: the current month
// plus up to `months` closed months walked back from it. The anchor is
// always the household's current month (Scope.Today) since the route takes
// no month parameter of its own -- so this always answers "history as of
// right now," never relative to whichever month the Budget screen happens
// to show.
func handleBudgetHistory(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		scope, _ := RequestScope(r)

		months := defaultHistoryMonths
		// A missing or unparseable value falls back to the default rather than
		// erroring, unlike the transaction filters that answer 422 for a
		// malformed id -- the spec names no failure mode here. Any value,
		// parsed or not, is still clamped below.
		if raw := r.URL.Query().Get("months"); raw != "" {
			if parsed, err := strconv.Atoi(raw); err == nil {
				months = parsed
			}
		}
		if months < minHistoryMonths {
			months = minHistoryMonths
		}
		if months > maxHistoryMonths {
			months = maxHistoryMonths
		}

		today := scope.Today
		rows, err := deps.Budgets.History(r.Context(), scope.HouseholdID, today, today, months)
		if err != nil {
			MapDomainError(w, r, err)
			return
		}

		out := budgetHistoryResponse{Months: make([]budgetHistoryMonthDTO, 0, len(rows))}
		for _, row := range rows {
			out.Months = append(out.Months, budgetHistoryMonthDTO{
				Month:         row.Month.Format(monthLayout),
				BudgetedMinor: row.Budgeted.Amount,
				SpentMinor:    row.Spent.Amount,
				Closed:        row.Closed,
			})
		}
		WriteJSON(w, http.StatusOK, out)
	}
}

func toBudgetMonthResponse(view usecase.BudgetMonthView) budgetMonthResponse {
	out := budgetMonthResponse{
		Currency:            view.Currency,
		Month:               view.Month.Format(monthLayout),
		Categories:          make([]budgetCategoryDTO, 0, len(view.Categories)),
		BudgetedMinor:       view.Budgeted.Amount,
		SpentMinor:          view.Spent.Amount,
		RemainingMinor:      view.Remaining,
		PercentUsed:         view.PercentUsed,
		PercentOK:           view.PercentOK,
		DaysLeft:            view.DaysLeft,
		DailyPaceMinor:      view.DailyPace,
		DailyPaceOK:         view.DailyPaceOK,
		ByPerson:            make([]budgetPersonDTO, 0, len(view.ByPerson)),
		ExcludedNoRate:      len(view.ExcludedNoRate),
		OverCount:           view.OverCount,
		RolledOverAt:        view.RolledOverAt,
		RolloverAmountMinor: view.RolloverAmountMinor,
	}
	if view.Budget != nil {
		dto := toBudgetDTO(*view.Budget)
		out.Budget = &dto
	}
	if view.RolloverGoalID != "" {
		id := view.RolloverGoalID
		out.RolloverGoalID = &id
	}
	for _, c := range view.Categories {
		out.Categories = append(out.Categories, budgetCategoryDTO{
			CategoryID: c.CategoryID,
			Name:       c.CategoryName,
			Archived:   c.Archived,
			CapMinor:   c.Cap.Amount,
			SpentMinor: c.Spent.Amount,
			Over:       c.Over,
		})
	}
	for _, p := range view.ByPerson {
		out.ByPerson = append(out.ByPerson, budgetPersonDTO{
			MembershipID: p.MembershipID,
			Name:         p.Name,
			SpentMinor:   p.Spent.Amount,
		})
	}
	return out
}

func toBudgetDTO(b domain.Budget) budgetDTO {
	dto := budgetDTO{Lines: make([]budgetLineDTO, 0, len(b.Lines))}
	if b.ExpectedIncome != nil {
		amount := b.ExpectedIncome.Amount
		dto.ExpectedIncomeMinor = &amount
	}
	for _, line := range b.Lines {
		dto.Lines = append(dto.Lines, budgetLineDTO{CategoryID: line.CategoryID, CapMinor: line.Cap.Amount})
	}
	return dto
}
