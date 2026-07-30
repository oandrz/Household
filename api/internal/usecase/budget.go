package usecase

import (
	"context"
	"errors"
	"time"

	"github.com/andreasoentoro/hearth/api/internal/domain"
)

// ExcludedTransaction is defined in monthsummary.go and reused here
// verbatim: BudgetMonthView.ExcludedNoRate is the same list Spent excludes
// from, described the same way, because it is the same rule.

// BudgetCategoryView is one row of the categories grid: a category's cap
// (zero Money when the month has no line for it, never a nil), its spend,
// and whether it is over. Archived is carried so the screen can still render
// an archived category's cap (spec decision 5) while marking it retired.
type BudgetCategoryView struct {
	CategoryID   string
	CategoryName string
	Archived     bool
	Cap          domain.Money
	Spent        domain.Money
	Over         bool
}

// BudgetPersonView is one row of Spending by person. Only memberships that
// actually paid for something this month get a row -- an unattributed
// transaction (PaidByMembershipID "") has nobody to attach a name to and is
// left out rather than becoming a synthetic "Unknown" row.
type BudgetPersonView struct {
	MembershipID string
	Name         string
	Spent        domain.Money
}

// BudgetMonthView is the whole Budget screen in one response. Budget is nil
// when the month has never been budgeted -- the empty state -- while
// Categories, Spent and ByPerson are still populated: the screen shows what
// was spent even before caps exist (spec decision 4's cost, and the
// TestBudgetMonthUnbudgetedStillReportsSpend behaviour).
type BudgetMonthView struct {
	Currency       string
	Month          time.Time
	Budget         *domain.Budget // nil = never set (empty state)
	Categories     []BudgetCategoryView
	Budgeted       domain.Money
	Spent          domain.Money // MonthSummary's exact rule
	Remaining      int64        // minor units; may be negative
	PercentUsed    int
	PercentOK      bool
	DaysLeft       int
	DailyPace      int64
	DailyPaceOK    bool
	ByPerson       []BudgetPersonView
	ExcludedNoRate []ExcludedTransaction
	OverCount      int
}

// BudgetHistoryMonth is one row of the History modal. Closed is false only
// for the viewed month -- the month History was called with, not whichever
// month happens to be "now" -- because the caller decides what "current"
// means for the screen it is drawing.
type BudgetHistoryMonth struct {
	Month    time.Time
	Budgeted domain.Money
	Spent    domain.Money
	Closed   bool // false only for the viewed (current) month
}

// BudgetLineInput is one row Save receives: a category and its cap in minor
// units. It carries no currency -- Save derives that from the household, the
// same reason NewTransaction carries no currency field.
type BudgetLineInput struct {
	CategoryID string
	CapMinor   int64
}

// BudgetDeps gathers every port BudgetService needs, mirroring
// TransactionDeps. Members is the same MembershipRepository.List the member
// handlers already use for names -- ByPerson needs display names, not a
// second, narrower port asking Postgres the same question a different way.
//
// There is no Clock here. Month, Save and History all take the time they
// need as parameters; a service that read time.Now() itself would make the
// days-left and history-window tests non-deterministic.
type BudgetDeps struct {
	Budgets      BudgetRepository
	Transactions TransactionRepository
	Categories   CategoryRepository
	Households   HouseholdRepository
	Members      MembershipRepository
	FX           FXRateProvider
}

// BudgetService composes the Budget screen from the same ledger Transactions
// already exposes: an envelope per category is a sum over the month's
// transactions by category, which TransactionRepository.MonthTotals already
// returns. It takes no actor parameter, by the rule this codebase follows:
// services enforce what is valid, middleware enforces who is asking.
type BudgetService struct {
	d BudgetDeps
}

func NewBudgetService(d BudgetDeps) *BudgetService {
	return &BudgetService{d: d}
}

// Month composes the whole screen for one household-month. today is always a
// parameter -- see BudgetDeps' doc comment -- so DaysLeft and the pace figure
// are deterministic in tests and driven by the clock port at the HTTP layer
// in production.
//
// Spent is computed exactly the way TransactionService.MonthSummary computes
// it (see monthsummary.go's own comment for why the order matters): convert
// each expense-kind transaction into the household's primary currency first,
// then add, per transaction, so a mixed-currency household never sums two
// different currencies together. A transaction with no available rate is
// excluded from Spent, from its category's figure, and from its person's
// figure alike, and named in ExcludedNoRate -- a quietly short total looks
// identical to a correct one, which is the failure this refuses.
func (s *BudgetService) Month(ctx context.Context, householdID string, month, today time.Time) (BudgetMonthView, error) {
	household, err := s.d.Households.Get(ctx, householdID)
	if err != nil {
		return BudgetMonthView{}, err
	}
	primary := household.PrimaryCurrency

	zero, err := domain.NewMoney(0, primary)
	if err != nil {
		return BudgetMonthView{}, err
	}

	var budget *domain.Budget
	b, err := s.d.Budgets.Get(ctx, householdID, month)
	switch {
	case errors.Is(err, domain.ErrNotFound):
		// No budget row: the empty state. Categories and Spent still get
		// filled in below -- decision 4's cost is an empty state each new
		// month, not a blind screen.
		budget = nil
	case err != nil:
		return BudgetMonthView{}, err
	default:
		budget = &b
	}

	// includeArchived=true: an archived category's cap must still render
	// (spec decision 5) -- archiving hides a category from new-cap pickers,
	// it does not erase its history.
	categories, err := s.d.Categories.List(ctx, householdID, true)
	if err != nil {
		return BudgetMonthView{}, err
	}

	views, err := s.d.Transactions.MonthTotals(ctx, householdID, month)
	if err != nil {
		return BudgetMonthView{}, err
	}

	caps := map[string]domain.Money{}
	budgeted := zero
	if budget != nil {
		for _, line := range budget.Lines {
			caps[line.CategoryID] = line.Cap
			budgeted, err = budgeted.Add(line.Cap)
			if err != nil {
				return BudgetMonthView{}, err
			}
		}
	}

	spent := zero
	spentByCategory := map[string]domain.Money{}
	spentByPerson := map[string]domain.Money{}
	// personOrder keeps ByPerson's row order deterministic (first-appearance
	// in views, which MonthTotals already returns in a stable order) --
	// ranging over the spentByPerson map directly would make the response
	// order vary run to run.
	var personOrder []string
	var excluded []ExcludedTransaction

	for _, view := range views {
		t := view.Transaction
		// Income is not spending, and a transfer is the same money arriving
		// somewhere else -- the exact MonthSummary rule. Deleting this guard
		// is the designated mutation: TestBudgetMonthSpentReusesTheMonthSummaryRule
		// pins it by adding an income transaction that must not move Spent.
		if t.Kind != domain.TransactionExpense {
			continue
		}

		inPrimary, err := s.convert(ctx, t.Amount, primary)
		if err != nil {
			excluded = append(excluded, ExcludedTransaction{
				TransactionID: t.ID,
				Currency:      t.Amount.Currency,
			})
			continue
		}

		spent, err = spent.Add(inPrimary)
		if err != nil {
			return BudgetMonthView{}, err
		}

		if t.CategoryID != "" {
			total := spentByCategory[t.CategoryID]
			if total.Currency == "" {
				total = zero
			}
			total, err = total.Add(inPrimary)
			if err != nil {
				return BudgetMonthView{}, err
			}
			spentByCategory[t.CategoryID] = total
		}

		if t.PaidByMembershipID != "" {
			total, seen := spentByPerson[t.PaidByMembershipID]
			if !seen {
				total = zero
				personOrder = append(personOrder, t.PaidByMembershipID)
			}
			total, err = total.Add(inPrimary)
			if err != nil {
				return BudgetMonthView{}, err
			}
			spentByPerson[t.PaidByMembershipID] = total
		}
	}

	categoryViews, overCount := buildCategoryViews(categories, caps, spentByCategory, zero)

	var byPerson []BudgetPersonView
	if len(personOrder) > 0 {
		names, err := s.memberNames(ctx, householdID)
		if err != nil {
			return BudgetMonthView{}, err
		}
		byPerson = make([]BudgetPersonView, 0, len(personOrder))
		for _, membershipID := range personOrder {
			byPerson = append(byPerson, BudgetPersonView{
				MembershipID: membershipID,
				Name:         names[membershipID],
				Spent:        spentByPerson[membershipID],
			})
		}
	}

	remaining := budgeted.Amount - spent.Amount
	percentUsed, percentOK := domain.PercentUsed(spent.Amount, budgeted.Amount)
	daysLeft := domain.DaysLeftInMonth(month, today)
	// domain.DailyPace only knows Remaining and DaysLeft. The spec also hides
	// this figure when "the viewed month is not the current one" -- but a
	// *future* month still gets a full DaysLeftInMonth and can have
	// Remaining > 0, so DailyPaceOK alone cannot express that half of the
	// rule. This view carries no "is this the current month" flag to let a
	// caller apply it either. Deliberately left to Task 9/11: the HTTP
	// handler or the page already holds the clock and the requested month,
	// so it is the one place that can compare them and hide the pace card
	// for a future month without this method growing a today-vs-month
	// dependency it does not otherwise need.
	dailyPace, dailyPaceOK := domain.DailyPace(remaining, daysLeft)

	return BudgetMonthView{
		Currency:       primary,
		Month:          month,
		Budget:         budget,
		Categories:     categoryViews,
		Budgeted:       budgeted,
		Spent:          spent,
		Remaining:      remaining,
		PercentUsed:    percentUsed,
		PercentOK:      percentOK,
		DaysLeft:       daysLeft,
		DailyPace:      dailyPace,
		DailyPaceOK:    dailyPaceOK,
		ByPerson:       byPerson,
		ExcludedNoRate: excluded,
		OverCount:      overCount,
	}, nil
}

// buildCategoryViews projects the household's expense categories into the
// grid's rows. A category with no cap line renders at zero and is never
// "over" -- Over requires an actual line, not just a nil-turned-zero Money,
// so a category nobody has budgeted yet cannot show as over merely because
// it has some spend (TestBudgetMonthUnbudgetedStillReportsSpend). A category
// budgeted at exactly zero -- "spend nothing on this" -- can still go over,
// because that zero came from a real line.
//
// Income categories are left out: caps envelope spending only (see
// CategoryService.Create's own comment for why an income category is never
// offered a cap in the first place).
func buildCategoryViews(categories []domain.Category, caps, spentByCategory map[string]domain.Money, zero domain.Money) ([]BudgetCategoryView, int) {
	views := make([]BudgetCategoryView, 0, len(categories))
	overCount := 0
	for _, cat := range categories {
		if cat.Kind != domain.CategoryExpense {
			continue
		}
		capMoney, hasCap := caps[cat.ID]
		if !hasCap {
			capMoney = zero
		}
		catSpent, hasSpent := spentByCategory[cat.ID]
		if !hasSpent {
			catSpent = zero
		}
		over := hasCap && catSpent.Amount > capMoney.Amount
		if over {
			overCount++
		}
		views = append(views, BudgetCategoryView{
			CategoryID:   cat.ID,
			CategoryName: cat.Name,
			Archived:     cat.IsArchived(),
			Cap:          capMoney,
			Spent:        catSpent,
			Over:         over,
		})
	}
	return views, overCount
}

// memberNames maps a membership id to the display name ByPerson shows,
// reading the same list the member handlers already use rather than a
// second, narrower port asking the same question a different way.
func (s *BudgetService) memberNames(ctx context.Context, householdID string) (map[string]string, error) {
	views, err := s.d.Members.List(ctx, householdID)
	if err != nil {
		return nil, err
	}
	names := make(map[string]string, len(views))
	for _, v := range views {
		names[v.Membership.ID] = v.User.DisplayName
	}
	return names, nil
}

// convert turns one amount into the household's primary currency. This
// duplicates TransactionService.convert deliberately, the same way
// TransactionService.convert itself documents duplicating AccountService's:
// each service declares its own dependencies, and hoisting this into a
// shared helper would give one service a reason to change when another's FX
// needs do.
func (s *BudgetService) convert(ctx context.Context, m domain.Money, primary string) (domain.Money, error) {
	if m.Currency == primary {
		return m, nil
	}
	rate, err := s.d.FX.Rate(ctx, m.Currency, primary)
	if err != nil {
		return domain.Money{}, err
	}
	amount, err := rate.Apply(m.Amount)
	if err != nil {
		return domain.Money{}, err
	}
	return domain.Money{Amount: amount, Currency: primary}, nil
}

// Save validates the whole line set before BudgetRepository ever sees it,
// then delegates the write wholesale -- BudgetRepository.Upsert's own doc
// comment is explicit that it never merges. Every cap is constructed via
// domain.NewMoney(capMinor, primary) here, in the service, so a caller can
// never make a Budget carry a currency the household does not have: the repo
// relabels to the household's primary currency regardless, but that must
// never be the only thing standing between a bad currency and a stored row.
//
// A duplicate category id or a negative cap is refused before any repo call
// -- domain.ErrBudgetLineDuplicate and domain.ErrBudgetCapNegative,
// following the per-field sentinel convention domain/errors.go already uses
// (there is no domain.ErrValidation). An unknown or foreign category id is
// not checked here at all: that is BudgetRepository.Upsert's own
// household-ownership check (validateLineCategories in the postgres
// adapter), and its error passes through unchanged rather than being
// duplicated or reinterpreted in this layer.
func (s *BudgetService) Save(ctx context.Context, householdID string, month time.Time, expectedIncomeMinor *int64, lines []BudgetLineInput) (domain.Budget, error) {
	seen := make(map[string]bool, len(lines))
	for _, line := range lines {
		if seen[line.CategoryID] {
			return domain.Budget{}, domain.ErrBudgetLineDuplicate
		}
		seen[line.CategoryID] = true
		if line.CapMinor < 0 {
			return domain.Budget{}, domain.ErrBudgetCapNegative
		}
	}

	household, err := s.d.Households.Get(ctx, householdID)
	if err != nil {
		return domain.Budget{}, err
	}
	primary := household.PrimaryCurrency

	budget := domain.Budget{
		HouseholdID: householdID,
		Month:       month,
	}
	// nil <-> "not provided" round-trips: a caller that sent no expected
	// income gets ExpectedIncome nil back, never a stored zero (the same
	// convention expectedIncomeMinor in the postgres adapter documents).
	if expectedIncomeMinor != nil {
		income, err := domain.NewMoney(*expectedIncomeMinor, primary)
		if err != nil {
			return domain.Budget{}, err
		}
		budget.ExpectedIncome = &income
	}

	lineValues := make([]domain.BudgetLine, 0, len(lines))
	for _, line := range lines {
		capMoney, err := domain.NewMoney(line.CapMinor, primary)
		if err != nil {
			return domain.Budget{}, err
		}
		lineValues = append(lineValues, domain.BudgetLine{
			CategoryID: line.CategoryID,
			Cap:        capMoney,
		})
	}
	budget.Lines = lineValues

	return s.d.Budgets.Upsert(ctx, budget)
}

// History reports the viewed month (if budgeted) plus up to `months` closed
// months walked back from it, newest first, exactly the windowing
// BudgetRepository.History's own doc comment pins -- a month without a
// budget row is simply absent, never zero-filled.
//
// Each row's Spent and Budgeted are computed by calling Month for that row's
// own month, rather than re-deriving spend here a second way: the whole
// point of "Spent reuses the MonthSummary rule exactly" is that there is
// exactly one place that rule lives, and History reusing Month is what keeps
// that true instead of merely asserted.
func (s *BudgetService) History(ctx context.Context, householdID string, month, today time.Time, months int) ([]BudgetHistoryMonth, error) {
	budgets, err := s.d.Budgets.History(ctx, householdID, month, months)
	if err != nil {
		return nil, err
	}

	viewed := startOfMonth(month)
	out := make([]BudgetHistoryMonth, 0, len(budgets))
	for _, b := range budgets {
		view, err := s.Month(ctx, householdID, b.Month, today)
		if err != nil {
			return nil, err
		}
		out = append(out, BudgetHistoryMonth{
			Month:    b.Month,
			Budgeted: view.Budgeted,
			Spent:    view.Spent,
			Closed:   !startOfMonth(b.Month).Equal(viewed),
		})
	}
	return out, nil
}

// startOfMonth truncates to the first of the month in UTC, the same
// normalisation fakeBudgetRepo.budgetKey and the postgres adapter's
// startOfMonth both apply -- Budget.Month is documented as "any instant in
// the month", so comparing two months for equality must not depend on which
// instant a caller happened to pass.
func startOfMonth(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), 1, 0, 0, 0, 0, time.UTC)
}
