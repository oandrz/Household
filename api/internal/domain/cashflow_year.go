package domain

import "time"

// CashflowYearMonth is one slot of the year chart.
type CashflowYearMonth struct {
	CashflowMonth
	// ProjectedLeftMinor is the usual left over, for months still to come in
	// the current year when there is an average to project from.
	ProjectedLeftMinor *int64
}

// CashflowYear is one calendar year of the page.
type CashflowYear struct {
	Year   int
	Months [12]CashflowYearMonth
	// KeptSoFarMinor sums LeftMinor over the year's finished months, grey
	// ones included; GreyMonths says how many of them were not trusted.
	KeptSoFarMinor int64
	// IncomeMinor sums the same finished months, so the share kept is like
	// for like.
	IncomeMinor int64
	GreyMonths  int
	// SameMonthsLastYearMinor is the same finished months a year earlier. It
	// is nil when any of them predates the history: a partial comparison
	// would mislead.
	SameMonthsLastYearMinor *int64
	// HeadingForMinor is the current year only, and only with an average.
	HeadingForMinor *int64
}

// isFinished reports whether the month's figures are a completed month's.
func isFinished(m CashflowMonth) bool {
	return m.Status == CashflowSolid || m.Status == CashflowGrey
}

// SummariseCashflowYear lays out the twelve months of year. months is what
// BuildCashflowMonths returned, grey-marked, from the first month of history
// through current. Months of the year before that history are CashflowNone,
// and months after current are CashflowFuture.
func SummariseCashflowYear(months []CashflowMonth, year int, current time.Time, avg CashflowAverage) CashflowYear {
	current = monthKey(current)
	y := CashflowYear{Year: year}
	for i := range y.Months {
		y.Months[i] = yearSlot(months, time.Date(year, time.Month(i+1), 1, 0, 0, 0, 0, time.UTC), current)
	}
	for _, slot := range y.Months {
		if !isFinished(slot.CashflowMonth) {
			continue
		}
		y.KeptSoFarMinor += slot.LeftMinor()
		y.IncomeMinor += slot.InMinor
		if slot.Status == CashflowGrey {
			y.GreyMonths++
		}
	}
	y.SameMonthsLastYearMinor = sameMonthsLastYear(months, year, current)
	if year == current.Year() && avg.Months > 0 {
		y.addProjections(avg, current)
	}
	return y
}

// yearSlot is the month at key: taken from months when history covers it,
// else none (before history) or future (after current).
func yearSlot(months []CashflowMonth, key, current time.Time) CashflowYearMonth {
	if key.After(current) {
		return CashflowYearMonth{CashflowMonth: CashflowMonth{Month: key, Status: CashflowFuture}}
	}
	if i := monthIndex(months, key); i >= 0 {
		return CashflowYearMonth{CashflowMonth: months[i]}
	}
	return CashflowYearMonth{CashflowMonth: CashflowMonth{Month: key, Status: CashflowNone}}
}

// addProjections fills the future months and "heading for". Every figure is
// one mulDivRoundHalfAway of the whole window, so nothing rounds twice. The
// remaining months include the current one, whose own partial figures are not
// in KeptSoFarMinor. Nothing is subtracted for yearly bills still due: the
// spread months already carry their share.
func (y *CashflowYear) addProjections(avg CashflowAverage, current time.Time) {
	if perMonth, ok := mulDivRoundHalfAway(avg.SumLeft(), 1, int64(avg.Months)); ok {
		for i := range y.Months {
			if y.Months[i].Status == CashflowFuture {
				v := perMonth
				y.Months[i].ProjectedLeftMinor = &v
			}
		}
	}
	remaining := int64(13 - int(current.Month()))
	rest, ok := mulDivRoundHalfAway(avg.SumLeft(), remaining, int64(avg.Months))
	if !ok {
		return
	}
	if total, err := addChecked(y.KeptSoFarMinor, rest); err == nil {
		y.HeadingForMinor = &total
	}
}

// sameMonthsLastYear sums the year before's months that line up with the
// finished months of year: January through the last finished month, or all
// twelve for a finished year.
func sameMonthsLastYear(months []CashflowMonth, year int, current time.Time) *int64 {
	count := 12
	switch {
	case year > current.Year():
		return nil
	case year == current.Year():
		count = int(current.Month()) - 1
	}
	if count == 0 {
		return nil
	}
	var sum int64
	for m := 1; m <= count; m++ {
		i := monthIndex(months, time.Date(year-1, time.Month(m), 1, 0, 0, 0, 0, time.UTC))
		if i < 0 || !isFinished(months[i]) {
			return nil
		}
		sum += months[i].LeftMinor()
	}
	return &sum
}
