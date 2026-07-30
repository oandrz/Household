package httpadapter

import (
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/andreasoentoro/hearth/api/internal/domain"
	"github.com/andreasoentoro/hearth/api/internal/usecase"
)

// maxBudgetRequestBodyBytes replaces the ordinary maxRequestBodyBytes for
// PUT /budgets/{month} only: that request always carries the household's
// entire category list as budget lines (full-replace, never a patch), and
// that list is not bounded anywhere in this codebase -- see
// decodeJSONBodyLimit's own doc comment in errors.go. 16 KiB holds several
// hundred lines, far more than any household is likely to create, while
// still refusing anything absurd.
const maxBudgetRequestBodyBytes = 16 * 1024

// defaultHistoryMonths, minHistoryMonths and maxHistoryMonths bound the
// `months` query parameter on GET /budgets/history. The window always
// includes the current month on top of this many closed months walked back
// from it (BudgetService.History's own doc comment), so 24 is two years of
// history -- generous for the "avg monthly spend" cards the History modal
// shows without letting a caller ask for an unbounded scan.
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
// and every figure below it are always real -- even when Budget is nil, the
// empty state -- because BudgetMonthView computes them regardless
// (BudgetService.Month's own doc comment: "the screen shows what was spent
// even before caps exist").
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

	// ExcludedNoRate is a count on the wire, unlike monthSummaryDTO's list of
	// the same name -- the Budget screen has nowhere to show individual
	// excluded transactions (no ledger rows on this screen), only the "N
	// transactions excluded" note the design calls for.
	ExcludedNoRate int `json:"excludedNoRate"`
	OverCount      int `json:"overCount"`
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

// parseBudgetMonth reads the {month} path segment. monthLayout ("2006-01")
// is transaction_handlers.go's constant, reused rather than redeclared --
// both routes take the same wire shape for a month.
func parseBudgetMonth(w http.ResponseWriter, r *http.Request) (time.Time, bool) {
	month, err := time.Parse(monthLayout, chi.URLParam(r, "month"))
	if err != nil {
		WriteError(w, http.StatusBadRequest, "INVALID_MONTH", "That month could not be read. Use YYYY-MM.", nil)
		return time.Time{}, false
	}
	return month, true
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

		view, err := deps.Budgets.Month(r.Context(), scope.HouseholdID, month, deps.Clock.Now())
		if err != nil {
			MapDomainError(w, r, err)
			return
		}
		WriteJSON(w, http.StatusOK, toBudgetMonthResponse(view))
	}
}

// handlePutBudgetMonth saves a whole month's budget (full replace) and
// answers with what was actually stored -- the same re-read-after-write
// shape writeTransaction and writeAccount use, except here Save's own
// return value already carries everything the response needs, so there is
// no second read.
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

// handleBudgetHistory serves the History modal's table: the current month
// plus up to `months` closed months walked back from it. The anchor is
// always the real current month (deps.Clock.Now()) -- the route takes no
// month parameter of its own (spec's own table: "Current month plus up to N
// closed months") -- so this always answers "history as of right now,"
// never history relative to whichever month the Budget screen happens to be
// showing.
func handleBudgetHistory(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		scope, _ := RequestScope(r)

		months := defaultHistoryMonths
		// A missing or unparseable value falls back to the default rather
		// than erroring: the spec names no failure mode for this parameter,
		// unlike the transactions filters that answer 422 for a malformed
		// id. Any parsed value, valid or not, is still clamped below.
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

		today := deps.Clock.Now()
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
		Currency:       view.Currency,
		Month:          view.Month.Format(monthLayout),
		Categories:     make([]budgetCategoryDTO, 0, len(view.Categories)),
		BudgetedMinor:  view.Budgeted.Amount,
		SpentMinor:     view.Spent.Amount,
		RemainingMinor: view.Remaining,
		PercentUsed:    view.PercentUsed,
		PercentOK:      view.PercentOK,
		DaysLeft:       view.DaysLeft,
		DailyPaceMinor: view.DailyPace,
		DailyPaceOK:    view.DailyPaceOK,
		ByPerson:       make([]budgetPersonDTO, 0, len(view.ByPerson)),
		ExcludedNoRate: len(view.ExcludedNoRate),
		OverCount:      view.OverCount,
	}
	if view.Budget != nil {
		dto := toBudgetDTO(*view.Budget)
		out.Budget = &dto
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
