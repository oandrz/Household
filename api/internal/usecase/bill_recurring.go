package usecase

import (
	"sort"

	"github.com/andreasoentoro/hearth/api/internal/domain"
)

// RecurringCategory is one row of the Recurring bills breakdown: what the
// live bills in one category cost when they repeat. CategoryID is "" for
// bills with no category, which share one row. Annual and Monthly are in
// the household's primary currency; ExcludedNoRate counts this category's
// bills left out of both because their currency has no rate, so the screen
// can say the row is short.
type RecurringCategory struct {
	CategoryID     string
	CategoryName   string
	Annual         domain.Money
	Monthly        domain.Money
	ExcludedNoRate int
}

// recurringRollup builds the Recurring bills total and its breakdown as
// List walks the live bills. Only bills that repeat reach it: a one-off is
// not a cost the household keeps paying.
type recurringRollup struct {
	zero domain.Money
	rows map[string]*RecurringCategory
}

func newRecurringRollup(zero domain.Money) *recurringRollup {
	return &recurringRollup{zero: zero, rows: map[string]*RecurringCategory{}}
}

// add puts one bill's annual equivalent, already in primary currency, into
// its category's row. A no-rate bill still opens its row, so a category
// whose every bill is excluded shows as short rather than vanishing.
func (r *recurringRollup) add(rec BillRecord, annual domain.Money, noRate bool) error {
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
	if noRate {
		row.ExcludedNoRate++
		return nil
	}
	sum, err := row.Annual.Add(annual)
	if err != nil {
		return err
	}
	row.Annual = sum
	return nil
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
	var total int64
	for i, row := range rows {
		annualMinors[i] = row.Annual.Amount
		total += row.Annual.Amount
	}
	for i, share := range domain.MonthlySharesMinor(annualMinors) {
		rows[i].Monthly = domain.Money{Amount: share, Currency: r.zero.Currency}
	}
	sortRecurringRows(rows, func(row RecurringCategory) int64 { return row.Monthly.Amount })

	annual = domain.Money{Amount: total, Currency: r.zero.Currency}
	monthly = domain.Money{Amount: total / 12, Currency: r.zero.Currency}
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
