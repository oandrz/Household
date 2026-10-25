package domain

import (
	"fmt"
	"slices"
	"time"
)

// CashflowItem is one transaction, already classified and converted into the
// primary currency, ready to be counted into a month.
type CashflowItem struct {
	OccurredOn time.Time
	Role       CashflowRole
	// AmountMinor is in the primary currency. It is 0 when Unconvertible is set.
	AmountMinor int64
	// Unconvertible is the original currency code when no exchange rate exists
	// for it, else "". Such an item adds no amount, only a reason to distrust
	// the months it covers.
	Unconvertible string
	// Spread is SpreadMonths(cadence): how many months the item covers. It is
	// 1 unless Role is CashflowSpreadOut.
	Spread int
	// Name is the bill or loan account name shown in the month's drill-down.
	Name string
}

// CashflowStatus says how much a month's figures can be trusted.
type CashflowStatus string

const (
	CashflowSolid   CashflowStatus = "solid"
	CashflowGrey    CashflowStatus = "grey"
	CashflowCurrent CashflowStatus = "current"
	CashflowFuture  CashflowStatus = "future"
	// CashflowNone marks a month before the household's first transaction.
	CashflowNone CashflowStatus = "none"
)

// GreyReason is why a finished month is not trusted. Go returns the code;
// the frontend turns it into a sentence.
type GreyReason string

const (
	GreyNoIncome      GreyReason = "no_income"
	GreyUnderUsual    GreyReason = "under_half_usual"
	GreyUnconvertible GreyReason = "unconvertible"
)

// Contribution kinds name the two adjustments that make money out differ from
// the ledger's "spent" figure.
const (
	ContributionSpreadBill = "spread_bill"
	ContributionLoan       = "loan"
)

// CashflowContribution is one line of a month's drill-down.
type CashflowContribution struct {
	Kind        string // ContributionSpreadBill or ContributionLoan
	Name        string
	AmountMinor int64 // this month's share
	PaidOn      time.Time
}

// CashflowMonth is one calendar month's figures.
type CashflowMonth struct {
	Month  time.Time // first of the month, UTC
	Status CashflowStatus
	// OutMinor includes SpreadMinor and LoanMinor.
	InMinor, OutMinor      int64
	SpreadMinor, LoanMinor int64
	// TransactionCount includes transfers that add no amount, because it
	// matches what the ledger lists for the month.
	TransactionCount        int
	Grey                    []GreyReason
	UnconvertibleCurrencies []string // sorted, unique
	Confirmed               bool
	Contributions           []CashflowContribution
}

// LeftMinor is what the month kept: money in minus money out.
func (m CashflowMonth) LeftMinor() int64 { return m.InMinor - m.OutMinor }

// BuildCashflowMonths returns one month for each calendar month from first
// through current, inclusive. current gets Status CashflowCurrent; every
// earlier month gets CashflowSolid, until MarkCashflowGrey runs.
//
// Items dated after current are ignored entirely: the page shows no month
// beyond the current one, and a spread share that would land there is dropped.
func BuildCashflowMonths(items []CashflowItem, first, current time.Time) ([]CashflowMonth, error) {
	first, current = monthKey(first), monthKey(current)
	var months []CashflowMonth
	for m := first; !m.After(current); m = m.AddDate(0, 1, 0) {
		status := CashflowSolid
		if m.Equal(current) {
			status = CashflowCurrent
		}
		months = append(months, CashflowMonth{Month: m, Status: status})
	}
	for _, item := range items {
		if err := addCashflowItem(months, item); err != nil {
			return nil, err
		}
	}
	return months, nil
}

// monthKey is the first of the month in UTC. transactions.occurred_on is a
// SQL date, so its calendar month is the ledger's month.
func monthKey(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), 1, 0, 0, 0, 0, time.UTC)
}

// monthIndex is where t's month sits in months, or -1 when outside the range.
func monthIndex(months []CashflowMonth, t time.Time) int {
	if len(months) == 0 {
		return -1
	}
	k, f := monthKey(t), months[0].Month
	i := (k.Year()-f.Year())*12 + int(k.Month()) - int(f.Month())
	if i < 0 || i >= len(months) {
		return -1
	}
	return i
}

func addCashflowItem(months []CashflowMonth, item CashflowItem) error {
	if item.Role == CashflowSpreadOut && item.Spread < 1 {
		return fmt.Errorf("%w: spread of %d months", ErrUnknownCashflowInput, item.Spread)
	}
	own := monthIndex(months, item.OccurredOn)
	if own >= 0 {
		months[own].TransactionCount++
	}
	if item.Role == CashflowSpreadOut {
		return addSpreadItem(months, item)
	}
	if own < 0 {
		return nil
	}
	return addSingleMonthItem(&months[own], item)
}

func addSingleMonthItem(m *CashflowMonth, item CashflowItem) error {
	if item.Unconvertible != "" {
		noteUnconvertible(m, item.Unconvertible)
		return nil
	}
	var err error
	switch item.Role {
	case CashflowIn:
		m.InMinor, err = addChecked(m.InMinor, item.AmountMinor)
	case CashflowOut:
		m.OutMinor, err = addChecked(m.OutMinor, item.AmountMinor)
	case CashflowLoanOut:
		if m.OutMinor, err = addChecked(m.OutMinor, item.AmountMinor); err != nil {
			return err
		}
		if m.LoanMinor, err = addChecked(m.LoanMinor, item.AmountMinor); err != nil {
			return err
		}
		m.Contributions = append(m.Contributions, CashflowContribution{
			Kind: ContributionLoan, Name: item.Name, AmountMinor: item.AmountMinor, PaidOn: item.OccurredOn,
		})
	case CashflowIgnore:
	default:
		return fmt.Errorf("%w: cashflow role %d", ErrUnknownCashflowInput, item.Role)
	}
	return err
}

// addSpreadItem puts one share of the payment into each month it covers,
// starting with the month it was paid. Shares that fall before the first
// month or after the current one are dropped.
func addSpreadItem(months []CashflowMonth, item CashflowItem) error {
	shares := SpreadForward(item.AmountMinor, item.Spread)
	start := monthKey(item.OccurredOn)
	for i, share := range shares {
		idx := monthIndex(months, start.AddDate(0, i, 0))
		if idx < 0 {
			continue
		}
		m := &months[idx]
		if item.Unconvertible != "" {
			noteUnconvertible(m, item.Unconvertible)
			continue
		}
		var err error
		if m.OutMinor, err = addChecked(m.OutMinor, share); err != nil {
			return err
		}
		if m.SpreadMinor, err = addChecked(m.SpreadMinor, share); err != nil {
			return err
		}
		m.Contributions = append(m.Contributions, CashflowContribution{
			Kind: ContributionSpreadBill, Name: item.Name, AmountMinor: share, PaidOn: item.OccurredOn,
		})
	}
	return nil
}

// noteUnconvertible records a currency with no rate, keeping the list sorted
// and free of repeats.
func noteUnconvertible(m *CashflowMonth, currency string) {
	i, found := slices.BinarySearch(m.UnconvertibleCurrencies, currency)
	if !found {
		m.UnconvertibleCurrencies = slices.Insert(m.UnconvertibleCurrencies, i, currency)
	}
}

// addChecked adds two amounts and refuses a result that would wrap.
func addChecked(a, b int64) (int64, error) {
	sum := a + b
	if (a > 0 && b > 0 && sum <= 0) || (a < 0 && b < 0 && sum >= 0) {
		return 0, fmt.Errorf("%w: %d + %d", ErrAmountOverflow, a, b)
	}
	return sum, nil
}
