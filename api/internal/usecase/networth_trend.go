package usecase

import (
	"context"
	"fmt"
	"math"
	"time"

	"github.com/andreasoentoro/hearth/api/internal/domain"
)

// trendMonths is the window the design draws: twelve bars, `Aug '25` to
// `Jul '26` on its own axis.
const trendMonths = 12

// TrendPoint is one bar of the twelve-month net worth chart.
//
// NetWorth is nil for a month no counted account had been tracked through
// yet. It is nil rather than zero for the reason NetWorthSummary.Computable
// exists: zero is a claim about the household's money, and the truth in that
// month is that we cannot know it.
//
// Complete is false when at least one counted account was still untracked in
// that month -- the bar is real, but it is missing an account the newest bar
// has, and the step up between them is coverage rather than growth. It is
// also false on a month with no figure at all, so a caller that reads
// Complete without checking NetWorth cannot mistake an empty month for a
// whole one.
type TrendPoint struct {
	Month    time.Time
	NetWorth *domain.Money
	Complete bool
}

// NetWorthTrend is the twelve-month series and the month-to-date change.
//
// ChangeBasisPoints is integer basis points -- 210 means 2.10% -- and is nil
// far more often than it is set. changeBasisPoints below has the four
// conditions and why each one exists.
type NetWorthTrend struct {
	Points            []TrendPoint
	ChangeBasisPoints *int64
}

// trendAccount is one counted account, carried out of Summary's own loop.
//
// inPrimary is the value that loop already added to the headline. Keeping it
// is the whole point: the newest bar reuses that number instead of converting
// the same balance a second time, so the bar and the figure above it cannot
// disagree even if the rate provider is asked twice and answers differently.
type trendAccount struct {
	account   domain.Account
	balance   domain.Money
	inPrimary domain.Money
}

// trend builds the twelve-month series for the accounts Summary counted.
//
// Every month is converted at TODAY's rate, not the rate that held in that
// month: there is no historical rate table, and fx.StaticProvider has one
// number in it. The chart therefore shows how the household's balances moved
// with the exchange rate held still -- the more useful of the two charts
// anyway, since an account whose balance never changed should not appear to
// rise and fall because a currency did (spec decision 2).
func (s *AccountService) trend(
	ctx context.Context,
	householdID string,
	conv *converter,
	counted []trendAccount,
	today time.Time,
	zero domain.Money,
) (*NetWorthTrend, error) {
	months := make([]time.Time, trendMonths)
	current := startOfMonth(today)
	for i := range months {
		months[i] = current.AddDate(0, -(trendMonths - 1 - i), 0)
	}

	movements, err := s.d.Accounts.MonthlyMovements(ctx, householdID, months[0])
	if err != nil {
		return nil, err
	}
	deltas, err := deltasByAccountMonth(movements, current, counted)
	if err != nil {
		return nil, err
	}

	// running/known/missing are accumulated across accounts and folded into
	// points at the end, because one account can only ever contribute to a
	// month, never decide it: "complete" is a fact about all of them.
	running := make([]domain.Money, trendMonths)
	known := make([]bool, trendMonths)
	missing := make([]bool, trendMonths)
	for i := range running {
		running[i] = zero
	}

	for _, a := range counted {
		native, err := walkBack(a.balance.Amount, deltas[a.account.ID], months)
		if err != nil {
			return nil, err
		}
		trackedFrom := startOfMonth(a.account.OpeningBalanceAsOf)

		for i, m := range months {
			if trackedFrom.After(m) {
				missing[i] = true
				continue
			}

			inPrimary := a.inPrimary
			if i != trendMonths-1 {
				inPrimary, err = conv.convert(ctx, domain.Money{
					Amount:   native[i],
					Currency: a.balance.Currency,
				})
				if err != nil {
					return nil, err
				}
			}

			signed, err := a.account.Type.SignedNetWorthAmount(inPrimary)
			if err != nil {
				return nil, err
			}
			running[i], err = running[i].Add(signed)
			if err != nil {
				return nil, err
			}
			known[i] = true
		}
	}

	points := make([]TrendPoint, trendMonths)
	for i := range points {
		points[i] = TrendPoint{Month: months[i], Complete: known[i] && !missing[i]}
		if known[i] {
			total := running[i]
			points[i].NetWorth = &total
		}
	}

	return &NetWorthTrend{Points: points}, nil
}

// deltasByAccountMonth indexes the repository's rows by account and month.
//
// A month later than the current one is counted as the current one. That is
// not a rounding convenience: AccountView.Balance has no upper bound on the
// transaction date, so a transaction dated next month is already inside the
// balance the walk anchors on. Left in its own bucket it would never be
// subtracted, and every bar older than today would be wrong by its amount
// while the newest bar still matched the headline.
func deltasByAccountMonth(
	movements []AccountMonthMovement,
	current time.Time,
	counted []trendAccount,
) (map[string]map[int]domain.Money, error) {
	currencies := make(map[string]string, len(counted))
	for _, a := range counted {
		currencies[a.account.ID] = a.balance.Currency
	}

	out := map[string]map[int]domain.Money{}
	for _, m := range movements {
		want, ok := currencies[m.AccountID]
		if !ok {
			// Archived, excluded by choice, or not in the views this summary
			// describes. Whatever is out of the headline is out of the chart.
			continue
		}
		// Fail closed. A delta in another currency cannot be subtracted from
		// this account's balance, and adding it anyway would corrupt every
		// older bar with a figure that still looks like money.
		if m.Delta.Currency != want {
			return nil, fmt.Errorf("%w: movement for account %s is %s, the account is %s",
				domain.ErrCurrencyMismatch, m.AccountID, m.Delta.Currency, want)
		}

		month := startOfMonth(m.Month)
		if month.After(current) {
			month = current
		}
		byMonth, ok := out[m.AccountID]
		if !ok {
			byMonth = map[int]domain.Money{}
			out[m.AccountID] = byMonth
		}
		key := monthKey(month)
		if existing, ok := byMonth[key]; ok {
			summed, err := existing.Add(m.Delta)
			if err != nil {
				return nil, err
			}
			byMonth[key] = summed
			continue
		}
		byMonth[key] = m.Delta
	}
	return out, nil
}

// walkBack turns one account's current balance into its balance at the end of
// every earlier month in the window: each step removes the month it is
// leaving. The newest slot is the live balance itself, untouched.
func walkBack(current int64, byMonth map[int]domain.Money, months []time.Time) ([]int64, error) {
	native := make([]int64, len(months))
	native[len(months)-1] = current
	for i := len(months) - 2; i >= 0; i-- {
		back, err := subtractDelta(native[i+1], byMonth[monthKey(months[i+1])].Amount)
		if err != nil {
			return nil, err
		}
		native[i] = back
	}
	return native, nil
}

// subtractDelta is balance - delta with the overflow refused rather than
// wrapped. math.MinInt64 is checked on its own because it has no positive
// counterpart, so negating it returns itself -- the same edge
// AccountType.SignedNetWorthAmount and Money.String already guard.
func subtractDelta(balance, delta int64) (int64, error) {
	if delta == math.MinInt64 {
		return 0, domain.ErrAmountOverflow
	}
	negated := -delta
	if (negated > 0 && balance > math.MaxInt64-negated) ||
		(negated < 0 && balance < math.MinInt64-negated) {
		return 0, domain.ErrAmountOverflow
	}
	return balance + negated, nil
}
