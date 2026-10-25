package domain_test

import (
	"errors"
	"math"
	"testing"
	"time"

	"github.com/andreasoentoro/hearth/api/internal/domain"
)

func cfDay(y int, m time.Month, d int) time.Time {
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
}

func cfMonth(y int, m time.Month) time.Time { return cfDay(y, m, 1) }

func TestBuildMonthsCountsInOutAndLoan(t *testing.T) {
	on := cfDay(2026, time.March, 5)
	items := []domain.CashflowItem{
		{OccurredOn: on, Role: domain.CashflowIn, AmountMinor: 1_000_000, Spread: 1},
		{OccurredOn: on, Role: domain.CashflowOut, AmountMinor: 600_000, Spread: 1},
		{OccurredOn: on, Role: domain.CashflowLoanOut, AmountMinor: 180_000, Spread: 1, Name: "Car loan"},
	}
	months, err := domain.BuildCashflowMonths(items, cfMonth(2026, time.March), cfMonth(2026, time.March))
	if err != nil {
		t.Fatal(err)
	}
	m := months[0]
	if m.InMinor != 1_000_000 || m.OutMinor != 780_000 || m.LoanMinor != 180_000 || m.LeftMinor() != 220_000 {
		t.Fatalf("got in=%d out=%d loan=%d left=%d", m.InMinor, m.OutMinor, m.LoanMinor, m.LeftMinor())
	}
	if len(m.Contributions) != 1 || m.Contributions[0].Kind != "loan" || m.Contributions[0].Name != "Car loan" {
		t.Fatalf("contributions = %+v", m.Contributions)
	}
}

func TestBuildMonthsSpreadsAYearlyPaymentForward(t *testing.T) {
	items := []domain.CashflowItem{{
		OccurredOn: cfDay(2026, time.March, 14), Role: domain.CashflowSpreadOut,
		AmountMinor: 210_000, Spread: 12, Name: "Insurance",
	}}
	months, err := domain.BuildCashflowMonths(items, cfMonth(2026, time.January), cfMonth(2026, time.October))
	if err != nil {
		t.Fatal(err)
	}
	if len(months) != 10 {
		t.Fatalf("got %d months, want 10", len(months))
	}
	for i, m := range months {
		want := int64(17_500)
		if i < 2 {
			want = 0
		}
		if m.SpreadMinor != want || m.OutMinor != want {
			t.Errorf("month %s: spread=%d out=%d, want %d", m.Month.Format("2006-01"), m.SpreadMinor, m.OutMinor, want)
		}
	}
	march := months[2]
	if len(march.Contributions) != 1 || march.Contributions[0].Kind != "spread_bill" ||
		march.Contributions[0].Name != "Insurance" || !march.Contributions[0].PaidOn.Equal(cfDay(2026, time.March, 14)) {
		t.Fatalf("march contributions = %+v", march.Contributions)
	}
}

func TestBuildMonthsSpreadIntoTheFutureIsCutAtCurrent(t *testing.T) {
	items := []domain.CashflowItem{{
		OccurredOn: cfDay(2026, time.September, 2), Role: domain.CashflowSpreadOut,
		AmountMinor: 210_000, Spread: 12,
	}}
	months, err := domain.BuildCashflowMonths(items, cfMonth(2026, time.September), cfMonth(2026, time.October))
	if err != nil {
		t.Fatal(err)
	}
	if len(months) != 2 {
		t.Fatalf("got %d months, want 2", len(months))
	}
	for _, m := range months {
		if m.SpreadMinor != 17_500 {
			t.Errorf("month %s spread = %d, want 17500", m.Month.Format("2006-01"), m.SpreadMinor)
		}
	}
}

func TestBuildMonthsCountsEveryTransactionInTransactionCount(t *testing.T) {
	on := cfDay(2026, time.March, 5)
	items := []domain.CashflowItem{
		{OccurredOn: on, Role: domain.CashflowIgnore, AmountMinor: 50_000, Spread: 1},
		{OccurredOn: on, Role: domain.CashflowOut, AmountMinor: 100, Spread: 1},
	}
	months, err := domain.BuildCashflowMonths(items, cfMonth(2026, time.March), cfMonth(2026, time.March))
	if err != nil {
		t.Fatal(err)
	}
	if months[0].TransactionCount != 2 || months[0].OutMinor != 100 {
		t.Fatalf("count=%d out=%d, want 2 and 100", months[0].TransactionCount, months[0].OutMinor)
	}
}

func TestBuildMonthsSkipsItemsAfterTheCurrentMonth(t *testing.T) {
	items := []domain.CashflowItem{
		{OccurredOn: cfDay(2026, time.November, 1), Role: domain.CashflowOut, AmountMinor: 900, Spread: 1},
	}
	months, err := domain.BuildCashflowMonths(items, cfMonth(2026, time.October), cfMonth(2026, time.October))
	if err != nil {
		t.Fatal(err)
	}
	if months[0].OutMinor != 0 || months[0].TransactionCount != 0 {
		t.Fatalf("future item leaked: out=%d count=%d", months[0].OutMinor, months[0].TransactionCount)
	}
}

func TestBuildMonthsStatusIsCurrentOnlyForTheLastMonth(t *testing.T) {
	months, err := domain.BuildCashflowMonths(nil, cfMonth(2026, time.January), cfMonth(2026, time.March))
	if err != nil {
		t.Fatal(err)
	}
	want := []domain.CashflowStatus{domain.CashflowSolid, domain.CashflowSolid, domain.CashflowCurrent}
	for i, m := range months {
		if m.Status != want[i] {
			t.Errorf("month %d status = %s, want %s", i, m.Status, want[i])
		}
	}
}

func TestBuildMonthsCollectsUnconvertibleCurrenciesSortedAndUnique(t *testing.T) {
	on := cfDay(2026, time.March, 5)
	items := []domain.CashflowItem{
		{OccurredOn: on, Role: domain.CashflowOut, Unconvertible: "IDR", Spread: 1},
		{OccurredOn: on, Role: domain.CashflowIn, Unconvertible: "AUD", Spread: 1},
		{OccurredOn: on, Role: domain.CashflowOut, Unconvertible: "IDR", Spread: 1},
	}
	months, err := domain.BuildCashflowMonths(items, cfMonth(2026, time.March), cfMonth(2026, time.March))
	if err != nil {
		t.Fatal(err)
	}
	got := months[0].UnconvertibleCurrencies
	if len(got) != 2 || got[0] != "AUD" || got[1] != "IDR" {
		t.Fatalf("currencies = %v, want [AUD IDR]", got)
	}
}

func TestBuildMonthsFailsOnOverflow(t *testing.T) {
	on := cfDay(2026, time.March, 5)
	big := int64(math.MaxInt64/2 + 1)
	items := []domain.CashflowItem{
		{OccurredOn: on, Role: domain.CashflowOut, AmountMinor: big, Spread: 1},
		{OccurredOn: on, Role: domain.CashflowOut, AmountMinor: big, Spread: 1},
	}
	_, err := domain.BuildCashflowMonths(items, cfMonth(2026, time.March), cfMonth(2026, time.March))
	if !errors.Is(err, domain.ErrAmountOverflow) {
		t.Fatalf("err = %v, want ErrAmountOverflow", err)
	}
}

func TestBuildMonthsRefusesASpreadBelowOne(t *testing.T) {
	items := []domain.CashflowItem{{OccurredOn: cfDay(2026, time.March, 5), Role: domain.CashflowSpreadOut, AmountMinor: 10, Spread: 0}}
	_, err := domain.BuildCashflowMonths(items, cfMonth(2026, time.March), cfMonth(2026, time.March))
	if !errors.Is(err, domain.ErrUnknownCashflowInput) {
		t.Fatalf("err = %v, want ErrUnknownCashflowInput", err)
	}
}
