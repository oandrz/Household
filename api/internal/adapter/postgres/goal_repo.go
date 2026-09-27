package postgres

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/andreasoentoro/hearth/api/internal/adapter/postgres/sqlcgen"
	"github.com/andreasoentoro/hearth/api/internal/domain"
	"github.com/andreasoentoro/hearth/api/internal/usecase"
)

// GoalRepo keeps the pool alongside the pool-backed *sqlcgen.Queries, like
// BudgetRepo, because Create and DeleteContribution each need to begin their
// own transaction -- something a *sqlcgen.Queries built once at construction
// time cannot do on its own.
type GoalRepo struct {
	q    *sqlcgen.Queries
	pool *pgxpool.Pool
}

func NewGoalRepo(db *DB) *GoalRepo {
	return &GoalRepo{q: sqlcgen.New(db.Pool()), pool: db.Pool()}
}

// var _ pins GoalRepo to usecase.GoalProgressReader at compile time, the
// narrower port VisionService depends on -- the same reason account_repo.go
// pins AccountRepo to AccountLookup and category_repo.go pins CategoryRepo
// to CategoryLookup: a signature drift from the port fails here, next to
// the repository, rather than at main.go's wiring.
var _ usecase.GoalProgressReader = (*GoalRepo)(nil)

func (r *GoalRepo) List(ctx context.Context, householdID string, includeArchived bool) ([]usecase.GoalRecord, error) {
	rows, err := r.q.ListGoalsWithTotals(ctx, sqlcgen.ListGoalsWithTotalsParams{
		HouseholdID:     uuid(householdID),
		IncludeArchived: includeArchived,
	})
	if err != nil {
		return nil, translate(err, "list goals with totals")
	}
	out := make([]usecase.GoalRecord, 0, len(rows))
	for _, row := range rows {
		rec, err := toGoalRecord(row.ID, row.HouseholdID, row.Name, row.TargetAmountMinor, row.Currency,
			row.TargetMonth, row.PlannedMonthlyMinor, row.ArchivedAt, row.ContributedMinor)
		if err != nil {
			return nil, err
		}
		out = append(out, rec)
	}
	return out, nil
}

func (r *GoalRepo) Get(ctx context.Context, householdID, goalID string) (usecase.GoalRecord, error) {
	row, err := r.q.GetGoalWithTotal(ctx, sqlcgen.GetGoalWithTotalParams{
		HouseholdID: uuid(householdID),
		ID:          uuid(goalID),
	})
	if err != nil {
		return usecase.GoalRecord{}, translate(err, "get goal with total")
	}
	return toGoalRecord(row.ID, row.HouseholdID, row.Name, row.TargetAmountMinor, row.Currency,
		row.TargetMonth, row.PlannedMonthlyMinor, row.ArchivedAt, row.ContributedMinor)
}

// Create writes the goal row and, when startingBalanceMinor is non-zero, its
// opening contribution, both inside one pgx.BeginFunc: a goal can never
// exist without the opening contribution its creation promised. A
// duplicate-name failure on the goal insert rolls back before the
// contribution insert runs, so no orphaned contribution can point at an
// unwritten goal (TestGoalCreateThatFailsWritesNothingAtAll). The reverse
// can't happen either: the only way the contribution insert fails is the
// CHECK on amount_minor <> 0, which this method never triggers (see the
// `startingBalanceMinor != 0` guard below) -- guarded by construction, not
// by a test.
func (r *GoalRepo) Create(ctx context.Context, g domain.Goal, startingBalanceMinor int64, createdOn time.Time) (domain.Goal, error) {
	var result domain.Goal
	err := pgx.BeginFunc(ctx, r.pool, func(tx pgx.Tx) error {
		q := r.q.WithTx(tx)

		row, err := q.CreateGoal(ctx, sqlcgen.CreateGoalParams{
			HouseholdID:         uuid(g.HouseholdID),
			Name:                g.Name,
			TargetAmountMinor:   g.Target.Amount,
			Currency:            g.Target.Currency,
			TargetMonth:         nullableDate(g.TargetMonth),
			PlannedMonthlyMinor: g.PlannedMonthly.Amount,
		})
		if err != nil {
			return translate(err, "create goal")
		}

		if startingBalanceMinor != 0 {
			if _, err := q.InsertGoalContribution(ctx, sqlcgen.InsertGoalContributionParams{
				GoalID:            row.ID,
				HouseholdID:       row.HouseholdID,
				AmountMinor:       startingBalanceMinor,
				OccurredOn:        dateOnly(createdOn),
				Note:              "",
				Source:            string(domain.ContributionStartingBalance),
				SourceBudgetMonth: pgtype.Date{}, // NULL: a starting balance is not a rollover
			}); err != nil {
				return translate(err, "insert opening contribution")
			}
		}

		goal, err := toGoal(row.ID, row.HouseholdID, row.Name, row.TargetAmountMinor, row.Currency,
			row.TargetMonth, row.PlannedMonthlyMinor, row.ArchivedAt)
		if err != nil {
			return err
		}
		result = goal
		return nil
	})
	if err != nil {
		return domain.Goal{}, err
	}
	return result, nil
}

// Update replaces name, target amount, target month and planned monthly.
// UpdateGoal's own SQL comment explains why currency and archived_at need no
// SET clause: currency is not mutable, and RETURNING hands both columns back
// exactly as they already were.
func (r *GoalRepo) Update(ctx context.Context, g domain.Goal) (domain.Goal, error) {
	row, err := r.q.UpdateGoal(ctx, sqlcgen.UpdateGoalParams{
		HouseholdID:         uuid(g.HouseholdID),
		ID:                  uuid(g.ID),
		Name:                g.Name,
		TargetAmountMinor:   g.Target.Amount,
		TargetMonth:         nullableDate(g.TargetMonth),
		PlannedMonthlyMinor: g.PlannedMonthly.Amount,
	})
	if err != nil {
		return domain.Goal{}, translate(err, "update goal")
	}
	return toGoal(row.ID, row.HouseholdID, row.Name, row.TargetAmountMinor, row.Currency,
		row.TargetMonth, row.PlannedMonthlyMinor, row.ArchivedAt)
}

// SetArchived stamps archived_at with at, or clears it when archived is
// false. SetGoalArchived's own COALESCE is "first stamp wins": archiving an
// already-archived goal keeps its original archived_at instead of moving it
// forward to at.
func (r *GoalRepo) SetArchived(ctx context.Context, householdID, goalID string, archived bool, at time.Time) (domain.Goal, error) {
	row, err := r.q.SetGoalArchived(ctx, sqlcgen.SetGoalArchivedParams{
		HouseholdID: uuid(householdID),
		ID:          uuid(goalID),
		Archived:    archived,
		At:          timestamptz(at),
	})
	if err != nil {
		return domain.Goal{}, translate(err, "set goal archived")
	}
	return toGoal(row.ID, row.HouseholdID, row.Name, row.TargetAmountMinor, row.Currency,
		row.TargetMonth, row.PlannedMonthlyMinor, row.ArchivedAt)
}

// AddContribution writes one row and echoes back c.Amount.Currency for the
// returned domain.Money: goal_contributions has no currency column of its
// own (00007_goals.sql -- a contribution is its goal's currency by
// construction), so nothing comes back to read one from. Safe because the
// port's own doc comment requires c.Amount's currency to equal the goal's.
func (r *GoalRepo) AddContribution(ctx context.Context, c domain.GoalContribution) (domain.GoalContribution, error) {
	row, err := r.q.InsertGoalContribution(ctx, sqlcgen.InsertGoalContributionParams{
		GoalID:            uuid(c.GoalID),
		HouseholdID:       uuid(c.HouseholdID),
		AmountMinor:       c.Amount.Amount,
		OccurredOn:        dateOnly(c.OccurredOn),
		Note:              c.Note,
		Source:            string(c.Source),
		SourceBudgetMonth: nullableDate(c.SourceBudgetMonth),
	})
	if err != nil {
		return domain.GoalContribution{}, translate(err, "insert goal contribution")
	}
	return toGoalContribution(row.ID, row.GoalID, row.HouseholdID, row.AmountMinor, c.Amount.Currency,
		row.OccurredOn, row.Note, row.Source, row.SourceBudgetMonth)
}

// DeleteContribution removes one row and, when it was a budget_rollover,
// clears that month's rolled_over_at/rollover_goal_id in the same
// transaction: leaving the stamp would strand the household with money gone
// from the goal, a month still claiming it rolled over, and a 409 on every
// retry.
//
// The stamp-clearing branch below is exercised by
// TestRollOverThenDeleteThenRollOverAgainSucceeds (budget_repo_test.go),
// which deletes a budget_rollover contribution and confirms the stamp
// clears.
func (r *GoalRepo) DeleteContribution(ctx context.Context, householdID, goalID, contributionID string) error {
	return pgx.BeginFunc(ctx, r.pool, func(tx pgx.Tx) error {
		q := r.q.WithTx(tx)

		row, err := q.DeleteGoalContribution(ctx, sqlcgen.DeleteGoalContributionParams{
			ID:          uuid(contributionID),
			GoalID:      uuid(goalID),
			HouseholdID: uuid(householdID),
		})
		if err != nil {
			return translate(err, "delete goal contribution")
		}

		if row.Source == string(domain.ContributionBudgetRollover) {
			if !row.SourceBudgetMonth.Valid {
				// 00007_goals.sql's own CHECK (rollover_names_its_month)
				// should make this unreachable: source = 'budget_rollover'
				// implies source_budget_month IS NOT NULL. Fail loud rather
				// than silently skip the clear -- a value this code did not
				// construct is refused here, not carried past it.
				return fmt.Errorf("postgres: deleted budget_rollover contribution %s has no source_budget_month", contributionID)
			}
			if err := q.ClearBudgetRollover(ctx, sqlcgen.ClearBudgetRolloverParams{
				HouseholdID: uuid(householdID),
				Month:       row.SourceBudgetMonth,
			}); err != nil {
				return translate(err, "clear budget rollover")
			}
		}
		return nil
	})
}

// ListContributions returns one goal's contributions, newest first, clamped
// to the same limit convention TransactionRepository.List already documents:
// limit <= 0 becomes defaultContributionLimit, and anything above
// maxContributionLimit is pulled back down to it.
func (r *GoalRepo) ListContributions(ctx context.Context, householdID, goalID string, limit int) ([]domain.GoalContribution, error) {
	rows, err := r.q.ListGoalContributions(ctx, sqlcgen.ListGoalContributionsParams{
		HouseholdID: uuid(householdID),
		GoalID:      uuid(goalID),
		RowLimit:    int32(clampContributionLimit(limit)),
	})
	if err != nil {
		return nil, translate(err, "list goal contributions")
	}
	out := make([]domain.GoalContribution, 0, len(rows))
	for _, row := range rows {
		c, err := toGoalContribution(row.ID, row.GoalID, row.HouseholdID, row.AmountMinor, row.Currency,
			row.OccurredOn, row.Note, row.Source, row.SourceBudgetMonth)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, nil
}

// MonthContributionTotals sums each unarchived goal's contributions inside
// one calendar month, excluding source = 'starting_balance' --
// MonthContributionTotals' own SQL comment (queries/goal.sql) explains why
// that exclusion is load-bearing.
func (r *GoalRepo) MonthContributionTotals(ctx context.Context, householdID string, month time.Time) ([]usecase.GoalMonthTotal, error) {
	rows, err := r.q.MonthContributionTotals(ctx, sqlcgen.MonthContributionTotalsParams{
		HouseholdID: uuid(householdID),
		Column2:     dateOnly(startOfMonth(month)),
	})
	if err != nil {
		return nil, translate(err, "month contribution totals")
	}
	out := make([]usecase.GoalMonthTotal, 0, len(rows))
	for _, row := range rows {
		out = append(out, usecase.GoalMonthTotal{GoalID: uuidToString(row.GoalID), AmountMinor: row.AmountMinor})
	}
	return out, nil
}

// ProgressByIDs implements usecase.GoalProgressReader, the one thing Vision
// needs from Goals. A goal id not found gets no entry, not an error: a
// deleted goal's measure renders as a label with no figure, not a broken
// page. GoalProgressByIDs' own SQL comment covers the household scoping and
// why archived_at isn't filtered. Percent is domain.GoalProgressPercent's
// own capped figure -- the same one GoalService.List uses, never a second
// formula.
func (r *GoalRepo) ProgressByIDs(ctx context.Context, householdID string, goalIDs []string) (map[string]usecase.GoalProgress, error) {
	if len(goalIDs) == 0 {
		return map[string]usecase.GoalProgress{}, nil
	}
	ids := make([]pgtype.UUID, 0, len(goalIDs))
	for _, id := range goalIDs {
		ids = append(ids, uuid(id))
	}
	rows, err := r.q.GoalProgressByIDs(ctx, sqlcgen.GoalProgressByIDsParams{
		HouseholdID: uuid(householdID),
		GoalIds:     ids,
	})
	if err != nil {
		return nil, translate(err, "goal progress by ids")
	}
	out := make(map[string]usecase.GoalProgress, len(rows))
	for _, row := range rows {
		id := uuidToString(row.ID)
		out[id] = usecase.GoalProgress{
			GoalID:  id,
			Name:    row.Name,
			Percent: domain.GoalProgressPercent(row.ContributedMinor, row.TargetAmountMinor),
		}
	}
	return out, nil
}

// defaultContributionLimit and maxContributionLimit are ListContributions'
// own copy of TransactionRepository.List's clamp (transaction_repo.go) --
// the port's own doc comment requires the identical rule and numbers rather
// than inventing a second convention.
const (
	defaultContributionLimit = 50
	maxContributionLimit     = 200
)

func clampContributionLimit(limit int) int {
	if limit <= 0 {
		return defaultContributionLimit
	}
	if limit > maxContributionLimit {
		return maxContributionLimit
	}
	return limit
}

// goalMoney builds a domain.Money from a goal's own currency column rather
// than a Money literal, so a goal never leaves this adapter without its own
// currency. A currency value this code did not construct -- corrupted data,
// or simply invalid before any ISO-code CHECK exists on goals.currency -- is
// refused here, fail-closed, rather than carried up as a Money the usecase
// layer would trust.
func goalMoney(minor int64, currency string) (domain.Money, error) {
	m, err := domain.NewMoney(minor, currency)
	if err != nil {
		return domain.Money{}, fmt.Errorf("postgres: goal money: %w", err)
	}
	return m, nil
}

// toGoal converts one goals row's columns into a domain.Goal. It takes plain
// fields rather than a generated row struct because CreateGoal, UpdateGoal
// and SetGoalArchived each return their own distinctly-named sqlc row type
// even though all three select the identical column list.
func toGoal(id, householdID pgtype.UUID, name string, targetMinor int64, currency string,
	targetMonth pgtype.Date, plannedMinor int64, archivedAt pgtype.Timestamptz) (domain.Goal, error) {
	target, err := goalMoney(targetMinor, currency)
	if err != nil {
		return domain.Goal{}, err
	}
	planned, err := goalMoney(plannedMinor, currency)
	if err != nil {
		return domain.Goal{}, err
	}
	return domain.Goal{
		ID:             uuidToString(id),
		HouseholdID:    uuidToString(householdID),
		Name:           name,
		Target:         target,
		TargetMonth:    dateToTimePtr(targetMonth),
		PlannedMonthly: planned,
		ArchivedAt:     timePtrOf(archivedAt),
	}, nil
}

// toGoalRecord is toGoal plus the contributed total ListGoalsWithTotals and
// GetGoalWithTotal both carry alongside the same goal columns.
func toGoalRecord(id, householdID pgtype.UUID, name string, targetMinor int64, currency string,
	targetMonth pgtype.Date, plannedMinor int64, archivedAt pgtype.Timestamptz, contributedMinor int64) (usecase.GoalRecord, error) {
	g, err := toGoal(id, householdID, name, targetMinor, currency, targetMonth, plannedMinor, archivedAt)
	if err != nil {
		return usecase.GoalRecord{}, err
	}
	return usecase.GoalRecord{Goal: g, ContributedMinor: contributedMinor}, nil
}

// toGoalContribution converts one goal_contributions row's columns (plus the
// currency joined in from its goal) into a domain.GoalContribution. source
// goes through domain.ParseContributionSource rather than a bare cast: a
// value this code did not construct is refused here, not carried up, the
// same rule toTransaction's own callers apply to transactions.kind.
func toGoalContribution(id, goalID, householdID pgtype.UUID, amountMinor int64, currency string,
	occurredOn pgtype.Date, note string, source string, sourceBudgetMonth pgtype.Date) (domain.GoalContribution, error) {
	amount, err := goalMoney(amountMinor, currency)
	if err != nil {
		return domain.GoalContribution{}, err
	}
	src, err := domain.ParseContributionSource(source)
	if err != nil {
		return domain.GoalContribution{}, fmt.Errorf("postgres: goal contribution: %w", err)
	}
	return domain.GoalContribution{
		ID:                uuidToString(id),
		GoalID:            uuidToString(goalID),
		HouseholdID:       uuidToString(householdID),
		Amount:            amount,
		OccurredOn:        dateToTime(occurredOn),
		Note:              note,
		Source:            src,
		SourceBudgetMonth: dateToTimePtr(sourceBudgetMonth),
	}, nil
}
