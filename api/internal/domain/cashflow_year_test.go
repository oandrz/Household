package domain_test

import (
	"testing"
	"time"

	"github.com/andreasoentoro/hearth/api/internal/domain"
)

// yearHistory builds finished months from first up to the month before cur,
// each keeping left[i], plus a current month with figures that must never
// be counted.
func yearHistory(first, cur time.Time, left func(i int) int64) []domain.CashflowMonth {
	var out []domain.CashflowMonth
	i := 0
	for m := first; m.Before(cur); m = m.AddDate(0, 1, 0) {
		out = append(out, domain.CashflowMonth{Month: m, Status: domain.CashflowSolid, InMinor: 1_000_000 + left(i), OutMinor: 1_000_000})
		i++
	}
	return append(out, domain.CashflowMonth{Month: cur, Status: domain.CashflowCurrent, InMinor: 9_000_000, OutMinor: 1})
}

func constLeft(v int64) func(int) int64 { return func(int) int64 { return v } }

func TestYearKeptSoFarIncludesGreyMonthsAndCountsThem(t *testing.T) {
	cur := cfMonth(2026, time.October)
	months := yearHistory(cfMonth(2026, time.January), cur, constLeft(100_000))
	months[3].Status = domain.CashflowGrey
	y := domain.SummariseCashflowYear(months, 2026, cur, avgOf(3, 3_000_000, 2_430_000))
	if y.KeptSoFarMinor != 900_000 || y.GreyMonths != 1 {
		t.Fatalf("kept=%d grey=%d", y.KeptSoFarMinor, y.GreyMonths)
	}
	if y.IncomeMinor != 9*1_100_000 {
		t.Fatalf("income = %d", y.IncomeMinor)
	}
}

func TestYearHeadingForHasNoBillSubtraction(t *testing.T) {
	cur := cfMonth(2026, time.October)
	// Nine months summing to 1,706,000.
	months := yearHistory(cfMonth(2026, time.January), cur, func(i int) int64 {
		if i == 0 {
			return 1_706_000 - 8*100_000
		}
		return 100_000
	})
	avg := avgOf(3, 3_000_000, 2_430_000) // sumLeft 570,000
	y := domain.SummariseCashflowYear(months, 2026, cur, avg)
	if y.KeptSoFarMinor != 1_706_000 {
		t.Fatalf("kept = %d", y.KeptSoFarMinor)
	}
	if y.HeadingForMinor == nil || *y.HeadingForMinor != 2_276_000 {
		t.Fatalf("heading for = %v, want 2,276,000", y.HeadingForMinor)
	}
}

func TestYearHeadingForRoundsOnce(t *testing.T) {
	cur := cfMonth(2026, time.October)
	months := yearHistory(cfMonth(2026, time.January), cur, constLeft(0))
	// 3 remaining * 100 / 3 months would be 100; 3 * 101 / 3 = 101;
	// 3 * 100 / 3 with sumLeft 100: exactly 100. Use sumLeft 2, n 3: 3*2/3 = 2.
	// Rounding each month first would give 3 * round(2/3) = 3.
	y := domain.SummariseCashflowYear(months, 2026, cur, avgOf(3, 12, 10))
	if y.HeadingForMinor == nil || *y.HeadingForMinor != 2 {
		t.Fatalf("heading for = %v, want 2", y.HeadingForMinor)
	}
}

func TestYearFutureMonthsCarryProjectionOnlyInTheCurrentYear(t *testing.T) {
	cur := cfMonth(2026, time.October)
	months := yearHistory(cfMonth(2026, time.January), cur, constLeft(100_000))
	avg := avgOf(3, 3_000_000, 2_430_000) // 570,000 over 3 months
	y := domain.SummariseCashflowYear(months, 2026, cur, avg)
	oct, nov, dec := y.Months[9], y.Months[10], y.Months[11]
	if oct.Status != domain.CashflowCurrent || oct.ProjectedLeftMinor != nil {
		t.Fatalf("october = %s %v", oct.Status, oct.ProjectedLeftMinor)
	}
	for _, m := range []domain.CashflowYearMonth{nov, dec} {
		if m.Status != domain.CashflowFuture || m.ProjectedLeftMinor == nil || *m.ProjectedLeftMinor != 190_000 {
			t.Fatalf("%s = %s %v", m.Month.Format("2006-01"), m.Status, m.ProjectedLeftMinor)
		}
	}
	if y.Months[8].ProjectedLeftMinor != nil {
		t.Fatal("a finished month must carry no projection")
	}

	// A finished year: no future months, no heading for.
	full := yearHistory(cfMonth(2025, time.January), cfMonth(2026, time.October), constLeft(100_000))
	past := domain.SummariseCashflowYear(full, 2025, cur, avg)
	if past.HeadingForMinor != nil {
		t.Fatal("a finished year has no heading for")
	}
	for _, m := range past.Months {
		if m.Status == domain.CashflowFuture || m.ProjectedLeftMinor != nil {
			t.Fatalf("finished year has %s %v", m.Status, m.ProjectedLeftMinor)
		}
	}
	if past.KeptSoFarMinor != 1_200_000 {
		t.Fatalf("kept = %d", past.KeptSoFarMinor)
	}
}

func TestYearWithNoAverageHasNoHeadingFor(t *testing.T) {
	cur := cfMonth(2026, time.October)
	months := yearHistory(cfMonth(2026, time.January), cur, constLeft(100_000))
	y := domain.SummariseCashflowYear(months, 2026, cur, avgOf(0, 0, 0))
	if y.HeadingForMinor != nil || y.Months[10].ProjectedLeftMinor != nil {
		t.Fatalf("heading=%v projected=%v", y.HeadingForMinor, y.Months[10].ProjectedLeftMinor)
	}
}

func TestYearMonthsBeforeHistoryAreNone(t *testing.T) {
	cur := cfMonth(2026, time.October)
	months := yearHistory(cfMonth(2026, time.April), cur, constLeft(100_000))
	y := domain.SummariseCashflowYear(months, 2026, cur, avgOf(3, 3_000_000, 2_430_000))
	for i := 0; i < 3; i++ {
		if y.Months[i].Status != domain.CashflowNone || y.Months[i].Month != cfMonth(2026, time.Month(i+1)) {
			t.Errorf("month %d = %s %v", i+1, y.Months[i].Status, y.Months[i].Month)
		}
	}
	if y.Months[3].Status != domain.CashflowSolid {
		t.Errorf("april = %s", y.Months[3].Status)
	}
	if y.KeptSoFarMinor != 600_000 {
		t.Errorf("kept = %d", y.KeptSoFarMinor)
	}
}

func TestYearSameMonthsLastYearComparesLikeForLike(t *testing.T) {
	cur := cfMonth(2026, time.October)
	months := yearHistory(cfMonth(2025, time.January), cur, func(i int) int64 {
		switch {
		case i < 9: // Jan..Sep 2025
			return 1_420_000 / 9 // 157,777 each; fixed below
		case i < 12: // Oct..Dec 2025: must not be included
			return 5_000_000
		}
		return 100_000
	})
	// Make January to September 2025 sum to exactly 1,420,000.
	months[0].InMinor += 1_420_000 - 9*(1_420_000/9)
	y := domain.SummariseCashflowYear(months, 2026, cur, avgOf(3, 3_000_000, 2_430_000))
	if y.SameMonthsLastYearMinor == nil || *y.SameMonthsLastYearMinor != 1_420_000 {
		t.Fatalf("got %v, want 1,420,000", y.SameMonthsLastYearMinor)
	}
}

func TestYearSameMonthsLastYearIsNilWhenAnyMonthPredatesHistory(t *testing.T) {
	cur := cfMonth(2026, time.October)
	// History begins February 2025, so January 2025 is missing.
	months := yearHistory(cfMonth(2025, time.February), cur, constLeft(100_000))
	y := domain.SummariseCashflowYear(months, 2026, cur, avgOf(3, 3_000_000, 2_430_000))
	if y.SameMonthsLastYearMinor != nil {
		t.Fatalf("got %d, want nil", *y.SameMonthsLastYearMinor)
	}
}

func TestYearOfAFinishedYearComparesAllTwelveMonths(t *testing.T) {
	cur := cfMonth(2026, time.October)
	months := yearHistory(cfMonth(2024, time.January), cur, constLeft(10_000))
	y := domain.SummariseCashflowYear(months, 2025, cur, avgOf(3, 3_000_000, 2_430_000))
	if y.SameMonthsLastYearMinor == nil || *y.SameMonthsLastYearMinor != 120_000 {
		t.Fatalf("got %v, want 120,000", y.SameMonthsLastYearMinor)
	}
}

func TestYearInJanuaryHasNoFinishedMonthsToCompare(t *testing.T) {
	cur := cfMonth(2026, time.January)
	months := yearHistory(cfMonth(2025, time.January), cur, constLeft(10_000))
	y := domain.SummariseCashflowYear(months, 2026, cur, avgOf(3, 3_000_000, 2_430_000))
	if y.SameMonthsLastYearMinor != nil || y.KeptSoFarMinor != 0 {
		t.Fatalf("same=%v kept=%d", y.SameMonthsLastYearMinor, y.KeptSoFarMinor)
	}
	if y.HeadingForMinor == nil || *y.HeadingForMinor != 12*190_000 {
		t.Fatalf("heading = %v", y.HeadingForMinor)
	}
}
