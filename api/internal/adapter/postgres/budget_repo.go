package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/andreasoentoro/hearth/api/internal/adapter/postgres/sqlcgen"
	"github.com/andreasoentoro/hearth/api/internal/domain"
	"github.com/andreasoentoro/hearth/api/internal/usecase"
)

// BudgetRepo keeps the pool alongside the pool-backed *sqlcgen.Queries, like
// InviteRepo and SignupRepo, because Upsert needs to begin its own
// transaction -- something a *sqlcgen.Queries built once at construction time
// cannot do on its own.
type BudgetRepo struct {
	q    *sqlcgen.Queries
	pool *pgxpool.Pool
}

func NewBudgetRepo(db *DB) *BudgetRepo {
	return &BudgetRepo{q: sqlcgen.New(db.Pool()), pool: db.Pool()}
}

func (r *BudgetRepo) Get(ctx context.Context, householdID string, month time.Time) (domain.Budget, error) {
	row, err := r.q.GetBudget(ctx, sqlcgen.GetBudgetParams{
		HouseholdID: uuid(householdID),
		Month:       dateOnly(startOfMonth(month)),
	})
	if err != nil {
		return domain.Budget{}, translate(err, "get budget")
	}

	lineRows, err := r.q.ListBudgetLines(ctx, row.ID)
	if err != nil {
		return domain.Budget{}, translate(err, "list budget lines")
	}

	return toBudget(row.ID, row.HouseholdID, row.Month, row.ExpectedIncomeMinor, row.PrimaryCurrency,
		toBudgetLines(lineRows, row.PrimaryCurrency),
		budgetRolloverStamp{row.RolledOverAt, row.RolloverGoalID, row.RolloverAmountMinor}), nil
}

// Upsert replaces one household-month's budget wholesale, inside one
// transaction: validate every line's category belongs to this household,
// upsert the parent row on (household_id, month), delete every existing
// line, then insert the new ones. Any failure -- including the category
// ownership check -- rolls the whole transaction back via pgx.BeginFunc, so
// a foreign-household category line can never leave the parent updated with
// its lines half-replaced (TestBudgetUpsertIsOneTransaction).
func (r *BudgetRepo) Upsert(ctx context.Context, b domain.Budget) (domain.Budget, error) {
	month := startOfMonth(b.Month)
	var result domain.Budget

	err := pgx.BeginFunc(ctx, r.pool, func(tx pgx.Tx) error {
		q := r.q.WithTx(tx)

		if err := validateLineCategories(ctx, q, b.HouseholdID, b.Lines); err != nil {
			return err
		}

		budgetRow, err := q.UpsertBudget(ctx, sqlcgen.UpsertBudgetParams{
			HouseholdID:         uuid(b.HouseholdID),
			Month:               dateOnly(month),
			ExpectedIncomeMinor: expectedIncomeMinor(b.ExpectedIncome),
		})
		if err != nil {
			return translate(err, "upsert budget")
		}

		// Full-replace, never merge (the port's own doc comment): every
		// existing line goes before any new one is written, in the same
		// transaction as the parent upsert above.
		if err := q.DeleteBudgetLines(ctx, budgetRow.ID); err != nil {
			return translate(err, "delete budget lines")
		}
		for _, line := range b.Lines {
			// translate, not a plain fmt.Errorf wrap: a caller-submitted
			// duplicate category id passes validateLineCategories (which
			// dedupes before counting) and only fails here, against
			// budget_lines' own UNIQUE (budget_id, category_id) -- by which
			// point DeleteBudgetLines has already run inside this same
			// transaction. Every statement in this transaction must return
			// through translate for the same reason UpsertBudget does: no
			// *pgconn.PgError may cross the adapter boundary, and this is
			// the one call site that can otherwise carry one all the way to
			// the usecase layer as a raw driver type.
			if err := q.InsertBudgetLine(ctx, sqlcgen.InsertBudgetLineParams{
				BudgetID:   budgetRow.ID,
				CategoryID: uuid(line.CategoryID),
				CapMinor:   line.Cap.Amount,
			}); err != nil {
				return translate(err, "insert budget line")
			}
		}

		// Read inside the same transaction the caps and expected income were
		// just written in, so the Budget this method returns can never carry
		// a currency the household didn't actually have at write time.
		currency, err := q.GetHouseholdPrimaryCurrency(ctx, uuid(b.HouseholdID))
		if err != nil {
			return translate(err, "get household primary currency")
		}

		// RolloverAmountMinor is deliberately nil here, not read off a second
		// query: UpsertBudget's RETURNING has no join to goal_contributions
		// (see budgetRolloverStamp's own comment), and the domain.Budget PUT
		// hands back is never read for it -- putBudgetResponse's budgetDTO
		// carries no rollover fields at all, unlike the GET response's
		// top-level ones.
		result = toBudget(budgetRow.ID, budgetRow.HouseholdID, budgetRow.Month, budgetRow.ExpectedIncomeMinor,
			currency, reCurrency(b.Lines, currency),
			budgetRolloverStamp{budgetRow.RolledOverAt, budgetRow.RolloverGoalID, nil})
		return nil
	})
	if err != nil {
		return domain.Budget{}, err
	}
	return result, nil
}

// History returns the household's budgets for the closed months walked back
// `months` from the viewed month, plus the viewed month itself if budgeted --
// newest first, absent months simply missing. ListBudgetsInRange's own
// comment explains why an inclusive [from, month] range over existing rows
// gives exactly that shape without any per-month presence check in Go: a
// month with no row never appears in the result at all.
func (r *BudgetRepo) History(ctx context.Context, householdID string, month time.Time, months int) ([]domain.Budget, error) {
	viewed := startOfMonth(month)
	from := viewed.AddDate(0, -months, 0)

	rows, err := r.q.ListBudgetsInRange(ctx, sqlcgen.ListBudgetsInRangeParams{
		HouseholdID: uuid(householdID),
		FromMonth:   dateOnly(from),
		ToMonth:     dateOnly(viewed),
	})
	if err != nil {
		return nil, translate(err, "list budgets in range")
	}
	if len(rows) == 0 {
		return []domain.Budget{}, nil
	}

	budgetIDs := make([]pgtype.UUID, len(rows))
	for i, row := range rows {
		budgetIDs[i] = row.ID
	}
	lineRows, err := r.q.ListBudgetLinesForBudgets(ctx, budgetIDs)
	if err != nil {
		return nil, translate(err, "list budget lines for budgets")
	}
	linesByBudget := make(map[pgtype.UUID][]domain.BudgetLine, len(rows))
	for _, lineRow := range lineRows {
		primaryCurrency := currencyOf(rows, lineRow.BudgetID)
		linesByBudget[lineRow.BudgetID] = append(linesByBudget[lineRow.BudgetID], domain.BudgetLine{
			CategoryID: uuidToString(lineRow.CategoryID),
			Cap:        domain.Money{Amount: lineRow.CapMinor, Currency: primaryCurrency},
		})
	}

	out := make([]domain.Budget, 0, len(rows))
	for _, row := range rows {
		// RolloverAmountMinor is nil here for the same reason Upsert's own
		// call site is: ListBudgetsInRange has no join to goal_contributions,
		// and budgetHistoryMonthDTO carries no rollover fields at all for
		// History's caller to read it from.
		out = append(out, toBudget(row.ID, row.HouseholdID, row.Month, row.ExpectedIncomeMinor, row.PrimaryCurrency,
			linesByBudget[row.ID], budgetRolloverStamp{row.RolledOverAt, row.RolloverGoalID, nil}))
	}
	return out, nil
}

// RollOverToGoal writes a budget month's unspent money into a goal as one
// contribution and stamps the month, in ONE transaction -- both statements or
// neither (usecase.BudgetRepository's own doc comment).
//
// in.Month is normalised with startOfMonth/dateOnly, the same pair Get and
// Upsert already use for budgets.month, before it is used anywhere in this
// method -- including as the value written into source_budget_month. That
// normalisation is load-bearing beyond this method: GoalRepo.DeleteContribution's
// ClearBudgetRollover matches budgets on the exact source_budget_month value
// read back off the deleted contribution row, so writing anything other than
// the first-of-month here would make that later clear match zero rows and
// silently strand the stamp.
//
// The stamp itself is a conditional UPDATE (StampBudgetRollover, WHERE
// rolled_over_at IS NULL). Zero rows updated is ambiguous by itself -- the
// month may never have been budgeted, or it may already be stamped -- so
// diagnoseUnstampedRollover below issues one follow-up SELECT inside this
// same transaction to tell the two apart, rather than guessing.
//
// A 23505 on goal_contributions' partial unique index
// (goal_contributions_one_rollover_per_month) also maps to
// domain.ErrRolloverAlreadyDone via translate's constraint-name check -- the
// belt-and-braces the index's own migration comment describes, so a
// concurrent pair that somehow both reach the INSERT cannot surface as an
// unmapped 500.
func (r *BudgetRepo) RollOverToGoal(ctx context.Context, in usecase.RollOverToGoalInput) (domain.GoalContribution, error) {
	month := dateOnly(startOfMonth(in.Month))
	var result domain.GoalContribution

	err := pgx.BeginFunc(ctx, r.pool, func(tx pgx.Tx) error {
		q := r.q.WithTx(tx)

		_, err := q.StampBudgetRollover(ctx, sqlcgen.StampBudgetRolloverParams{
			HouseholdID:    uuid(in.HouseholdID),
			Month:          month,
			RolloverGoalID: uuid(in.GoalID),
		})
		if err != nil {
			if !errors.Is(err, pgx.ErrNoRows) {
				return translate(err, "stamp budget rollover")
			}
			return diagnoseUnstampedRollover(ctx, q, in.HouseholdID, month)
		}

		row, err := q.InsertGoalContribution(ctx, sqlcgen.InsertGoalContributionParams{
			GoalID:      uuid(in.GoalID),
			HouseholdID: uuid(in.HouseholdID),
			AmountMinor: in.Amount.Amount,
			OccurredOn:  dateOnly(in.OccurredOn),
			// note stays empty: the design's "From July's unspent budget" copy
			// is composed in the frontend from source + sourceBudgetMonth, not
			// written here (RollOverToGoalInput's own doc comment).
			Note:              "",
			Source:            string(domain.ContributionBudgetRollover),
			SourceBudgetMonth: month,
		})
		if err != nil {
			return translate(err, "insert budget rollover contribution")
		}

		contribution, cerr := toGoalContribution(row.ID, row.GoalID, row.HouseholdID, row.AmountMinor,
			in.Amount.Currency, row.OccurredOn, row.Note, row.Source, row.SourceBudgetMonth)
		if cerr != nil {
			return cerr
		}
		result = contribution
		return nil
	})
	if err != nil {
		return domain.GoalContribution{}, err
	}
	return result, nil
}

// diagnoseUnstampedRollover runs when StampBudgetRollover's conditional
// UPDATE matches zero rows, which is ambiguous by itself: the month may never
// have been budgeted at all, or it may already be stamped by an earlier
// rollover. It reads the row back inside the SAME transaction to tell the two
// apart, per usecase.BudgetRepository.RollOverToGoal's own doc comment --
// a second, separate transaction here could race a concurrent Upsert or
// rollover and read a different answer than the UPDATE above just saw.
func diagnoseUnstampedRollover(ctx context.Context, q *sqlcgen.Queries, householdID string, month pgtype.Date) error {
	stamp, err := q.GetBudgetRolloverStamp(ctx, sqlcgen.GetBudgetRolloverStampParams{
		HouseholdID: uuid(householdID),
		Month:       month,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.ErrNotFound
		}
		return translate(err, "get budget rollover stamp")
	}
	if !stamp.Valid {
		// StampBudgetRollover's own WHERE clause requires rolled_over_at IS
		// NULL for it to have updated a row, so matching zero rows against an
		// existing budget row should mean rolled_over_at is NOT NULL. Reading
		// NULL back here anyway is a state this code did not construct --
		// fail loud rather than carry an ambiguous result upward as either
		// sentinel.
		return fmt.Errorf("postgres: budget for household %s matched no rows on stamp but rolled_over_at reads NULL", householdID)
	}
	return domain.ErrRolloverAlreadyDone
}

// currencyOf is ListBudgetLinesForBudgets' rows losing their household's
// primary currency, which only ListBudgetsInRange's rows carry -- every
// budget in one History call is the same household, so every row's currency
// agrees, but the line rows have to look it up by budget id regardless
// because that is the only field they share with the budget rows.
func currencyOf(budgets []sqlcgen.ListBudgetsInRangeRow, budgetID pgtype.UUID) string {
	for _, b := range budgets {
		if b.ID == budgetID {
			return b.PrimaryCurrency
		}
	}
	return ""
}

// validateLineCategories refuses an Upsert whose lines include a category
// that is not this household's -- including a category that belongs to
// another household outright, the case a foreign-key check alone cannot
// catch, because the FK only proves the row exists somewhere. Deduplicating
// before counting means a caller-supplied duplicate category id (itself
// invalid: budget_lines' own UNIQUE (budget_id, category_id) would refuse it
// at insert time) can never make a legitimate household look short a
// category it does own.
func validateLineCategories(ctx context.Context, q *sqlcgen.Queries, householdID string, lines []domain.BudgetLine) error {
	if len(lines) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(lines))
	ids := make([]pgtype.UUID, 0, len(lines))
	for _, line := range lines {
		if _, ok := seen[line.CategoryID]; ok {
			continue
		}
		seen[line.CategoryID] = struct{}{}
		ids = append(ids, uuid(line.CategoryID))
	}

	count, err := q.CountCategoriesInHousehold(ctx, sqlcgen.CountCategoriesInHouseholdParams{
		HouseholdID: uuid(householdID),
		CategoryIds: ids,
	})
	if err != nil {
		return translate(err, "count budget line categories")
	}
	if int(count) != len(ids) {
		// Wrapped, not translate()'d: this is an application-level check
		// against a plain SELECT count, not a Postgres error code, so there
		// is nothing for translate's pgconn.PgError switch to match. The
		// wrap is what lets the HTTP layer's MapDomainError recognise this
		// with errors.Is instead of falling through to an unmapped 500 --
		// see domain.ErrBudgetCategoryUnknown's own doc comment.
		return fmt.Errorf("postgres: household %s: %w", householdID, domain.ErrBudgetCategoryUnknown)
	}
	return nil
}

// expectedIncomeMinor implements the nil <-> SQL NULL convention for
// ExpectedIncome: nil means "chose not to say" and must reach the database as
// NULL, never as a stored zero -- zero is a claim the household never made
// (migrations/00006_budgets.sql's own comment).
func expectedIncomeMinor(m *domain.Money) *int64 {
	if m == nil {
		return nil
	}
	amount := m.Amount
	return &amount
}

// reCurrency rebuilds a Budget's lines with the currency actually read from
// the household inside the same transaction, rather than trusting whatever
// currency the caller's domain.Money happened to carry -- caps have no
// currency column of their own (see UpsertBudget's comment), so the value
// this method returns must always be the household's, not the caller's.
func reCurrency(lines []domain.BudgetLine, currency string) []domain.BudgetLine {
	out := make([]domain.BudgetLine, len(lines))
	for i, line := range lines {
		out[i] = domain.BudgetLine{
			CategoryID: line.CategoryID,
			Cap:        domain.Money{Amount: line.Cap.Amount, Currency: currency},
		}
	}
	return out
}

// toBudgetLines converts ListBudgetLines' rows -- which carry no currency
// column of their own -- into domain.BudgetLine using the household's
// primary currency read alongside them.
func toBudgetLines(rows []sqlcgen.ListBudgetLinesRow, currency string) []domain.BudgetLine {
	out := make([]domain.BudgetLine, len(rows))
	for i, row := range rows {
		out[i] = domain.BudgetLine{
			CategoryID: uuidToString(row.CategoryID),
			Cap:        domain.Money{Amount: row.CapMinor, Currency: currency},
		}
	}
	return out
}

// budgetRolloverStamp bundles rolled_over_at and rollover_goal_id, the two
// columns 00007_goals.sql added to budgets and its rollover_stamp_is_whole
// CHECK constraint keeps in lockstep at the schema level -- both NULL or
// both set, never one without the other. Passing them into toBudget as one
// value, rather than as two more positional params, is what keeps a caller
// from ever being able to wire one half to a different row's other half.
//
// RolloverAmountMinor rides along in the same struct for convenience, but it
// is NOT the same guarantee: it comes from goal_contributions, a different
// table, reached only by GetBudget's own LEFT JOIN -- there is no CHECK
// constraint tying it to the two columns above, and Upsert/History's own
// call sites below pass it as nil on purpose (see their comments). Only
// Get's call site ever has a real value to pass.
type budgetRolloverStamp struct {
	RolledOverAt        pgtype.Timestamptz
	RolloverGoalID      pgtype.UUID
	RolloverAmountMinor *int64
}

func toBudget(id, householdID pgtype.UUID, month pgtype.Date, expectedIncomeMinor *int64, currency string,
	lines []domain.BudgetLine, stamp budgetRolloverStamp) domain.Budget {
	b := domain.Budget{
		ID:                  uuidToString(id),
		HouseholdID:         uuidToString(householdID),
		Month:               dateToTime(month),
		Lines:               lines,
		RolledOverAt:        timePtrOf(stamp.RolledOverAt),
		RolloverGoalID:      optionalIDToString(stamp.RolloverGoalID),
		RolloverAmountMinor: stamp.RolloverAmountMinor,
	}
	if expectedIncomeMinor != nil {
		b.ExpectedIncome = &domain.Money{Amount: *expectedIncomeMinor, Currency: currency}
	}
	return b
}
