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

// BudgetRepo keeps the pool alongside *sqlcgen.Queries, like InviteRepo and
// SignupRepo, because Upsert needs to begin its own transaction that a
// Queries built once at construction time cannot start on its own.
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
			// dedupes first) but fails here against budget_lines' UNIQUE
			// (budget_id, category_id), after DeleteBudgetLines has already
			// run in this transaction. Every statement here must go through
			// translate so no *pgconn.PgError crosses the adapter boundary --
			// this is the one call site that could otherwise leak one to the
			// usecase layer.
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
		// (see budgetRolloverStamp's own comment), and putBudgetResponse's
		// budgetDTO carries no rollover fields for the caller to read it
		// from anyway.
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

// History returns the household's budgets for the closed months walked
// back `months` from the viewed month, plus the viewed month itself if
// budgeted -- newest first, with absent months simply missing. See
// ListBudgetsInRange's comment for why the inclusive range needs no
// per-month presence check in Go.
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
		// RolloverAmountMinor is nil here for the same reason as Upsert's call
		// site: ListBudgetsInRange has no join to goal_contributions, and
		// budgetHistoryMonthDTO carries no rollover fields to read it from.
		out = append(out, toBudget(row.ID, row.HouseholdID, row.Month, row.ExpectedIncomeMinor, row.PrimaryCurrency,
			linesByBudget[row.ID], budgetRolloverStamp{row.RolledOverAt, row.RolloverGoalID, nil}))
	}
	return out, nil
}

// RollOverToGoal writes a budget month's unspent money into a goal as one
// contribution and stamps the month, in ONE transaction: both statements or
// neither (usecase.BudgetRepository's own doc comment).
//
// in.Month is normalised with startOfMonth/dateOnly before use anywhere
// here, including source_budget_month: GoalRepo.DeleteContribution's
// ClearBudgetRollover later matches budgets on that exact value, so
// anything but the first-of-month would silently strand the stamp.
//
// The stamp is a conditional UPDATE (StampBudgetRollover, WHERE
// rolled_over_at IS NULL). Zero rows updated is ambiguous -- never
// budgeted, or already stamped -- so diagnoseUnstampedRollover issues one
// follow-up SELECT in the same transaction to tell the two apart.
//
// A 23505 on the partial unique index
// goal_contributions_one_rollover_per_month maps to
// domain.ErrRolloverAlreadyDone via translate, so a concurrent pair racing
// the INSERT cannot surface as an unmapped 500.
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
// UPDATE matches zero rows -- ambiguous by itself, since the month may
// never have been budgeted or may already be stamped. It reads the row
// back in the SAME transaction: a separate one could race a concurrent
// Upsert or rollover and read a different answer than the UPDATE just saw.
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
		// StampBudgetRollover's WHERE clause requires rolled_over_at IS NULL
		// to match a row, so zero rows matched against an existing budget
		// should mean rolled_over_at is NOT NULL. Reading NULL back anyway is
		// a state this code did not construct, so it fails loud instead of
		// guessing.
		return fmt.Errorf("postgres: budget for household %s matched no rows on stamp but rolled_over_at reads NULL", householdID)
	}
	return domain.ErrRolloverAlreadyDone
}

// currencyOf looks up a budget's primary currency for
// ListBudgetLinesForBudgets' rows, which don't carry it -- only
// ListBudgetsInRange's rows do. Every budget in one History call shares the
// same household and currency, but budget id is the only field the two row
// types share, so the lookup still goes through it.
func currencyOf(budgets []sqlcgen.ListBudgetsInRangeRow, budgetID pgtype.UUID) string {
	for _, b := range budgets {
		if b.ID == budgetID {
			return b.PrimaryCurrency
		}
	}
	return ""
}

// validateLineCategories refuses an Upsert whose lines include a category
// that is not this household's -- unknown, or another household's
// outright, the case a foreign-key check alone can't catch, since the FK
// only proves the row exists somewhere. Deduplicating before counting
// stops a caller-supplied duplicate id (itself invalid: budget_lines'
// UNIQUE (budget_id, category_id) would refuse it at insert) from making a
// legitimate household look short a category it does own.
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
		// against a plain SELECT count, not a Postgres error code, so
		// there's nothing for translate's pgconn.PgError switch to match.
		// The wrap lets the HTTP layer's MapDomainError recognise it via
		// errors.Is instead of falling through to an unmapped 500.
		return fmt.Errorf("postgres: household %s: %w", householdID, domain.ErrBudgetCategoryUnknown)
	}
	return nil
}

// expectedIncomeMinor implements the nil <-> SQL NULL convention for
// ExpectedIncome: nil means "chose not to say" and must reach the database
// as NULL, never a stored zero -- zero is a claim the household never made.
func expectedIncomeMinor(m *domain.Money) *int64 {
	if m == nil {
		return nil
	}
	amount := m.Amount
	return &amount
}

// reCurrency rebuilds a Budget's lines with the currency read from the
// household inside the same transaction, rather than trusting whatever
// currency the caller's domain.Money happened to carry: caps have no
// currency column of their own, so the returned value must always be the
// household's.
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
// columns 00007_goals.sql added to budgets and kept in lockstep by its
// rollover_stamp_is_whole CHECK constraint -- both NULL or both set.
// Passing them into toBudget as one value keeps a caller from wiring one
// half to a different row's other half.
//
// RolloverAmountMinor rides along for convenience but is not the same
// guarantee: it comes from goal_contributions via GetBudget's LEFT JOIN,
// with no CHECK tying it to the other two. Upsert and History pass it as
// nil on purpose; only Get has a real value.
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
