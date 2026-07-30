package usecase_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/andreasoentoro/hearth/api/internal/domain"
	"github.com/andreasoentoro/hearth/api/internal/usecase"
)

// budgetFixture wires a BudgetService against in-memory doubles, one
// household ("house-1", SGD) with two expense categories and one income
// category -- the income category exists so a test can prove Categories
// leaves it out, the same way validateCategory keeps an income category off
// a transaction's expense-only side.
type budgetFixture struct {
	svc          *usecase.BudgetService
	budgets      *fakeBudgetRepo
	transactions *fakeTransactionRepo
	categories   *fakeCategoryRepo
	households   *householdDouble
	members      *membershipDouble
}

func newBudgetFixture(t *testing.T) *budgetFixture {
	t.Helper()

	households := newHouseholdDouble()
	households.put(domain.Household{ID: "house-1", PrimaryCurrency: "SGD"})

	users := newUserDouble()
	members := newMembershipDouble(users)

	categories := &fakeCategoryRepo{categories: []domain.Category{
		{ID: "cat-groceries", HouseholdID: "house-1", Name: "Groceries", Kind: domain.CategoryExpense, SortOrder: 1},
		{ID: "cat-dining", HouseholdID: "house-1", Name: "Dining out", Kind: domain.CategoryExpense, SortOrder: 2},
		{ID: "cat-income", HouseholdID: "house-1", Name: "Salary", Kind: domain.CategoryIncome, SortOrder: 3},
	}}

	transactions := &fakeTransactionRepo{}
	budgets := newFakeBudgetRepo()

	svc := usecase.NewBudgetService(usecase.BudgetDeps{
		Budgets:      budgets,
		Transactions: transactions,
		Categories:   categories,
		Households:   households,
		Members:      members,
		FX:           staticTestRates{},
	})

	return &budgetFixture{
		svc: svc, budgets: budgets, transactions: transactions,
		categories: categories, households: households, members: members,
	}
}

// addExpense appends an expense transaction straight into the fake ledger --
// BudgetService only ever reads MonthTotals, so a test does not need to go
// through TransactionService.Create to give it something to compose.
func (f *budgetFixture) addExpense(id, categoryID, paidBy string, occurredOn time.Time, amountMinor int64, currency string) {
	f.transactions.transactions = append(f.transactions.transactions, domain.Transaction{
		ID: id, HouseholdID: "house-1", Kind: domain.TransactionExpense,
		OccurredOn: occurredOn, Description: id, CategoryID: categoryID,
		PaidByMembershipID: paidBy, FromAccountID: "acc-1",
		Amount: domain.Money{Amount: amountMinor, Currency: currency},
	})
}

func (f *budgetFixture) addMember(membershipID, userID, name string) {
	f.members.users.put(usecase.StoredUser{User: domain.User{ID: userID, DisplayName: name}})
	f.members.put(domain.Membership{ID: membershipID, HouseholdID: "house-1", UserID: userID})
}

func julyMonth() time.Time { return time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC) }

func findCategory(views []usecase.BudgetCategoryView, categoryID string) (usecase.BudgetCategoryView, bool) {
	for _, v := range views {
		if v.CategoryID == categoryID {
			return v, true
		}
	}
	return usecase.BudgetCategoryView{}, false
}

func findPerson(views []usecase.BudgetPersonView, membershipID string) (usecase.BudgetPersonView, bool) {
	for _, v := range views {
		if v.MembershipID == membershipID {
			return v, true
		}
	}
	return usecase.BudgetPersonView{}, false
}

// TestBudgetMonthComposesTheDesignsFigures pins the spec's own worked
// example: two caps, two matching expenses, and every derived figure that
// follows from them.
func TestBudgetMonthComposesTheDesignsFigures(t *testing.T) {
	f := newBudgetFixture(t)
	ctx := context.Background()
	july := julyMonth()

	if _, err := f.svc.Save(ctx, "house-1", july, nil, []usecase.BudgetLineInput{
		{CategoryID: "cat-groceries", CapMinor: 80000}, // S$800.00
		{CategoryID: "cat-dining", CapMinor: 45000},    // S$450.00
	}); err != nil {
		t.Fatalf("save: %v", err)
	}

	f.addExpense("tx-groceries", "cat-groceries", "", july.AddDate(0, 0, 5), 64000, "SGD") // S$640.00
	f.addExpense("tx-dining", "cat-dining", "", july.AddDate(0, 0, 10), 46500, "SGD")      // S$465.00

	got, err := f.svc.Month(ctx, "house-1", july, july.AddDate(0, 0, 17))
	if err != nil {
		t.Fatalf("month: %v", err)
	}

	if got.Budgeted.Amount != 125000 {
		t.Fatalf("budgeted = %d, want 125000 (S$1250.00)", got.Budgeted.Amount)
	}
	if got.Spent.Amount != 110500 {
		t.Fatalf("spent = %d, want 110500 (S$1105.00)", got.Spent.Amount)
	}
	if got.Remaining != 14500 {
		t.Fatalf("remaining = %d, want 14500 minor", got.Remaining)
	}
	if !got.PercentOK || got.PercentUsed != 88 {
		t.Fatalf("percentUsed = %d (ok=%v), want 88 (ok)", got.PercentUsed, got.PercentOK)
	}

	dining, ok := findCategory(got.Categories, "cat-dining")
	if !ok || !dining.Over {
		t.Fatalf("dining = %+v, want Over true", dining)
	}
	groceries, ok := findCategory(got.Categories, "cat-groceries")
	if !ok || groceries.Over {
		t.Fatalf("groceries = %+v, want Over false", groceries)
	}
	if got.OverCount != 1 {
		t.Fatalf("overCount = %d, want 1", got.OverCount)
	}

	// The fixture also has an income category ("cat-income"). Caps envelope
	// spending only, so it must never get a Categories row -- exercising the
	// Kind filter buildCategoryViews applies, not just asserting the two
	// expense rows exist.
	if len(got.Categories) != 2 {
		t.Fatalf("categories = %+v, want exactly 2 (an income category must be excluded)", got.Categories)
	}
	if _, ok := findCategory(got.Categories, "cat-income"); ok {
		t.Fatal("cat-income appeared in Categories -- an income category can never carry a cap")
	}

	// month == today's month here, so the pace card must be shown: today is
	// Jul 18 (july.AddDate(0, 0, 17)), so 14 days left (Jul 18 through Jul 31
	// inclusive), 14500 minor remaining, floored.
	if !got.DailyPaceOK {
		t.Fatal("dailyPaceOK = false, want true -- the viewed month is the current one")
	}
	if got.DaysLeft != 14 {
		t.Fatalf("daysLeft = %d, want 14", got.DaysLeft)
	}
	if got.DailyPace != 1035 {
		t.Fatalf("dailyPace = %d, want 1035 (14500/14, floored)", got.DailyPace)
	}
}

// TestBudgetMonthSpentReusesTheMonthSummaryRule is the spec's "reused
// exactly" claim, tested rather than assumed: an income transaction and a
// transfer must not move Spent, and a transaction with no available rate
// must be excluded from both the total and its own category's figure while
// still being named in ExcludedNoRate.
func TestBudgetMonthSpentReusesTheMonthSummaryRule(t *testing.T) {
	f := newBudgetFixture(t)
	ctx := context.Background()
	july := julyMonth()

	// An enormous income figure: if the Kind guard were ever dropped, this
	// alone would move Spent far enough to fail the assertion below.
	f.transactions.transactions = append(f.transactions.transactions, domain.Transaction{
		ID: "tx-income", HouseholdID: "house-1", Kind: domain.TransactionIncome,
		OccurredOn: july.AddDate(0, 0, 1), Description: "Salary", CategoryID: "cat-income",
		ToAccountID: "acc-1", Amount: domain.Money{Amount: 999999999, Currency: "SGD"},
	})
	f.transactions.transactions = append(f.transactions.transactions, domain.Transaction{
		ID: "tx-transfer", HouseholdID: "house-1", Kind: domain.TransactionTransfer,
		OccurredOn: july.AddDate(0, 0, 2), Description: "To savings",
		FromAccountID: "acc-1", ToAccountID: "acc-2",
		Amount: domain.Money{Amount: 50000, Currency: "SGD"},
	})
	// USD: staticTestRates only knows SGD<->IDR, so this has no rate.
	f.addExpense("tx-no-rate", "cat-groceries", "", july.AddDate(0, 0, 3), 3999, "USD")

	got, err := f.svc.Month(ctx, "house-1", july, july.AddDate(0, 0, 17))
	if err != nil {
		t.Fatalf("month: %v", err)
	}

	if got.Spent.Amount != 0 {
		t.Fatalf("spent = %d, want 0 -- income and the transfer must not count, and the USD expense has no rate",
			got.Spent.Amount)
	}
	groceries, ok := findCategory(got.Categories, "cat-groceries")
	if !ok || groceries.Spent.Amount != 0 {
		t.Fatalf("groceries = %+v, want spent 0 -- the no-rate expense must not reach its category either", groceries)
	}
	if len(got.ExcludedNoRate) != 1 || got.ExcludedNoRate[0].TransactionID != "tx-no-rate" ||
		got.ExcludedNoRate[0].Currency != "USD" {
		t.Fatalf("excluded = %v, want one USD transaction named tx-no-rate", got.ExcludedNoRate)
	}
}

// TestBudgetMonthHidesDailyPaceForAFutureMonth: the spec hides "S$X/day
// left" when Remaining <= 0 *or* the viewed month is not the current one.
// domain.DailyPace alone only ever checks the first half -- a future month
// still gets a full DaysLeftInMonth and, with nothing spent yet, Remaining >
// 0, so without Month comparing `month` to `today` itself this would read as
// available. August is budgeted, has no spend, and is unambiguously after
// July (`today`): DaysLeft and Remaining are both positive here, which is
// exactly what would let a naive implementation show the card.
func TestBudgetMonthHidesDailyPaceForAFutureMonth(t *testing.T) {
	f := newBudgetFixture(t)
	ctx := context.Background()
	august := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	today := time.Date(2026, 7, 15, 0, 0, 0, 0, time.UTC)

	if _, err := f.svc.Save(ctx, "house-1", august, nil, []usecase.BudgetLineInput{
		{CategoryID: "cat-groceries", CapMinor: 80000},
	}); err != nil {
		t.Fatalf("save: %v", err)
	}

	got, err := f.svc.Month(ctx, "house-1", august, today)
	if err != nil {
		t.Fatalf("month: %v", err)
	}

	if got.Remaining <= 0 || got.DaysLeft <= 0 {
		t.Fatalf("remaining = %d, daysLeft = %d -- both must be positive for this test to actually exercise the guard",
			got.Remaining, got.DaysLeft)
	}
	if got.DailyPaceOK {
		t.Fatal("dailyPaceOK = true, want false -- August is not today's month (July), so the pace card must hide " +
			"even though Remaining and DaysLeft are both positive")
	}
	if got.DailyPace != 0 {
		t.Fatalf("dailyPace = %d, want 0 when hidden", got.DailyPace)
	}
}

// TestBudgetMonthUnbudgetedStillReportsSpend: the month has no budget row at
// all, yet the categories grid still shows real spend against a zero cap,
// and that zero cap must never itself read as "over".
func TestBudgetMonthUnbudgetedStillReportsSpend(t *testing.T) {
	f := newBudgetFixture(t)
	ctx := context.Background()
	july := julyMonth()

	f.addExpense("tx-groceries", "cat-groceries", "", july.AddDate(0, 0, 5), 12000, "SGD")

	got, err := f.svc.Month(ctx, "house-1", july, july.AddDate(0, 0, 17))
	if err != nil {
		t.Fatalf("month: %v", err)
	}

	if got.Budget != nil {
		t.Fatalf("budget = %+v, want nil (never set)", got.Budget)
	}
	if got.Spent.Amount != 12000 {
		t.Fatalf("spent = %d, want 12000", got.Spent.Amount)
	}
	groceries, ok := findCategory(got.Categories, "cat-groceries")
	if !ok {
		t.Fatal("groceries missing from Categories -- it must still render even with no budget")
	}
	if groceries.Cap.Amount != 0 {
		t.Fatalf("groceries cap = %d, want 0 (no line exists)", groceries.Cap.Amount)
	}
	if groceries.Spent.Amount != 12000 {
		t.Fatalf("groceries spent = %d, want 12000", groceries.Spent.Amount)
	}
	if groceries.Over {
		t.Fatal("groceries.Over = true, want false -- a category with no cap line can never be over")
	}
}

// TestBudgetMonthArchivedCategoryWithCapStillRenders: archiving hides a
// category from new-cap pickers, never from a month it already has a line
// in (spec decision 5).
func TestBudgetMonthArchivedCategoryWithCapStillRenders(t *testing.T) {
	f := newBudgetFixture(t)
	ctx := context.Background()
	july := julyMonth()

	if _, err := f.svc.Save(ctx, "house-1", july, nil, []usecase.BudgetLineInput{
		{CategoryID: "cat-dining", CapMinor: 45000},
	}); err != nil {
		t.Fatalf("save: %v", err)
	}
	if _, err := f.categories.SetArchived(ctx, "house-1", "cat-dining", true); err != nil {
		t.Fatalf("archive: %v", err)
	}

	got, err := f.svc.Month(ctx, "house-1", july, july.AddDate(0, 0, 17))
	if err != nil {
		t.Fatalf("month: %v", err)
	}

	dining, ok := findCategory(got.Categories, "cat-dining")
	if !ok {
		t.Fatal("dining missing from Categories -- an archived cap must still render")
	}
	if !dining.Archived {
		t.Fatal("dining.Archived = false, want true")
	}
	if dining.Cap.Amount != 45000 {
		t.Fatalf("dining cap = %d, want 45000", dining.Cap.Amount)
	}
}

// TestBudgetMonthGroupsSpendByPerson: two memberships each get a converted
// total; the unattributed expense (PaidByMembershipID "") gets no row at
// all, per the spec's rejection of a synthetic "shared" bucket.
func TestBudgetMonthGroupsSpendByPerson(t *testing.T) {
	f := newBudgetFixture(t)
	ctx := context.Background()
	july := julyMonth()

	f.addMember("membership-andreas", "user-andreas", "Andreas")
	f.addMember("membership-mira", "user-mira", "Mira")

	f.addExpense("tx-1", "cat-groceries", "membership-andreas", july.AddDate(0, 0, 5), 3000, "SGD")
	f.addExpense("tx-2", "cat-dining", "membership-mira", july.AddDate(0, 0, 6), 5000, "SGD")
	f.addExpense("tx-3", "cat-groceries", "", july.AddDate(0, 0, 7), 2000, "SGD")

	got, err := f.svc.Month(ctx, "house-1", july, july.AddDate(0, 0, 17))
	if err != nil {
		t.Fatalf("month: %v", err)
	}

	if len(got.ByPerson) != 2 {
		t.Fatalf("byPerson = %+v, want exactly 2 rows", got.ByPerson)
	}
	andreas, ok := findPerson(got.ByPerson, "membership-andreas")
	if !ok || andreas.Name != "Andreas" || andreas.Spent.Amount != 3000 {
		t.Fatalf("andreas = %+v, want name Andreas, spent 3000", andreas)
	}
	mira, ok := findPerson(got.ByPerson, "membership-mira")
	if !ok || mira.Name != "Mira" || mira.Spent.Amount != 5000 {
		t.Fatalf("mira = %+v, want name Mira, spent 5000", mira)
	}
	if _, ok := findPerson(got.ByPerson, ""); ok {
		t.Fatal("an unattributed row exists -- PaidByMembershipID \"\" must never get a ByPerson row")
	}
}

// TestBudgetSaveValidates covers Save's whole contract in one test, the same
// shape TestBudgetHistoryMarksOnlyTheCurrentMonthOpen uses for its own
// multi-assertion story: duplicate, negative-cap and negative-income are all
// refused before the repository is ever called, an unknown category's error
// passes through untouched, and a nil expected income round-trips as nil.
func TestBudgetSaveValidates(t *testing.T) {
	f := newBudgetFixture(t)
	ctx := context.Background()
	july := julyMonth()

	_, err := f.svc.Save(ctx, "house-1", july, nil, []usecase.BudgetLineInput{
		{CategoryID: "cat-groceries", CapMinor: 1000},
		{CategoryID: "cat-groceries", CapMinor: 2000},
	})
	if !errors.Is(err, domain.ErrBudgetLineDuplicate) {
		t.Fatalf("duplicate category err = %v, want domain.ErrBudgetLineDuplicate", err)
	}

	_, err = f.svc.Save(ctx, "house-1", july, nil, []usecase.BudgetLineInput{
		{CategoryID: "cat-groceries", CapMinor: -1},
	})
	if !errors.Is(err, domain.ErrBudgetCapNegative) {
		t.Fatalf("negative cap err = %v, want domain.ErrBudgetCapNegative", err)
	}

	negativeIncome := int64(-500000)
	_, err = f.svc.Save(ctx, "house-1", july, &negativeIncome, []usecase.BudgetLineInput{
		{CategoryID: "cat-groceries", CapMinor: 1000},
	})
	if !errors.Is(err, domain.ErrBudgetIncomeNegative) {
		t.Fatalf("negative income err = %v, want domain.ErrBudgetIncomeNegative", err)
	}

	// Arm the repository double's household-ownership check: only these two
	// ids belong to "house-1" as far as Upsert is concerned.
	f.budgets.knownCategories("cat-groceries", "cat-dining")

	_, err = f.svc.Save(ctx, "house-1", july, nil, []usecase.BudgetLineInput{
		{CategoryID: "cat-nope", CapMinor: 1000},
	})
	if err == nil {
		t.Fatal("unknown category err = nil, want the repository's own error to pass through")
	}
	if errors.Is(err, domain.ErrBudgetLineDuplicate) || errors.Is(err, domain.ErrBudgetCapNegative) {
		t.Fatalf("unknown category err = %v, want the repo's passthrough error, not a service-level sentinel", err)
	}

	saved, err := f.svc.Save(ctx, "house-1", july, nil, []usecase.BudgetLineInput{
		{CategoryID: "cat-groceries", CapMinor: 80000},
	})
	if err != nil {
		t.Fatalf("save with nil expected income: %v", err)
	}
	if saved.ExpectedIncome != nil {
		t.Fatalf("expectedIncome = %+v, want nil -- nil must round-trip as nil, not a stored zero", saved.ExpectedIncome)
	}
}

// TestBudgetHistoryMarksOnlyTheCurrentMonthOpen pins the windowing semantics
// fakeBudgetRepo.History documents: the viewed month (if budgeted) plus
// closed months walked back, newest first, an unbudgeted month simply
// absent rather than zero-filled.
func TestBudgetHistoryMarksOnlyTheCurrentMonthOpen(t *testing.T) {
	f := newBudgetFixture(t)
	ctx := context.Background()

	may := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	june := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	july := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	today := time.Date(2026, 7, 15, 0, 0, 0, 0, time.UTC)

	for _, m := range []time.Time{may, june, july} {
		if _, err := f.svc.Save(ctx, "house-1", m, nil, []usecase.BudgetLineInput{
			{CategoryID: "cat-groceries", CapMinor: 10000},
		}); err != nil {
			t.Fatalf("save %v: %v", m, err)
		}
	}
	// April is never budgeted at all -- it must not appear.

	got, err := f.svc.History(ctx, "house-1", july, today, 3)
	if err != nil {
		t.Fatalf("history: %v", err)
	}

	if len(got) != 3 {
		t.Fatalf("history = %+v, want exactly 3 rows (July, June, May -- April absent)", got)
	}
	if !got[0].Month.Equal(july) || got[0].Closed {
		t.Fatalf("row 0 = %+v, want July, Closed false", got[0])
	}
	if !got[1].Month.Equal(june) || !got[1].Closed {
		t.Fatalf("row 1 = %+v, want June, Closed true", got[1])
	}
	if !got[2].Month.Equal(may) || !got[2].Closed {
		t.Fatalf("row 2 = %+v, want May, Closed true", got[2])
	}
}

// TestBudgetHistoryClosedFollowsTodayNotTheAnchorMonth pins which of
// History's two time parameters governs "current": `today`, not `month`.
// The previous test alone cannot tell them apart -- it always calls History
// with `month` and `today` in the same calendar month, so implementing
// Closed off either one passes it identically. Here the anchor is June while
// today is July: none of the returned rows (June, May) is the month actually
// in progress, so every row -- including the anchor month itself -- must
// come back Closed.
func TestBudgetHistoryClosedFollowsTodayNotTheAnchorMonth(t *testing.T) {
	f := newBudgetFixture(t)
	ctx := context.Background()

	may := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	june := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	today := time.Date(2026, 7, 15, 0, 0, 0, 0, time.UTC)

	for _, m := range []time.Time{may, june} {
		if _, err := f.svc.Save(ctx, "house-1", m, nil, []usecase.BudgetLineInput{
			{CategoryID: "cat-groceries", CapMinor: 10000},
		}); err != nil {
			t.Fatalf("save %v: %v", m, err)
		}
	}

	// Anchor on June (not July, the month `today` actually falls in).
	got, err := f.svc.History(ctx, "house-1", june, today, 3)
	if err != nil {
		t.Fatalf("history: %v", err)
	}

	if len(got) != 2 {
		t.Fatalf("history = %+v, want exactly 2 rows (June, May)", got)
	}
	if !got[0].Month.Equal(june) || !got[0].Closed {
		t.Fatalf("row 0 = %+v, want June, Closed true -- June is the query's anchor but not today's month", got[0])
	}
	if !got[1].Month.Equal(may) || !got[1].Closed {
		t.Fatalf("row 1 = %+v, want May, Closed true", got[1])
	}
}
