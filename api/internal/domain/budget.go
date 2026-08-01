package domain

import "time"

// BudgetLine is a category's spending cap in a month's budget.
type BudgetLine struct {
	CategoryID string
	Cap        Money
}

// Budget is a household's monthly spending plan with per-category caps and
// optional expected income.
type Budget struct {
	ID             string
	HouseholdID    string
	Month          time.Time // first of month
	ExpectedIncome *Money    // nil = not provided, hides the income cards
	Lines          []BudgetLine

	// RolledOverAt and RolloverGoalID are the stamp 00007_goals.sql adds to
	// budgets: nil/"" until BudgetService.RollOver moves this month's
	// unspent money into a goal, and set exactly once thereafter. The two
	// always move together -- both nil, or both populated -- which
	// migrations/00007_goals.sql's rollover_stamp_is_whole CHECK constraint
	// enforces at the schema level, so nothing in this layer needs to guard
	// against seeing one without the other. RolloverGoalID follows the same
	// "" <-> SQL NULL convention Account.OwnerMembershipID documents.
	RolledOverAt   *time.Time
	RolloverGoalID string

	// RolloverAmountMinor is the amount RollOver actually moved -- read off
	// the goal_contributions row it wrote (source = 'budget_rollover'), never
	// recomputed from Budgeted minus Spent. That recomputation is exactly the
	// bug this field exists to close: Remaining moves with every later
	// transaction in the month (a backdated entry, an edit, a delete), so a
	// caller that rendered Remaining next to a past-tense "moved into X"
	// sentence was quietly restating a completed action with a live number
	// (see BudgetService.Month's own comment on RolloverAmountMinor for the
	// concrete failure).
	//
	// Unlike RolledOverAt/RolloverGoalID, this is NOT DB-CHECK-enforced to
	// move with the other two -- it comes from a different table
	// (goal_contributions), read via a join, not a column on budgets itself.
	// It is populated with a real value ONLY by BudgetRepository.Get. Upsert
	// and History both return a domain.Budget too, but neither one's result
	// ever reaches BudgetMonthView -- BudgetService.Month builds its Budget
	// exclusively through Get -- so both leave this nil regardless of
	// whether the month was actually rolled over, and that nil is never
	// observed by anything that renders it.
	RolloverAmountMinor *int64
}

// DaysLeftInMonth is the spec's pinned rule: today counts, because the
// household can still spend today. A past month has no days left; a future
// month has all of them. Comparison is by calendar month, not by instant, so
// "today at 23:59" and "today at 00:00" agree.
func DaysLeftInMonth(month, today time.Time) int {
	mStart := time.Date(month.Year(), month.Month(), 1, 0, 0, 0, 0, time.UTC)
	tStart := time.Date(today.Year(), today.Month(), 1, 0, 0, 0, 0, time.UTC)
	daysIn := mStart.AddDate(0, 1, -1).Day()
	switch {
	case mStart.Before(tStart):
		return 0
	case mStart.After(tStart):
		return daysIn
	default:
		return daysIn - today.Day() + 1
	}
}

// PercentUsed rounds to the nearest whole percent, half away from zero,
// sign-aware on negative net spend (e.g., refunds exceeding the month's
// spend). ok=false when nothing is budgeted; the caller hides the figure
// rather than showing NaN or infinity.
func PercentUsed(spentMinor, budgetedMinor int64) (int, bool) {
	if budgetedMinor == 0 {
		return 0, false
	}
	half := budgetedMinor / 2
	n := spentMinor * 100
	if n < 0 {
		return int((n - half) / budgetedMinor), true
	}
	return int((n + half) / budgetedMinor), true
}

// DailyPace floors: telling a household it can spend S$137/day when the true
// figure is 136.92 overshoots by month end; flooring never does.
func DailyPace(remainingMinor int64, daysLeft int) (int64, bool) {
	if remainingMinor <= 0 || daysLeft <= 0 {
		return 0, false
	}
	return remainingMinor / int64(daysLeft), true
}
