package domain

import (
	"slices"
	"strings"
	"time"
)

// CashflowReconcileStatus says whether a month's sum can be shown.
type CashflowReconcileStatus string

const (
	// ReconcileShown means the lines add up exactly to the month's money out.
	ReconcileShown CashflowReconcileStatus = "shown"
	// ReconcileNoRate means a transaction or an earlier bill has no exchange
	// rate, so the month's figures are incomplete.
	ReconcileNoRate CashflowReconcileStatus = "no_rate"
	// ReconcileMismatch means the lines did not add up. This is a bug, never a
	// state of the household's data.
	ReconcileMismatch CashflowReconcileStatus = "mismatch"
)

// CashflowLineKind names the three kinds of adjustment between the ledger's
// "spent" and a month's money out.
type CashflowLineKind string

const (
	LineBillInFull CashflowLineKind = "bill_in_full"
	LineBillShare  CashflowLineKind = "bill_share"
	LineLoan       CashflowLineKind = "loan"
)

// CashflowSumLine is one step from the ledger's "spent" to money out.
type CashflowSumLine struct {
	Kind CashflowLineKind
	Name string
	// AmountMinor is signed: a whole bill is negative (the ledger counted it
	// but Cashflow does not), a share and a loan are positive.
	AmountMinor int64
	PaidOn      time.Time
	ShareIndex  int // bill_share only
	Spread      int // bill_in_full and bill_share
}

// CashflowReconciliation explains how a month's money out follows from the
// ledger's spent figure.
type CashflowReconciliation struct {
	Status     CashflowReconcileStatus
	SpentMinor int64
	// Lines is never nil, and is empty unless Status is ReconcileShown.
	Lines              []CashflowSumLine
	UnratedCount       int
	UnratedEarlierBill bool
	// UnratedCurrencies is never nil.
	UnratedCurrencies []string
}

// ReconcileCashflowMonth works out the sum "spent, minus bills paid in full,
// plus their shares, plus loans = money out" for one month.
//
// It is exact or it is silent. The lines are only returned when they add up to
// m.OutMinor to the minor unit: a sum that is almost right would teach the
// reader to distrust the page, so a mismatch (or an overflow, or a
// contribution kind this code does not know) hides the lines instead of
// showing a wrong one. A month with unrated transactions is also silent,
// because its money out is missing amounts the lines could never account for.
// It never returns an error: every failure is a Status the caller can show.
func ReconcileCashflowMonth(m CashflowMonth) CashflowReconciliation {
	r := CashflowReconciliation{
		Status:             ReconcileNoRate,
		SpentMinor:         m.LedgerSpentMinor,
		Lines:              []CashflowSumLine{},
		UnratedCount:       m.UnratedHere,
		UnratedEarlierBill: m.UnratedEarlierBill,
		UnratedCurrencies:  append([]string{}, m.UnconvertibleCurrencies...),
	}
	if len(m.UnconvertibleCurrencies) > 0 {
		return r
	}
	r.Status = ReconcileMismatch
	lines, ok := buildSumLines(m)
	if !ok {
		return r
	}
	total := m.LedgerSpentMinor
	var err error
	for _, l := range lines {
		if total, err = addChecked(total, l.AmountMinor); err != nil {
			return r
		}
	}
	if total != m.OutMinor {
		return r
	}
	r.Status = ReconcileShown
	r.Lines = lines
	return r
}

// buildSumLines orders the lines: each whole bill followed by its own first
// share, then the other shares, then loans. ok is false for a contribution
// kind it does not know.
func buildSumLines(m CashflowMonth) (lines []CashflowSumLine, ok bool) {
	lines = []CashflowSumLine{}
	var shares, loans []CashflowContribution
	for _, c := range m.Contributions {
		switch c.Kind {
		case ContributionSpreadBill:
			shares = append(shares, c)
		case ContributionLoan:
			loans = append(loans, c)
		default:
			return nil, false
		}
	}
	sortContributions(shares)
	sortContributions(loans)

	bills := slices.Clone(m.PaidInFull)
	slices.SortStableFunc(bills, func(a, b CashflowPaidBill) int {
		if c := a.PaidOn.Compare(b.PaidOn); c != 0 {
			return c
		}
		return strings.Compare(a.Name, b.Name)
	})
	used := make([]bool, len(shares))
	for _, b := range bills {
		lines = append(lines, CashflowSumLine{
			Kind: LineBillInFull, Name: b.Name, AmountMinor: -b.AmountMinor, PaidOn: b.PaidOn, Spread: b.Spread,
		})
		// The first share of the same bill, paid the same day, lands in the
		// month the bill was paid. Matching on ShareIndex 1 keeps a later
		// share of an earlier bill from being taken for it.
		for i, s := range shares {
			if !used[i] && s.Name == b.Name && s.PaidOn.Equal(b.PaidOn) && s.ShareIndex == 1 {
				used[i] = true
				lines = append(lines, shareLine(s))
				break
			}
		}
	}
	for i, s := range shares {
		if !used[i] {
			lines = append(lines, shareLine(s))
		}
	}
	for _, l := range loans {
		lines = append(lines, CashflowSumLine{Kind: LineLoan, Name: l.Name, AmountMinor: l.AmountMinor, PaidOn: l.PaidOn})
	}
	return lines, true
}

func shareLine(c CashflowContribution) CashflowSumLine {
	return CashflowSumLine{
		Kind: LineBillShare, Name: c.Name, AmountMinor: c.AmountMinor, PaidOn: c.PaidOn,
		ShareIndex: c.ShareIndex, Spread: c.Spread,
	}
}

// sortContributions orders by date paid, then name, keeping equal entries in
// the order they were added.
func sortContributions(cs []CashflowContribution) {
	slices.SortStableFunc(cs, func(a, b CashflowContribution) int {
		if c := a.PaidOn.Compare(b.PaidOn); c != 0 {
			return c
		}
		return strings.Compare(a.Name, b.Name)
	})
}
