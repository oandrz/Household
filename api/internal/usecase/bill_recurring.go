package usecase

import (
	"sort"

	"github.com/andreasoentoro/hearth/api/internal/domain"
)

// RecurringCategory is one row of the Recurring bills breakdown: what the
// live bills in one category cost when they repeat. CategoryID is "" for
// bills with no category, which share one row. Annual and Monthly are in
// the household's primary currency. ExcludedNoRate counts this category's
// bills left out because their currency has no rate, and ExcludedTooLarge
// those left out because their figure is too large to add up exactly, so the
// screen can say the row is short.
type RecurringCategory struct {
	CategoryID       string
	CategoryName     string
	Annual           domain.Money
	Monthly          domain.Money
	ExcludedNoRate   int
	ExcludedTooLarge int
}

// exclusion says why a repeating bill is left out of the recurring totals.
type exclusion int

const (
	excludeNone exclusion = iota
	excludeNoRate
	excludeTooLarge
)

// recurringRollup builds the Recurring bills total and its breakdown as
// List walks the live bills. Only bills that repeat reach it: a one-off is
// not a cost the household keeps paying.
//
// Every figure it holds stays at or under domain.MaxExactTotalMinor. A bill
// that would take its row or the total past that is left out and counted in
// ExcludedTooLarge instead. This departs on purpose from the rule that an
// overflowing sum fails the read (domain.MaxAmountMinor's comment): failing
// here would take down the Bills page, which is where the bill is fixed or
// archived, and the row still tells the household its figure is short.
type recurringRollup struct {
	zero  domain.Money
	total int64
	rows  map[string]*RecurringCategory
}

func newRecurringRollup(zero domain.Money) *recurringRollup {
	return &recurringRollup{zero: zero, rows: map[string]*RecurringCategory{}}
}

// add puts one bill's annual equivalent, already in primary currency, into
// its category's row, unless why already excludes it. It returns why the bill
// ended up excluded, if it did: a bill that fits on its own can still be
// excluded as too large here, when adding it would pass the exact ceiling.
// An excluded bill still opens its row, so a category whose every bill is
// excluded shows as short rather than vanishing.
func (r *recurringRollup) add(rec BillRecord, annual domain.Money, why exclusion) (exclusion, error) {
	row, ok := r.rows[rec.Bill.CategoryID]
	if !ok {
		row = &RecurringCategory{
			CategoryID:   rec.Bill.CategoryID,
			CategoryName: rec.CategoryName,
			Annual:       r.zero,
			Monthly:      r.zero,
		}
		r.rows[rec.Bill.CategoryID] = row
	}
	if why == excludeNone && (!fitsExactly(row.Annual.Amount, annual.Amount) || !fitsExactly(r.total, annual.Amount)) {
		why = excludeTooLarge
	}
	switch why {
	case excludeNoRate:
		row.ExcludedNoRate++
		return why, nil
	case excludeTooLarge:
		row.ExcludedTooLarge++
		return why, nil
	}
	sum, err := row.Annual.Add(annual)
	if err != nil {
		return excludeNone, err
	}
	row.Annual = sum
	r.total += annual.Amount
	return excludeNone, nil
}

// fitsExactly reports whether total+amount stays at or under the exact
// ceiling. Both are non-negative here: a bill's amount must be positive.
func fitsExactly(total, amount int64) bool {
	return amount <= domain.MaxExactTotalMinor-total
}

// totals returns the yearly total, the monthly total and the rows, largest
// monthly figure first. The monthly total is the yearly one divided once,
// the rule the subscriptions total follows, and the rows' monthly figures
// are domain.MonthlySharesMinor of their yearly ones so they add up to it.
func (r *recurringRollup) totals() (annual, monthly domain.Money, rows []RecurringCategory) {
	rows = make([]RecurringCategory, 0, len(r.rows))
	for _, row := range r.rows {
		rows = append(rows, *row)
	}
	// Sorted before the shares are taken, so a tie in remainders goes to
	// the same row on every request rather than following map order.
	sortRecurringRows(rows, func(row RecurringCategory) int64 { return row.Annual.Amount })

	annualMinors := make([]int64, len(rows))
	for i, row := range rows {
		annualMinors[i] = row.Annual.Amount
	}
	for i, share := range domain.MonthlySharesMinor(annualMinors) {
		rows[i].Monthly = domain.Money{Amount: share, Currency: r.zero.Currency}
	}
	sortRecurringRows(rows, func(row RecurringCategory) int64 { return row.Monthly.Amount })

	annual = domain.Money{Amount: r.total, Currency: r.zero.Currency}
	monthly = domain.Money{Amount: r.total / 12, Currency: r.zero.Currency}
	return annual, monthly, rows
}

// sortRecurringRows orders rows by amount, largest first, then by name and
// ID so equal rows always come out in the same order.
func sortRecurringRows(rows []RecurringCategory, amount func(RecurringCategory) int64) {
	sort.SliceStable(rows, func(i, j int) bool {
		a, b := amount(rows[i]), amount(rows[j])
		if a != b {
			return a > b
		}
		if rows[i].CategoryName != rows[j].CategoryName {
			return rows[i].CategoryName < rows[j].CategoryName
		}
		return rows[i].CategoryID < rows[j].CategoryID
	})
}
