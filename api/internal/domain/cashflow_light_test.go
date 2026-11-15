package domain_test

import (
	"testing"
	"time"

	"github.com/andreasoentoro/hearth/api/internal/domain"
)

// solidMonth is a last finished month with a long enough history behind it
// that the under-half-of-usual check ran on it. healthy() and the other
// averages below likewise have Unchecked 0: every averaged month was checked.
func solidMonth(in, out int64) domain.CashflowMonth {
	return domain.CashflowMonth{
		Month: cfMonth(2026, time.September), Status: domain.CashflowSolid, InMinor: in, OutMinor: out,
		UsualChecked: true,
	}
}

func avgOf(n int, sumIn, sumOut int64) domain.CashflowAverage {
	return domain.CashflowAverage{Months: n, SumIn: sumIn, SumOut: sumOut}
}

// healthy is an average of 3 months: 19% kept, usual out 60,000 a month.
func healthy() domain.CashflowAverage { return avgOf(3, 300_000, 243_000) }

// usualLeft190k keeps 190,000 a month on average: 570,000 over 3 months.
func usualLeft190k() domain.CashflowAverage { return avgOf(3, 3_000_000, 2_430_000) }

func knownCash(minor int64) domain.CashCushion {
	return domain.CashCushion{Known: true, CashMinor: minor}
}

func codes(v domain.CashflowVerdict) []string {
	var out []string
	for _, r := range v.Reasons {
		out = append(out, r.Code)
	}
	return out
}

func hasCode(v domain.CashflowVerdict, code string) bool {
	for _, c := range codes(v) {
		if c == code {
			return true
		}
	}
	return false
}

func wantColour(t *testing.T, v domain.CashflowVerdict, colour domain.LightColour, code string) {
	t.Helper()
	if v.Colour != colour || !hasCode(v, code) {
		t.Fatalf("got %s %v, want %s with %s", v.Colour, codes(v), colour, code)
	}
}

func TestJudgeIsGreyWithNoFinishedMonth(t *testing.T) {
	v := domain.JudgeCashflow(domain.CashflowMonth{}, false, healthy(), knownCash(1_000_000), nil)
	wantColour(t, v, domain.LightGrey, "no_finished_month")
}

func TestJudgeIsGreyWhenLastFinishedMonthIsGrey(t *testing.T) {
	last := solidMonth(100_000, 50_000)
	last.Status = domain.CashflowGrey
	v := domain.JudgeCashflow(last, true, healthy(), knownCash(1_000_000), nil)
	wantColour(t, v, domain.LightGrey, "last_month_not_enough_data")
}

func TestJudgeIsGreyWhenNoSolidMonthToAverage(t *testing.T) {
	v := domain.JudgeCashflow(solidMonth(100_000, 50_000), true, avgOf(0, 0, 0), knownCash(1_000_000), nil)
	wantColour(t, v, domain.LightGrey, "no_months_to_average")
}

func TestJudgeRedWhenAverageLeftIsZeroOrNegative(t *testing.T) {
	v := domain.JudgeCashflow(solidMonth(100_000, 100_000), true, avgOf(3, 300_000, 300_000), knownCash(10_000_000), nil)
	wantColour(t, v, domain.LightRed, "left_not_positive")
	v = domain.JudgeCashflow(solidMonth(100_000, 100_000), true, avgOf(3, 300_000, 300_001), knownCash(10_000_000), nil)
	wantColour(t, v, domain.LightRed, "left_not_positive")
	v = domain.JudgeCashflow(solidMonth(100_000, 100_000), true, avgOf(3, 300_000, 299_999), knownCash(10_000_000), nil)
	if hasCode(v, "left_not_positive") {
		t.Fatalf("left of +1 must not be red: %v", codes(v))
	}
}

func TestJudgeRedWhenCushionUnderOneMonth(t *testing.T) {
	// sumOut 243,000 over 3 months: one month is 81,000.
	v := domain.JudgeCashflow(solidMonth(100_000, 81_000), true, healthy(), knownCash(80_999), nil)
	wantColour(t, v, domain.LightRed, "cushion_under_1")
	v = domain.JudgeCashflow(solidMonth(100_000, 81_000), true, healthy(), knownCash(81_000), nil)
	if hasCode(v, "cushion_under_1") || v.Colour == domain.LightRed {
		t.Fatalf("exactly one month must not be red: %s %v", v.Colour, codes(v))
	}
}

func TestJudgeBoundaries(t *testing.T) {
	last := solidMonth(100_000, 50_000)
	t.Run("kept 9.99% is yellow", func(t *testing.T) {
		v := domain.JudgeCashflow(last, true, avgOf(3, 10_000, 9_001), knownCash(1_000_000), nil)
		wantColour(t, v, domain.LightYellow, "kept_under_10")
		if v.Reasons[0].KeptBasisPoints == nil || *v.Reasons[0].KeptBasisPoints != 999 {
			t.Fatalf("basis points = %v, want 999", v.Reasons[0].KeptBasisPoints)
		}
	})
	t.Run("kept exactly 10% is not", func(t *testing.T) {
		v := domain.JudgeCashflow(last, true, avgOf(3, 10_000, 9_000), knownCash(1_000_000), nil)
		if hasCode(v, "kept_under_10") || v.Colour != domain.LightGreen {
			t.Fatalf("got %s %v", v.Colour, codes(v))
		}
	})
	// sumOut 9,000 over n=3: three months of cushion is 9,000 of cash.
	t.Run("cushion exactly 3 months is not under 3", func(t *testing.T) {
		v := domain.JudgeCashflow(last, true, avgOf(3, 10_000, 9_000), knownCash(9_000), nil)
		if hasCode(v, "cushion_under_3") {
			t.Fatalf("got %v", codes(v))
		}
	})
	t.Run("one minor unit less is under 3", func(t *testing.T) {
		v := domain.JudgeCashflow(last, true, avgOf(3, 10_000, 9_000), knownCash(8_999), nil)
		wantColour(t, v, domain.LightYellow, "cushion_under_3")
		if got := v.Reasons[0].CushionTenths; got == nil || *got != 30 {
			t.Fatalf("tenths = %v, want 30", got)
		}
	})
	t.Run("last month exactly zero is not negative", func(t *testing.T) {
		v := domain.JudgeCashflow(solidMonth(5, 5), true, avgOf(3, 10_000, 9_000), knownCash(1_000_000), nil)
		if hasCode(v, "last_month_negative") {
			t.Fatalf("got %v", codes(v))
		}
		v = domain.JudgeCashflow(solidMonth(5, 6), true, avgOf(3, 10_000, 9_000), knownCash(1_000_000), nil)
		wantColour(t, v, domain.LightYellow, "last_month_negative")
	})
	t.Run("bill equal to usual left over is not due soon", func(t *testing.T) {
		bill := domain.BillDue{Name: "Insurance", AmountMinor: 190_000}
		v := domain.JudgeCashflow(last, true, usualLeft190k(), knownCash(10_000_000), []domain.BillDue{bill})
		if hasCode(v, "bill_due_soon") {
			t.Fatalf("got %v", codes(v))
		}
		bill.AmountMinor = 190_001
		v = domain.JudgeCashflow(last, true, usualLeft190k(), knownCash(10_000_000), []domain.BillDue{bill})
		wantColour(t, v, domain.LightYellow, "bill_due_soon")
	})
}

func TestJudgeDoesNotOverflowOnHugeFigures(t *testing.T) {
	const big = int64(1) << 62
	v := domain.JudgeCashflow(solidMonth(big, 1), true, avgOf(3, big, 1), knownCash(big), nil)
	if v.Colour != domain.LightGreen {
		t.Fatalf("got %s %v", v.Colour, codes(v))
	}
}

func TestJudgeYellowWhenCushionUnknown(t *testing.T) {
	v := domain.JudgeCashflow(solidMonth(100_000, 50_000), true, healthy(), domain.CashCushion{Reason: "no_cash_accounts"}, nil)
	wantColour(t, v, domain.LightYellow, "cushion_unknown")
}

func TestJudgeUnknownCushionIsNeverRed(t *testing.T) {
	// CashMinor is stale garbage when Known is false and must not be read.
	v := domain.JudgeCashflow(solidMonth(100_000, 50_000), true, healthy(), domain.CashCushion{CashMinor: -5}, nil)
	if v.Colour != domain.LightYellow {
		t.Fatalf("got %s %v", v.Colour, codes(v))
	}
}

func TestJudgeYellowWhenLastMonthNegative(t *testing.T) {
	v := domain.JudgeCashflow(solidMonth(100_000, 120_000), true, healthy(), knownCash(1_000_000), nil)
	wantColour(t, v, domain.LightYellow, "last_month_negative")
}

func TestJudgeYellowWhenADueBillExceedsUsualLeftOver(t *testing.T) {
	last := solidMonth(100_000, 50_000)
	bills := []domain.BillDue{{Name: "Car insurance", AmountMinor: 210_000}}
	v := domain.JudgeCashflow(last, true, usualLeft190k(), knownCash(10_000_000), bills)
	wantColour(t, v, domain.LightYellow, "bill_due_soon")
	if v.Reasons[0].BillName != "Car insurance" || v.Reasons[0].BillUnconvertible {
		t.Fatalf("reason = %+v, want Car insurance, convertible", v.Reasons[0])
	}

	// No comparison was made for a bill with no rate, and the reason says so.
	bills = []domain.BillDue{{Name: "Foreign tax", Unconvertible: true}}
	v = domain.JudgeCashflow(last, true, usualLeft190k(), knownCash(10_000_000), bills)
	wantColour(t, v, domain.LightYellow, "bill_due_soon")
	if v.Reasons[0].BillName != "Foreign tax" || !v.Reasons[0].BillUnconvertible {
		t.Fatalf("reason = %+v, want Foreign tax, unconvertible", v.Reasons[0])
	}
}

func TestJudgeWorstColourWinsAndKeepsOnlyItsReasons(t *testing.T) {
	// Average left is zero (red) and last month is negative (yellow).
	v := domain.JudgeCashflow(solidMonth(100_000, 120_000), true, avgOf(3, 300_000, 300_000), knownCash(10_000_000), nil)
	if v.Colour != domain.LightRed || len(v.Reasons) != 1 || v.Reasons[0].Code != "left_not_positive" {
		t.Fatalf("got %s %v", v.Colour, codes(v))
	}
}

func TestJudgeGreyBeatsEverythingElse(t *testing.T) {
	v := domain.JudgeCashflow(domain.CashflowMonth{}, false, avgOf(3, 300_000, 300_000), domain.CashCushion{}, nil)
	if v.Colour != domain.LightGrey || len(v.Reasons) != 1 {
		t.Fatalf("got %s %v", v.Colour, codes(v))
	}
}

func TestJudgeGreen(t *testing.T) {
	// Kept 19%, cushion 4.2 months, nothing due.
	avg := avgOf(3, 300_000, 243_000)
	cash := int64(340_200) // 4.2 * 81,000
	v := domain.JudgeCashflow(solidMonth(100_000, 81_000), true, avg, knownCash(cash), nil)
	if v.Colour != domain.LightGreen || len(v.Reasons) != 0 {
		t.Fatalf("got %s %v", v.Colour, codes(v))
	}
}

func TestKeptBasisPointsRoundsHalfAway(t *testing.T) {
	if got, ok := domain.KeptBasisPoints(1_999, 10_000); !ok || got != 1_999 {
		t.Fatalf("got %d %v", got, ok)
	}
	if got, ok := domain.KeptBasisPoints(1, 3); !ok || got != 3_333 {
		t.Fatalf("got %d %v", got, ok)
	}
	if got, ok := domain.KeptBasisPoints(5, 100_000); !ok || got != 1 {
		t.Fatalf("half a basis point rounds away from zero: got %d %v", got, ok)
	}
	if got, ok := domain.KeptBasisPoints(-1, 3); !ok || got != -3_333 {
		t.Fatalf("got %d %v", got, ok)
	}
	if _, ok := domain.KeptBasisPoints(1, 0); ok {
		t.Fatal("income of 0 must report false")
	}
}

func TestCushionTenths(t *testing.T) {
	if got, ok := domain.CushionTenths(84_000, 60_000, 3); !ok || got != 42 {
		t.Fatalf("got %d %v", got, ok)
	}
	if _, ok := domain.CushionTenths(84_000, 0, 3); ok {
		t.Fatal("no spending must report false")
	}
	if _, ok := domain.CushionTenths(84_000, 60_000, 0); ok {
		t.Fatal("no months must report false")
	}
}

func TestCashflowAverageOfTakesThreeSolidMonthsBeforeCurrent(t *testing.T) {
	cur := cfMonth(2026, time.October)
	mk := func(m time.Month, st domain.CashflowStatus, in, out int64) domain.CashflowMonth {
		return domain.CashflowMonth{Month: cfMonth(2026, m), Status: st, InMinor: in, OutMinor: out}
	}
	months := []domain.CashflowMonth{
		mk(time.June, domain.CashflowSolid, 9_999, 9_999), // outside the window
		mk(time.July, domain.CashflowSolid, 100, 60),
		mk(time.August, domain.CashflowGrey, 5_000, 1),
		mk(time.September, domain.CashflowSolid, 200, 120),
		mk(time.October, domain.CashflowCurrent, 7_000, 7_000),
	}
	avg := domain.CashflowAverageOf(months, cur)
	if avg.Months != 2 || avg.SumIn != 300 || avg.SumOut != 180 || avg.SumLeft() != 120 {
		t.Fatalf("got %+v", avg)
	}
}

func TestJudgeIsNeverGreenWithNoSpendingToMeasureCushion(t *testing.T) {
	// Three solid months of income and no spending: the cushion is
	// cash / 0, undefined, even though the caller marked it Known.
	avg := avgOf(3, 300_000, 0)
	for _, cash := range []int64{50_000, -50_000} {
		v := domain.JudgeCashflow(solidMonth(100_000, 0), true, avg, knownCash(cash), nil)
		wantColour(t, v, domain.LightYellow, "cushion_unknown")
	}
}

func TestAveragePerMonthRoundsOnceFromTheSum(t *testing.T) {
	avg := domain.CashflowAverage{Months: 3, SumIn: 100, SumOut: 0}
	if got := avg.PerMonth(avg.SumIn); got != 33 {
		t.Errorf("100/3 = %d, want 33", got)
	}
	if got := avg.PerMonth(-50); got != -17 {
		t.Errorf("-50/3 = %d, want -17 (half away from zero)", got)
	}
	if got := (domain.CashflowAverage{}).PerMonth(500); got != 0 {
		t.Errorf("no months averaged = %d, want 0", got)
	}
}

func TestJudgeIsNeverGreenOnFewerThanThreeAveragedMonths(t *testing.T) {
	last := solidMonth(100_000, 81_000)
	for _, months := range []int{1, 2} {
		// Otherwise green: 19% kept, a large cushion, nothing due.
		avg := avgOf(months, int64(months)*100_000, int64(months)*81_000)
		v := domain.JudgeCashflow(last, true, avg, knownCash(10_000_000), nil)
		wantColour(t, v, domain.LightYellow, domain.ReasonShortAverage)
	}
	v := domain.JudgeCashflow(last, true, avgOf(3, 300_000, 243_000), knownCash(10_000_000), nil)
	if v.Colour != domain.LightGreen {
		t.Fatalf("three months should be green, got %s %v", v.Colour, codes(v))
	}
}

func TestJudgeShortAverageDoesNotHideRed(t *testing.T) {
	v := domain.JudgeCashflow(solidMonth(100_000, 120_000), true, avgOf(1, 100_000, 120_000), knownCash(10_000_000), nil)
	if v.Colour != domain.LightRed || hasCode(v, domain.ReasonShortAverage) {
		t.Fatalf("got %s %v", v.Colour, codes(v))
	}
}
