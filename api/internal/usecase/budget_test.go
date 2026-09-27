package usecase_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/andreasoentoro/hearth/api/internal/domain"
	"github.com/andreasoentoro/hearth/api/internal/usecase"
)

// budgetFixture wires a BudgetService against in-memory doubles: household
// "house-1" (SGD), two expense categories, and one income category so a
// test can prove Categories excludes it (the same Kind filter
// validateCategory applies).
type budgetFixture struct {
	svc          *usecase.BudgetService
	budgets      *fakeBudgetRepo
	transactions *fakeTransactionRepo
	categories   *fakeCategoryRepo
	households   *householdDouble
	members      *membershipDouble
	goals        *goalDouble
	fx           *fxDouble
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
	// goals is wired both ways: fakeBudgetRepo.RollOverToGoal writes into it,
	// and goalDouble.DeleteContribution reaches back to clear a rollover
	// stamp (see fakeBudgetRepo.setGoals).
	goals := newGoalDouble()
	budgets.setGoals(goals)
	goals.setBudgets(budgets)

	fx := newFXDouble()
	svc := usecase.NewBudgetService(usecase.BudgetDeps{
		Budgets:      budgets,
		Transactions: transactions,
		Categories:   categories,
		Households:   households,
		Members:      members,
		FX:           fx,
		Goals:        goals,
	})

	return &budgetFixture{
		svc: svc, budgets: budgets, transactions: transactions,
		categories: categories, households: households, members: members,
		goals: goals, fx: fx,
	}
}

// seedGoal writes a goal directly through the GoalRepository double, the
// same way addExpense seeds the ledger: a RollOver test needs a real id
// without depending on GoalService's own validation. householdID lets a
// test build a goal in another household, which Get's scoping must catch
// (TestBudgetRollOverRefusesAGoalFromAnotherHousehold).
func (f *budgetFixture) seedGoal(t *testing.T, householdID, name, currency string, targetMinor int64, archived bool) domain.Goal {
	t.Helper()
	ctx := context.Background()
	g, err := f.goals.Create(ctx, domain.Goal{
		HouseholdID: householdID,
		Name:        name,
		Target:      domain.Money{Amount: targetMinor, Currency: currency},
	}, 0, julyMonth())
	if err != nil {
		t.Fatalf("seed goal: %v", err)
	}
	if archived {
		g, err = f.goals.SetArchived(ctx, householdID, g.ID, true, julyMonth())
		if err != nil {
			t.Fatalf("archive seeded goal: %v", err)
		}
	}
	return g
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

	// cat-income must never get a Categories row -- caps envelope spending
	// only, so this exercises buildCategoryViews' Kind filter, not just the
	// row count.
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

// TestBudgetMonthSpentReusesTheMonthSummaryRule proves the spec's "reused
// exactly" claim: income and transfers never move Spent, and a no-rate
// expense is excluded from Spent and its category but still named in
// ExcludedNoRate.
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
	// USD: the FX double only knows SGD<->IDR, so this has no rate.
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

// TestBudgetMonthHidesDailyPaceForAFutureMonth: domain.DailyPace only ever
// checks Remaining <= 0, so Month itself must also compare `month` to
// `today`. August here is budgeted with no spend, so DaysLeft and Remaining
// are both positive -- exactly what would let a naive implementation still
// show the pace card.
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

// TestBudgetMonthUnbudgetedStillReportsSpend: the month has no budget row
// at all, yet Categories still shows real spend against a zero cap -- and
// that zero cap must never read as "over".
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
// in.
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
// total; the unattributed expense (PaidByMembershipID "") gets its own row
// rather than being dropped (see TestByPersonRowsSumToSpent). This is not
// the "Kids (shared)" grouping the spec rejects -- it attributes spend to
// nobody, not to people who never paid.
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

	if len(got.ByPerson) != 3 {
		t.Fatalf("byPerson = %+v, want exactly 3 rows (two members plus unattributed)", got.ByPerson)
	}
	andreas, ok := findPerson(got.ByPerson, "membership-andreas")
	if !ok || andreas.Name != "Andreas" || andreas.Spent.Amount != 3000 {
		t.Fatalf("andreas = %+v, want name Andreas, spent 3000", andreas)
	}
	mira, ok := findPerson(got.ByPerson, "membership-mira")
	if !ok || mira.Name != "Mira" || mira.Spent.Amount != 5000 {
		t.Fatalf("mira = %+v, want name Mira, spent 5000", mira)
	}
	unattributed, ok := findPerson(got.ByPerson, "")
	if !ok || unattributed.Name != "" || unattributed.Spent.Amount != 2000 {
		t.Fatalf("unattributed = %+v, want empty name, spent 2000 -- copy for the row lives in the frontend, not Go", unattributed)
	}
}

// TestByPersonRowsSumToSpent guards a shipped defect: a transaction with no
// payer was counted in Spent but dropped from ByPerson, so the rows
// quietly summed to less than Spent. Don't drop the unattributed row again
// -- Bills makes a payer-less transaction (a bill with no "Paid by") the
// common case, not the exception.
func TestByPersonRowsSumToSpent(t *testing.T) {
	f := newBudgetFixture(t)
	ctx := context.Background()
	july := julyMonth()

	f.addMember("membership-andreas", "user-andreas", "Andreas")

	// The unattributed expense is dated BEFORE the attributed one, and is
	// the first transaction appended -- proving personOrder sorts it last
	// on its own merit, not because it arrived after every attributed row.
	f.addExpense("tx-utilities", "cat-dining", "", july.AddDate(0, 0, 5), 14230, "SGD") // a bill payment, no payer
	f.addExpense("tx-groceries", "cat-groceries", "membership-andreas", july.AddDate(0, 0, 6), 12000, "SGD")

	got, err := f.svc.Month(ctx, "house-1", july, july.AddDate(0, 0, 17))
	if err != nil {
		t.Fatalf("month: %v", err)
	}
	if len(got.ByPerson) != 2 {
		t.Fatalf("got %d rows, want 2 -- unattributed spend needs a row of its own", len(got.ByPerson))
	}
	// The unattributed bucket sorts last, regardless of when its first
	// transaction happened to appear.
	last := got.ByPerson[len(got.ByPerson)-1]
	if last.MembershipID != "" {
		t.Fatalf("last row membership = %q, want the empty unattributed key", last.MembershipID)
	}
	var total int64
	for _, p := range got.ByPerson {
		total += p.Spent.Amount
	}
	// The whole point: the card's rows must account for every cent of Spent,
	// or it quietly disagrees with the figure above it.
	if total != got.Spent.Amount {
		t.Fatalf("rows sum to %d but Spent is %d", total, got.Spent.Amount)
	}
}

// TestByPersonRowsSumToSpentAcrossCurrencyConversion extends the
// sum-to-Spent guarantee to a mixed-currency month: Spent and the
// unattributed bucket both call convert() once per transaction and Add the
// identical result, so a foreign-currency unattributed transaction must not
// make the rows fall short of Spent.
func TestByPersonRowsSumToSpentAcrossCurrencyConversion(t *testing.T) {
	f := newBudgetFixture(t)
	ctx := context.Background()
	july := julyMonth()

	f.addMember("membership-andreas", "user-andreas", "Andreas")
	f.addExpense("tx-groceries", "cat-groceries", "membership-andreas", july.AddDate(0, 0, 5), 12000, "SGD")
	// IDR, not SGD -- the FX double knows SGD<->IDR, so this converts
	// rather than landing in ExcludedNoRate.
	f.addExpense("tx-foreign-bill", "cat-dining", "", july.AddDate(0, 0, 6), 1_000_000, "IDR")

	got, err := f.svc.Month(ctx, "house-1", july, july.AddDate(0, 0, 17))
	if err != nil {
		t.Fatalf("month: %v", err)
	}
	if len(got.ExcludedNoRate) != 0 {
		t.Fatalf("excludedNoRate = %+v, want none -- the FX double knows SGD<->IDR", got.ExcludedNoRate)
	}
	var total int64
	for _, p := range got.ByPerson {
		total += p.Spent.Amount
	}
	if total != got.Spent.Amount {
		t.Fatalf("rows sum to %d but Spent is %d -- conversion must land in the unattributed bucket the same way it lands in Spent", total, got.Spent.Amount)
	}
}

// TestBudgetSaveValidates covers Save's whole contract in one test:
// duplicate, negative-cap, and negative-income are refused before the
// repository is called; an unknown category's error passes through
// untouched; and a nil expected income round-trips as nil.
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

// TestBudgetHistoryMarksOnlyTheCurrentMonthOpen pins History's windowing:
// the viewed month (if budgeted) plus closed months walked back newest
// first, with an unbudgeted month simply absent rather than zero-filled.
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

// TestBudgetHistoryClosedFollowsTodayNotTheAnchorMonth pins which param
// governs "current": `today`, not `month`. The previous test can't tell
// them apart -- it always calls History with `month` and `today` in the
// same calendar month. Here the anchor is June while today is July, so
// every returned row, including the anchor month itself, must come back
// Closed.
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

// --- RollOver ------------------------------------------------------------
//
// Every RollOver test dates `today` in August so July is unambiguously
// closed, per DaysLeftInMonth's month-vs-month comparison rule.

// TestBudgetRollOverWritesTheMonthsRemainingIntoTheGoal pins the spec's own
// worked example: July budgeted S$5,200.00, spent S$3,420.00, so RollOver
// must write exactly one contribution of the difference, S$1,780.00, dated
// `today`, sourced budget_rollover, naming July as its source month.
func TestBudgetRollOverWritesTheMonthsRemainingIntoTheGoal(t *testing.T) {
	f := newBudgetFixture(t)
	ctx := context.Background()
	july := julyMonth()
	today := time.Date(2026, 8, 5, 0, 0, 0, 0, time.UTC)

	if _, err := f.svc.Save(ctx, "house-1", july, nil, []usecase.BudgetLineInput{
		{CategoryID: "cat-groceries", CapMinor: 520000}, // S$5,200.00
	}); err != nil {
		t.Fatalf("save: %v", err)
	}
	f.addExpense("tx-groceries", "cat-groceries", "", july.AddDate(0, 0, 5), 342000, "SGD") // S$3,420.00

	goal := f.seedGoal(t, "house-1", "Emergency fund", "SGD", 1000000, false)

	got, err := f.svc.RollOver(ctx, "house-1", july, goal.ID, today)
	if err != nil {
		t.Fatalf("rollover: %v", err)
	}

	if got.Amount.Amount != 178000 || got.Amount.Currency != "SGD" {
		t.Fatalf("amount = %+v, want 178000 SGD (S$1,780.00)", got.Amount)
	}
	if !got.OccurredOn.Equal(today) {
		t.Fatalf("occurredOn = %v, want today (%v)", got.OccurredOn, today)
	}
	if got.Source != domain.ContributionBudgetRollover {
		t.Fatalf("source = %v, want budget_rollover", got.Source)
	}
	if got.SourceBudgetMonth == nil || !got.SourceBudgetMonth.Equal(july) {
		t.Fatalf("sourceBudgetMonth = %v, want July", got.SourceBudgetMonth)
	}

	contribs, err := f.goals.ListContributions(ctx, "house-1", goal.ID, 0)
	if err != nil {
		t.Fatalf("list contributions: %v", err)
	}
	if len(contribs) != 1 {
		t.Fatalf("contributions = %+v, want exactly 1", contribs)
	}
	if _, done := f.budgets.rolledOverGoalID("house-1", july); !done {
		t.Fatal("July is not stamped as rolled over")
	}
}

// TestBudgetMonthRolloverAmountSurvivesALaterTransaction: Remaining is
// Budgeted minus Spent, recomputed live from the ledger on every call --
// not a record of what a past rollover actually moved. A backdated
// transaction, an edit, or a delete can all move Remaining after rollover,
// and none is blocked anywhere in this codebase. This test exercises a
// late expense landing in July after rollover: RolloverAmountMinor must
// still report the original 178000, not the new, lower Remaining.
func TestBudgetMonthRolloverAmountSurvivesALaterTransaction(t *testing.T) {
	f := newBudgetFixture(t)
	ctx := context.Background()
	july := julyMonth()
	today := time.Date(2026, 8, 5, 0, 0, 0, 0, time.UTC)

	if _, err := f.svc.Save(ctx, "house-1", july, nil, []usecase.BudgetLineInput{
		{CategoryID: "cat-groceries", CapMinor: 520000}, // S$5,200.00
	}); err != nil {
		t.Fatalf("save: %v", err)
	}
	f.addExpense("tx-groceries", "cat-groceries", "", july.AddDate(0, 0, 5), 342000, "SGD") // S$3,420.00

	goal := f.seedGoal(t, "house-1", "Bali trip", "SGD", 1000000, false)

	if _, err := f.svc.RollOver(ctx, "house-1", july, goal.ID, today); err != nil {
		t.Fatalf("rollover: %v", err)
	}

	before, err := f.svc.Month(ctx, "house-1", july, today)
	if err != nil {
		t.Fatalf("Month before the late expense: %v", err)
	}
	if before.RolloverAmountMinor == nil || *before.RolloverAmountMinor != 178000 {
		t.Fatalf("RolloverAmountMinor right after rollover = %v, want 178000 (S$1,780.00)", before.RolloverAmountMinor)
	}

	// A late July receipt entered in August -- nothing in this codebase has
	// a closed-month guard to stop it. It lands in the same category and
	// month, after the rollover already happened.
	f.addExpense("tx-late-receipt", "cat-groceries", "", july.AddDate(0, 0, 28), 15000, "SGD") // S$150.00

	after, err := f.svc.Month(ctx, "house-1", july, today)
	if err != nil {
		t.Fatalf("Month after the late expense: %v", err)
	}

	// Remaining DID move -- proving this test actually exercises the defect,
	// not a fixture that happens not to trigger it.
	if after.Remaining != 163000 {
		t.Fatalf("Remaining after the late expense = %d, want 163000 (178000 - 15000) -- "+
			"fixture is not exercising the defect if this does not hold", after.Remaining)
	}
	// RolloverAmountMinor must NOT have moved with it: it is the record of
	// what RollOver actually wrote, not Remaining recomputed a second time.
	if after.RolloverAmountMinor == nil || *after.RolloverAmountMinor != 178000 {
		t.Fatalf("RolloverAmountMinor after the late expense = %v, want unchanged 178000 -- "+
			"a live Remaining must never leak into the record of what a past rollover moved",
			after.RolloverAmountMinor)
	}
}

// TestBudgetRollOverRefusesAnOpenMonth is the rule stated twice on purpose:
// only a CLOSED month can be rolled over -- mid-month "unspent" is still
// moving, and money moved out of a figure that later shrinks can't be
// undone. The goal id here is never seeded on purpose: the month check
// must run before any repository call.
func TestBudgetRollOverRefusesAnOpenMonth(t *testing.T) {
	f := newBudgetFixture(t)
	ctx := context.Background()
	today := time.Date(2026, 8, 5, 0, 0, 0, 0, time.UTC)

	current := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	if _, err := f.svc.RollOver(ctx, "house-1", current, "goal-nonexistent", today); !errors.Is(err, domain.ErrRolloverMonthOpen) {
		t.Fatalf("current month err = %v, want domain.ErrRolloverMonthOpen", err)
	}

	future := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	if _, err := f.svc.RollOver(ctx, "house-1", future, "goal-nonexistent", today); !errors.Is(err, domain.ErrRolloverMonthOpen) {
		t.Fatalf("future month err = %v, want domain.ErrRolloverMonthOpen", err)
	}

	// A ListContributions check on "goal-nonexistent" would be tautological
	// -- an unseeded goal has no contributions regardless. rolledOverGoalID
	// is the one piece of state a wrongly-permissive month check could
	// actually touch.
	if _, done := f.budgets.rolledOverGoalID("house-1", current); done {
		t.Fatal("current month stamped as rolled over -- the month check must refuse before any write")
	}
	if _, done := f.budgets.rolledOverGoalID("house-1", future); done {
		t.Fatal("future month stamped as rolled over -- the month check must refuse before any write")
	}
}

// TestBudgetRollOverRefusesAMonthWithNoBudgetRow: a closed month can have
// spend with no caps, so this must surface as domain.ErrNotFound, not
// ErrRolloverNothingUnspent -- Budgeted is zero for an unbudgeted month, so
// a naive Remaining <= 0 check would read it as nothing to move instead.
func TestBudgetRollOverRefusesAMonthWithNoBudgetRow(t *testing.T) {
	f := newBudgetFixture(t)
	ctx := context.Background()
	july := julyMonth()
	today := time.Date(2026, 8, 5, 0, 0, 0, 0, time.UTC)

	f.addExpense("tx-groceries", "cat-groceries", "", july.AddDate(0, 0, 5), 12000, "SGD")

	goal := f.seedGoal(t, "house-1", "Emergency fund", "SGD", 1000000, false)

	_, err := f.svc.RollOver(ctx, "house-1", july, goal.ID, today)
	if !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("err = %v, want domain.ErrNotFound", err)
	}
	if errors.Is(err, domain.ErrRolloverNothingUnspent) {
		t.Fatal("err is ErrRolloverNothingUnspent -- a missing budget row must not read as a silent zero-remaining refusal")
	}
}

// TestBudgetRollOverRefusesNothingUnspent: spend at or above the cap leaves
// nothing to move.
func TestBudgetRollOverRefusesNothingUnspent(t *testing.T) {
	f := newBudgetFixture(t)
	ctx := context.Background()
	july := julyMonth()
	today := time.Date(2026, 8, 5, 0, 0, 0, 0, time.UTC)

	if _, err := f.svc.Save(ctx, "house-1", july, nil, []usecase.BudgetLineInput{
		{CategoryID: "cat-groceries", CapMinor: 50000},
	}); err != nil {
		t.Fatalf("save: %v", err)
	}
	f.addExpense("tx-groceries", "cat-groceries", "", july.AddDate(0, 0, 5), 60000, "SGD") // over budget

	goal := f.seedGoal(t, "house-1", "Emergency fund", "SGD", 1000000, false)

	_, err := f.svc.RollOver(ctx, "house-1", july, goal.ID, today)
	if !errors.Is(err, domain.ErrRolloverNothingUnspent) {
		t.Fatalf("err = %v, want domain.ErrRolloverNothingUnspent", err)
	}

	if contribs, _ := f.goals.ListContributions(ctx, "house-1", goal.ID, 0); len(contribs) != 0 {
		t.Fatalf("contributions = %+v, want none written", contribs)
	}
}

// TestBudgetRollOverRefusesANonPrimaryCurrencyGoal: budgets have no
// currency column and are implicitly the household's primary; a goal
// carries its own. Converting inside a rollover would store an unauditable
// rate, so a mismatched goal is refused even though the FX double could
// convert it -- the refusal is about auditability, not availability.
func TestBudgetRollOverRefusesANonPrimaryCurrencyGoal(t *testing.T) {
	f := newBudgetFixture(t)
	ctx := context.Background()
	july := julyMonth()
	today := time.Date(2026, 8, 5, 0, 0, 0, 0, time.UTC)

	if _, err := f.svc.Save(ctx, "house-1", july, nil, []usecase.BudgetLineInput{
		{CategoryID: "cat-groceries", CapMinor: 50000},
	}); err != nil {
		t.Fatalf("save: %v", err)
	}
	f.addExpense("tx-groceries", "cat-groceries", "", july.AddDate(0, 0, 5), 20000, "SGD")

	goal := f.seedGoal(t, "house-1", "Bali trip", "IDR", 100000000, false)

	_, err := f.svc.RollOver(ctx, "house-1", july, goal.ID, today)
	if !errors.Is(err, domain.ErrRolloverCurrencyMismatch) {
		t.Fatalf("err = %v, want domain.ErrRolloverCurrencyMismatch", err)
	}

	if contribs, _ := f.goals.ListContributions(ctx, "house-1", goal.ID, 0); len(contribs) != 0 {
		t.Fatalf("contributions = %+v, want none written", contribs)
	}
}

// TestBudgetRollOverRefusesAnArchivedGoal.
func TestBudgetRollOverRefusesAnArchivedGoal(t *testing.T) {
	f := newBudgetFixture(t)
	ctx := context.Background()
	july := julyMonth()
	today := time.Date(2026, 8, 5, 0, 0, 0, 0, time.UTC)

	if _, err := f.svc.Save(ctx, "house-1", july, nil, []usecase.BudgetLineInput{
		{CategoryID: "cat-groceries", CapMinor: 50000},
	}); err != nil {
		t.Fatalf("save: %v", err)
	}
	f.addExpense("tx-groceries", "cat-groceries", "", july.AddDate(0, 0, 5), 20000, "SGD")

	goal := f.seedGoal(t, "house-1", "Emergency fund", "SGD", 1000000, true)

	_, err := f.svc.RollOver(ctx, "house-1", july, goal.ID, today)
	if !errors.Is(err, domain.ErrGoalArchived) {
		t.Fatalf("err = %v, want domain.ErrGoalArchived", err)
	}

	if contribs, _ := f.goals.ListContributions(ctx, "house-1", goal.ID, 0); len(contribs) != 0 {
		t.Fatalf("contributions = %+v, want none written", contribs)
	}
}

// TestBudgetRollOverTwiceIsRefused: the double stamps like the real
// repository (a conditional write that finds the month already stamped),
// so the second call must fail with exactly one contribution ever written.
func TestBudgetRollOverTwiceIsRefused(t *testing.T) {
	f := newBudgetFixture(t)
	ctx := context.Background()
	july := julyMonth()
	today := time.Date(2026, 8, 5, 0, 0, 0, 0, time.UTC)

	if _, err := f.svc.Save(ctx, "house-1", july, nil, []usecase.BudgetLineInput{
		{CategoryID: "cat-groceries", CapMinor: 520000},
	}); err != nil {
		t.Fatalf("save: %v", err)
	}
	f.addExpense("tx-groceries", "cat-groceries", "", july.AddDate(0, 0, 5), 342000, "SGD")

	goal := f.seedGoal(t, "house-1", "Emergency fund", "SGD", 1000000, false)

	if _, err := f.svc.RollOver(ctx, "house-1", july, goal.ID, today); err != nil {
		t.Fatalf("first rollover: %v", err)
	}

	_, err := f.svc.RollOver(ctx, "house-1", july, goal.ID, today)
	if !errors.Is(err, domain.ErrRolloverAlreadyDone) {
		t.Fatalf("second rollover err = %v, want domain.ErrRolloverAlreadyDone", err)
	}

	contribs, err := f.goals.ListContributions(ctx, "house-1", goal.ID, 0)
	if err != nil {
		t.Fatalf("list contributions: %v", err)
	}
	if len(contribs) != 1 {
		t.Fatalf("contributions = %+v, want exactly 1 (the second call must not have written a second one)", contribs)
	}
}

// TestBudgetRollOverRefusesAGoalFromAnotherHousehold: RollOverToGoal writes
// the goal id into a SQL value position, so an id from another household
// must be caught by this service's own Goals.Get before the repository is
// called -- otherwise it hits a foreign-key violation and surfaces as an
// unmapped 500 instead of a clean domain error. Nothing may be written: not
// a contribution on the foreign goal, not a stamp on July.
func TestBudgetRollOverRefusesAGoalFromAnotherHousehold(t *testing.T) {
	f := newBudgetFixture(t)
	ctx := context.Background()
	july := julyMonth()
	today := time.Date(2026, 8, 5, 0, 0, 0, 0, time.UTC)

	if _, err := f.svc.Save(ctx, "house-1", july, nil, []usecase.BudgetLineInput{
		{CategoryID: "cat-groceries", CapMinor: 520000},
	}); err != nil {
		t.Fatalf("save: %v", err)
	}
	f.addExpense("tx-groceries", "cat-groceries", "", july.AddDate(0, 0, 5), 342000, "SGD")

	foreign := f.seedGoal(t, "house-2", "Not yours", "SGD", 1000000, false)

	_, err := f.svc.RollOver(ctx, "house-1", july, foreign.ID, today)
	if !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("err = %v, want domain.ErrNotFound", err)
	}

	if contribs, _ := f.goals.ListContributions(ctx, "house-2", foreign.ID, 0); len(contribs) != 0 {
		t.Fatalf("contributions = %+v, want none written on the foreign goal", contribs)
	}
	if _, done := f.budgets.rolledOverGoalID("house-1", july); done {
		t.Fatal("July stamped as rolled over even though the goal fetch should have failed first, before any write")
	}
}

// The budget's Spent reuses the month-summary rule, including this half of
// it: only ErrNoRate may leave an expense out; a failed lookup fails Month.
func TestBudgetMonthFailsWhenTheRateLookupItselfFails(t *testing.T) {
	f := newBudgetFixture(t)
	f.fx.failWith(errProviderDown)
	ctx := context.Background()
	july := julyMonth()

	f.addExpense("tx-idr", "cat-groceries", "", july.AddDate(0, 0, 3), 12_410, "IDR")

	_, err := f.svc.Month(ctx, "house-1", july, july.AddDate(0, 0, 17))
	if !errors.Is(err, errProviderDown) {
		t.Fatalf("Month error = %v, want the provider's error", err)
	}
}
