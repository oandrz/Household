package usecase_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/andreasoentoro/hearth/api/internal/domain"
	"github.com/andreasoentoro/hearth/api/internal/usecase"
)

// cashflowToday is the household's calendar day in every test here.
var cashflowToday = time.Date(2026, 7, 20, 0, 0, 0, 0, time.UTC)

type cashflowFixture struct {
	svc      *usecase.CashflowService
	rows     *cashflowRowsDouble
	confs    *cashflowConfirmationDouble
	accounts *fakeAccountRepo
	bills    *fakeBillRepo
	fx       *fxDouble
}

func newCashflowFixture(t *testing.T) *cashflowFixture {
	t.Helper()
	households := newHouseholdDouble()
	households.put(domain.Household{ID: "house-1", PrimaryCurrency: "SGD"})
	f := &cashflowFixture{
		rows:     &cashflowRowsDouble{},
		confs:    &cashflowConfirmationDouble{},
		accounts: newFakeAccountRepo(),
		bills:    &fakeBillRepo{},
		fx:       newFXDouble(),
	}
	f.svc = usecase.NewCashflowService(usecase.CashflowDeps{
		Households: households, Rows: f.rows, Confirmations: f.confs,
		Accounts: f.accounts, Bills: f.bills, FX: f.fx,
	})
	return f
}

func cfDay(y int, m time.Month, d int) time.Time { return time.Date(y, m, d, 0, 0, 0, 0, time.UTC) }

func sgd(minor int64) domain.Money { return domain.Money{Amount: minor, Currency: "SGD"} }

func (f *cashflowFixture) add(row usecase.CashflowRow) { f.rows.rows = append(f.rows.rows, row) }

func (f *cashflowFixture) income(on time.Time, m domain.Money) {
	f.add(usecase.CashflowRow{OccurredOn: on, Kind: domain.TransactionIncome, Amount: m})
}

func (f *cashflowFixture) expense(on time.Time, m domain.Money) {
	f.add(usecase.CashflowRow{OccurredOn: on, Kind: domain.TransactionExpense, Amount: m})
}

// solidQuarter plants April, May and June 2026 as three ordinary months:
// S$5,000 in and S$3,000 out each, so the 3-month average out is S$9,000.
func (f *cashflowFixture) solidQuarter() {
	for _, m := range []time.Month{time.April, time.May, time.June} {
		f.income(cfDay(2026, m, 25), sgd(500_000))
		f.expense(cfDay(2026, m, 10), sgd(300_000))
	}
}

func (f *cashflowFixture) addAccount(id string, typ domain.AccountType, balance domain.Money, countsTowardNetWorth bool, archived bool) {
	a := domain.Account{
		ID: id, HouseholdID: "house-1", Type: typ,
		OpeningBalance: balance, CountTowardNetWorth: countsTowardNetWorth,
	}
	if archived {
		at := cashflowToday
		a.ArchivedAt = &at
	}
	f.accounts.accounts[id] = a
}

func (f *cashflowFixture) addBill(id string, cadence domain.Cadence, amount int64, due time.Time, archived bool) {
	b := domain.Bill{
		ID: id, HouseholdID: "house-1", Name: id, Amount: sgd(amount),
		Cadence: cadence, NextDue: &due,
	}
	if archived {
		at := cashflowToday
		b.ArchivedAt = &at
	}
	f.bills.add(usecase.BillRecord{Bill: b})
}

func (f *cashflowFixture) page(t *testing.T) usecase.CashflowPage {
	t.Helper()
	p, err := f.svc.Page(context.Background(), "house-1", cashflowToday, 0)
	if err != nil {
		t.Fatalf("Page: %v", err)
	}
	return p
}

func hasReason(v domain.CashflowVerdict, code string) bool {
	for _, r := range v.Reasons {
		if r.Code == code {
			return true
		}
	}
	return false
}

func TestPageForAnEmptyHousehold(t *testing.T) {
	f := newCashflowFixture(t)
	for _, year := range []int{0, 2026} {
		p, err := f.svc.Page(context.Background(), "house-1", cashflowToday, year)
		if err != nil {
			t.Fatalf("year %d: %v", year, err)
		}
		if p.HasTransactions || p.OldestYear != 0 {
			t.Fatalf("year %d: HasTransactions=%v OldestYear=%d", year, p.HasTransactions, p.OldestYear)
		}
		if p.Light.Colour != domain.LightGrey || !hasReason(p.Light, domain.ReasonNoFinishedMonth) {
			t.Fatalf("year %d: light = %+v, want grey no_finished_month", year, p.Light)
		}
	}
	if _, err := f.svc.Page(context.Background(), "house-1", cashflowToday, 2025); !errors.Is(err, domain.ErrCashflowYearOutOfRange) {
		t.Fatalf("year 2025 with no history: err = %v, want out of range", err)
	}
}

func TestPageForAHouseholdThatStartedThisMonth(t *testing.T) {
	f := newCashflowFixture(t)
	f.income(cfDay(2026, time.July, 5), sgd(500_000))
	p := f.page(t)
	if p.LastFinished != nil {
		t.Fatalf("LastFinished = %+v, want nil", p.LastFinished)
	}
	if p.Light.Colour != domain.LightGrey || !hasReason(p.Light, domain.ReasonNoFinishedMonth) {
		t.Fatalf("light = %+v, want grey no_finished_month", p.Light)
	}
	if p.Recent[11].Status != domain.CashflowCurrent || p.Recent[10].Status != domain.CashflowNone {
		t.Fatalf("recent tail = %v, %v; want current then none", p.Recent[11].Status, p.Recent[10].Status)
	}
}

func TestPageMoneyOutMatchesMonthSummaryAfterAdjustments(t *testing.T) {
	// The ledger and the cashflow page read the same June transactions.
	ledger, ledgerRepo := transactionFixture(t)
	f := newCashflowFixture(t)
	june := cfDay(2026, time.June, 12)
	idr := domain.Money{Amount: 1_241_000, Currency: "IDR"}
	yearly := sgd(120_100) // spreads as 10,008 x 12 with the remainder in June

	f.income(june, sgd(900_000))
	f.expense(june, sgd(10_000))
	f.expense(june, idr)
	f.add(usecase.CashflowRow{OccurredOn: june, Kind: domain.TransactionExpense, Amount: yearly,
		BillCadence: domain.CadenceYearly, BillName: "Insurance"})
	f.add(usecase.CashflowRow{OccurredOn: june, Kind: domain.TransactionTransfer, Amount: sgd(30_000),
		ToAccountType: domain.AccountLoan, ToAccountName: "Car loan"})
	ledgerRepo.transactions = append(ledgerRepo.transactions, []domain.Transaction{
		{HouseholdID: "house-1", Kind: domain.TransactionIncome, OccurredOn: june, Amount: sgd(900_000)},
		{HouseholdID: "house-1", Kind: domain.TransactionExpense, OccurredOn: june, Amount: sgd(10_000)},
		{HouseholdID: "house-1", Kind: domain.TransactionExpense, OccurredOn: june, Amount: idr},
		{HouseholdID: "house-1", Kind: domain.TransactionExpense, OccurredOn: june, Amount: yearly},
		{HouseholdID: "house-1", Kind: domain.TransactionTransfer, OccurredOn: june, Amount: sgd(30_000)},
	}...)

	summary, err := ledger.MonthSummary(context.Background(), "house-1", june)
	if err != nil {
		t.Fatalf("MonthSummary: %v", err)
	}
	juneMonth := f.page(t).Recent[10]
	if juneMonth.SpreadMinor != 10_012 || juneMonth.LoanMinor != 30_000 {
		t.Fatalf("spread = %d, loan = %d; want 10012 and 30000", juneMonth.SpreadMinor, juneMonth.LoanMinor)
	}
	got := juneMonth.OutMinor - juneMonth.SpreadMinor - juneMonth.LoanMinor + yearly.Amount
	if got != summary.Spent.Amount {
		t.Fatalf("adjusted money out = %d, MonthSummary.Spent = %d", got, summary.Spent.Amount)
	}
}

func TestPageGreysAMonthWithAnUnconvertibleTransaction(t *testing.T) {
	f := newCashflowFixture(t)
	f.fx.withNoRates()
	f.income(cfDay(2026, time.June, 25), sgd(500_000))
	f.expense(cfDay(2026, time.June, 10), domain.Money{Amount: 1_000_000, Currency: "IDR"})
	june := f.page(t).Recent[10]
	if june.Status != domain.CashflowGrey {
		t.Fatalf("June status = %v, want grey", june.Status)
	}
	if len(june.UnconvertibleCurrencies) != 1 || june.UnconvertibleCurrencies[0] != "IDR" {
		t.Fatalf("unconvertible = %v, want [IDR]", june.UnconvertibleCurrencies)
	}
	if june.OutMinor != 0 {
		t.Fatalf("OutMinor = %d, the unconvertible amount must not be added", june.OutMinor)
	}
}

func TestPageDoesNotGreyAMonthForAnIgnoredTransferWithoutARate(t *testing.T) {
	f := newCashflowFixture(t)
	f.fx.withNoRates()
	f.income(cfDay(2026, time.June, 25), sgd(500_000))
	f.add(usecase.CashflowRow{OccurredOn: cfDay(2026, time.June, 3), Kind: domain.TransactionTransfer,
		Amount: domain.Money{Amount: 1_000_000, Currency: "IDR"}, ToAccountType: domain.AccountCash})
	june := f.page(t).Recent[10]
	if len(june.UnconvertibleCurrencies) != 0 {
		t.Fatalf("unconvertible = %v, an ignored transfer needs no rate", june.UnconvertibleCurrencies)
	}
	if june.TransactionCount != 2 {
		t.Fatalf("TransactionCount = %d, want the ignored transfer counted too", june.TransactionCount)
	}
}

func TestPageRefusesAnUnknownTransactionKind(t *testing.T) {
	f := newCashflowFixture(t)
	f.add(usecase.CashflowRow{OccurredOn: cfDay(2026, time.June, 3), Kind: "gift", Amount: sgd(100)})
	if _, err := f.svc.Page(context.Background(), "house-1", cashflowToday, 0); !errors.Is(err, domain.ErrUnknownCashflowInput) {
		t.Fatalf("err = %v, want ErrUnknownCashflowInput", err)
	}
}

func TestPageCushionUsesOnlyCashAccountsThatCountTowardNetWorth(t *testing.T) {
	f := newCashflowFixture(t)
	f.solidQuarter()
	f.addAccount("cash-counted", domain.AccountCash, sgd(840_000), true, false)
	f.addAccount("cash-not-counted", domain.AccountCash, sgd(500_000), false, false)
	f.addAccount("investment", domain.AccountInvestment, sgd(5_000_000), true, false)
	f.addAccount("cash-archived", domain.AccountCash, sgd(100_000), true, true)
	p := f.page(t)
	if !p.Cushion.Known || p.Cushion.CashMinor != 840_000 {
		t.Fatalf("cushion = %+v, want known with S$8,400", p.Cushion)
	}
	if p.CushionTenths == nil || *p.CushionTenths != 28 {
		t.Fatalf("tenths = %v, want 28 (S$8,400 over S$3,000 a month)", p.CushionTenths)
	}
}

func TestPageCushionUnknownWithNoCashAccounts(t *testing.T) {
	f := newCashflowFixture(t)
	f.solidQuarter()
	f.addAccount("investment", domain.AccountInvestment, sgd(5_000_000), true, false)
	p := f.page(t)
	if p.Cushion.Known || p.Cushion.Reason != domain.CushionNoCashAccounts || p.CushionTenths != nil {
		t.Fatalf("cushion = %+v tenths %v, want unknown no_cash_accounts", p.Cushion, p.CushionTenths)
	}
	if p.Light.Colour == domain.LightGreen {
		t.Fatal("the light must not be green when the cushion is unknown")
	}
}

func TestPageCushionUnknownWhenACashAccountCannotConvert(t *testing.T) {
	f := newCashflowFixture(t)
	f.fx.withNoRates()
	f.solidQuarter()
	f.addAccount("sgd-cash", domain.AccountCash, sgd(840_000), true, false)
	f.addAccount("idr-cash", domain.AccountCash, domain.Money{Amount: 100_000_000, Currency: "IDR"}, true, false)
	p := f.page(t)
	if p.Cushion.Known || p.Cushion.Reason != domain.CushionUnconvertibleAccount {
		t.Fatalf("cushion = %+v, want unknown unconvertible_account", p.Cushion)
	}
}

func TestPageCushionUnknownWithoutSolidMonthsOrSpending(t *testing.T) {
	f := newCashflowFixture(t)
	f.addAccount("cash", domain.AccountCash, sgd(840_000), true, false)
	f.income(cfDay(2026, time.July, 5), sgd(500_000))
	if got := f.page(t).Cushion.Reason; got != domain.CushionNoSolidMonths {
		t.Fatalf("reason = %q, want no_solid_months", got)
	}

	f = newCashflowFixture(t)
	f.addAccount("cash", domain.AccountCash, sgd(840_000), true, false)
	f.confs.rows = []usecase.CashflowConfirmation{{Month: cfDay(2026, time.June, 1)}}
	f.income(cfDay(2026, time.June, 25), sgd(500_000)) // income, but nothing spent
	p := f.page(t)
	if p.Cushion.Reason != domain.CushionNoSpending || p.Cushion.Known || p.CushionTenths != nil {
		t.Fatalf("cushion = %+v, want unknown no_spending", p.Cushion)
	}
}

func TestPageDueSoonTakesLiveQuarterlyAndYearlyBillsWithin60Days(t *testing.T) {
	// Average left over is S$2,000 a month, so a S$2,500 bill is a warning.
	due := func(days int) time.Time { return cashflowToday.AddDate(0, 0, days) }
	cases := []struct {
		name    string
		cadence domain.Cadence
		due     time.Time
		archive bool
		want    bool
	}{
		{"yearly due in exactly 60 days", domain.CadenceYearly, due(60), false, true},
		{"yearly due in 61 days", domain.CadenceYearly, due(61), false, false},
		{"quarterly due tomorrow", domain.CadenceQuarterly, due(1), false, true},
		{"overdue yearly bill", domain.CadenceYearly, due(-10), false, true},
		{"monthly bill", domain.CadenceMonthly, due(5), false, false},
		{"archived yearly bill", domain.CadenceYearly, due(5), true, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newCashflowFixture(t)
			f.solidQuarter()
			f.addAccount("cash", domain.AccountCash, sgd(5_000_000), true, false)
			f.addBill("bill", tc.cadence, 250_000, tc.due, tc.archive)
			got := hasReason(f.page(t).Light, domain.ReasonBillDueSoon)
			if got != tc.want {
				t.Fatalf("bill_due_soon = %v, want %v (light %+v)", got, tc.want, f.page(t).Light)
			}
		})
	}
}

func TestPageBillsToComeOnlyForTheCurrentYear(t *testing.T) {
	f := newCashflowFixture(t)
	f.income(cfDay(2025, time.November, 25), sgd(500_000)) // history reaches back to 2025
	f.addBill("december", domain.CadenceQuarterly, 100, cfDay(2026, time.December, 31), false)
	f.addBill("september", domain.CadenceYearly, 200, cfDay(2026, time.September, 1), false)
	f.addBill("overdue", domain.CadenceYearly, 300, cfDay(2026, time.June, 1), false)
	f.addBill("next-year", domain.CadenceYearly, 400, cfDay(2027, time.January, 1), false)
	f.addBill("monthly", domain.CadenceMonthly, 500, cfDay(2026, time.August, 1), false)
	f.addBill("archived", domain.CadenceYearly, 600, cfDay(2026, time.August, 1), true)

	p := f.page(t)
	var names []string
	for _, b := range p.BillsToCome {
		names = append(names, b.Name)
	}
	want := []string{"overdue", "september", "december"}
	if len(names) != len(want) {
		t.Fatalf("bills to come = %v, want %v", names, want)
	}
	for i := range want {
		if names[i] != want[i] {
			t.Fatalf("bills to come = %v, want %v", names, want)
		}
	}

	last, err := f.svc.Page(context.Background(), "house-1", cashflowToday, 2025)
	if err != nil {
		t.Fatalf("Page 2025: %v", err)
	}
	if len(last.BillsToCome) != 0 {
		t.Fatalf("a past year lists bills to come: %v", last.BillsToCome)
	}
}

// A bill whose due date has passed stays in the list but is marked overdue,
// by the same rule as the Bills page: due today is not overdue yet.
func TestPageBillsToComeMarksOnlyPastDueBillsOverdue(t *testing.T) {
	f := newCashflowFixture(t)
	f.solidQuarter()
	f.addBill("yesterday", domain.CadenceYearly, 100, cashflowToday.AddDate(0, 0, -1), false)
	f.addBill("today", domain.CadenceYearly, 200, cashflowToday, false)
	f.addBill("tomorrow", domain.CadenceYearly, 300, cashflowToday.AddDate(0, 0, 1), false)

	got := map[string]bool{}
	for _, b := range f.page(t).BillsToCome {
		got[b.Name] = b.Overdue
	}
	want := map[string]bool{"yesterday": true, "today": false, "tomorrow": false}
	for name, overdue := range want {
		if v, ok := got[name]; !ok || v != overdue {
			t.Fatalf("bills to come overdue = %v, want %v", got, want)
		}
	}
}

func TestPageBillWithoutARateIsUnconvertibleAndFiresTheBillDueCheck(t *testing.T) {
	f := newCashflowFixture(t)
	f.fx.withNoRates()
	f.solidQuarter()
	f.addAccount("cash", domain.AccountCash, sgd(5_000_000), true, false)
	f.bills.add(usecase.BillRecord{Bill: domain.Bill{
		ID: "b", HouseholdID: "house-1", Name: "Rent", Cadence: domain.CadenceYearly,
		Amount: domain.Money{Amount: 1000, Currency: "IDR"}, NextDue: ptr(cashflowToday.AddDate(0, 0, 10)),
	}})
	p := f.page(t)
	if !hasReason(p.Light, domain.ReasonBillDueSoon) {
		t.Fatalf("light = %+v, want bill_due_soon", p.Light)
	}
	if len(p.BillsToCome) != 1 || !p.BillsToCome[0].Unconvertible || p.BillsToCome[0].AmountMinor != 0 {
		t.Fatalf("bills to come = %+v, want one unconvertible bill of 0", p.BillsToCome)
	}
}

func ptr[T any](v T) *T { return &v }

func TestPageYearOutOfRange(t *testing.T) {
	f := newCashflowFixture(t)
	f.income(cfDay(2025, time.November, 25), sgd(500_000))
	for _, year := range []int{1999, 2024, 2027} {
		if _, err := f.svc.Page(context.Background(), "house-1", cashflowToday, year); !errors.Is(err, domain.ErrCashflowYearOutOfRange) {
			t.Fatalf("year %d: err = %v, want out of range", year, err)
		}
	}
	if _, err := f.svc.Page(context.Background(), "house-1", cashflowToday, 2025); err != nil {
		t.Fatalf("the oldest year must be allowed: %v", err)
	}
}

func TestPageAppliesConfirmations(t *testing.T) {
	f := newCashflowFixture(t)
	f.income(cfDay(2026, time.April, 25), sgd(500_000))
	f.expense(cfDay(2026, time.April, 10), sgd(300_000))
	f.income(cfDay(2026, time.May, 25), sgd(500_000))
	f.expense(cfDay(2026, time.May, 10), sgd(300_000))
	f.expense(cfDay(2026, time.June, 10), sgd(300_000)) // June: spending, no income

	if got := f.page(t).Recent[10]; got.Status != domain.CashflowGrey {
		t.Fatalf("before confirming, June = %v, want grey", got.Status)
	}

	// A stored month that is not exactly midnight UTC must still match.
	confirmed := time.Date(2026, time.June, 1, 0, 0, 0, 0, time.FixedZone("SGT", 8*3600))
	f.confs.rows = []usecase.CashflowConfirmation{{Month: confirmed, ConfirmedByName: "Ethan"}}
	p := f.page(t)
	if june := p.Recent[10]; june.Status != domain.CashflowSolid || !june.Confirmed {
		t.Fatalf("after confirming, June = %v confirmed=%v, want solid and confirmed", june.Status, june.Confirmed)
	}
	if c, ok := p.Confirmations[cfDay(2026, time.June, 1)]; !ok || c.ConfirmedByName != "Ethan" {
		t.Fatalf("confirmations = %v, want June keyed at midnight UTC", p.Confirmations)
	}
}

func TestConfirmRefusesMonthsOutsideTheWindow(t *testing.T) {
	f := newCashflowFixture(t)
	for _, month := range []time.Time{
		cfDay(2026, time.July, 1),   // the current month
		cfDay(2026, time.August, 1), // next month
		cfDay(1999, time.December, 1),
		cfDay(2026, time.June, 15), // not the first
	} {
		_, err := f.svc.Confirm(context.Background(), "house-1", month, "m-1", cashflowToday)
		if !errors.Is(err, domain.ErrCashflowMonthOutOfRange) {
			t.Fatalf("%s: err = %v, want out of range", month.Format("2006-01-02"), err)
		}
	}
	if f.confs.calls != 0 {
		t.Fatalf("a refused month reached the repository %d times", f.confs.calls)
	}
	got, err := f.svc.Confirm(context.Background(), "house-1", cfDay(2026, time.June, 1), "m-9", cashflowToday)
	if err != nil {
		t.Fatalf("last month: %v", err)
	}
	if f.confs.lastMembers != "m-9" || !got.Month.Equal(cfDay(2026, time.June, 1)) {
		t.Fatalf("stored membership %q, month %v", f.confs.lastMembers, got.Month)
	}
}

func TestUnconfirmRefusesTheSameWindow(t *testing.T) {
	f := newCashflowFixture(t)
	for _, month := range []time.Time{
		cfDay(2026, time.July, 1), cfDay(2026, time.August, 1), cfDay(1999, time.December, 1), cfDay(2026, time.June, 15),
	} {
		if err := f.svc.Unconfirm(context.Background(), "house-1", month, cashflowToday); !errors.Is(err, domain.ErrCashflowMonthOutOfRange) {
			t.Fatalf("%s: err = %v, want out of range", month.Format("2006-01-02"), err)
		}
	}
	if f.confs.calls != 0 {
		t.Fatalf("a refused month reached the repository %d times", f.confs.calls)
	}
	if err := f.svc.Unconfirm(context.Background(), "house-1", cfDay(2026, time.June, 1), cashflowToday); err != nil {
		t.Fatalf("last month: %v", err)
	}
}

func TestPagePropagatesAnFXFailure(t *testing.T) {
	f := newCashflowFixture(t)
	outage := errors.New("rate provider down")
	f.fx.failWith(outage)
	f.income(cfDay(2026, time.June, 25), sgd(500_000))
	f.expense(cfDay(2026, time.June, 10), domain.Money{Amount: 1_000_000, Currency: "IDR"})
	if _, err := f.svc.Page(context.Background(), "house-1", cashflowToday, 0); !errors.Is(err, outage) {
		t.Fatalf("err = %v, want the provider outage, not a grey month", err)
	}
}

func TestPagePropagatesAnFXFailureOnACashAccount(t *testing.T) {
	// Every transaction is in SGD, so only the IDR account asks for a rate.
	f := newCashflowFixture(t)
	f.solidQuarter()
	f.addAccount("idr-cash", domain.AccountCash, domain.Money{Amount: 100_000_000, Currency: "IDR"}, true, false)
	outage := errors.New("rate provider down")
	f.fx.failWith(outage)
	if _, err := f.svc.Page(context.Background(), "house-1", cashflowToday, 0); !errors.Is(err, outage) {
		t.Fatalf("err = %v, want the provider outage, not an unknown cushion", err)
	}
}

func TestPagePropagatesAnFXFailureOnABill(t *testing.T) {
	// Every transaction is in SGD, so only the IDR bill asks for a rate.
	f := newCashflowFixture(t)
	f.solidQuarter()
	f.bills.add(usecase.BillRecord{Bill: domain.Bill{
		ID: "b", HouseholdID: "house-1", Name: "Villa tax", Cadence: domain.CadenceQuarterly,
		Amount: domain.Money{Amount: 1_000_000, Currency: "IDR"}, NextDue: ptr(cashflowToday.AddDate(0, 0, 10)),
	}})
	outage := errors.New("rate provider down")
	f.fx.failWith(outage)
	if _, err := f.svc.Page(context.Background(), "house-1", cashflowToday, 0); !errors.Is(err, outage) {
		t.Fatalf("err = %v, want the provider outage, not an unconvertible bill", err)
	}
}

func TestPageOldestYearIsNeverBeforeTheFirstShownYear(t *testing.T) {
	// One mistyped 1999 date must not offer a year the page then refuses.
	f := newCashflowFixture(t)
	f.income(cfDay(1999, time.December, 25), sgd(500_000))
	p := f.page(t)
	if p.OldestYear != 2000 {
		t.Fatalf("OldestYear = %d, want 2000", p.OldestYear)
	}
	if _, err := f.svc.Page(context.Background(), "house-1", cashflowToday, p.OldestYear); err != nil {
		t.Fatalf("the oldest year offered must load: %v", err)
	}
}

func TestPageLightNeedsTheMissingSpendingCheckOnEveryAveragedMonth(t *testing.T) {
	// Three ordinary months with a big cushion: everything else is green, but
	// none of April, May or June had three solid months before it.
	f := newCashflowFixture(t)
	f.solidQuarter()
	f.addAccount("cash", domain.AccountCash, sgd(5_000_000), true, false)
	p := f.page(t)
	if p.Light.Colour != domain.LightYellow || !hasReason(p.Light, domain.ReasonSpendingNotChecked) {
		t.Fatalf("light = %+v, want yellow spending_not_checked", p.Light)
	}
	if p.Average.Unchecked != 3 {
		t.Fatalf("average = %+v, want 3 unchecked", p.Average)
	}

	// Three more ordinary months before them, and the light can be green.
	// Rows arrive oldest first, as the repository returns them.
	f = newCashflowFixture(t)
	for _, m := range []time.Month{time.January, time.February, time.March} {
		f.income(cfDay(2026, m, 25), sgd(500_000))
		f.expense(cfDay(2026, m, 10), sgd(300_000))
	}
	f.solidQuarter()
	f.addAccount("cash", domain.AccountCash, sgd(5_000_000), true, false)
	if p := f.page(t); p.Light.Colour != domain.LightGreen {
		t.Fatalf("light = %+v, want green with six checked months", p.Light)
	}
}
