package usecase

import (
	"context"
	"strings"
	"time"

	"github.com/andreasoentoro/hearth/api/internal/domain"
)

// GoalView is one card: the stored goal plus every derived figure the screen
// shows. RequiredMonthly is present only for a dated, unachieved goal --
// RequiredMonthlyOK false means the card shows no "needs S$X/mo" line rather
// than a zero.
//
// Contributed and RequiredMonthly are in the GOAL's own currency, not the
// household's primary: the card renders an IDR goal in IDR. Only the two
// summary totals below convert. Converting here as well would leave the card
// saying "S$2,600 of Rp 40,000,000", which is not a sentence.
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

// GoalUpdate is a PATCH: a nil field is unchanged. ClearTargetMonth is how a
// dated goal loses its date, the same explicit-clear convention
// clearReceivedAmount uses on transactions -- a nil pointer already means
// "unchanged", so it cannot also mean "clear".
//
// There is deliberately no Currency field: GoalRepository.Update's own doc
// comment says currency is not mutable, and the only place a currency is
// ever supplied is NewGoal, at creation. A caller cannot even attempt a
// currency change through this type -- there is no field to put one in, so
// the compiler refuses it before any runtime check would get the chance to.
// If a JSON request body carries a "currency" key on a PATCH anyway, that is
// Task 8's PATCH decoder's own problem to refuse (the design spec's own
// Error handling section requires it, 422, via ErrGoalCurrencyImmutable) --
// it cannot be this struct's, because nothing here ever sees that key.
type GoalUpdate struct {
	Name                *string
	TargetMinor         *int64
	TargetMonth         *time.Time
	ClearTargetMonth    bool
	PlannedMonthlyMinor *int64
}

// NewContribution is what AddContribution receives. It carries no currency:
// a contribution is its goal's currency by construction (design decision 5),
// so the service reads the goal's own Target.Currency rather than trusting a
// caller to supply the right one.
type NewContribution struct {
	HouseholdID string
	GoalID      string
	AmountMinor int64
	OccurredOn  time.Time
	Note        string
}

// GoalDeps gathers every port GoalService needs, mirroring BudgetDeps. There
// is no Clock here for the same reason BudgetDeps has none: List, Create,
// SetArchived and AddContribution all take the time they need as a
// parameter (today, createdOn, at, in.OccurredOn), so nothing in this
// service reads time.Now() and every test is deterministic.
type GoalDeps struct {
	Goals      GoalRepository
	Households HouseholdRepository
	FX         FXRateProvider
}

// GoalService composes the Goals screen and every write against it. Like
// every other service here it takes no actor parameter: services enforce
// what is *valid*, middleware enforces who is *asking* -- the money
// capability and the owner check live in the router (Task 8).
type GoalService struct {
	d GoalDeps
}

func NewGoalService(d GoalDeps) *GoalService {
	return &GoalService{d: d}
}

// List composes the whole Goals screen for one household: each goal's card
// (contributed, percent, status, required monthly) plus the page summary.
// today is always a parameter -- see GoalDeps' own comment -- so status and
// the next-goal figure are deterministic in tests and driven by the clock
// port at the HTTP layer in production.
//
// The household is read once for the primary currency, the goals once, and
// MonthContributionTotals once -- three repository calls regardless of how
// many goals the household has.
//
// The summary's two totals follow the exact rule BudgetService.Month's Spent
// figure does (docs/LEARNING.md pattern 12): convert EACH goal's own figure
// into the household's primary currency first, then add -- never sum minor
// units across currencies and convert the total. A goal whose currency has
// no available rate is excluded from BOTH totals and counted in
// ExcludedNoRate, never silently dropped; a quietly short total would look
// identical to a correct one. The counts (dated/no-date/on-track) and the
// next-goal figure need no conversion at all -- they are currency-independent
// -- and always consider live (unarchived) goals only, even when
// includeArchived is true and the returned card list also carries archived
// ones: the "X of Y on track" copy and the Monthly contributions card are
// never about a goal nobody is tracking anymore.
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

	var (
		onTrackCount, datedCount, noDateCount, excludedNoRate int
		nextGoalID, nextGoalName                              string
		nextGoalMonth                                         *time.Time
	)

	views := make([]GoalView, 0, len(records))
	for _, rec := range records {
		g := rec.Goal
		status := domain.GoalStatusFor(g, rec.ContributedMinor, today)
		percent := domain.GoalProgressPercent(rec.ContributedMinor, g.Target.Amount)

		view := GoalView{
			Goal:        g,
			Contributed: domain.Money{Amount: rec.ContributedMinor, Currency: g.Target.Currency},
			Percent:     percent,
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
		views = append(views, view)

		if g.IsArchived() {
			// The card still renders (above); the summary never counts an
			// archived goal, in either count or either total.
			continue
		}

		switch {
		case status == domain.GoalAchieved:
			// In neither count -- it is not a goal to be on track for.
		case g.TargetMonth == nil:
			noDateCount++
		default:
			datedCount++
			if status == domain.GoalOnTrack {
				onTrackCount++
			}
			if nextGoalID == "" || g.TargetMonth.Before(*nextGoalMonth) ||
				(g.TargetMonth.Equal(*nextGoalMonth) && g.Name < nextGoalName) {
				nextGoalID, nextGoalName, nextGoalMonth = g.ID, g.Name, g.TargetMonth
			}
		}

		// Convert-then-add, per goal: plannedInPrimary and (if this goal
		// received anything this month) actualInPrimary share the goal's one
		// currency, so either both convert or neither does. Splitting these
		// into two independently-guarded conversions would let the two
		// totals disagree about which goals had a rate, which the "excluded
		// from BOTH totals" rule above forbids.
		plannedInPrimary, convErr := s.convert(ctx, g.PlannedMonthly, primary)
		excluded := convErr != nil

		var actualInPrimary domain.Money
		hasActual := false
		if amt, ok := actualByGoal[g.ID]; ok && !excluded {
			actualInPrimary, convErr = s.convert(ctx, domain.Money{Amount: amt, Currency: g.Target.Currency}, primary)
			if convErr != nil {
				excluded = true
			} else {
				hasActual = true
			}
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
			OnTrackCount:        onTrackCount,
			DatedCount:          datedCount,
			NoDateCount:         noDateCount,
			ExcludedNoRate:      excludedNoRate,
			NextGoalID:          nextGoalID,
			NextGoalName:        nextGoalName,
			NextGoalMonth:       nextGoalMonth,
		},
	}, nil
}

// Create validates and writes a new goal. Every check runs before the
// repository is ever called. The target month, if given, is normalised to
// the first of its month -- the same convention target_month and
// budgets.month already use -- so a caller passing the 15th does not leave a
// goal whose stored date disagrees with what MonthsLeftInclusive assumes
// about it.
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

	// A negative StartingBalanceMinor is passed through unchanged: a goal is
	// allowed to start in deficit if the household says so (a debt being
	// tracked as a savings goal, for instance), and GoalRepository.Create's
	// own contract writes no contribution row at all when the figure is
	// exactly zero.
	return s.d.Goals.Create(ctx, goal, in.StartingBalanceMinor, createdOn)
}

// Update merges the patch onto the stored goal and validates the *result*,
// the same ordering AccountService.Update's own comment explains: a nil
// field means "leave alone," and validating the assembled whole (rather than
// only the incoming fields) is what stops two independently-legal changes
// from combining into an illegal one. Currency is never touched -- GoalUpdate
// carries no field for it (see that type's own comment), so g.Target.Currency
// is always whatever Get just read off the stored row.
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
	// ClearTargetMonth wins over a nil TargetMonth being ambiguous: without
	// it, there would be no way to distinguish "leave the date alone" from
	// "the household picked 'No target date'" -- both would arrive as
	// patch.TargetMonth == nil. Both false/nil leaves the stored date
	// completely untouched.
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
// directly (GoalDeps has none, deliberately -- see its own comment). This
// signature is one parameter wider than the brief's own sketch of it:
// GoalRepository.SetArchived (widened in Task 3's fix round) takes
// `at time.Time`, and there is no Clock in GoalDeps to produce one from
// inside this method, so it must arrive as a parameter the same way
// Create's createdOn does. Task 8's handler supplies it from its own
// injected clock.
func (s *GoalService) SetArchived(ctx context.Context, householdID, goalID string, archived bool, at time.Time) (domain.Goal, error) {
	return s.d.Goals.SetArchived(ctx, householdID, goalID, archived, at)
}

// AddContribution writes one manual contribution, in the goal's own
// currency -- never the household's primary -- after two checks, both
// before any write: the amount must be non-zero (goal_contributions' own
// CHECK (amount_minor <> 0)), and the goal must not be archived.
//
// Before either of those, it reads the goal via
// Goals.Get(in.HouseholdID, in.GoalID). This is not a redundant lookup:
// InsertGoalContribution's SQL has no constraint tying
// goal_contributions.goal_id to its own household_id column
// (00007_goals.sql), so nothing stops a caller from writing a contribution
// against a goal id that belongs to a DIFFERENT household. A row like that
// is invisible to the victim's own ListContributions (which filters by the
// row's own household_id, and the forged row carries the attacker's) but IS
// summed into the victim's ContributedMinor: GetGoalWithTotal and
// ListGoalsWithTotals both join goal_contributions to goals by goal_id
// alone, with no household_id check on the contribution side. That is a
// cross-household write with no error returned to the attacker and no trace
// visible in the one place the victim would look. Get(...) is what stands
// between a caller's household id and the goal id it named: a goal that does
// not exist in THIS household is indistinguishable from one that does not
// exist at all (the port's own contract), so its domain.ErrNotFound is
// returned unchanged, before anything is written.
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
// AddContribution needs one: GoalRepository.DeleteContribution's own doc
// comment requires the adapter to scope its DELETE by household_id AND
// goal_id AND the contribution id together, so a foreign household id
// simply matches no row (domain.ErrNotFound) -- there is no INSERT-shaped
// gap here to close.
func (s *GoalService) DeleteContribution(ctx context.Context, householdID, goalID, contributionID string) error {
	return s.d.Goals.DeleteContribution(ctx, householdID, goalID, contributionID)
}

// Contributions lists one goal's recent contributions, newest first, at the
// repository's own default limit (ListContributions treats limit <= 0 as
// its default of 50, following TransactionRepository.List's own
// convention).
func (s *GoalService) Contributions(ctx context.Context, householdID, goalID string) ([]domain.GoalContribution, error) {
	return s.d.Goals.ListContributions(ctx, householdID, goalID, 0)
}

// convert turns one amount into the household's primary currency. This
// duplicates BudgetService.convert deliberately -- see that method's own
// comment: each service declares its own dependencies, and hoisting this
// into a shared helper would give one service a reason to change when
// another's FX needs do.
func (s *GoalService) convert(ctx context.Context, m domain.Money, primary string) (domain.Money, error) {
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
