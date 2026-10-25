package postgres

import (
	"context"
	"time"

	"github.com/andreasoentoro/hearth/api/internal/adapter/postgres/sqlcgen"
	"github.com/andreasoentoro/hearth/api/internal/usecase"
)

// CashflowConfirmationRepo stores the months a household has confirmed on the
// cashflow page. It implements usecase.CashflowConfirmationRepository.
type CashflowConfirmationRepo struct {
	q *sqlcgen.Queries
}

func NewCashflowConfirmationRepo(db *DB) *CashflowConfirmationRepo {
	return &CashflowConfirmationRepo{q: sqlcgen.New(db.Pool())}
}

func (r *CashflowConfirmationRepo) List(ctx context.Context, householdID string) ([]usecase.CashflowConfirmation, error) {
	rows, err := r.q.ListCashflowConfirmations(ctx, uuid(householdID))
	if err != nil {
		return nil, translate(err, "list cashflow confirmations")
	}
	out := make([]usecase.CashflowConfirmation, 0, len(rows))
	for _, row := range rows {
		out = append(out, usecase.CashflowConfirmation{
			Month:           dateToTime(row.Month),
			ConfirmedByName: row.ConfirmedByName,
			ConfirmedAt:     timeOf(row.ConfirmedAt),
		})
	}
	return out, nil
}

func (r *CashflowConfirmationRepo) Confirm(ctx context.Context, householdID string, month time.Time, membershipID string, at time.Time) (usecase.CashflowConfirmation, error) {
	row, err := r.q.ConfirmCashflowMonth(ctx, sqlcgen.ConfirmCashflowMonthParams{
		HouseholdID:             uuid(householdID),
		Month:                   dateOnly(month),
		ConfirmedByMembershipID: uuid(membershipID),
		ConfirmedAt:             timestamptz(at),
	})
	if err != nil {
		return usecase.CashflowConfirmation{}, translate(err, "confirm cashflow month")
	}
	return usecase.CashflowConfirmation{
		Month:           dateToTime(row.Month),
		ConfirmedByName: row.ConfirmedByName,
		ConfirmedAt:     timeOf(row.ConfirmedAt),
	}, nil
}

// Unconfirm deletes the mark. DELETE of no rows is not an error, which is the
// port's contract.
func (r *CashflowConfirmationRepo) Unconfirm(ctx context.Context, householdID string, month time.Time) error {
	err := r.q.UnconfirmCashflowMonth(ctx, sqlcgen.UnconfirmCashflowMonthParams{
		HouseholdID: uuid(householdID),
		Month:       dateOnly(month),
	})
	return translate(err, "unconfirm cashflow month")
}

var _ usecase.CashflowConfirmationRepository = (*CashflowConfirmationRepo)(nil)
