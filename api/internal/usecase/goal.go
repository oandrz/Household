package usecase

import (
	"context"
	"strings"
	"time"

	"github.com/andreasoentoro/hearth/api/internal/domain"
)

// GoalView is one card: the stored goal plus every derived figure the
// screen shows. RequiredMonthly is set only for a dated, unachieved goal;
// RequiredMonthlyOK false means no "needs S$X/mo" line, not a zero.
//
// Contributed and RequiredMonthly stay in the GOAL's own currency, not the
// household's primary (only the two summary totals below convert) -- else
// the card would read "S$2,600 of Rp 40,000,000", not a sentence.
type GoalView struct {
	Goal              domain.Goal
	Contributed       domain.Money
	Percent           int
	Status            domain.GoalStatus
	RequiredMonthly   domain.Money
	RequiredMonthlyOK bool
}

// GoalsSummary is the page header and the Monthly contributions card.
// PlannedMonthlyTotal and ActualThisMonth are both in the household's primary
// currency; a goal whose currency has no rate to primary is excluded from both
// and counted in ExcludedNoRate, never silently dropped.
type GoalsSummary struct {
	Currency            string
	PlannedMonthlyTotal domain.Money
	ActualThisMonth     domain.Money
	OnTrackCount        int
	DatedCount          int
	NoDateCount         int
	ExcludedNoRate      int
	NextGoalID          string
	NextGoalName        string
	NextGoalMonth       *time.Time
}

// GoalsView is the whole Goals screen in one response: every card plus the
// page summary, composed by one call to List.
type GoalsView struct {
	Goals   []GoalView
	Summary GoalsSummary
}

// NewGoal is what Create receives. Currency defaults to the household's
// primary at the HTTP layer, never here -- the service refuses an empty one
// rather than guessing.
type NewGoal struct {
	HouseholdID          string
	Name                 string
	TargetMinor          int64
	Currency             string
	TargetMonth          *time.Time
	PlannedMonthlyMinor  int64
	StartingBalanceMinor int64
}

// GoalUpdate is a PATCH: a nil field is unchanged. ClearTargetMonth is the
// explicit-clear convention (clearReceivedAmount's, on transactions): a nil
// pointer already means "unchanged", so it cannot also mean "clear".
//
// There is deliberately no Currency field: GoalRepository.Update says
// currency is not mutable, and only NewGoal supplies one, at creation. A
// caller cannot even attempt a currency change through this type -- the
// compiler refuses it before any runtime check could. A stray "currency"
// key on a PATCH body is the HTTP layer's PATCH decoder's job to refuse
// (422, via ErrGoalCurrencyImmutable), not this struct's.
type GoalUpdate struct {
	Name                *string
	TargetMinor         *int64
	TargetMonth         *time.Time
	ClearTargetMonth    bool
	PlannedMonthlyMinor *int64
}

// NewContribution is what AddContribution receives. It carries no currency:
// a contribution is its goal's currency by construction, so the service
// reads the goal's own Target.Currency rather than trusting a caller to
// supply the right one.
type NewContribution struct {
	HouseholdID string
	GoalID      string
	AmountMinor int64
	OccurredOn  time.Time
	Note        string
}

// GoalDeps gathers every port GoalService needs, mirroring BudgetDeps.
// There is no Clock: List, Create, SetArchived and AddContribution all take
// the time they need as a parameter, so every test is deterministic.
type GoalDeps struct {
	Goals      GoalRepository
	Households HouseholdRepository
	FX         FXRateProvider
}

// GoalService composes the Goals screen and every write against it. Like
// every other service here it takes no actor parameter: services enforce
// what is *valid*, middleware enforces who is *asking* -- the money
// capability and the owner check live in the router.
type GoalService struct {
	d GoalDeps
}

func NewGoalService(d GoalDeps) *GoalService {
	return &GoalService{d: d}
}

// List composes the whole Goals screen for one household: each goal's card
// (contributed, percent, status, required monthly) plus the page summary,
// in three repository calls regardless of how many goals exist. today is
// always a parameter -- see GoalDeps' own comment -- so status and the
// next-goal figure are deterministic in tests.
//
// The summary's two totals follow BudgetService.Month's Spent rule
// (docs/LEARNING.md pattern 12): convert EACH goal's own figure into
// primary first, then add -- never sum minor units across currencies and
// convert the total. A no-rate goal is excluded from BOTH totals and
// counted in ExcludedNoRate, never silently dropped -- a quietly short
// total looks correct. The counts (dated/no-date/on-track) and the
// next-goal figure need no conversion -- they always consider live goals
// only, even when includeArchived is true and the card list also carries
// archived ones -- "X of Y on track" and the Monthly contributions card
// are never about a goal nobody tracks anymore.
func (s *GoalService) List(ctx context.Context, householdID string, includeArchived bool, today time.Time) (GoalsView, error) {
	household, err := s.d.Households.Get(ctx, householdID)
	if err != nil {
		return GoalsView{}, err
	}
	primary := household.PrimaryCurrency

	records, err := s.d.Goals.List(ctx, householdID, includeArchived)
	if err != nil {
		return GoalsView{}, err
	}

	monthTotals, err := s.d.Goals.MonthContributionTotals(ctx, householdID, today)
	if err != nil {
		return GoalsView{}, err
	}
	actualByGoal := make(map[string]int64, len(monthTotals))
	for _, t := range monthTotals {
		actualByGoal[t.GoalID] = t.AmountMinor
	}

	plannedTotal, err := domain.NewMoney(0, primary)
	if err != nil {
		return GoalsView{}, err
	}
	actualTotal := plannedTotal

	var counts goalCounts
	excludedNoRate := 0

	conv := NewConverter(s.d.FX, primary)
	views := make([]GoalView, 0, len(records))
	for _, rec := range records {
		g := rec.Goal
		view := goalCardView(rec, today)
		views = append(views, view)

		if g.IsArchived() {
			// The card still renders (above); the summary never counts an
			// archived goal, in either count or either total.
			continue
		}
		counts.add(g, view.Status)

		plannedInPrimary, actualInPrimary, hasActual, excluded, err := s.monthlyInPrimary(ctx, conv, g, actualByGoal)
		if err != nil {
			return GoalsView{}, err
		}
		if excluded {
			excludedNoRate++
			continue
		}

		plannedTotal, err = plannedTotal.Add(plannedInPrimary)
		if err != nil {
			return GoalsView{}, err
		}
		if hasActual {
			actualTotal, err = actualTotal.Add(actualInPrimary)
			if err != nil {
				return GoalsView{}, err
			}
		}
	}

	return GoalsView{
		Goals: views,
		Summary: GoalsSummary{
			Currency:            primary,
			PlannedMonthlyTotal: plannedTotal,
			ActualThisMonth:     actualTotal,
			OnTrackCount:        counts.onTrack,
			DatedCount:          counts.dated,
			NoDateCount:         counts.noDate,
			ExcludedNoRate:      excludedNoRate,
			NextGoalID:          counts.nextID,
			NextGoalName:        counts.nextName,
			NextGoalMonth:       counts.nextMonth,
		},
	}, nil
}

// goalCardView is one goal's card: contributed, percent, status and, for a
// live dated goal not yet achieved, the monthly amount still required.
func goalCardView(rec GoalRecord, today time.Time) GoalView {
	g := rec.Goal
	status := domain.GoalStatusFor(g, rec.ContributedMinor, today)
	view := GoalView{
		Goal:        g,
		Contributed: domain.Money{Amount: rec.ContributedMinor, Currency: g.Target.Currency},
		Percent:     domain.GoalProgressPercent(rec.ContributedMinor, g.Target.Amount),
		Status:      status,
	}
	if !g.IsArchived() && g.TargetMonth != nil && status != domain.GoalAchieved {
		monthsLeft := domain.MonthsLeftInclusive(*g.TargetMonth, today)
		remaining := domain.GoalRemainingMinor(rec.ContributedMinor, g.Target.Amount)
		if required, ok := domain.RequiredMonthlyMinor(remaining, monthsLeft); ok {
			view.RequiredMonthly = domain.Money{Amount: required, Currency: g.Target.Currency}
			view.RequiredMonthlyOK = true
		}
	}
	return view
}

// View is one goal exactly as List renders its card, archived included, for
// a caller that needs a single one -- a write handler answering with the
// card it just changed. One repository read, where List would cost three
// plus a summary nobody asked for; a missing goal is domain.ErrNotFound.
func (s *GoalService) View(ctx context.Context, householdID, goalID string, today time.Time) (GoalView, error) {
	rec, err := s.d.Goals.Get(ctx, householdID, goalID)
	if err != nil {
		return GoalView{}, err
	}
	return goalCardView(rec, today), nil
}

// goalCounts is the currency-independent half of the Goals summary: the
// dated, no-date and on-track counts and the next goal to land. List feeds it
// live goals only.
type goalCounts struct {
	onTrack, dated, noDate int
	nextID, nextName       string
	nextMonth              *time.Time
}

func (c *goalCounts) add(g domain.Goal, status domain.GoalStatus) {
	switch {
	case status == domain.GoalAchieved:
		// In neither count -- it is not a goal to be on track for.
	case g.TargetMonth == nil:
		c.noDate++
	default:
		c.dated++
		if status == domain.GoalOnTrack {
			c.onTrack++
		}
		if c.nextID == "" || g.TargetMonth.Before(*c.nextMonth) ||
			(g.TargetMonth.Equal(*c.nextMonth) && g.Name < c.nextName) {
			c.nextID, c.nextName, c.nextMonth = g.ID, g.Name, g.TargetMonth
		}
	}
}

// monthlyInPrimary converts one goal's planned monthly figure and, if it
// received anything this month, its actual figure into primary. excluded
// means the goal's currency has no rate, so neither figure is added to a
// total.
//
// Convert-then-add, per goal: planned and actual share the goal's one
// currency, so either both convert or neither does -- splitting them into
// two independently-guarded conversions would let the two totals disagree
// about which goals had a rate. Converter.TryConvert decides "excluded";
// any error it returns is returned here.
func (s *GoalService) monthlyInPrimary(ctx context.Context, conv *Converter, g domain.Goal, actualByGoal map[string]int64) (planned, actual domain.Money, hasActual, excluded bool, err error) {
	planned, hasRate, err := conv.TryConvert(ctx, g.PlannedMonthly)
	if err != nil {
		return domain.Money{}, domain.Money{}, false, false, err
	}
	if !hasRate {
		return domain.Money{}, domain.Money{}, false, true, nil
	}
	amount, ok := actualByGoal[g.ID]
	if !ok {
		return planned, domain.Money{}, false, false, nil
	}
	actual, hasRate, err = conv.TryConvert(ctx, domain.Money{Amount: amount, Currency: g.Target.Currency})
	if err != nil {
		return domain.Money{}, domain.Money{}, false, false, err
	}
	if !hasRate {
		return domain.Money{}, domain.Money{}, false, true, nil
	}
	return planned, actual, true, false, nil
}

// Create validates and writes a new goal, every check before the
// repository is called. The target month, if given, normalises to the
// first of its month -- the same convention target_month and budgets.month
// use -- so a caller passing the 15th can't leave a date
// MonthsLeftInclusive disagrees with.
func (s *GoalService) Create(ctx context.Context, in NewGoal, createdOn time.Time) (domain.Goal, error) {
	name := strings.TrimSpace(in.Name)
	if name == "" {
		return domain.Goal{}, domain.ErrGoalNameRequired
	}
	if in.TargetMinor <= 0 {
		return domain.Goal{}, domain.ErrGoalTargetNotPositive
	}
	if in.PlannedMonthlyMinor < 0 {
		return domain.Goal{}, domain.ErrGoalPlannedMonthlyNegative
	}

	// domain.NewMoney validates the currency through domain.ParseCurrency,
	// the single reference for what a valid code is -- an unknown currency
	// surfaces as that function's own error, wrapping domain.ErrInvalidMoney.
	target, err := domain.NewMoney(in.TargetMinor, in.Currency)
	if err != nil {
		return domain.Goal{}, err
	}
	// target.Currency, not in.Currency: NewMoney has already uppercased and
	// validated it, so PlannedMonthly cannot end up disagreeing with Target
	// over casing on a currency that happens to parse both ways.
	planned, err := domain.NewMoney(in.PlannedMonthlyMinor, target.Currency)
	if err != nil {
		return domain.Goal{}, err
	}

	var targetMonth *time.Time
	if in.TargetMonth != nil {
		m := startOfMonth(*in.TargetMonth)
		targetMonth = &m
	}

	goal := domain.Goal{
		HouseholdID:    in.HouseholdID,
		Name:           name,
		Target:         target,
		TargetMonth:    targetMonth,
		PlannedMonthly: planned,
	}

	// A negative StartingBalanceMinor passes through unchanged: a goal may
	// start in deficit if the household says so. GoalRepository.Create
	// writes no contribution row at all when the figure is exactly zero.
	return s.d.Goals.Create(ctx, goal, in.StartingBalanceMinor, createdOn)
}

// Update merges the patch onto the stored goal and validates the *result*
// -- the same ordering AccountService.Update explains: validating the
// assembled whole, not just the incoming fields, stops two
// independently-legal changes combining into an illegal one. Currency is
// never touched -- GoalUpdate has no field for it, so g.Target.Currency
// stays whatever Get read off the stored row.
func (s *GoalService) Update(ctx context.Context, householdID, goalID string, patch GoalUpdate) (domain.Goal, error) {
	rec, err := s.d.Goals.Get(ctx, householdID, goalID)
	if err != nil {
		return domain.Goal{}, err
	}
	g := rec.Goal

	if patch.Name != nil {
		name := strings.TrimSpace(*patch.Name)
		if name == "" {
			return domain.Goal{}, domain.ErrGoalNameRequired
		}
		g.Name = name
	}
	if patch.TargetMinor != nil {
		if *patch.TargetMinor <= 0 {
			return domain.Goal{}, domain.ErrGoalTargetNotPositive
		}
		g.Target.Amount = *patch.TargetMinor
	}
	if patch.PlannedMonthlyMinor != nil {
		if *patch.PlannedMonthlyMinor < 0 {
			return domain.Goal{}, domain.ErrGoalPlannedMonthlyNegative
		}
		g.PlannedMonthly.Amount = *patch.PlannedMonthlyMinor
	}
	// ClearTargetMonth wins over a nil TargetMonth: without it there is no
	// way to tell "leave alone" from "picked 'No target date'" -- both
	// arrive as nil. Both false/nil leaves the stored date untouched.
	if patch.ClearTargetMonth {
		g.TargetMonth = nil
	} else if patch.TargetMonth != nil {
		m := startOfMonth(*patch.TargetMonth)
		g.TargetMonth = &m
	}

	return s.d.Goals.Update(ctx, g)
}

// SetArchived archives or restores a goal, stamping ArchivedAt with at
// rather than reading time.Now() -- the same convention Create's createdOn
// and List's today follow, so this service never reaches for a clock
// directly (GoalDeps has none, deliberately). at must arrive as a
// parameter for the same reason: GoalRepository.SetArchived takes
// `at time.Time`, and there is no Clock here to produce one from. The HTTP
// handler supplies it from its own injected clock.
func (s *GoalService) SetArchived(ctx context.Context, householdID, goalID string, archived bool, at time.Time) (domain.Goal, error) {
	return s.d.Goals.SetArchived(ctx, householdID, goalID, archived, at)
}

// AddContribution writes one manual contribution, in the goal's own
// currency -- never the household's primary -- after two checks, both
// before any write: the amount must be non-zero (goal_contributions' own
// CHECK (amount_minor <> 0)), and the goal must not be archived.
//
// Before either check, it reads the goal via Goals.Get(in.HouseholdID,
// in.GoalID) -- not a redundant lookup. InsertGoalContribution's SQL has no
// constraint tying goal_contributions.goal_id to household_id
// (00007_goals.sql), so a caller could write a contribution against
// another household's goal id. That forged row is invisible to the
// victim's own ListContributions (filtered by the row's attacker-owned
// household_id) but IS summed into the victim's ContributedMinor --
// GetGoalWithTotal and ListGoalsWithTotals join by goal_id alone, with no
// household_id check on the contribution side. Get(...) is the barrier: a
// goal outside THIS household reads as domain.ErrNotFound, same as one
// that doesn't exist, refused before anything is written.
func (s *GoalService) AddContribution(ctx context.Context, in NewContribution) (domain.GoalContribution, error) {
	if in.AmountMinor == 0 {
		return domain.GoalContribution{}, domain.ErrContributionAmountZero
	}

	rec, err := s.d.Goals.Get(ctx, in.HouseholdID, in.GoalID)
	if err != nil {
		return domain.GoalContribution{}, err
	}
	if rec.Goal.IsArchived() {
		return domain.GoalContribution{}, domain.ErrGoalArchived
	}

	c := domain.GoalContribution{
		GoalID:      in.GoalID,
		HouseholdID: in.HouseholdID,
		Amount:      domain.Money{Amount: in.AmountMinor, Currency: rec.Goal.Target.Currency},
		OccurredOn:  in.OccurredOn,
		Note:        in.Note,
		Source:      domain.ContributionManual,
	}
	return s.d.Goals.AddContribution(ctx, c)
}

// DeleteContribution removes one contribution. It needs no guard the way
// AddContribution needs one: GoalRepository.DeleteContribution scopes its
// DELETE by household_id AND goal_id AND the contribution id together, so a
// foreign household id simply matches no row (domain.ErrNotFound).
func (s *GoalService) DeleteContribution(ctx context.Context, householdID, goalID, contributionID string) error {
	return s.d.Goals.DeleteContribution(ctx, householdID, goalID, contributionID)
}

// Contributions lists one goal's recent contributions, newest first, at
// the repository's default limit (ListContributions treats limit <= 0 as
// 50, following TransactionRepository.List's convention).
func (s *GoalService) Contributions(ctx context.Context, householdID, goalID string) ([]domain.GoalContribution, error) {
	return s.d.Goals.ListContributions(ctx, householdID, goalID, 0)
}
