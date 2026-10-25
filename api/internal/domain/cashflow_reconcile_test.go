package domain_test

import (
	"reflect"
	"testing"
	"time"

	"github.com/andreasoentoro/hearth/api/internal/domain"
)

func out(on time.Time, amt int64) domain.CashflowItem {
	return domain.CashflowItem{OccurredOn: on, Role: domain.CashflowOut, AmountMinor: amt, Spread: 1}
}

func bill(on time.Time, name string, amt int64, spread int) domain.CashflowItem {
	return domain.CashflowItem{OccurredOn: on, Role: domain.CashflowSpreadOut, AmountMinor: amt, Spread: spread, Name: name}
}

func loan(on time.Time, name string, amt int64) domain.CashflowItem {
	return domain.CashflowItem{OccurredOn: on, Role: domain.CashflowLoanOut, AmountMinor: amt, Spread: 1, Name: name}
}

func reconcileMonths(t *testing.T, items []domain.CashflowItem, first, current time.Time) []domain.CashflowMonth {
	t.Helper()
	months, err := domain.BuildCashflowMonths(items, first, current)
	if err != nil {
		t.Fatal(err)
	}
	return months
}

// assertAddsUp checks the rule the sum exists for: spent plus every line is money out.
func assertAddsUp(t *testing.T, m domain.CashflowMonth, r domain.CashflowReconciliation) {
	t.Helper()
	total := r.SpentMinor
	for _, l := range r.Lines {
		total += l.AmountMinor
	}
	if total != m.OutMinor {
		t.Fatalf("spent %d + lines = %d, want money out %d", r.SpentMinor, total, m.OutMinor)
	}
}

func septemberItems() []domain.CashflowItem {
	sep := func(d int) time.Time { return cfDay(2026, time.September, d) }
	return []domain.CashflowItem{
		out(sep(2), 200000), out(sep(9), 100000), out(sep(15), 9450),
		bill(sep(20), "Car insurance", 210000, 12),
		loan(sep(28), "Car loan", 80000),
	}
}

func TestReconcileSeptemberAddsUpToMoneyOut(t *testing.T) {
	months := reconcileMonths(t, septemberItems(), cfMonth(2026, time.September), cfMonth(2026, time.October))
	sep := months[0]
	r := domain.ReconcileCashflowMonth(sep)
	if r.Status != domain.ReconcileShown || r.SpentMinor != 519450 {
		t.Fatalf("status=%s spent=%d", r.Status, r.SpentMinor)
	}
	paid := cfDay(2026, time.September, 20)
	want := []domain.CashflowSumLine{
		{Kind: domain.LineBillInFull, Name: "Car insurance", AmountMinor: -210000, PaidOn: paid, Spread: 12},
		{Kind: domain.LineBillShare, Name: "Car insurance", AmountMinor: 17500, PaidOn: paid, ShareIndex: 1, Spread: 12},
		{Kind: domain.LineLoan, Name: "Car loan", AmountMinor: 80000, PaidOn: cfDay(2026, time.September, 28)},
	}
	if !reflect.DeepEqual(r.Lines, want) {
		t.Fatalf("lines = %+v", r.Lines)
	}
	if sep.OutMinor != 406950 {
		t.Fatalf("out = %d", sep.OutMinor)
	}
	assertAddsUp(t, sep, r)
}

func TestReconcileLaterMonthCarriesOnlyTheShare(t *testing.T) {
	months := reconcileMonths(t, septemberItems(), cfMonth(2026, time.September), cfMonth(2026, time.October))
	oct := months[1]
	r := domain.ReconcileCashflowMonth(oct)
	if r.Status != domain.ReconcileShown || len(r.Lines) != 1 {
		t.Fatalf("status=%s lines=%+v", r.Status, r.Lines)
	}
	if l := r.Lines[0]; l.Kind != domain.LineBillShare || l.ShareIndex != 2 || l.AmountMinor != 17500 {
		t.Fatalf("line = %+v", l)
	}
	if r.SpentMinor != 0 {
		t.Fatalf("spent = %d, the bill must not be counted in October", r.SpentMinor)
	}
	assertAddsUp(t, oct, r)
}

func TestReconcileQuarterlyBill(t *testing.T) {
	items := []domain.CashflowItem{bill(cfDay(2026, time.September, 3), "Water", 30000, 3)}
	m := reconcileMonths(t, items, cfMonth(2026, time.September), cfMonth(2026, time.September))[0]
	r := domain.ReconcileCashflowMonth(m)
	if len(r.Lines) != 2 || r.Lines[1].ShareIndex != 1 || r.Lines[1].Spread != 3 || r.Lines[1].AmountMinor != 10000 {
		t.Fatalf("lines = %+v", r.Lines)
	}
	assertAddsUp(t, m, r)
}

func TestReconcileRemainderLandsOnTheFirstShare(t *testing.T) {
	items := []domain.CashflowItem{bill(cfDay(2026, time.September, 3), "Insurance", 100001, 12)}
	m := reconcileMonths(t, items, cfMonth(2026, time.September), cfMonth(2026, time.September))[0]
	r := domain.ReconcileCashflowMonth(m)
	if r.Status != domain.ReconcileShown || r.Lines[1].AmountMinor != 8338 {
		t.Fatalf("status=%s lines=%+v", r.Status, r.Lines)
	}
	assertAddsUp(t, m, r)
}

func TestReconcileTwoBillsSameNameSameDay(t *testing.T) {
	on := cfDay(2026, time.September, 5)
	items := []domain.CashflowItem{bill(on, "Insurance", 120000, 12), bill(on, "Insurance", 120000, 12)}
	m := reconcileMonths(t, items, cfMonth(2026, time.September), cfMonth(2026, time.September))[0]
	r := domain.ReconcileCashflowMonth(m)
	kinds := []domain.CashflowLineKind{}
	for _, l := range r.Lines {
		kinds = append(kinds, l.Kind)
	}
	want := []domain.CashflowLineKind{domain.LineBillInFull, domain.LineBillShare, domain.LineBillInFull, domain.LineBillShare}
	if r.Status != domain.ReconcileShown || !reflect.DeepEqual(kinds, want) {
		t.Fatalf("status=%s kinds=%v", r.Status, kinds)
	}
	assertAddsUp(t, m, r)
}

func TestReconcileBillPaidInTheCurrentMonth(t *testing.T) {
	cur := cfMonth(2026, time.October)
	items := []domain.CashflowItem{bill(cfDay(2026, time.October, 10), "Tax", 120000, 12)}
	m := reconcileMonths(t, items, cfMonth(2026, time.September), cur)[1]
	r := domain.ReconcileCashflowMonth(m)
	if r.Status != domain.ReconcileShown || len(r.Lines) != 2 || r.Lines[0].Kind != domain.LineBillInFull || r.Lines[1].ShareIndex != 1 {
		t.Fatalf("status=%s lines=%+v", r.Status, r.Lines)
	}
	assertAddsUp(t, m, r)
}

func TestReconcileBillPaidBeforeHistoryBegan(t *testing.T) {
	items := []domain.CashflowItem{bill(cfDay(2026, time.August, 20), "Insurance", 120000, 12)}
	m := reconcileMonths(t, items, cfMonth(2026, time.September), cfMonth(2026, time.September))[0]
	r := domain.ReconcileCashflowMonth(m)
	if r.Status != domain.ReconcileShown || len(r.Lines) != 1 || r.Lines[0].Kind != domain.LineBillShare || r.Lines[0].ShareIndex != 2 {
		t.Fatalf("status=%s lines=%+v", r.Status, r.Lines)
	}
	assertAddsUp(t, m, r)
}

func TestReconcileNothingToExplain(t *testing.T) {
	items := []domain.CashflowItem{out(cfDay(2026, time.September, 2), 5000)}
	m := reconcileMonths(t, items, cfMonth(2026, time.September), cfMonth(2026, time.September))[0]
	r := domain.ReconcileCashflowMonth(m)
	if r.Status != domain.ReconcileShown || r.Lines == nil || len(r.Lines) != 0 || r.SpentMinor != m.OutMinor {
		t.Fatalf("got %+v", r)
	}
}

func TestReconcileUnratedTransactionHidesTheSum(t *testing.T) {
	items := []domain.CashflowItem{{
		OccurredOn: cfDay(2026, time.September, 2), Role: domain.CashflowOut, Spread: 1, Unconvertible: "USD",
	}}
	m := reconcileMonths(t, items, cfMonth(2026, time.September), cfMonth(2026, time.September))[0]
	r := domain.ReconcileCashflowMonth(m)
	if r.Status != domain.ReconcileNoRate || r.UnratedCount != 1 || !reflect.DeepEqual(r.UnratedCurrencies, []string{"USD"}) || len(r.Lines) != 0 || r.Lines == nil {
		t.Fatalf("got %+v", r)
	}
}

func TestReconcileUnratedEarlierBillHidesTheSum(t *testing.T) {
	items := []domain.CashflowItem{{
		OccurredOn: cfDay(2026, time.August, 5), Role: domain.CashflowSpreadOut, Spread: 12, Name: "Insurance", Unconvertible: "USD",
	}}
	m := reconcileMonths(t, items, cfMonth(2026, time.August), cfMonth(2026, time.September))[1]
	r := domain.ReconcileCashflowMonth(m)
	if r.Status != domain.ReconcileNoRate || r.UnratedCount != 0 || !r.UnratedEarlierBill {
		t.Fatalf("got %+v", r)
	}
}

func TestReconcileMismatchHidesTheSum(t *testing.T) {
	m := reconcileMonths(t, septemberItems(), cfMonth(2026, time.September), cfMonth(2026, time.October))[0]
	m.OutMinor++
	r := domain.ReconcileCashflowMonth(m)
	if r.Status != domain.ReconcileMismatch || r.Lines == nil || len(r.Lines) != 0 {
		t.Fatalf("got %+v", r)
	}
}

func TestReconcileOrdersEarlierSharesThenLoans(t *testing.T) {
	items := []domain.CashflowItem{
		bill(cfDay(2026, time.August, 3), "Aug bill", 120000, 12),
		bill(cfDay(2026, time.July, 3), "Jul bill", 120000, 12),
		loan(cfDay(2026, time.September, 1), "Loan", 5000),
	}
	m := reconcileMonths(t, items, cfMonth(2026, time.July), cfMonth(2026, time.September))[2]
	r := domain.ReconcileCashflowMonth(m)
	names := []string{}
	for _, l := range r.Lines {
		names = append(names, l.Name)
	}
	if !reflect.DeepEqual(names, []string{"Jul bill", "Aug bill", "Loan"}) {
		t.Fatalf("order = %v", names)
	}
	assertAddsUp(t, m, r)
}
