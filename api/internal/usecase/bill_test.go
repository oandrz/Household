package usecase_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/andreasoentoro/hearth/api/internal/domain"
	"github.com/andreasoentoro/hearth/api/internal/usecase"
)

// day parses "2026-08-09" into a UTC midnight time.Time -- the same helper
// domain/bill_test.go and postgres/bill_repo_test.go each define for their
// own package.
func day(s string) time.Time {
	t, err := time.Parse("2006-01-02", s)
	if err != nil {
		panic(err)
	}
	return t
}

// --- BillService fixtures -------------------------------------------------
//
// Task 7's MarkPaid/UndoPayment tests reuse every helper below: newBillService
// itself, withAccount/withArchivedAccount, and the bill/billOn/oneOff family.
// newBillServiceWith and newBillServiceWithFX are Task 6's own thin wrappers,
// for a test that only ever needs a *usecase.BillService back.

// billServiceFixture is what newBillService's opts mutate before the service
// is built: the account double every Create/Update/MarkPaid guard consults,
// and the FX double every summary conversion consults.
type billServiceFixture struct {
	accounts *fakeAccountLookup
	fx       usecase.FXRateProvider
}

type billServiceOption func(*billServiceFixture)

// withAccount registers id in the AccountLookup double with currency, live --
// for a test whose bill pays from an account other than the default "acct-1"
// SGD, most often paired with billOn.
func withAccount(id, currency string) billServiceOption {
	return func(f *billServiceFixture) {
		f.accounts.accounts[id] = fakeAccountRecord{householdID: "h1", currency: currency}
	}
}

// withArchivedAccount is withAccount's archived form, for a test proving a
// bill cannot be created against, or paid from, an account that has since
// been archived.
func withArchivedAccount(id, currency string) billServiceOption {
	return func(f *billServiceFixture) {
		f.accounts.accounts[id] = fakeAccountRecord{householdID: "h1", currency: currency, archived: true}
	}
}

// withFX swaps the FX double -- newBillServiceWithFX's own reason to exist,
// factored out as one more option rather than a second constructor with its
// own household/account wiring to keep in sync with newBillService's.
func withFX(fx usecase.FXRateProvider) billServiceOption {
	return func(f *billServiceFixture) { f.fx = fx }
}

// newBillService wires a BillService against repo -- already constructed, so
// a test can seed it directly and read its state back afterward, e.g. Task
// 7's repo.lastWrite -- and a household "h1" (SGD primary currency), with
// "acct-1" (SGD, live) pre-registered as the account every plain bill()
// fixture pays from. opts... layer on anything else: another account, an
// archived one, or a different FX double.
func newBillService(t *testing.T, repo *fakeBillRepo, opts ...billServiceOption) *usecase.BillService {
	t.Helper()
	households := newHouseholdDouble()
	households.put(domain.Household{ID: "h1", PrimaryCurrency: "SGD"})

	f := &billServiceFixture{
		accounts: &fakeAccountLookup{accounts: map[string]fakeAccountRecord{
			"acct-1": {householdID: "h1", currency: "SGD"},
		}},
		fx: staticTestRates{},
	}
	for _, opt := range opts {
		opt(f)
	}

	return usecase.NewBillService(usecase.BillDeps{
		Bills:      repo,
		Households: households,
		FX:         f.fx,
		Accounts:   f.accounts,
	})
}

// newBillServiceWith is newBillService for a test that only ever calls List:
// it builds its own repo, seeds it with records (each getting a "bill-N" id
// via fakeBillRepo.add), and returns just the service.
func newBillServiceWith(t *testing.T, records ...usecase.BillRecord) *usecase.BillService {
	t.Helper()
	repo := &fakeBillRepo{}
	for _, r := range records {
		repo.add(r)
	}
	return newBillService(t, repo)
}

// newBillServiceWithFX is newBillServiceWith with the FX double swapped, for
// TestSummaryExcludesABillWithNoRateAndCountsIt: staticTestRates already
// knows SGD<->IDR, so that test needs a double with no rates at all, not the
// default one.
func newBillServiceWithFX(t *testing.T, fx usecase.FXRateProvider, records ...usecase.BillRecord) *usecase.BillService {
	t.Helper()
	repo := &fakeBillRepo{}
	for _, r := range records {
		repo.add(r)
	}
	return newBillService(t, repo, withFX(fx))
}

// noRateFX has no rate for anything -- staticTestRates already knows
// SGD<->IDR, which is exactly the pair TestSummaryExcludesABillWithNoRateAndCountsIt
// needs to fail, so that double cannot stand in for "no rate available" here.
type noRateFX struct{}

func (noRateFX) Rate(_ context.Context, from, to string) (usecase.Rate, error) {
	return usecase.Rate{}, fmt.Errorf("no rate available for %s to %s", from, to)
}

// bill is the default fixture: a monthly SGD bill, paid from "acct-1" --
// newBillService's always-registered account -- categorised "cat-utilities".
// A test names only the thing it is actually about.
func bill(name, dueOn string, amountMinor int64) usecase.BillRecord {
	due := day(dueOn)
	return usecase.BillRecord{
		Bill: domain.Bill{
			HouseholdID:      "h1",
			Name:             name,
			Amount:           domain.Money{Amount: amountMinor, Currency: "SGD"},
			Cadence:          domain.CadenceMonthly,
			NextDue:          &due,
			DueAnchorDay:     due.Day(),
			CategoryID:       "cat-utilities",
			PayFromAccountID: "acct-1",
		},
		CategoryName: "Utilities",
		AccountName:  "Everyday",
	}
}

// billOn is bill's general form for a currency other than the default SGD:
// its account id is derived from the currency ("acct-idr" for IDR), so a
// fixture built here and a withAccount(id, currency) call in the same test
// always agree without either hard-coding the other's id.
func billOn(name, currency, dueOn string, amountMinor int64) usecase.BillRecord {
	rec := bill(name, dueOn, amountMinor)
	rec.Bill.Amount.Currency = currency
	rec.Bill.PayFromAccountID = "acct-" + strings.ToLower(currency)
	return rec
}

// autopayBill is bill with Autopay ticked.
func autopayBill(name, dueOn string, amountMinor int64) usecase.BillRecord {
	rec := bill(name, dueOn, amountMinor)
	rec.Bill.Autopay = true
	return rec
}

// subscription is a recurring bill ticked IsSubscription at cadence, due a
// fixed date the rollup tests never look at -- only the cadence and amount
// matter to them.
func subscription(name string, cadence domain.Cadence, amountMinor int64) usecase.BillRecord {
	rec := bill(name, "2026-08-20", amountMinor)
	rec.Bill.Cadence = cadence
	rec.Bill.IsSubscription = true
	return rec
}

// oneOffSubscription is ticked IsSubscription but cadence one_off -- the
// rollup's own exclusion for a one-off (domain.AnnualEquivalentMinor's
// ok=false) is what a naive "just check IsSubscription" implementation would
// miss.
func oneOffSubscription(name string, amountMinor int64) usecase.BillRecord {
	rec := bill(name, "2026-08-20", amountMinor)
	rec.Bill.Cadence = domain.CadenceOneOff
	rec.Bill.IsSubscription = true
	return rec
}

// oneOff is a plain one-off bill, NOT ticked as a subscription -- distinct
// from oneOffSubscription, which is. Task 7's MarkPaid tests are what need
// this one; the rollup tests are what need the ticked form.
func oneOff(name, dueOn string, amountMinor int64) usecase.BillRecord {
	rec := bill(name, dueOn, amountMinor)
	rec.Bill.Cadence = domain.CadenceOneOff
	return rec
}

// archivedBill stamps rec's bill archived at an arbitrary past date -- no
// test reads the exact timestamp, only that ArchivedAt is non-nil. Named
// archivedBill, not the brief's own "archived", because networth_test.go
// already declares a package-level archived (an *AccountView mutator) in
// this same usecase_test package -- see this task's own report for the
// rename.
func archivedBill(rec usecase.BillRecord) usecase.BillRecord {
	at := day("2026-01-01")
	rec.Bill.ArchivedAt = &at
	return rec
}

// --- List ------------------------------------------------------------------

// Every unpaid bill is on the page under one heading or the other. A 30-day
// filter alone would leave a yearly insurance bill invisible for eleven months
// while the header kept counting it (spec decision 5's rider).
func TestListSplitsDueSoonFromLaterAndKeepsBoth(t *testing.T) {
	today := day("2026-08-09")
	svc := newBillServiceWith(t,
		bill("Income tax", "2026-08-24", 23000),
		bill("Car insurance", "2026-11-01", 96500),
	)
	view, err := svc.List(context.Background(), "h1", false, today)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(view.Bills) != 2 {
		t.Fatalf("got %d bills, want both on the page", len(view.Bills))
	}
	byName := map[string]usecase.BillView{}
	for _, b := range view.Bills {
		byName[b.Bill.Name] = b
	}
	if !byName["Income tax"].DueSoon {
		t.Error("a bill 15 days out belongs under Due soon")
	}
	if byName["Car insurance"].DueSoon {
		t.Error("a bill 84 days out belongs under Later")
	}
}

func TestListMarksAnOverdueBillOverdueAndSortsItFirst(t *testing.T) {
	today := day("2026-08-09")
	svc := newBillServiceWith(t,
		bill("Netflix", "2026-08-20", 1998),
		bill("Income tax", "2026-07-24", 23000), // overdue
	)
	view, err := svc.List(context.Background(), "h1", false, today)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if view.Bills[0].Bill.Name != "Income tax" {
		t.Fatalf("first row = %q, want the overdue bill", view.Bills[0].Bill.Name)
	}
	if !view.Bills[0].Overdue {
		t.Error("a due date in the past is overdue")
	}
	// Overdue is also Due soon: it belongs above the Later heading, not below.
	if !view.Bills[0].DueSoon {
		t.Error("an overdue bill belongs under Due soon")
	}
	if view.Bills[1].Overdue {
		t.Error("a future due date is not overdue")
	}
}

// TestListDueSoonBoundaryIsThirtyDaysInclusive pins the boundary the two
// tests above never approach (15 and 84 days out, nowhere near 30): the 30th
// day itself is Due soon, the 31st is Later.
func TestListDueSoonBoundaryIsThirtyDaysInclusive(t *testing.T) {
	today := day("2026-08-09")
	svc := newBillServiceWith(t,
		bill("Exactly thirty days out", "2026-09-08", 5000), // today+30
		bill("Thirty-one days out", "2026-09-09", 5000),     // today+31
	)
	view, err := svc.List(context.Background(), "h1", false, today)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	byName := map[string]usecase.BillView{}
	for _, b := range view.Bills {
		byName[b.Bill.Name] = b
	}
	if !byName["Exactly thirty days out"].DueSoon {
		t.Error("exactly 30 days out is Due soon -- the boundary is inclusive")
	}
	if byName["Thirty-one days out"].DueSoon {
		t.Error("31 days out belongs under Later")
	}
}

// Integer-first: multiply every bill up to a year, add, then divide once.
func TestSubscriptionsRollupMultipliesThenDividesOnce(t *testing.T) {
	svc := newBillServiceWith(t,
		subscription("Netflix", domain.CadenceMonthly, 1998),
		subscription("YouTube Premium", domain.CadenceMonthly, 1798),
		subscription("Spotify family", domain.CadenceMonthly, 1698),
		subscription("Disney+", domain.CadenceMonthly, 1198),
		subscription("iCloud 200GB", domain.CadenceMonthly, 398),
		// Not ticked as a subscription: it must not reach either figure.
		bill("SP utilities", "2026-08-08", 14230),
		// A one-off is excluded even when ticked -- it is not a recurring cost.
		oneOffSubscription("Domain renewal", 3500),
	)
	view, err := svc.List(context.Background(), "h1", false, day("2026-08-09"))
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	// 70.90/mo is the design's own figure; 70.90 * 12 = 850.80 is its own
	// annual line, and the two agree because one is derived from the other.
	if got := view.Summary.SubscriptionsAnnual.Amount; got != 85080 {
		t.Fatalf("annual = %d, want 85080", got)
	}
	if got := view.Summary.SubscriptionsMonthly.Amount; got != 7090 {
		t.Fatalf("monthly = %d, want 7090", got)
	}
}

func TestSubscriptionsRollupNormalisesANonMonthlyCadence(t *testing.T) {
	svc := newBillServiceWith(t,
		subscription("Insurance", domain.CadenceQuarterly, 30000),
		subscription("Domain", domain.CadenceYearly, 1200),
	)
	view, err := svc.List(context.Background(), "h1", false, day("2026-08-09"))
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	// 30000*4 + 1200*1 = 121200 a year; 121200/12 = 10100 a month.
	if got := view.Summary.SubscriptionsAnnual.Amount; got != 121200 {
		t.Fatalf("annual = %d, want 121200", got)
	}
	if got := view.Summary.SubscriptionsMonthly.Amount; got != 10100 {
		t.Fatalf("monthly = %d, want 10100", got)
	}
}

// TestSubscriptionsRollupDividesTheCombinedAnnualTotalNotEachBill is the
// mutation-catching test the other two rollup tests cannot be: their own
// numbers happen to divide evenly whether the implementation divides once at
// the end (correct) or divides each bill's own annual figure first (wrong).
// Two yearly bills of 1206 do not: 1206+1206=2412, 2412/12=201, but
// 1206/12=100 (floored) twice is only 200 -- a minor unit lost per bill that
// only the combined total's own division avoids.
func TestSubscriptionsRollupDividesTheCombinedAnnualTotalNotEachBill(t *testing.T) {
	svc := newBillServiceWith(t,
		subscription("Cloud backup A", domain.CadenceYearly, 1206),
		subscription("Cloud backup B", domain.CadenceYearly, 1206),
	)
	view, err := svc.List(context.Background(), "h1", false, day("2026-08-09"))
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if got := view.Summary.SubscriptionsAnnual.Amount; got != 2412 {
		t.Fatalf("annual = %d, want 2412", got)
	}
	if got := view.Summary.SubscriptionsMonthly.Amount; got != 201 {
		t.Fatalf("monthly = %d, want 201 -- dividing per bill before adding loses a minor unit the combined total does not", got)
	}
}

func TestSummaryExcludesABillWithNoRateAndCountsIt(t *testing.T) {
	// Household primary SGD; one bill on an IDR account the FX double has no
	// rate for.
	svc := newBillServiceWithFX(t, noRateFX{},
		bill("SP utilities", "2026-08-08", 14230),         // SGD
		billOn("Arisan", "IDR", "2026-08-15", 50_000_000), // no rate
	)
	view, err := svc.List(context.Background(), "h1", false, day("2026-08-09"))
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if got := view.Summary.DueThisMonth.Amount; got != 14230 {
		t.Fatalf("due = %d, want 14230 -- the no-rate bill must not be summed", got)
	}
	// Excluded, never silently dropped: the screen says how many.
	if view.Summary.ExcludedNoRate != 1 {
		t.Fatalf("ExcludedNoRate = %d, want 1", view.Summary.ExcludedNoRate)
	}
}

func TestNextDueIsOmittedWhenThereIsNone(t *testing.T) {
	svc := newBillServiceWith(t)
	view, err := svc.List(context.Background(), "h1", false, day("2026-08-09"))
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	// nil, not a zero Money: the card renders nothing rather than "S$0.00".
	if view.Summary.NextDueOn != nil {
		t.Fatalf("NextDueOn = %v, want nil for a household with no bills", view.Summary.NextDueOn)
	}
}

func TestAutopayCountsOnlyUnarchivedBills(t *testing.T) {
	svc := newBillServiceWith(t,
		autopayBill("Income tax", "2026-08-24", 23000),
		archivedBill(autopayBill("Old gym", "2026-08-02", 8000)),
		bill("School fees", "2026-08-15", 38000),
	)
	view, err := svc.List(context.Background(), "h1", false, day("2026-08-09"))
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if view.Summary.AutopayCount != 1 || view.Summary.BillCount != 2 {
		t.Fatalf("got %d of %d on autopay, want 1 of 2 -- the archived bill counts in neither",
			view.Summary.AutopayCount, view.Summary.BillCount)
	}
}

// --- Create ------------------------------------------------------------

func TestBillCreateValidates(t *testing.T) {
	svc := newBillService(t, &fakeBillRepo{})
	ctx := context.Background()
	today := day("2026-08-09")

	base := usecase.NewBill{
		HouseholdID: "h1", Name: "Rent", AmountMinor: 250000,
		Cadence: domain.CadenceMonthly, NextDue: day("2026-08-20"),
		PayFromAccountID: "acct-1",
	}

	blank := base
	blank.Name = "   "
	if _, err := svc.Create(ctx, blank, today); !errors.Is(err, domain.ErrBillNameRequired) {
		t.Fatalf("blank name err = %v, want domain.ErrBillNameRequired", err)
	}

	zeroAmount := base
	zeroAmount.AmountMinor = 0
	if _, err := svc.Create(ctx, zeroAmount, today); !errors.Is(err, domain.ErrBillAmountNotPositive) {
		t.Fatalf("zero amount err = %v, want domain.ErrBillAmountNotPositive", err)
	}

	badCadence := base
	badCadence.Cadence = domain.Cadence("fortnightly")
	if _, err := svc.Create(ctx, badCadence, today); !errors.Is(err, domain.ErrUnknownCadence) {
		t.Fatalf("bad cadence err = %v, want domain.ErrUnknownCadence", err)
	}

	created, err := svc.Create(ctx, base, today)
	if err != nil {
		t.Fatalf("valid create: %v", err)
	}
	if created.Bill.DueAnchorDay != 20 {
		t.Fatalf("anchor = %d, want 20 -- derived from NextDue.Day(), never a caller-supplied value", created.Bill.DueAnchorDay)
	}
}

// TestBillCreateRefusesAnArchivedPayFromAccount needs its own fixture (an
// account registered archived), not the shared live one TestBillCreateValidates
// uses for every other guard.
func TestBillCreateRefusesAnArchivedPayFromAccount(t *testing.T) {
	svc := newBillService(t, &fakeBillRepo{}, withArchivedAccount("acct-1", "SGD"))

	_, err := svc.Create(context.Background(), usecase.NewBill{
		HouseholdID: "h1", Name: "Rent", AmountMinor: 250000,
		Cadence: domain.CadenceMonthly, NextDue: day("2026-08-20"),
		PayFromAccountID: "acct-1",
	}, day("2026-08-09"))
	if !errors.Is(err, domain.ErrForbidden) {
		t.Fatalf("err = %v, want domain.ErrForbidden", err)
	}
}

// --- Update ------------------------------------------------------------

// TestUpdateReDerivesTheAnchorWhenNextDueMoves is the case Task 5's own
// anchor test (the mechanical rewind, which must NOT touch the anchor) is the
// mirror image of: an explicit edit to NextDue IS the household picking a new
// anchor, so the NEXT advance must land on the day they just chose, not the
// day the bill used to carry.
func TestUpdateReDerivesTheAnchorWhenNextDueMoves(t *testing.T) {
	svc := newBillServiceWith(t, bill("SP utilities", "2026-08-08", 14230)) // anchor 8

	newDue := day("2026-08-15")
	updated, err := svc.Update(context.Background(), "h1", "bill-1", usecase.BillPatch{
		NextDue: &newDue,
	}, day("2026-08-09"))
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if updated.Bill.DueAnchorDay != 15 {
		t.Fatalf("anchor = %d, want 15 -- an explicit NextDue edit re-derives it", updated.Bill.DueAnchorDay)
	}

	// The next advance must land on the 15th, not the 8th the bill used to
	// carry -- exactly what a service that forgot to re-derive the anchor
	// would get wrong.
	next, ok := domain.NextDue(updated.Bill.Cadence, *updated.Bill.NextDue, updated.Bill.DueAnchorDay)
	if !ok || next.Day() != 15 {
		t.Fatalf("next advance = %v, want the 15th", next)
	}
}

// TestUpdateLeavesTheAnchorAloneWhenNextDueIsNotPatched is the companion
// case: an ordinary rename must not touch the anchor at all.
func TestUpdateLeavesTheAnchorAloneWhenNextDueIsNotPatched(t *testing.T) {
	svc := newBillServiceWith(t, bill("SP utilities", "2026-08-08", 14230)) // anchor 8

	newName := "SP utilities (renamed)"
	updated, err := svc.Update(context.Background(), "h1", "bill-1", usecase.BillPatch{
		Name: &newName,
	}, day("2026-08-09"))
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if updated.Bill.DueAnchorDay != 8 {
		t.Fatalf("anchor = %d, want unchanged 8 -- Update did not touch NextDue", updated.Bill.DueAnchorDay)
	}
}

// TestUpdateAppliesOnlyPatchedFieldsAndHandsTheRepoAWholeBill is the "the
// port never merges" invariant, tested rather than assumed: every field the
// patch left alone must survive the round trip through the repository.
func TestUpdateAppliesOnlyPatchedFieldsAndHandsTheRepoAWholeBill(t *testing.T) {
	repo := &fakeBillRepo{}
	repo.add(autopayBill("Income tax", "2026-08-24", 23000))
	svc := newBillService(t, repo)

	newAmount := int64(25000)
	if _, err := svc.Update(context.Background(), "h1", "bill-1", usecase.BillPatch{
		AmountMinor: &newAmount,
	}, day("2026-08-09")); err != nil {
		t.Fatalf("Update: %v", err)
	}

	stored := repo.records[0].Bill
	if stored.Name != "Income tax" {
		t.Errorf("name = %q, want unchanged \"Income tax\"", stored.Name)
	}
	if stored.Amount.Amount != 25000 {
		t.Errorf("amount = %d, want 25000", stored.Amount.Amount)
	}
	if !stored.Autopay {
		t.Error("autopay = false, want unchanged true")
	}
	if stored.CategoryID != "cat-utilities" {
		t.Errorf("category = %q, want unchanged \"cat-utilities\"", stored.CategoryID)
	}
	if stored.PayFromAccountID != "acct-1" {
		t.Errorf("pay-from account = %q, want unchanged \"acct-1\"", stored.PayFromAccountID)
	}
}

func TestUpdateClearsCategoryAndPayer(t *testing.T) {
	repo := &fakeBillRepo{}
	rec := bill("SP utilities", "2026-08-08", 14230)
	rec.Bill.PaidByMembershipID = "m-andreas"
	repo.add(rec)
	svc := newBillService(t, repo)

	updated, err := svc.Update(context.Background(), "h1", "bill-1", usecase.BillPatch{
		ClearCategory: true,
		ClearPayer:    true,
	}, day("2026-08-09"))
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if updated.Bill.CategoryID != "" {
		t.Errorf("category = %q, want cleared", updated.Bill.CategoryID)
	}
	if updated.Bill.PaidByMembershipID != "" {
		t.Errorf("payer = %q, want cleared", updated.Bill.PaidByMembershipID)
	}
}

func TestUpdateRefusesRepointingToADifferentCurrencyAccount(t *testing.T) {
	repo := &fakeBillRepo{}
	repo.add(bill("SP utilities", "2026-08-08", 14230)) // SGD, acct-1
	svc := newBillService(t, repo, withAccount("acct-idr", "IDR"))

	other := "acct-idr"
	_, err := svc.Update(context.Background(), "h1", "bill-1", usecase.BillPatch{
		PayFromAccountID: &other,
	}, day("2026-08-09"))
	if !errors.Is(err, domain.ErrBillCurrencyImmutable) {
		t.Fatalf("err = %v, want domain.ErrBillCurrencyImmutable", err)
	}
}

func TestUpdateValidatesNameAndAmountAndCadence(t *testing.T) {
	repo := &fakeBillRepo{}
	repo.add(bill("SP utilities", "2026-08-08", 14230))
	svc := newBillService(t, repo)
	ctx := context.Background()
	today := day("2026-08-09")

	blank := "   "
	if _, err := svc.Update(ctx, "h1", "bill-1", usecase.BillPatch{Name: &blank}, today); !errors.Is(err, domain.ErrBillNameRequired) {
		t.Fatalf("blank name err = %v, want domain.ErrBillNameRequired", err)
	}

	zero := int64(0)
	if _, err := svc.Update(ctx, "h1", "bill-1", usecase.BillPatch{AmountMinor: &zero}, today); !errors.Is(err, domain.ErrBillAmountNotPositive) {
		t.Fatalf("zero amount err = %v, want domain.ErrBillAmountNotPositive", err)
	}

	bad := domain.Cadence("fortnightly")
	if _, err := svc.Update(ctx, "h1", "bill-1", usecase.BillPatch{Cadence: &bad}, today); !errors.Is(err, domain.ErrUnknownCadence) {
		t.Fatalf("bad cadence err = %v, want domain.ErrUnknownCadence", err)
	}
}

// --- SetArchived ---------------------------------------------------------

func TestSetArchivedReturnsAViewWithNoSecondGet(t *testing.T) {
	repo := &fakeBillRepo{}
	repo.add(bill("Old gym", "2026-07-01", 8000))
	svc := newBillService(t, repo)

	at := day("2026-08-09")
	view, err := svc.SetArchived(context.Background(), "h1", "bill-1", true, at)
	if err != nil {
		t.Fatalf("SetArchived: %v", err)
	}
	if !view.Bill.IsArchived() {
		t.Fatal("bill not archived")
	}
	// Overdue relative to `at`, computed by SetArchived itself with no second
	// Get -- BillRepository.SetArchived's own doc comment is why one is not
	// needed.
	if !view.Overdue {
		t.Error("overdue = false, want true (due 2026-07-01, archived at 2026-08-09)")
	}

	restored, err := svc.SetArchived(context.Background(), "h1", "bill-1", false, at)
	if err != nil {
		t.Fatalf("SetArchived (restore): %v", err)
	}
	if restored.Bill.IsArchived() {
		t.Fatal("bill still archived after restore")
	}
}
