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
// (zero Money when unset, never nil), its spend, and whether it is over.
// Archived lets the screen still render a retired category's cap.
type BudgetCategoryView struct {
	CategoryID   string
	CategoryName string
	Archived     bool
	Cap          domain.Money
	Spent        domain.Money
	Over         bool
}

// BudgetPersonView is one row of "Spending by person".
//
// MembershipID "" collects spend with no payer on file. Keep that row:
// without it the rows sum to less than Spent and nothing on screen explains
// the gap. Its Name is empty on purpose - the frontend owns all copy here.
type BudgetPersonView struct {
	MembershipID string
	Name         string
	Spent        domain.Money
}

// BudgetMonthView is the whole Budget screen in one response. Budget is nil
// in the empty state -- a never-budgeted month, since budgets never copy
// forward on their own -- while Categories, Spent and ByPerson are still
// populated: the screen shows what was spent even before caps exist
// (TestBudgetMonthUnbudgetedStillReportsSpend).
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

	// RolledOverAt and RolloverGoalID mirror domain.Budget's own rollover
	// stamp, read out to the top level so callers need not reach through
	// the nilable Budget pointer above -- nil/"" says "never budgeted" as
	// directly as Budget == nil does.
	RolledOverAt   *time.Time
	RolloverGoalID string

	// RolloverAmountMinor is domain.Budget's own field, carried up the same
	// way: the amount RollOver actually wrote, nil until a rollover
	// happens. It is deliberately NOT `Remaining` -- Remaining recomputes
	// from today's ledger on every call, so a backdated transaction or an
	// edit inside an already-rolled-over month would silently change a
	// "moved into X" sentence built from it. This field reads off the
	// goal_contributions row RollOver wrote instead, so that sentence stays
	// true.
	RolloverAmountMinor *int64
}

// BudgetHistoryMonth is one row of the History modal. Closed is false only
// for the month containing `today` -- "the current month" means the real
// one, never whichever month a caller anchored the walk-back window on. A
// picker sitting on a past month marks every returned row Closed, that
// month included.
type BudgetHistoryMonth struct {
	Month    time.Time
	Budgeted domain.Money
	Spent    domain.Money
	Closed   bool // false only for the month containing `today`
}

// BudgetLineInput is one row Save receives: a category and its cap in minor
// units. It carries no currency -- Save derives that from the household, the
// same reason NewTransaction carries no currency field.
type BudgetLineInput struct {
	CategoryID string
	CapMinor   int64
}

// BudgetDeps gathers every port BudgetService needs, mirroring
// TransactionDeps. Members is MembershipRepository.List, the same one the
// member handlers use for names -- a second, narrower port would ask
// Postgres the same question another way.
//
// There is no Clock: Month, Save and History all take the time they need as
// parameters, so the days-left and history-window tests stay deterministic.
type BudgetDeps struct {
	Budgets      BudgetRepository
	Transactions TransactionRepository
	Categories   CategoryRepository
	Households   HouseholdRepository
	Members      MembershipRepository
	FX           FXRateProvider
	// Goals is read only by RollOver, to fetch the target goal before any
	// write (see that method's own comment for why the order matters). It
	// is the one-method GoalLookup, not GoalRepository -- Budget never
	// writes goals.
	Goals GoalLookup
}

// BudgetService composes the Budget screen from the same ledger
// Transactions already exposes: an envelope per category is a sum over
// TransactionRepository.MonthTotals. It takes no actor parameter: services
// enforce what is valid, middleware enforces who is asking.
type BudgetService struct {
	d BudgetDeps
}

func NewBudgetService(d BudgetDeps) *BudgetService {
	return &BudgetService{d: d}
}

// Month composes the whole screen for one household-month. today is always
// a parameter -- see BudgetDeps' doc comment -- so DaysLeft and the pace
// figure are deterministic in tests.
//
// Spent follows TransactionService.MonthSummary's exact rule: convert each
// expense transaction into primary first, then add, so a mixed-currency
// household never sums two currencies together. A no-rate transaction is
// excluded from Spent, its category and its person alike, and named in
// ExcludedNoRate -- a quietly short total must never look like a correct
// one.
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
		// filled in below -- budgets never copy forward on their own, so a
		// new month is an empty state, not a blind screen.
		budget = nil
	case err != nil:
		return BudgetMonthView{}, err
	default:
		budget = &b
	}

	// includeArchived=true: an archived category's cap must still render --
	// archiving hides a category from new-cap pickers, not its history.
	categories, err := s.d.Categories.List(ctx, householdID, true)
	if err != nil {
		return BudgetMonthView{}, err
	}

	views, err := s.d.Transactions.MonthTotals(ctx, householdID, month)
	if err != nil {
		return BudgetMonthView{}, err
	}

	caps, budgeted, err := sumCaps(budget, zero)
	if err != nil {
		return BudgetMonthView{}, err
	}

	tally, err := s.tallySpend(ctx, views, primary, zero)
	if err != nil {
		return BudgetMonthView{}, err
	}

	categoryViews, overCount := buildCategoryViews(categories, caps, tally.byCategory, zero)

	byPerson, err := s.buildPersonViews(ctx, householdID, tally)
	if err != nil {
		return BudgetMonthView{}, err
	}

	remaining := budgeted.Amount - tally.spent.Amount
	percentUsed, percentOK := domain.PercentUsed(tally.spent.Amount, budgeted.Amount)
	daysLeft := domain.DaysLeftInMonth(month, today)
	dailyPace, dailyPaceOK := domain.DailyPace(remaining, daysLeft)
	// domain.DailyPace only knows Remaining and DaysLeft, so on its own it
	// can't enforce "hidden unless the viewed month is the current one": a
	// future month still gets a full DaysLeftInMonth and Remaining > 0,
	// which would read DailyPaceOK true before the month has even started.
	// Only this method holds both `month` and `today`, so the check happens
	// here rather than being duplicated by every caller.
	if !startOfMonth(month).Equal(startOfMonth(today)) {
		dailyPace, dailyPaceOK = 0, false
	}

	// A never-budgeted month has no stamp to report either -- nil/"" is
	// budget == nil's own answer, not a separate case to handle.
	var rolledOverAt *time.Time
	var rolloverGoalID string
	var rolloverAmountMinor *int64
	if budget != nil {
		rolledOverAt = budget.RolledOverAt
		rolloverGoalID = budget.RolloverGoalID
		rolloverAmountMinor = budget.RolloverAmountMinor
	}

	return BudgetMonthView{
		Currency:            primary,
		Month:               month,
		Budget:              budget,
		Categories:          categoryViews,
		Budgeted:            budgeted,
		Spent:               tally.spent,
		Remaining:           remaining,
		PercentUsed:         percentUsed,
		PercentOK:           percentOK,
		DaysLeft:            daysLeft,
		DailyPace:           dailyPace,
		DailyPaceOK:         dailyPaceOK,
		ByPerson:            byPerson,
		ExcludedNoRate:      tally.excluded,
		OverCount:           overCount,
		RolledOverAt:        rolledOverAt,
		RolloverGoalID:      rolloverGoalID,
		RolloverAmountMinor: rolloverAmountMinor,
	}, nil
}

// sumCaps indexes a saved budget's cap lines by category and totals them. A
// month with no budget row has no caps and budgets zero.
func sumCaps(budget *domain.Budget, zero domain.Money) (map[string]domain.Money, domain.Money, error) {
	caps := map[string]domain.Money{}
	budgeted := zero
	if budget == nil {
		return caps, budgeted, nil
	}
	for _, line := range budget.Lines {
		caps[line.CategoryID] = line.Cap
		var err error
		budgeted, err = budgeted.Add(line.Cap)
		if err != nil {
			return nil, domain.Money{}, err
		}
	}
	return caps, budgeted, nil
}

// spendTally is one month's expense spend in the primary currency, split the
// three ways the Budget screen shows it, plus the transactions that could not
// be converted.
type spendTally struct {
	spent      domain.Money
	byCategory map[string]domain.Money
	byPerson   map[string]domain.Money
	// personOrder keeps ByPerson deterministic: real members in
	// first-appearance order (MonthTotals' own stable order), with "" always
	// appended last after tallySpend's loop -- ranging over byPerson
	// directly would vary run to run, and letting "" join at its own first
	// appearance could put an unattributed transaction ahead of real
	// members.
	personOrder []string
	excluded    []ExcludedTransaction
}

// tallySpend sums a month's expense transactions, converting each into primary
// before adding it, per Month's own comment.
func (s *BudgetService) tallySpend(ctx context.Context, views []TransactionView, primary string, zero domain.Money) (spendTally, error) {
	conv := NewConverter(s.d.FX, primary)
	tally := spendTally{
		spent:      zero,
		byCategory: map[string]domain.Money{},
		byPerson:   map[string]domain.Money{},
	}

	for _, view := range views {
		t := view.Transaction
		// Income is not spending, and a transfer is money arriving
		// somewhere else -- the exact MonthSummary rule.
		// TestBudgetMonthSpentReusesTheMonthSummaryRule pins this guard: an
		// income transaction added there must never move Spent.
		if t.Kind != domain.TransactionExpense {
			continue
		}

		inPrimary, hasRate, err := conv.TryConvert(ctx, t.Amount)
		if err != nil {
			return spendTally{}, err
		}
		if !hasRate {
			tally.excluded = append(tally.excluded, ExcludedTransaction{
				TransactionID: t.ID,
				Currency:      t.Amount.Currency,
			})
			continue
		}

		tally.spent, err = tally.spent.Add(inPrimary)
		if err != nil {
			return spendTally{}, err
		}

		if t.CategoryID != "" {
			total := tally.byCategory[t.CategoryID]
			if total.Currency == "" {
				total = zero
			}
			total, err = total.Add(inPrimary)
			if err != nil {
				return spendTally{}, err
			}
			tally.byCategory[t.CategoryID] = total
		}

		// Accumulate unconditionally, keyed on the possibly-empty payer id.
		// Don't guard on `t.PaidByMembershipID != ""` here: that guard let
		// ByPerson's rows sum to less than Spent. `tally.byCategory` above
		// DOES guard on `t.CategoryID != ""` -- deliberately, since
		// Categories has no uncategorised row for a transaction to land in.
		// "" is appended to personOrder after the loop, so it always renders
		// last regardless of when the first unattributed transaction
		// appeared.
		total, seen := tally.byPerson[t.PaidByMembershipID]
		if !seen {
			total = zero
			if t.PaidByMembershipID != "" {
				tally.personOrder = append(tally.personOrder, t.PaidByMembershipID)
			}
		}
		total, err = total.Add(inPrimary)
		if err != nil {
			return spendTally{}, err
		}
		tally.byPerson[t.PaidByMembershipID] = total
	}
	if _, sawUnattributed := tally.byPerson[""]; sawUnattributed {
		tally.personOrder = append(tally.personOrder, "")
	}
	return tally, nil
}

// buildPersonViews names each payer, in personOrder's order. With no spend at
// all it answers nil without reading the member list.
func (s *BudgetService) buildPersonViews(ctx context.Context, householdID string, tally spendTally) ([]BudgetPersonView, error) {
	if len(tally.personOrder) == 0 {
		return nil, nil
	}
	names, err := s.memberNames(ctx, householdID)
	if err != nil {
		return nil, err
	}
	byPerson := make([]BudgetPersonView, 0, len(tally.personOrder))
	for _, membershipID := range tally.personOrder {
		// names[""] is never set by memberNames (it only ever keys on
		// real membership ids), so this already comes back "" for the
		// unattributed row with no extra case needed here.
		byPerson = append(byPerson, BudgetPersonView{
			MembershipID: membershipID,
			Name:         names[membershipID],
			Spent:        tally.byPerson[membershipID],
		})
	}
	return byPerson, nil
}

// buildCategoryViews projects the household's expense categories into the
// grid's rows. A category with no cap line renders at zero and is never
// "over" -- Over requires an actual line, not a nil-turned-zero Money, so a
// never-budgeted category can't show over merely from having spend
// (TestBudgetMonthUnbudgetedStillReportsSpend). A category budgeted at
// exactly zero can still go over, since that zero came from a real line.
//
// Income categories are left out: caps envelope spending only (see
// CategoryService.Create's own comment).
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

// Save validates the whole line set before BudgetRepository ever sees it,
// then delegates the write wholesale -- BudgetRepository.Upsert never
// merges. Every cap is built via domain.NewMoney(capMinor, primary) here,
// so a caller can never make a Budget carry a currency the household
// doesn't have; the repo relabels to primary regardless, but that must not
// be the only defence.
//
// A duplicate category id, a negative cap, or a negative expected income is
// refused before any repo call (domain.ErrBudgetLineDuplicate,
// ErrBudgetCapNegative, ErrBudgetIncomeNegative -- per-field sentinels,
// since there is no domain.ErrValidation). domain.NewMoney does not itself
// refuse a negative amount -- a
// transaction's Money can legitimately be negative -- so nothing downstream
// would otherwise catch it. An unknown or foreign category id is NOT
// checked here: that is BudgetRepository.Upsert's own household-ownership
// check, and its error passes through unchanged.
func (s *BudgetService) Save(ctx context.Context, householdID string, month time.Time, expectedIncomeMinor *int64, lines []BudgetLineInput) (domain.Budget, error) {
	if expectedIncomeMinor != nil && *expectedIncomeMinor < 0 {
		return domain.Budget{}, domain.ErrBudgetIncomeNegative
	}

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

// RollOver moves a CLOSED month's unspent budget into a goal, as one
// contribution, once. It is the manual half of the design's "Roll unspent
// into savings" toggle: nothing here runs on a clock, and a stored toggle
// that only acted when clicked would read as automatic when it isn't --
// this button is the honest version.
//
// Every refusal THIS METHOD can detect happens before anything is written,
// in this order:
//   - a current or future month            -> domain.ErrRolloverMonthOpen
//   - a month with no budget row           -> domain.ErrNotFound
//   - Remaining <= 0                       -> domain.ErrRolloverNothingUnspent
//   - an archived goal                     -> domain.ErrGoalArchived
//   - a goal not in the primary currency   -> domain.ErrRolloverCurrencyMismatch
//
// domain.ErrRolloverAlreadyDone is not in that list: it surfaces from
// INSIDE BudgetRepository.RollOverToGoal's own transaction, when its
// conditional UPDATE matches zero rows and diagnoseUnstampedRollover tells
// that apart from "never budgeted." The transaction still rolls back, so
// nothing is left half-written.
//
// The closed-month check compares month-starts, not instants (via
// startOfMonth) -- a mid-month "unspent" figure is still moving, and money
// moved out of a number that later shrinks is a wrong number the household
// cannot undo.
//
// Remaining comes from Month, never recomputed a second way. Month's Budget
// field is checked for nil BEFORE Remaining is read: an unbudgeted month's
// Budgeted is zero, which would otherwise make Remaining go negative and
// misreport domain.ErrNotFound as domain.ErrRolloverNothingUnspent.
//
// The currency refusal: budgets carry no currency column and are
// implicitly primary, while a goal carries an explicit one. Converting
// inside a rollover would store a rate nobody can audit, so a non-primary
// goal is refused even with a live rate available -- this is about
// auditability, not availability.
//
// s.d.Goals.Get runs BEFORE BudgetRepository.RollOverToGoal: GoalID reaches
// the repository's SQL in a value position, so an invalid id would
// otherwise surface as an unmapped 500 (a foreign-key violation) instead of
// this method's own domain.ErrNotFound. Get is needed anyway, for the
// goal's ArchivedAt and currency, so fetching it first costs nothing extra.
func (s *BudgetService) RollOver(ctx context.Context, householdID string, month time.Time, goalID string, today time.Time) (domain.GoalContribution, error) {
	if !startOfMonth(month).Before(startOfMonth(today)) {
		return domain.GoalContribution{}, domain.ErrRolloverMonthOpen
	}

	view, err := s.Month(ctx, householdID, month, today)
	if err != nil {
		return domain.GoalContribution{}, err
	}
	if view.Budget == nil {
		return domain.GoalContribution{}, domain.ErrNotFound
	}
	if view.Remaining <= 0 {
		return domain.GoalContribution{}, domain.ErrRolloverNothingUnspent
	}

	rec, err := s.d.Goals.Get(ctx, householdID, goalID)
	if err != nil {
		return domain.GoalContribution{}, err
	}
	if rec.Goal.IsArchived() {
		return domain.GoalContribution{}, domain.ErrGoalArchived
	}
	if rec.Goal.Target.Currency != view.Currency {
		return domain.GoalContribution{}, domain.ErrRolloverCurrencyMismatch
	}

	return s.d.Budgets.RollOverToGoal(ctx, RollOverToGoalInput{
		HouseholdID: householdID,
		Month:       month,
		GoalID:      goalID,
		Amount:      domain.Money{Amount: view.Remaining, Currency: view.Currency},
		OccurredOn:  today,
	})
}

// History reports the viewed month (if budgeted) plus up to `months` closed
// months walked back from it, newest first -- a month without a budget row
// is simply absent, never zero-filled. `month` decides that window; `today`
// decides Closed (see BudgetHistoryMonth's doc comment).
//
// Each row's Spent and Budgeted are computed by calling Month for that
// row's own month, rather than re-deriving spend a second way -- reusing
// Month is what keeps "Spent reuses the MonthSummary rule" true, not merely
// asserted.
func (s *BudgetService) History(ctx context.Context, householdID string, month, today time.Time, months int) ([]BudgetHistoryMonth, error) {
	budgets, err := s.d.Budgets.History(ctx, householdID, month, months)
	if err != nil {
		return nil, err
	}

	current := startOfMonth(today)
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
			Closed:   !startOfMonth(b.Month).Equal(current),
		})
	}
	return out, nil
}

// startOfMonth reads t.Year() and t.Month() in t's own location, without
// converting to UTC first, and returns midnight UTC on the first of that
// month. Every caller passes a UTC-located time today: the HTTP handlers
// via time.Parse and clock.System, and the daily digest because
// NudgeService.RunOnce re-anchors the local calendar date to UTC midnight
// before Compose runs -- that step, not this one, is where the local zone
// matters (see RunOnce's own comment).
// It applies the same normalisation as budgetKey (the fakeBudgetRepo
// double) and the postgres adapter's startOfMonth -- Budget.Month is
// documented as "any instant in the month", so comparing two months for
// equality must not depend on which instant a caller happened to pass.
func startOfMonth(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), 1, 0, 0, 0, 0, time.UTC)
}
