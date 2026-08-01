package domain

import (
	"fmt"
	"time"
)

// GoalStatus is what the card's pill says. "none" is a real state, not a
// missing one: a goal with no target date has nothing to be on track against.
type GoalStatus string

const (
	GoalOnTrack    GoalStatus = "on_track"
	GoalBehind     GoalStatus = "behind"
	GoalAchieved   GoalStatus = "achieved"
	GoalStatusNone GoalStatus = "none"
)

// ContributionSource says where a contribution came from. It arrives from a
// database column, so ParseContributionSource refuses anything else.
type ContributionSource string

const (
	ContributionManual          ContributionSource = "manual"
	ContributionStartingBalance ContributionSource = "starting_balance"
	ContributionBudgetRollover  ContributionSource = "budget_rollover"
)

func ParseContributionSource(s string) (ContributionSource, error) {
	switch ContributionSource(s) {
	case ContributionManual:
		return ContributionManual, nil
	case ContributionStartingBalance:
		return ContributionStartingBalance, nil
	case ContributionBudgetRollover:
		return ContributionBudgetRollover, nil
	default:
		// The default is the point: this value arrives from a database column
		// or a request body, so an unrecognised one is refused rather than
		// carried further, the same rule ParseTransactionKind follows.
		return "", fmt.Errorf("%w: %q", ErrUnknownContributionSource, s)
	}
}

type Goal struct {
	ID             string
	HouseholdID    string
	Name           string
	Target         Money
	TargetMonth    *time.Time // nil = no target date; else the first of a month
	PlannedMonthly Money
	ArchivedAt     *time.Time
}

func (g Goal) IsArchived() bool { return g.ArchivedAt != nil }

type GoalContribution struct {
	ID                string
	GoalID            string
	HouseholdID       string
	Amount            Money
	OccurredOn        time.Time
	Note              string
	Source            ContributionSource
	SourceBudgetMonth *time.Time // set only when Source is ContributionBudgetRollover
}

// MonthsLeftInclusive counts whole calendar months from today's month to the
// target month, counting both ends: Aug -> Dec is 5, and a target in the
// current month is 1, because the household can still contribute this month.
// A target month already past returns 0, and callers must treat 0 as "behind",
// never as a divisor.
func MonthsLeftInclusive(targetMonth, today time.Time) int {
	target := time.Date(targetMonth.Year(), targetMonth.Month(), 1, 0, 0, 0, 0, time.UTC)
	now := time.Date(today.Year(), today.Month(), 1, 0, 0, 0, 0, time.UTC)
	months := (target.Year()-now.Year())*12 + int(target.Month()) - int(now.Month())
	if months < 0 {
		return 0
	}
	return months + 1
}

// RequiredMonthlyMinor rounds UP. Rounding down states a figure that does not
// actually reach the target. ok is false when monthsLeft <= 0 — there is no
// honest number, and the caller must not divide.
func RequiredMonthlyMinor(remainingMinor int64, monthsLeft int) (int64, bool) {
	if monthsLeft <= 0 {
		return 0, false
	}
	if remainingMinor <= 0 {
		return 0, true
	}
	m := int64(monthsLeft)
	return (remainingMinor + m - 1) / m, true
}

func GoalRemainingMinor(contributedMinor, targetMinor int64) int64 {
	if contributedMinor >= targetMinor {
		return 0
	}
	return targetMinor - contributedMinor
}

// GoalProgressPercent is contributed/target to the nearest whole percent,
// capped at 100 for the ring and floored at 0 so a net-negative goal never
// renders a reversed ring.
func GoalProgressPercent(contributedMinor, targetMinor int64) int {
	if targetMinor <= 0 || contributedMinor <= 0 {
		return 0
	}
	pct := int((contributedMinor*100 + targetMinor/2) / targetMinor)
	if pct > 100 {
		return 100
	}
	return pct
}

// GoalStatusFor is the spec's status table as one function. It never divides
// by a non-positive months-left.
func GoalStatusFor(g Goal, contributedMinor int64, today time.Time) GoalStatus {
	if contributedMinor >= g.Target.Amount {
		return GoalAchieved
	}
	if g.IsArchived() || g.TargetMonth == nil {
		return GoalStatusNone
	}
	monthsLeft := MonthsLeftInclusive(*g.TargetMonth, today)
	required, ok := RequiredMonthlyMinor(GoalRemainingMinor(contributedMinor, g.Target.Amount), monthsLeft)
	if !ok {
		// The target month has passed and the goal was not met. Behind is the
		// honest answer, and no division happens to produce it.
		return GoalBehind
	}
	if required <= g.PlannedMonthly.Amount {
		return GoalOnTrack
	}
	return GoalBehind
}
