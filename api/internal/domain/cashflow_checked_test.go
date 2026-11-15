package domain_test

import (
	"testing"
	"time"

	"github.com/andreasoentoro/hearth/api/internal/domain"
)

// These tests sit on the hand-off between the two guards against missing
// spending. "Spending under half of usual" can only run on a month with three
// solid months before it; until it has run on every month the light rests on,
// the light must stay at yellow. Each test runs the real pipeline: grey
// marking, the average, then the verdict.

// cfFigures is one finished month's money in and money out.
type cfFigures struct{ in, out int64 }

// judgeHistory lays out finished months from start, then the current month,
// and returns the months as marked and the page's verdict. Cash is large and
// nothing is due, so only the months themselves can hold the light back.
func judgeHistory(t *testing.T, start time.Time, finished []cfFigures, confirmed map[time.Time]bool) ([]domain.CashflowMonth, domain.CashflowVerdict) {
	t.Helper()
	months := make([]domain.CashflowMonth, 0, len(finished)+1)
	for i, f := range finished {
		months = append(months, domain.CashflowMonth{
			Month: start.AddDate(0, i, 0), Status: domain.CashflowSolid, InMinor: f.in, OutMinor: f.out,
		})
	}
	current := start.AddDate(0, len(finished), 0)
	months = append(months, domain.CashflowMonth{Month: current, Status: domain.CashflowCurrent})
	domain.MarkCashflowGrey(months, confirmed)
	avg := domain.CashflowAverageOf(months, current)
	v := domain.JudgeCashflow(months[len(months)-2], true, avg, knownCash(100_000_000), nil)
	return months, v
}

func TestThreeFinishedMonthsWithASparseLastMonthAreNeverGreen(t *testing.T) {
	// September's S$94.50 is almost certainly spending nobody entered. With
	// only two months before it, the under-half-of-usual check cannot run, so
	// nothing has shown September is real.
	_, v := judgeHistory(t, cfMonth(2026, time.July), []cfFigures{
		{500_000, 400_000}, {500_000, 400_000}, {500_000, 9_450},
	}, nil)
	wantColour(t, v, domain.LightYellow, domain.ReasonSpendingNotChecked)
}

func TestAGreyMonthBeforeTheWindowKeepsTheLightAtYellow(t *testing.T) {
	// January has no income, so it is grey and does not count as usual.
	// April then has only February and March before it: its check never ran,
	// although the window February to April holds three solid months.
	_, v := judgeHistory(t, cfMonth(2026, time.January), []cfFigures{
		{0, 300_000}, {500_000, 300_000}, {500_000, 300_000}, {500_000, 300_000},
	}, nil)
	wantColour(t, v, domain.LightYellow, domain.ReasonSpendingNotChecked)
}

func TestAnEarlierWindowMonthThatWasNeverCheckedKeepsTheLightAtYellow(t *testing.T) {
	// April, the last month, had three solid months before it and was
	// checked. February and March, also in the window, were not: a missing
	// expense there would still inflate the share kept.
	steady := cfFigures{500_000, 300_000}
	months, v := judgeHistory(t, cfMonth(2026, time.January), []cfFigures{steady, steady, steady, steady}, nil)
	if !months[3].UsualChecked {
		t.Fatal("April should be checked")
	}
	wantColour(t, v, domain.LightYellow, domain.ReasonSpendingNotChecked)
}

func TestSixCheckedMonthsCanBeGreen(t *testing.T) {
	// April, May and June each have three solid months before them, so the
	// check ran on every month of the window.
	steady := cfFigures{500_000, 300_000}
	_, v := judgeHistory(t, cfMonth(2026, time.January), []cfFigures{
		steady, steady, steady, steady, steady, steady,
	}, nil)
	if v.Colour != domain.LightGreen || len(v.Reasons) != 0 {
		t.Fatalf("got %s %v, want green", v.Colour, codes(v))
	}
}

func TestAConfirmedMonthCountsAsChecked(t *testing.T) {
	// March has no income and only two months before it, so its check never
	// ran. Someone confirmed it, and "count it" is their word that the month
	// is right. April and May have three solid months before them.
	confirmed := map[time.Time]bool{cfMonth(2026, time.March): true}
	months, v := judgeHistory(t, cfMonth(2026, time.January), []cfFigures{
		{500_000, 300_000}, {500_000, 300_000}, {0, 300_000}, {600_000, 300_000}, {600_000, 300_000},
	}, confirmed)
	if march := months[2]; !march.Confirmed || !march.UsualChecked {
		t.Fatalf("March confirmed=%v checked=%v, want both", march.Confirmed, march.UsualChecked)
	}
	if v.Colour != domain.LightGreen {
		t.Fatalf("got %s %v, want green", v.Colour, codes(v))
	}
}

func TestMarkCashflowGreyRecordsWhetherTheUsualCheckRan(t *testing.T) {
	months := finishedMonths(400, 400, 400, 400, 100)
	domain.MarkCashflowGrey(months, nil)
	for i, want := range []bool{false, false, false, true, true} {
		if months[i].UsualChecked != want {
			t.Errorf("month %d checked = %v, want %v", i+1, months[i].UsualChecked, want)
		}
	}
}

func TestAStoredConfirmationOnASolidMonthDoesNotCountAsChecked(t *testing.T) {
	// The confirmation clears nothing on a month with no judgement, so it is
	// not in effect and cannot stand in for the check.
	months := finishedMonths(400)
	domain.MarkCashflowGrey(months, map[time.Time]bool{months[0].Month: true})
	if months[0].Confirmed || months[0].UsualChecked {
		t.Fatalf("confirmed=%v checked=%v, want neither", months[0].Confirmed, months[0].UsualChecked)
	}
}

func TestCashflowAverageCountsTheMonthsNotChecked(t *testing.T) {
	cur := cfMonth(2026, time.October)
	mk := func(m time.Month, st domain.CashflowStatus, checked bool) domain.CashflowMonth {
		return domain.CashflowMonth{Month: cfMonth(2026, m), Status: st, InMinor: 100, OutMinor: 60, UsualChecked: checked}
	}
	months := []domain.CashflowMonth{
		mk(time.June, domain.CashflowSolid, false), // outside the window
		mk(time.July, domain.CashflowSolid, false),
		mk(time.August, domain.CashflowGrey, false), // grey months are not averaged
		mk(time.September, domain.CashflowSolid, true),
		mk(time.October, domain.CashflowCurrent, false),
	}
	avg := domain.CashflowAverageOf(months, cur)
	if avg.Months != 2 || avg.Unchecked != 1 {
		t.Fatalf("got %+v, want 2 months with 1 unchecked", avg)
	}
}

func TestJudgeYellowWhenAnyMonthItRestsOnWasNotChecked(t *testing.T) {
	checked := solidMonth(100_000, 81_000)
	unchecked := checked
	unchecked.UsualChecked = false

	v := domain.JudgeCashflow(unchecked, true, healthy(), knownCash(10_000_000), nil)
	wantColour(t, v, domain.LightYellow, domain.ReasonSpendingNotChecked)

	avg := healthy()
	avg.Unchecked = 1
	v = domain.JudgeCashflow(checked, true, avg, knownCash(10_000_000), nil)
	wantColour(t, v, domain.LightYellow, domain.ReasonSpendingNotChecked)
	if hasCode(v, domain.ReasonShortAverage) {
		t.Fatalf("three averaged months are not short: %v", codes(v))
	}
}

func TestSpendingNotCheckedDoesNotHideRedOrGrey(t *testing.T) {
	unchecked := solidMonth(100_000, 120_000)
	unchecked.UsualChecked = false
	avg := avgOf(3, 300_000, 300_000)
	avg.Unchecked = 3
	v := domain.JudgeCashflow(unchecked, true, avg, knownCash(10_000_000), nil)
	if v.Colour != domain.LightRed || hasCode(v, domain.ReasonSpendingNotChecked) {
		t.Fatalf("got %s %v, want red alone", v.Colour, codes(v))
	}
	unchecked.Status = domain.CashflowGrey
	v = domain.JudgeCashflow(unchecked, true, avg, knownCash(10_000_000), nil)
	if v.Colour != domain.LightGrey || hasCode(v, domain.ReasonSpendingNotChecked) {
		t.Fatalf("got %s %v, want grey alone", v.Colour, codes(v))
	}
}
