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
// household's primary currency, so the screen can say which and why. It is an
// explicit field rather than something the frontend infers by comparing lists:
// a limited member's response carries no amounts at all, so inference there
// would produce a wrong or empty notice.
type ExcludedAccount struct {
	AccountID string
	Currency  string
}

// NetWorthSummary is everything the Finances screen shows above the accounts
// list.
//
// Computable is false when at least one account exists and none of them could
// be converted -- the state a household reaches by changing its primary
// currency in Settings while fx.StaticProvider knows only SGD<->IDR. A zero
// must never be shown for it: zero is a claim about the household's money, and
// the truth is that we cannot compute it. A household with no accounts at all
// is computable and genuinely zero.
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

// Summary composes the figures above the accounts list from views the caller
// has already listed, rather than listing again -- the handler needs both
// halves of one response and they must describe the same set of rows.
//
// The order of operations is not incidental. domain.Money.Add refuses to add
// two different currencies, deliberately, so each account is converted into
// the household's primary currency *first* and only then summed. Summing first
// and converting after fails on the second account of a mixed-currency
// household. Rounding therefore happens per account (half away from zero, as
// Rate.Apply already does) and the total is never re-rounded, so the figure is
// deterministic.
//
// today drives the twelve-month window and is taken as a parameter, never read
// from a clock in here, so every figure is deterministic in tests and the wall
// clock is read exactly once, at the HTTP layer. RetroService.List is the same
// shape for the same reason.
func (s *AccountService) Summary(ctx context.Context, householdID string, views []AccountView, today time.Time) (NetWorthSummary, error) {
	household, err := s.d.Households.Get(ctx, householdID)
	if err != nil {
		return NetWorthSummary{}, err
	}
	primary := household.PrimaryCurrency
	conv := &converter{fx: s.d.FX, primary: primary, rates: map[string]Rate{}}

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
	// considered counts every non-archived view Summary looked at; converted
	// counts how many of those actually converted. Computable is judged
	// against considered, not len(views): an archived account is skipped
	// outright (see below), so a household whose only accounts are archived
	// must still read as a genuine, computable zero, not as "cannot compute."
	considered := 0
	converted := 0
	counted := make([]trendAccount, 0, len(views))

	for _, view := range views {
		if view.Account.IsArchived() {
			continue
		}
		considered++

		inPrimary, err := conv.convert(ctx, view.Balance)
		if err != nil {
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

// converter turns balances into one primary currency, looking each rate up at
// most once per request.
//
// One lookup, reused, is not an optimisation. Summary's headline and the
// trend's newest bar must apply the SAME rate to the same account, or the
// chart's last bar disagrees with the figure printed directly above it.
// fx.StaticProvider returns one number forever, so two independent lookups
// agree today by coincidence; a live provider could return two different rates
// inside one request, and no test against the static provider would ever see
// it.
type converter struct {
	fx      FXRateProvider
	primary string
	rates   map[string]Rate
}

// convert turns one balance into the household's primary currency. A
// same-currency balance short-circuits without consulting the provider at all
// -- that is the overwhelmingly common case, it is exact, and it means a
// single-currency household never depends on a rate table it does not need.
func (c *converter) convert(ctx context.Context, m domain.Money) (domain.Money, error) {
	if m.Currency == c.primary {
		return m, nil
	}
	rate, ok := c.rates[m.Currency]
	if !ok {
		var err error
		rate, err = c.fx.Rate(ctx, m.Currency, c.primary)
		if err != nil {
			return domain.Money{}, err
		}
		c.rates[m.Currency] = rate
	}
	amount, err := rate.Apply(m.Amount)
	if err != nil {
		return domain.Money{}, err
	}
	return domain.Money{Amount: amount, Currency: c.primary}, nil
}
