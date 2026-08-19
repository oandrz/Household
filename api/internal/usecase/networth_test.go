package usecase_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/andreasoentoro/hearth/api/internal/domain"
	"github.com/andreasoentoro/hearth/api/internal/usecase"
)

// account is a terse builder for the views Summary consumes, so each test
// below reads as the scenario it describes rather than as struct literals.
func account(t *testing.T, kind domain.AccountType, minor int64, currency string, opts ...func(*usecase.AccountView)) usecase.AccountView {
	t.Helper()
	money, err := domain.NewMoney(minor, currency)
	if err != nil {
		t.Fatalf("NewMoney: %v", err)
	}
	v := usecase.AccountView{
		Account: domain.Account{
			ID: currency + "-" + string(kind), HouseholdID: "h-1", Type: kind,
			OpeningBalance: money, CountTowardNetWorth: true,
		},
		Balance: money,
	}
	for _, opt := range opts {
		opt(&v)
	}
	return v
}

func notCounted(v *usecase.AccountView) { v.Account.CountTowardNetWorth = false }

func archived(v *usecase.AccountView) {
	at := time.Date(2026, 7, 20, 9, 0, 0, 0, time.UTC)
	v.Account.ArchivedAt = &at
}

// TestSummarySubtractsDebtsFromAssets is the design's own Finances figures in
// miniature: assets minus liabilities, in the household's primary currency.
func TestSummarySubtractsDebtsFromAssets(t *testing.T) {
	svc, _ := newAccountService(t) // household h-1 has primary SGD

	got, err := svc.Summary(context.Background(), "h-1", []usecase.AccountView{
		account(t, domain.AccountCash, 6_199_000, "SGD"),
		account(t, domain.AccountLoan, 1_450_000, "SGD"),
	}, fixedNow)
	if err != nil {
		t.Fatalf("Summary: %v", err)
	}
	if !got.Computable {
		t.Fatal("Computable = false, want true")
	}
	if got.NetWorth.Amount != 4_749_000 {
		t.Errorf("NetWorth = %d, want 4749000", got.NetWorth.Amount)
	}
	if got.Assets.Amount != 6_199_000 {
		t.Errorf("Assets = %d, want 6199000", got.Assets.Amount)
	}
	if got.Liabilities.Amount != 1_450_000 {
		t.Errorf("Liabilities = %d, want 1450000 (the sum owed, unsigned)", got.Liabilities.Amount)
	}
}

// TestSummaryConvertsBeforeAdding is the test that fails if the loop is
// written the other way round. domain.Money.Add refuses two currencies, so
// summing first and converting after errors on the second account.
//
// The expected figure comes from the design's own screen: Rp 85,400,000 is
// 8_540_000_000 IDR minor units, which at {1, 12410} is 688_155 SGD minor
// units -- S$6,881.55, the "≈ S$6,880" the mockup rounds for display.
func TestSummaryConvertsBeforeAdding(t *testing.T) {
	svc, _ := newAccountService(t)

	got, err := svc.Summary(context.Background(), "h-1", []usecase.AccountView{
		account(t, domain.AccountCash, 824_055, "SGD"),
		account(t, domain.AccountCash, 8_540_000_000, "IDR"),
	}, fixedNow)
	if err != nil {
		t.Fatalf("Summary: %v", err)
	}
	if got.NetWorth.Currency != "SGD" {
		t.Errorf("Currency = %q, want SGD", got.NetWorth.Currency)
	}
	if got.NetWorth.Amount != 1_512_210 {
		t.Errorf("NetWorth = %d, want 1512210 (824055 + 688155)", got.NetWorth.Amount)
	}
}

func TestSummaryExcludesAndNamesAnAccountWithNoRate(t *testing.T) {
	svc, _ := newAccountService(t)

	got, err := svc.Summary(context.Background(), "h-1", []usecase.AccountView{
		account(t, domain.AccountCash, 824_055, "SGD"),
		account(t, domain.AccountCash, 500_000, "USD"),
	}, fixedNow)
	if err != nil {
		t.Fatalf("Summary: %v", err)
	}
	if got.NetWorth.Amount != 824_055 {
		t.Errorf("NetWorth = %d, want 824055 -- the USD account must not be counted", got.NetWorth.Amount)
	}
	if len(got.ExcludedNoRate) != 1 || got.ExcludedNoRate[0].Currency != "USD" {
		t.Errorf("ExcludedNoRate = %+v, want one USD entry", got.ExcludedNoRate)
	}
}

// TestSummaryIsNotComputableWhenNothingConverts is the state a household
// reaches by changing its primary currency in Settings. Zero would be a claim
// about their money; the truth is that we cannot compute it.
func TestSummaryIsNotComputableWhenNothingConverts(t *testing.T) {
	svc, _ := newAccountService(t)

	got, err := svc.Summary(context.Background(), "h-1", []usecase.AccountView{
		account(t, domain.AccountCash, 500_000, "USD"),
		account(t, domain.AccountCash, 300_000, "EUR"),
	}, fixedNow)
	if err != nil {
		t.Fatalf("Summary: %v", err)
	}
	if got.Computable {
		t.Fatal("Computable = true, want false when no account converts")
	}
	if len(got.ExcludedNoRate) != 2 {
		t.Errorf("ExcludedNoRate = %d entries, want 2", len(got.ExcludedNoRate))
	}
}

// TestSummaryHasNoAccountsIsComputable distinguishes "nothing to add up, so
// zero" from "cannot add up". A household with no accounts genuinely has a net
// worth of zero.
func TestSummaryHasNoAccountsIsComputable(t *testing.T) {
	svc, _ := newAccountService(t)

	got, err := svc.Summary(context.Background(), "h-1", nil, fixedNow)
	if err != nil {
		t.Fatalf("Summary: %v", err)
	}
	if !got.Computable || got.NetWorth.Amount != 0 {
		t.Errorf("got %+v, want a computable zero", got)
	}
}

// TestSummaryKeepsAnUncountedAccountInTheBreakdown pins the consequence of the
// toggle's own copy, "Include this balance in the family total": the total,
// specifically. The bars will not always sum to net worth, and the screen says
// so rather than the service quietly hiding the account.
func TestSummaryKeepsAnUncountedAccountInTheBreakdown(t *testing.T) {
	svc, _ := newAccountService(t)

	got, err := svc.Summary(context.Background(), "h-1", []usecase.AccountView{
		account(t, domain.AccountCash, 100_000, "SGD"),
		account(t, domain.AccountInvestment, 900_000, "SGD", notCounted),
	}, fixedNow)
	if err != nil {
		t.Fatalf("Summary: %v", err)
	}
	if got.NetWorth.Amount != 100_000 {
		t.Errorf("NetWorth = %d, want 100000", got.NetWorth.Amount)
	}
	if got.ExcludedByChoice != 1 {
		t.Errorf("ExcludedByChoice = %d, want 1", got.ExcludedByChoice)
	}
	if len(got.Breakdown) != 2 {
		t.Fatalf("Breakdown = %d entries, want 2 -- an uncounted account still has a bar", len(got.Breakdown))
	}
}

// TestSummaryBreakdownDrawsOnlyPopulatedTypes: the chart is one bar per type
// that has an account, not a fixed five, so a household with two cash accounts
// does not get three empty bars.
func TestSummaryBreakdownDrawsOnlyPopulatedTypes(t *testing.T) {
	svc, _ := newAccountService(t)

	got, err := svc.Summary(context.Background(), "h-1", []usecase.AccountView{
		account(t, domain.AccountCash, 100_000, "SGD"),
		account(t, domain.AccountCash, 200_000, "SGD"),
	}, fixedNow)
	if err != nil {
		t.Fatalf("Summary: %v", err)
	}
	if len(got.Breakdown) != 1 {
		t.Fatalf("Breakdown = %d entries, want 1", len(got.Breakdown))
	}
	if got.Breakdown[0].Type != domain.AccountCash || got.Breakdown[0].Total.Amount != 300_000 {
		t.Errorf("Breakdown[0] = %+v, want {cash 300000}", got.Breakdown[0])
	}
}

// TestSummaryBreakdownIsOrderedByType pins the rule that the bars do not
// reshuffle between two identical requests. Go randomises map iteration, so
// building the breakdown by ranging over the byType map would order it
// differently run to run -- and a chart whose bars swap places on a refresh
// reads as a bug in the numbers, not in the sort.
//
// Five types are used deliberately: with map iteration the odds of landing on
// the sorted order by chance are 1 in 120, so this test fails almost every run
// against the wrong implementation rather than occasionally.
func TestSummaryBreakdownIsOrderedByType(t *testing.T) {
	svc, _ := newAccountService(t)

	got, err := svc.Summary(context.Background(), "h-1", []usecase.AccountView{
		account(t, domain.AccountCreditCard, 12_000, "SGD"),
		account(t, domain.AccountProperty, 8_856_000, "SGD"),
		account(t, domain.AccountCash, 100_000, "SGD"),
		account(t, domain.AccountLoan, 1_450_000, "SGD"),
		account(t, domain.AccountInvestment, 11_230_000, "SGD"),
	}, fixedNow)
	if err != nil {
		t.Fatalf("Summary: %v", err)
	}

	want := domain.AccountTypes()
	if len(got.Breakdown) != len(want) {
		t.Fatalf("Breakdown = %d entries, want %d", len(got.Breakdown), len(want))
	}
	for i, wantType := range want {
		if got.Breakdown[i].Type != wantType {
			t.Errorf("Breakdown[%d].Type = %q, want %q -- assets before debts, in domain order",
				i, got.Breakdown[i].Type, wantType)
		}
	}
}

// TestSummarySkipsArchivedAccounts: archived means out of the picture, not
// hidden but still counted. It leaves the total and its type's bar together.
func TestSummarySkipsArchivedAccounts(t *testing.T) {
	svc, _ := newAccountService(t)

	live := account(t, domain.AccountCash, 100_000, "SGD")
	gone := account(t, domain.AccountInvestment, 900_000, "SGD", archived)

	got, err := svc.Summary(context.Background(), "h-1", []usecase.AccountView{live, gone}, fixedNow)
	if err != nil {
		t.Fatalf("Summary: %v", err)
	}
	if got.NetWorth.Amount != 100_000 {
		t.Errorf("NetWorth = %d, want 100000", got.NetWorth.Amount)
	}
	if len(got.Breakdown) != 1 || got.Breakdown[0].Type != domain.AccountCash {
		t.Errorf("Breakdown = %+v, want only the live cash account", got.Breakdown)
	}
}

// TestSummaryOfAnAllArchivedHouseholdIsAGenuineZero is why the Computable
// guard counts the accounts this summary is *about* rather than len(views).
// Every account here is archived, so the loop skips all of them and nothing
// converts -- but the honest answer is zero, not "we cannot work this out".
// Guarding on len(views) would report the latter.
func TestSummaryOfAnAllArchivedHouseholdIsAGenuineZero(t *testing.T) {
	svc, _ := newAccountService(t)

	got, err := svc.Summary(context.Background(), "h-1", []usecase.AccountView{
		account(t, domain.AccountCash, 100_000, "SGD", archived),
	}, fixedNow)
	if err != nil {
		t.Fatalf("Summary: %v", err)
	}
	if !got.Computable {
		t.Fatal("Computable = false, want true -- an all-archived household has a real zero")
	}
	if got.NetWorth.Amount != 0 {
		t.Errorf("NetWorth = %d, want 0", got.NetWorth.Amount)
	}
}

// TestSummaryLooksUpEachRateOnce is the guard on the trend's central promise,
// written before the trend exists. The newest bar must equal the headline
// figure, and it can only do that if both are converted with the same rate --
// so Summary must consult the provider once per currency, not once per
// account and again per month. fx.StaticProvider returns one number forever,
// so nothing else in the suite would ever notice the difference.
func TestSummaryLooksUpEachRateOnce(t *testing.T) {
	counter := &countingRates{}
	svc := newAccountServiceWithFX(t, counter)

	// Distinct ids: `account()` derives one from the currency and the type, so
	// three IDR cash accounts would otherwise share it -- harmless to Summary,
	// but the trend keys its movements by account id and a later test adding
	// one here would silently give all three the same history.
	views := []usecase.AccountView{
		account(t, domain.AccountCash, 1_000_000_000, "IDR"),
		account(t, domain.AccountCash, 2_000_000_000, "IDR"),
		account(t, domain.AccountCash, 3_000_000_000, "IDR"),
	}
	for i := range views {
		views[i].Account.ID = fmt.Sprintf("idr-%d", i)
	}

	if _, err := svc.Summary(context.Background(), "h-1", views, fixedNow); err != nil {
		t.Fatalf("Summary: %v", err)
	}
	if counter.calls != 1 {
		t.Errorf("provider called %d times for three accounts in one currency, want 1", counter.calls)
	}
}

// countingRates is staticTestRates that remembers how often it was asked.
type countingRates struct{ calls int }

func (c *countingRates) Rate(ctx context.Context, from, to string) (usecase.Rate, error) {
	c.calls++
	return staticTestRates{}.Rate(ctx, from, to)
}

// newAccountServiceWithFX is newAccountService with the FX double swapped,
// the same shape bill_test.go's newBillServiceWithFX already uses.
func newAccountServiceWithFX(t *testing.T, fx usecase.FXRateProvider) *usecase.AccountService {
	t.Helper()
	households := newHouseholdDouble()
	households.put(domain.Household{
		ID: "h-1", Name: "Andreas & Christine", FamilyName: "Oentoro",
		PrimaryCurrency: "SGD", ShowSecondaryCurrency: true, SecondaryCurrency: "IDR", FXRateMode: "auto",
	})
	return usecase.NewAccountService(usecase.AccountDeps{
		Accounts:   newFakeAccountRepo(),
		Households: households,
		FX:         fx,
		Clock:      &fixedClock{now: fixedNow},
	})
}
