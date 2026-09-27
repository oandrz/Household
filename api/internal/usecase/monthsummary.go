package usecase

import (
	"context"
	"time"

	"github.com/andreasoentoro/hearth/api/internal/domain"
)

// ExcludedTransaction names one transaction left out of the month's spend
// because no rate was available, so the screen can say which currency. It's
// explicit rather than frontend-inferred: a quietly short total looks
// identical to a correct one.
type ExcludedTransaction struct {
	TransactionID string
	Currency      string
}

// MonthSummary is the two figures the Transactions screen shows above the
// ledger.
//
// Count is "247 in July" -- every transaction in the month, all three
// kinds, because it counts what the ledger below it shows. Spent is "Spent
// this month S$3,420.18" -- expenses only: income isn't spending, and a
// transfer is the same money arriving somewhere else.
type MonthSummary struct {
	Currency       string
	Month          time.Time
	Count          int
	Spent          domain.Money
	ExcludedNoRate []ExcludedTransaction
}

// MonthSummary composes both figures from one read of the month.
//
// domain.Money.Add refuses to add different currencies, so each expense
// converts to the primary currency *first*, then sums -- otherwise it fails
// on the second transaction of a mixed-currency household (LEARNING pattern
// 12). Rounding is per transaction, half away from zero, never re-rounded,
// so the total is deterministic.
//
// A transaction before its account's opening balance still counts in Spent:
// the money was spent, only the *balance* ignores it, since a balance is
// anchored to an asserted figure and spend is not. Don't "simplify" this
// split away.
func (s *TransactionService) MonthSummary(ctx context.Context, householdID string, month time.Time) (MonthSummary, error) {
	household, err := s.d.Households.Get(ctx, householdID)
	if err != nil {
		return MonthSummary{}, err
	}
	primary := household.PrimaryCurrency

	zero, err := domain.NewMoney(0, primary)
	if err != nil {
		return MonthSummary{}, err
	}

	views, err := s.d.Transactions.MonthTotals(ctx, householdID, month)
	if err != nil {
		return MonthSummary{}, err
	}

	summary := MonthSummary{
		Currency: primary,
		Month:    month,
		Count:    len(views),
		Spent:    zero,
	}

	conv := NewConverter(s.d.FX, primary)
	for _, view := range views {
		if view.Transaction.Kind != domain.TransactionExpense {
			continue
		}
		inPrimary, hasRate, err := conv.TryConvert(ctx, view.Transaction.Amount)
		if err != nil {
			return MonthSummary{}, err
		}
		if !hasRate {
			summary.ExcludedNoRate = append(summary.ExcludedNoRate, ExcludedTransaction{
				TransactionID: view.Transaction.ID,
				Currency:      view.Transaction.Amount.Currency,
			})
			continue
		}
		summary.Spent, err = summary.Spent.Add(inPrimary)
		if err != nil {
			return MonthSummary{}, err
		}
	}
	return summary, nil
}
