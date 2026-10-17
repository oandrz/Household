package postgres

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/andreasoentoro/hearth/api/internal/adapter/postgres/sqlcgen"
	"github.com/andreasoentoro/hearth/api/internal/domain"
	"github.com/andreasoentoro/hearth/api/internal/usecase"
)

var _ usecase.HouseholdUsage = (*HouseholdUsageRepo)(nil)

// HouseholdUsageRepo answers the per-household caps' counts, one query per
// kind, all in queries/usage.sql.
type HouseholdUsageRepo struct{ q *sqlcgen.Queries }

func NewHouseholdUsageRepo(db *DB) *HouseholdUsageRepo {
	return &HouseholdUsageRepo{q: sqlcgen.New(db.Pool())}
}

// Count refuses a kind it has no query for, rather than answering zero and
// letting the write through.
func (r *HouseholdUsageRepo) Count(ctx context.Context, householdID string, kind domain.LimitKind) (int, error) {
	query, ok := r.counters()[kind]
	if !ok {
		return 0, fmt.Errorf("count %q: no query for this kind", kind)
	}
	n, err := query(ctx, uuid(householdID))
	if err != nil {
		return 0, translate(err, "count "+string(kind))
	}
	return int(n), nil
}

func (r *HouseholdUsageRepo) counters() map[domain.LimitKind]func(context.Context, pgtype.UUID) (int64, error) {
	return map[domain.LimitKind]func(context.Context, pgtype.UUID) (int64, error){
		domain.LimitTransactions:       r.q.CountTransactions,
		domain.LimitAccounts:           r.q.CountAccounts,
		domain.LimitCategories:         r.q.CountCategories,
		domain.LimitGoals:              r.q.CountGoals,
		domain.LimitBills:              r.q.CountBills,
		domain.LimitHoldings:           r.q.CountHoldings,
		domain.LimitSpaces:             r.q.CountSpaces,
		domain.LimitAgreementSections:  r.q.CountAgreementSections,
		domain.LimitGoalContributions:  r.q.CountGoalContributions,
		domain.LimitBillPayments:       r.q.CountBillPayments,
		domain.LimitHoldingEvents:      r.q.CountHoldingEvents,
		domain.LimitHoldingValuations:  r.q.CountHoldingValuations,
		domain.LimitHoldingIncome:      r.q.CountHoldingIncome,
		domain.LimitRetroActions:       r.q.CountRetroActions,
		domain.LimitAgreementProposals: r.q.CountAgreementProposals,
		domain.LimitMembers:            r.q.CountMembers,
		domain.LimitPendingInvites:     r.q.CountPendingInvitesInHousehold,
		domain.LimitActiveAPITokens:    r.q.CountActiveAPITokens,
	}
}

func (r *HouseholdUsageRepo) TransactionsInMonth(ctx context.Context, householdID string, month time.Time) (int, error) {
	start := time.Date(month.Year(), month.Month(), 1, 0, 0, 0, 0, time.UTC)
	n, err := r.q.CountTransactionsInMonth(ctx, sqlcgen.CountTransactionsInMonthParams{
		HouseholdID:    uuid(householdID),
		MonthStart:     dateOnly(start),
		NextMonthStart: dateOnly(start.AddDate(0, 1, 0)),
	})
	if err != nil {
		return 0, translate(err, "count transactions in month")
	}
	return int(n), nil
}
