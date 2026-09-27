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
// NetWorth is nil, not zero, for a month no counted account was tracked
// through yet -- the same "zero is a claim" reasoning as
// NetWorthSummary.Computable.
//
// Complete is false when a counted account was still untracked that month
// (the step up is coverage, not growth) or when NetWorth is nil, so a
// caller can't mistake an empty month for a whole one.
type TrendPoint struct {
	Month    time.Time
	NetWorth *domain.Money
	Complete bool
}

// NetWorthTrend is the twelve-month series and the month-to-date change.
//
// ChangeBasisPoints is integer basis points (210 = 2.10%), nil far more
// often than set -- see changeBasisPoints for its four conditions.
type NetWorthTrend struct {
	Points            []TrendPoint
	ChangeBasisPoints *int64
}

// trendAccount is one counted account, carried out of Summary's own loop.
//
// inPrimary is the value that loop already added to the headline. Reusing it
// for the newest bar -- rather than converting the same balance again -- is
// what guarantees the bar can never disagree with the headline, even if the
// rate provider answers differently on a second call: the last bar IS the
// headline figure, by construction. Converter's per-request cache
// (TestSummaryLooksUpEachRateOnce) keeps other figures consistent, but this
// guarantee does not depend on it.
//
// The `i != trendMonths-1` guard in trend() can't be proven by an ordinary
// test here: with the cache in place, reconverting the newest month returns
// the same cached rate regardless of the guard. It is proven necessary only
// by disabling the cache and watching TestTheNewestBarIsTheHeadlineFigure
// fail without it -- don't add a white-box test; it would only observe the
// cache.
type trendAccount struct {
	account   domain.Account
	balance   domain.Money
	inPrimary domain.Money
}

// trend builds the twelve-month series for the accounts Summary counted.
//
// Every month converts at TODAY's rate, not the rate that held then: there
// is no historical rate table. The chart shows balance movement with the
// rate held still -- more useful than the alternative, where an unchanged
// balance would appear to rise and fall because a currency did.
//
// counted's balances come from the handler's AccountView.List;
// MonthlyMovements below is a second, unwrapped read, so there is a narrow
// window where a transaction dated in months[1..11] (walkBack never reads
// months[0]'s movement) can be written or deleted between them. The newest
// bar still matches the headline exactly (both come from the same List
// call), but an older bar can silently drift by that amount until the next
// GET /accounts re-reads both together and closes the window.
//
// This is accepted deliberately: the window is one request wide and
// self-heals on the next refresh -- a smaller version of the retroactivity
// this whole feature already accepts, since the trend is derived from
// transactions on every read rather than snapshotted. A transaction (or
// repeatable-read isolation) is more machinery than this narrow risk earns
// today. Revisit if CSV import turns "one transaction" into "a bulk insert
// of hundreds," or if a snapshot table arrives and this stops self-healing
// for free.
func (s *AccountService) trend(
	ctx context.Context,
	householdID string,
	conv *Converter,
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
		// AccountService.validate gives a full day of slack on this date, so a
		// UTC+8 "today" can land in next month. The account is already in the
		// headline regardless, so it belongs in the newest bar -- the same
		// reason deltasByAccountMonth clamps a future-dated movement.
		if trackedFrom.After(current) {
			trackedFrom = current
		}

		for i, m := range months {
			if trackedFrom.After(m) {
				missing[i] = true
				continue
			}

			inPrimary := a.inPrimary
			if i != trendMonths-1 {
				inPrimary, err = conv.Convert(ctx, domain.Money{
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

	return &NetWorthTrend{
		Points:            points,
		ChangeBasisPoints: changeBasisPoints(points[trendMonths-1], points[trendMonths-2]),
	}, nil
}

// changeBasisPoints is the "▲ 2.1%" beside the headline figure, in integer
// basis points: 210 means 2.10%. It returns nil far more often than a
// number:
//
//   - either month unknown: nothing to compare.
//   - either month incomplete: the step is partly coverage, not growth.
//   - base <= 0: a percentage of zero is undefined, and a negative base
//     inverts its own sign (-10,000 to -5,000 would show as -50%).
//   - arithmetic would overflow: fail closed rather than render a wrapped
//     number.
//
// Rounding is half away from zero, matching Rate.Apply.
func changeBasisPoints(current, previous TrendPoint) *int64 {
	if current.NetWorth == nil || previous.NetWorth == nil {
		return nil
	}
	if !current.Complete || !previous.Complete {
		return nil
	}

	base := previous.NetWorth.Amount
	if base <= 0 {
		return nil
	}
	now := current.NetWorth.Amount
	if now < math.MinInt64+base {
		return nil
	}
	delta := now - base

	if delta > math.MaxInt64/10_000 || delta < math.MinInt64/10_000 {
		return nil
	}
	scaled := delta * 10_000
	half := base / 2
	if scaled > math.MaxInt64-half || scaled < math.MinInt64+half {
		return nil
	}
	if scaled < 0 {
		scaled -= half
	} else {
		scaled += half
	}
	points := scaled / base
	return &points
}

// deltasByAccountMonth indexes the repository's rows by account and month.
//
// A month later than the current one folds into the current one: this isn't
// rounding, it's correctness -- AccountView.Balance has no upper bound on
// the transaction date, so a next-month transaction is already inside the
// anchor balance. Left in its own bucket, it would never be subtracted,
// leaving every older bar wrong.
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
