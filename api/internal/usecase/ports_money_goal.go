// This file holds the money slice's goal ports. ports.go lists every ports
// file.

package usecase

import (
	"context"
	"time"

	"github.com/andreasoentoro/hearth/api/internal/domain"
)

// GoalRecord is one goal with the only derived figure the repository can
// supply: the sum of its contributions. Every other figure on the screen
// (percent, status, required monthly) is domain arithmetic the service does,
// not something SQL should be asked to know.
type GoalRecord struct {
	Goal             domain.Goal
	ContributedMinor int64
}

// GoalMonthTotal is one goal's contributions inside one calendar month.
type GoalMonthTotal struct {
	GoalID      string
	AmountMinor int64
}

// GoalLookup is what a service OUTSIDE the goals feature needs to know
// about a goal, and nothing more: fetch one by id. BudgetService.RollOver
// is the only caller, reading the target goal before rolling a month's
// unspent money into it. Narrow on purpose, the same interface-segregation
// reason HoldingCounter exists: the budget service must not grow a
// dependency on goal writes it was never meant to make. Every
// GoalRepository satisfies it, so wiring passes the same repository.
type GoalLookup interface {
	// Get has GoalRepository.Get's contract exactly: domain.ErrNotFound for
	// an unknown id and for another household's goal alike.
	Get(ctx context.Context, householdID, goalID string) (GoalRecord, error)
}

// GoalRepository's implementation must not trust a contribution's household
// scoping to be self-evident: goal_contributions (00007_goals.sql) has no
// database-level constraint tying its household_id to its own goal_id's
// household_id, so a row could carry a household_id that disagrees with the
// goal it names. Every method that reads or writes a contribution --
// AddContribution, DeleteContribution, ListContributions and
// MonthContributionTotals -- must therefore filter by household_id AND
// goal_id together, never by contribution id or goal id alone, or a
// contribution could leak across households.
type GoalRepository interface {
	// List returns one household's goals with their contributed totals,
	// ordered: dated goals first (newest TargetMonth first, ties by name), then
	// dateless goals (TargetMonth == nil) by name -- a dateless goal never
	// sorts ahead of a dated one, pinned here so an ORDER BY (e.g. target_month
	// DESC NULLS LAST, name) can't silently pick the opposite NULL placement.
	// includeArchived is a UNION, not a filter swap: false returns the live
	// goals, true returns live AND archived together, each carrying its own
	// ArchivedAt -- the AccountRepository.List / CategoryRepository.List
	// contract. Don't implement it as "archived instead".
	List(ctx context.Context, householdID string, includeArchived bool) ([]GoalRecord, error)
	// Get reports domain.ErrNotFound when no goal with this id exists in this
	// household — including when one exists in a different household, which
	// must be indistinguishable from not existing at all.
	Get(ctx context.Context, householdID, goalID string) (GoalRecord, error)
	// Create writes the goal and, when startingBalanceMinor is non-zero, its
	// opening contribution (source starting_balance, dated createdOn) in ONE
	// transaction. A goal whose opening contribution is missing is not a state
	// this port can produce. A name colliding with UNIQUE (household_id, name)
	// — archived rows included — surfaces as domain.ErrGoalNameTaken.
	Create(ctx context.Context, g domain.Goal, startingBalanceMinor int64, createdOn time.Time) (domain.Goal, error)
	// Update replaces every mutable column: name, target, target month
	// (nil clears it), planned monthly. Currency is NOT mutable — see
	// GoalService.Update's own comment. Same collision contract as Create.
	Update(ctx context.Context, g domain.Goal) (domain.Goal, error)
	// SetArchived stamps archived_at with at, or clears it when archived is
	// false -- same signature as AccountRepository.SetArchived, at supplied by
	// the caller rather than time.Now() inside the port. Archiving is
	// idempotent: a second call keeps the FIRST stamp (COALESCE(archived_at,
	// $at), the same rule CategoryRepository.SetArchived applies), and keeps
	// every contribution and rollover reference intact -- there is no delete,
	// the accounts precedent.
	SetArchived(ctx context.Context, householdID, goalID string, archived bool, at time.Time) (domain.Goal, error)
	// AddContribution writes one row. c.ID is ignored; the database assigns
	// it. c.Amount's currency must equal the goal's — the service checks, and
	// the column does not exist to hold a second answer.
	AddContribution(ctx context.Context, c domain.GoalContribution) (domain.GoalContribution, error)
	// DeleteContribution removes one row and, when that row is a
	// budget_rollover, clears its month's rolled_over_at and rollover_goal_id
	// on budgets IN THE SAME TRANSACTION. Leaving the stamp would strand the
	// household: money gone from the goal, a month claiming it rolled over,
	// and 409 on every retry. domain.ErrNotFound when there was nothing to
	// remove.
	DeleteContribution(ctx context.Context, householdID, goalID, contributionID string) error
	// ListContributions returns one goal's contributions, newest first, at
	// most limit rows. limit follows TransactionRepository.List's own
	// convention rather than inventing a second one: limit <= 0 is treated
	// as 50, and anything above 200 is clamped down to it. A real LIMIT 0
	// returns zero rows, the opposite of "no cap", which is exactly why this
	// port pins a default instead of passing limit through unclamped.
	ListContributions(ctx context.Context, householdID, goalID string, limit int) ([]domain.GoalContribution, error)
	// MonthContributionTotals sums each unarchived goal's contributions inside
	// one calendar month, EXCLUDING source 'starting_balance'. The exclusion is
	// load-bearing and lives here so no caller can forget it: a household
	// creating four goals with existing balances would otherwise read
	// "S$41,200 added in August" for money that never moved.
	MonthContributionTotals(ctx context.Context, householdID string, month time.Time) ([]GoalMonthTotal, error)
}
