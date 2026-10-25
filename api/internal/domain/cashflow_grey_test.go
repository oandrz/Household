package domain_test

import (
	"slices"
	"testing"
	"time"

	"github.com/andreasoentoro/hearth/api/internal/domain"
)

// finishedMonths builds solid months starting January 2026 from money-out
// figures, each with income so only the usual-spend rule can grey them.
func finishedMonths(outs ...int64) []domain.CashflowMonth {
	months := make([]domain.CashflowMonth, 0, len(outs)+1)
	for i, out := range outs {
		months = append(months, domain.CashflowMonth{
			Month: time.Date(2026, time.Month(1+i), 1, 0, 0, 0, 0, time.UTC), Status: domain.CashflowSolid,
			InMinor: 1_000_000, OutMinor: out,
		})
	}
	return months
}

func lastIsGrey(t *testing.T, outs ...int64) bool {
	t.Helper()
	months := finishedMonths(outs...)
	domain.MarkCashflowGrey(months, nil)
	return months[len(months)-1].Status == domain.CashflowGrey
}

func TestGreyWhenNoIncome(t *testing.T) {
	months := finishedMonths(500)
	months[0].InMinor = 0
	domain.MarkCashflowGrey(months, nil)
	if months[0].Status != domain.CashflowGrey || !slices.Equal(months[0].Grey, []domain.GreyReason{domain.GreyNoIncome}) {
		t.Fatalf("status=%s grey=%v", months[0].Status, months[0].Grey)
	}
}

func TestUnderHalfOfUsualAppliesOnlyAfterThreeSolidMonths(t *testing.T) {
	if lastIsGrey(t, 400_000, 400_000, 100_000) {
		t.Fatal("month went grey with only 2 earlier solid months")
	}
	months := finishedMonths(400_000, 400_000, 400_000, 100_000)
	domain.MarkCashflowGrey(months, nil)
	if got := months[3]; got.Status != domain.CashflowGrey || !slices.Equal(got.Grey, []domain.GreyReason{domain.GreyUnderUsual}) {
		t.Fatalf("status=%s grey=%v, want grey under_half_usual", got.Status, got.Grey)
	}
}

func TestUsualIsTheMedianOfTheLastSixSolidMonths(t *testing.T) {
	// Last six of the seven are 400 x5 and 1000, so the middle pair is 400/400
	// and the oldest 100 is out of the window.
	base := []int64{100, 400, 400, 400, 400, 400, 1000}
	if !lastIsGrey(t, append(slices.Clone(base), 199)...) {
		t.Error("out 199 should be grey: 4*199 < 400+400")
	}
	if lastIsGrey(t, append(slices.Clone(base), 200)...) {
		t.Error("out 200 should stay solid: 4*200 == 400+400")
	}
}

func TestUsualWithAnOddCountUsesTheMiddleMonth(t *testing.T) {
	// Three earlier months: median 400, so 199 is grey and 200 is not.
	if !lastIsGrey(t, 100, 400, 900, 199) {
		t.Error("out 199 should be grey against median 400")
	}
	if lastIsGrey(t, 100, 400, 900, 200) {
		t.Error("out 200 should stay solid against median 400")
	}
}

func TestUsualSkipsGreyMonths(t *testing.T) {
	// Month 4 has no income, so it is grey and its out of 10 must not drag the
	// median down. Solid outs before month 7 are 400,400,400: median 400.
	months := finishedMonths(400, 400, 400, 10, 150)
	months[3].InMinor = 0
	domain.MarkCashflowGrey(months, nil)
	if months[3].Status != domain.CashflowGrey {
		t.Fatal("month 4 should be grey for no income")
	}
	if months[4].Status != domain.CashflowGrey {
		t.Fatal("150 is under half of 400, so month 5 should be grey")
	}
}

func TestConfirmationClearsNoIncomeAndUnderUsualButNotUnconvertible(t *testing.T) {
	months := finishedMonths(500, 500)
	months[0].InMinor = 0
	months[1].InMinor = 0
	months[1].UnconvertibleCurrencies = []string{"IDR"}
	confirmed := map[time.Time]bool{months[0].Month: true, months[1].Month: true}
	domain.MarkCashflowGrey(months, confirmed)

	if months[0].Status != domain.CashflowSolid || !months[0].Confirmed || len(months[0].Grey) != 0 {
		t.Errorf("confirmed no-income month: status=%s confirmed=%v grey=%v", months[0].Status, months[0].Confirmed, months[0].Grey)
	}
	if months[1].Status != domain.CashflowGrey || !slices.Equal(months[1].Grey, []domain.GreyReason{domain.GreyUnconvertible}) {
		t.Errorf("unconvertible month: status=%s grey=%v", months[1].Status, months[1].Grey)
	}
}

func TestConfirmedUnderUsualMonthCountsAsSolidForLaterMedians(t *testing.T) {
	months := finishedMonths(400, 400, 400, 100, 100, 100)
	confirmed := map[time.Time]bool{months[3].Month: true}
	domain.MarkCashflowGrey(months, confirmed)
	if months[3].Status != domain.CashflowSolid || !months[3].Confirmed {
		t.Fatalf("month 4: status=%s confirmed=%v", months[3].Status, months[3].Confirmed)
	}
	// Month 5: solid before it are 400,400,400,100 -> middle pair 400/400, so
	// 100 is under half. Month 6: 400,400,400,100 (month 5 grey) -> same.
	if months[4].Status != domain.CashflowGrey || months[5].Status != domain.CashflowGrey {
		t.Fatalf("months 5,6 = %s,%s, want grey", months[4].Status, months[5].Status)
	}
}

func TestUnconvertibleSpreadGreysEveryCoveredMonth(t *testing.T) {
	items := []domain.CashflowItem{
		{OccurredOn: cfDay(2026, time.January, 1), Role: domain.CashflowIn, AmountMinor: 100, Spread: 1},
		{OccurredOn: cfDay(2026, time.February, 1), Role: domain.CashflowIn, AmountMinor: 100, Spread: 1},
		{OccurredOn: cfDay(2026, time.March, 1), Role: domain.CashflowIn, AmountMinor: 100, Spread: 1},
		{OccurredOn: cfDay(2026, time.March, 20), Role: domain.CashflowSpreadOut, Unconvertible: "IDR", Spread: 12},
	}
	months, err := domain.BuildCashflowMonths(items, cfMonth(2026, time.January), cfMonth(2026, time.October))
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range months {
		if m.Month.Month() >= time.March && !slices.Equal(m.UnconvertibleCurrencies, []string{"IDR"}) {
			t.Errorf("%s currencies = %v, want [IDR]", m.Month.Format("2006-01"), m.UnconvertibleCurrencies)
		}
	}
	domain.MarkCashflowGrey(months, nil)
	for _, m := range months[:9] {
		covered := m.Month.Month() >= time.March
		isGrey := slices.Contains(m.Grey, domain.GreyUnconvertible)
		if covered != isGrey {
			t.Errorf("%s: unconvertible grey = %v, want %v", m.Month.Format("2006-01"), isGrey, covered)
		}
	}
}

func TestCurrentMonthIsNeverGrey(t *testing.T) {
	months := finishedMonths(500, 500)
	months[1].Status = domain.CashflowCurrent
	months[1].InMinor = 0
	months[1].UnconvertibleCurrencies = []string{"IDR"}
	domain.MarkCashflowGrey(months, nil)
	if months[1].Status != domain.CashflowCurrent || len(months[1].Grey) != 0 {
		t.Fatalf("status=%s grey=%v", months[1].Status, months[1].Grey)
	}
}
