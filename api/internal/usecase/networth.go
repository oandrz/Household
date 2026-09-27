package usecase

import (
	"context"
	"time"

	"github.com/andreasoentoro/hearth/api/internal/domain"
)

// BreakdownEntry is one bar of the assets-and-liabilities chart. Totals are
// unsigned sums of what that type holds or owes -- the chart draws debts below
// the line from Type.IsLiability(), rather than from a negative number.
type BreakdownEntry struct {
	Type  domain.AccountType
	Total domain.Money
}

// ExcludedAccount names one account that could not be converted into the
// household's primary currency, so the screen can say which and why. It's
// explicit rather than frontend-inferred, since a limited member's response
// carries no amounts to infer from.
type ExcludedAccount struct {
	AccountID string
	Currency  string
}

// NetWorthSummary is everything the Finances screen shows above the accounts
// list.
//
// Computable is false only when accounts exist but none could convert (e.g.
// after a primary-currency change fx.StaticProvider can't price). Never show
// zero for that case -- zero is a claim about the household's money, not "we
// can't compute it." No accounts at all is still computable, genuinely zero.
type NetWorthSummary struct {
	Currency         string
	NetWorth         domain.Money
	Assets           domain.Money
	Liabilities      domain.Money
	Breakdown        []BreakdownEntry
	ExcludedNoRate   []ExcludedAccount
	ExcludedByChoice int
	Computable       bool
	// Trend is the twelve-month series, nil when there is nothing to chart --
	// an incomputable summary, or a household with no counted accounts.
	Trend *NetWorthTrend
}

// Summary composes the figures above the accounts list from views the
// caller already listed: the handler needs both halves of one response
// describing the same rows.
//
// domain.Money.Add refuses mixed currencies, so each account converts to the
// primary currency *first*, then sums -- summing first fails on the second
// account of a mixed-currency household. Rounding is per account, half away
// from zero, never re-rounded, so the total is deterministic.
//
// today is a parameter rather than a clock read here, so it's deterministic
// in tests and read once, at the HTTP layer.
func (s *AccountService) Summary(ctx context.Context, householdID string, views []AccountView, today time.Time) (NetWorthSummary, error) {
	household, err := s.d.Households.Get(ctx, householdID)
	if err != nil {
		return NetWorthSummary{}, err
	}
	primary := household.PrimaryCurrency
	conv := NewConverter(s.d.FX, primary)

	zero, err := domain.NewMoney(0, primary)
	if err != nil {
		return NetWorthSummary{}, err
	}

	summary := NetWorthSummary{
		Currency:    primary,
		NetWorth:    zero,
		Assets:      zero,
		Liabilities: zero,
		Computable:  true,
	}

	byType := map[domain.AccountType]domain.Money{}
	// considered counts every non-archived view; converted counts how many of
	// those actually converted. Computable is judged against considered, not
	// len(views), so a household whose only accounts are archived reads as a
	// genuine, computable zero -- not "cannot compute."
	considered := 0
	converted := 0
	counted := make([]trendAccount, 0, len(views))

	for _, view := range views {
		if view.Account.IsArchived() {
			continue
		}
		considered++

		inPrimary, hasRate, err := conv.TryConvert(ctx, view.Balance)
		if err != nil {
			return NetWorthSummary{}, err
		}
		if !hasRate {
			summary.ExcludedNoRate = append(summary.ExcludedNoRate, ExcludedAccount{
				AccountID: view.Account.ID,
				Currency:  view.Balance.Currency,
			})
			continue
		}
		converted++

		// The breakdown covers every convertible account, counted or not: the
		// toggle's copy is "Include this balance in the family total", and the
		// total is what it governs.
		running, ok := byType[view.Account.Type]
		if !ok {
			running = zero
		}
		running, err = running.Add(inPrimary)
		if err != nil {
			return NetWorthSummary{}, err
		}
		byType[view.Account.Type] = running

		if !view.Account.CountTowardNetWorth {
			summary.ExcludedByChoice++
			continue
		}

		// Everything past this point is in the headline, so it is in the
		// chart: the two must describe the same set of accounts or only the
		// newest bar agrees with the figure above it.
		counted = append(counted, trendAccount{
			account:   view.Account,
			balance:   view.Balance,
			inPrimary: inPrimary,
		})

		if view.Account.Type.IsLiability() {
			summary.Liabilities, err = summary.Liabilities.Add(inPrimary)
		} else {
			summary.Assets, err = summary.Assets.Add(inPrimary)
		}
		if err != nil {
			return NetWorthSummary{}, err
		}

		signed, err := view.Account.Type.SignedNetWorthAmount(inPrimary)
		if err != nil {
			return NetWorthSummary{}, err
		}
		summary.NetWorth, err = summary.NetWorth.Add(signed)
		if err != nil {
			return NetWorthSummary{}, err
		}
	}

	if considered > 0 && converted == 0 {
		summary.Computable = false
	}

	if summary.Computable && len(counted) > 0 {
		trend, err := s.trend(ctx, householdID, conv, counted, today, zero)
		if err != nil {
			return NetWorthSummary{}, err
		}
		summary.Trend = trend
	}

	// Ordered by domain.AccountTypes rather than by map iteration, so the
	// chart's bars do not reshuffle between two identical requests.
	for _, accountType := range domain.AccountTypes() {
		if total, ok := byType[accountType]; ok {
			summary.Breakdown = append(summary.Breakdown, BreakdownEntry{Type: accountType, Total: total})
		}
	}
	return summary, nil
}
