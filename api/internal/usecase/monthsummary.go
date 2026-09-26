package usecase

import (
	"context"
	"time"

	"github.com/andreasoentoro/hearth/api/internal/domain"
)

// ExcludedTransaction names one transaction left out of the month's spend
// because no rate was available, so the screen can say which currency. An
// explicit field rather than something the frontend infers by comparing the
// list to the total: a total that is quietly short looks identical to a
// correct one, which is the failure worth preventing.
type ExcludedTransaction struct {
	TransactionID string
	Currency      string
}

// MonthSummary is the two figures the Transactions screen shows above the
// ledger. Both were undefined before this feature; they are pinned here.
//
// Count is "247 in July" -- every transaction in the month, all three kinds,
// because it counts what the ledger below it is showing.
//
// Spent is "Spent this month S$3,420.18" -- expenses only. Income is not
// spending, and a transfer is the same money arriving somewhere else;
// counting either would tell a household it spent money it still has.
type MonthSummary struct {
	Currency       string
	Month          time.Time
	Count          int
	Spent          domain.Money
	ExcludedNoRate []ExcludedTransaction
}

// MonthSummary composes both figures from one read of the month.
//
// The order of operations is not incidental: domain.Money.Add refuses to add
// two different currencies, deliberately, so each expense is converted into
// the household's primary currency *first* and only then summed. Summing
// first and converting after fails on the second transaction of a
// mixed-currency household -- docs/LEARNING.md pattern 12, and the same order
// AccountService.Summary uses for the same reason. Rounding therefore happens
// per transaction (half away from zero, as Rate.Apply already does) and the
// total is never re-rounded, so the figure is deterministic.
//
// A transaction dated before its account's opening balance still counts here.
// The money was spent; only the account's *balance* ignores it, because a
// balance is anchored to a figure someone asserted was true on a date and
// spend is not. See the spec's decision 6 -- this split is the thing most
// likely to get "simplified" later.
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
