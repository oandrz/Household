package postgres

import (
	"context"
	"fmt"

	"github.com/andreasoentoro/hearth/api/internal/adapter/postgres/sqlcgen"
	"github.com/andreasoentoro/hearth/api/internal/domain"
	"github.com/andreasoentoro/hearth/api/internal/usecase"
)

// CashflowRowRepo reads a household's whole transaction history for the
// cashflow page. It implements usecase.CashflowRowReader.
type CashflowRowRepo struct {
	q *sqlcgen.Queries
}

func NewCashflowRowRepo(db *DB) *CashflowRowRepo {
	return &CashflowRowRepo{q: sqlcgen.New(db.Pool())}
}

func (r *CashflowRowRepo) Rows(ctx context.Context, householdID string) ([]usecase.CashflowRow, error) {
	rows, err := r.q.ListCashflowRows(ctx, uuid(householdID))
	if err != nil {
		return nil, translate(err, "list cashflow rows")
	}
	out := make([]usecase.CashflowRow, 0, len(rows))
	seen := make(map[string]struct{}, len(rows))
	for _, row := range rows {
		id := uuidToString(row.ID)
		// Two bill payments linked to one expense would make the join return
		// it twice, and the service would then count the payment twice.
		if _, dup := seen[id]; dup {
			return nil, fmt.Errorf("cashflow rows: transaction %s appears more than once", id)
		}
		seen[id] = struct{}{}

		parsed, err := parseCashflowRow(row)
		if err != nil {
			return nil, fmt.Errorf("cashflow rows: transaction %s: %w", id, err)
		}
		parsed.TransactionID = id
		out = append(out, parsed)
	}
	return out, nil
}

func parseCashflowRow(row sqlcgen.ListCashflowRowsRow) (usecase.CashflowRow, error) {
	kind, err := domain.ParseTransactionKind(row.Kind)
	if err != nil {
		return usecase.CashflowRow{}, err
	}
	cashflowRow := usecase.CashflowRow{
		OccurredOn: dateToTime(row.OccurredOn),
		Kind:       kind,
		Amount:     domain.Money{Amount: row.AmountMinor, Currency: row.AmountCurrency},
		BillName:   stringOrEmpty(row.BillName),
	}
	// Only a transfer reports its destination. An income row has a
	// to-account too, but its type means nothing to the classification.
	if kind == domain.TransactionTransfer {
		if row.ToAccountType == nil {
			return usecase.CashflowRow{}, fmt.Errorf("transfer has no destination account type")
		}
		cashflowRow.ToAccountType, err = domain.ParseAccountType(*row.ToAccountType)
		if err != nil {
			return usecase.CashflowRow{}, err
		}
		cashflowRow.ToAccountName = stringOrEmpty(row.ToAccountName)
	}
	if row.BillCadence != nil {
		cashflowRow.BillCadence, err = domain.ParseCadence(*row.BillCadence)
		if err != nil {
			return usecase.CashflowRow{}, err
		}
	}
	return cashflowRow, nil
}

var _ usecase.CashflowRowReader = (*CashflowRowRepo)(nil)
