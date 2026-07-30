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
		toBudgetLines(lineRows, row.PrimaryCurrency)), nil
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

		result = toBudget(budgetRow.ID, budgetRow.HouseholdID, budgetRow.Month, budgetRow.ExpectedIncomeMinor,
			currency, reCurrency(b.Lines, currency))
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
		out = append(out, toBudget(row.ID, row.HouseholdID, row.Month, row.ExpectedIncomeMinor, row.PrimaryCurrency,
			linesByBudget[row.ID]))
	}
	return out, nil
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
		return fmt.Errorf("postgres: a budget line's category does not belong to household %s", householdID)
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

func toBudget(id, householdID pgtype.UUID, month pgtype.Date, expectedIncomeMinor *int64, currency string,
	lines []domain.BudgetLine) domain.Budget {
	b := domain.Budget{
		ID:          uuidToString(id),
		HouseholdID: uuidToString(householdID),
		Month:       dateToTime(month),
		Lines:       lines,
	}
	if expectedIncomeMinor != nil {
		b.ExpectedIncome = &domain.Money{Amount: *expectedIncomeMinor, Currency: currency}
	}
	return b
}
