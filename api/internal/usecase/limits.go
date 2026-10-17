package usecase

import (
	"context"
	"time"

	"github.com/andreasoentoro/hearth/api/internal/domain"
)

// refuseAtCap answers *domain.LimitReachedError when the household already
// holds kind's cap, and passes a count error through: a count that cannot
// be read is not room to write.
//
// Count, then write, is not atomic: two saves in the same instant can both
// see one place left and both land, leaving a household a few rows over.
// Accepted rather than locked: the overshoot is bounded by the requests in
// flight at that moment, and the cap exists to bound disk use, not to be an
// exact count (ADR 13).
func refuseAtCap(ctx context.Context, usage HouseholdUsage, householdID string, kind domain.LimitKind) error {
	n, err := usage.Count(ctx, householdID, kind)
	if err != nil {
		return err
	}
	if limit := kind.Max(); n >= limit {
		return &domain.LimitReachedError{Kind: kind, Limit: limit}
	}
	return nil
}

// refuseFullMonth is refuseAtCap for the month day falls in.
func refuseFullMonth(ctx context.Context, usage HouseholdUsage, householdID string, day time.Time) error {
	month := startOfMonth(day)
	n, err := usage.TransactionsInMonth(ctx, householdID, month)
	if err != nil {
		return err
	}
	if n >= domain.MaxTransactionsPerMonth {
		return &domain.LimitReachedError{Kind: domain.LimitTransactionsInMonth, Limit: domain.MaxTransactionsPerMonth, Month: month}
	}
	return nil
}

// refuseFullLedger is both transaction caps, for every write that adds a
// ledger row: a transaction, and a bill payment, which writes one too.
func refuseFullLedger(ctx context.Context, usage HouseholdUsage, householdID string, day time.Time) error {
	if err := refuseAtCap(ctx, usage, householdID, domain.LimitTransactions); err != nil {
		return err
	}
	return refuseFullMonth(ctx, usage, householdID, day)
}
