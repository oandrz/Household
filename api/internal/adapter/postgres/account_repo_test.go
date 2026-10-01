package postgres_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/andreasoentoro/hearth/api/internal/adapter/postgres"
	"github.com/andreasoentoro/hearth/api/internal/domain"
	"github.com/andreasoentoro/hearth/api/internal/usecase"
)

// fixtureOwnerName is the display name newAccountFixture's membership is
// created with. TestAccountWithARealOwnerCarriesTheirDisplayName asserts
// against it directly rather than a fourth return value, since no other
// fixture caller in this file needs an owner name.
const fixtureOwnerName = "Christine"

// TestSharedAccountRoundTripsAsAnEmptyOwner is the "" <-> SQL NULL convention
// at the boundary it exists for: a shared account stores NULL and must come
// back as "", never as a zero uuid or any other sentinel.
func TestSharedAccountRoundTripsAsAnEmptyOwner(t *testing.T) {
	db, householdID, _ := newAccountFixture(t)
	repo := postgres.NewAccountRepo(db)
	ctx := context.Background()

	created, err := repo.Create(ctx, domain.Account{
		HouseholdID:        householdID,
		Nickname:           "OCBC Joint Savings",
		Type:               domain.AccountCash,
		OwnerMembershipID:  "",
		OpeningBalance:     domain.Money{Amount: 4_690_000, Currency: "SGD"},
		OpeningBalanceAsOf: time.Date(2026, 7, 26, 0, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if created.OwnerMembershipID != "" {
		t.Errorf("OwnerMembershipID = %q, want \"\"", created.OwnerMembershipID)
	}

	view, err := repo.Get(ctx, householdID, created.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if view.Account.OwnerMembershipID != "" || view.OwnerName != "" {
		t.Errorf("read back %+v, want an empty owner and an empty owner name", view)
	}
}

// TestAccountWithARealOwnerCarriesTheirDisplayName is the other half of
// TestSharedAccountRoundTripsAsAnEmptyOwner. The in-memory double leaves
// OwnerName empty, so this is the only test that sees a real value.
// OwnerName comes from a LEFT JOIN in GetAccount and ListAccounts that could
// silently select the wrong column or table and still compile and return
// some string, so only an assertion on the actual value catches that --
// checked in both Get and List, not just one of the three query converters.
func TestAccountWithARealOwnerCarriesTheirDisplayName(t *testing.T) {
	db, householdID, membershipID := newAccountFixture(t)
	repo := postgres.NewAccountRepo(db)
	ctx := context.Background()

	created, err := repo.Create(ctx, domain.Account{
		HouseholdID:        householdID,
		Nickname:           "Christine's CPF",
		Type:               domain.AccountInvestment,
		OwnerMembershipID:  membershipID,
		OpeningBalance:     domain.Money{Amount: 12_000_000, Currency: "SGD"},
		OpeningBalanceAsOf: time.Date(2026, 7, 26, 0, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	view, err := repo.Get(ctx, householdID, created.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if view.Account.OwnerMembershipID != membershipID {
		t.Errorf("OwnerMembershipID = %q, want %q", view.Account.OwnerMembershipID, membershipID)
	}
	if view.OwnerName != fixtureOwnerName {
		t.Errorf("Get: OwnerName = %q, want %q", view.OwnerName, fixtureOwnerName)
	}

	list, err := repo.List(ctx, householdID, false)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(list) != 1 || list[0].OwnerName != fixtureOwnerName {
		t.Errorf("List: %+v, want one account owned by %q", list, fixtureOwnerName)
	}
}

// TestRemovingAMemberLeavesTheirAccountsShared proves ON DELETE SET NULL. This
// is database behaviour, so only a database can show it -- no service test can.
func TestRemovingAMemberLeavesTheirAccountsShared(t *testing.T) {
	db, householdID, membershipID := newAccountFixture(t)
	repo := postgres.NewAccountRepo(db)
	ctx := context.Background()

	created, err := repo.Create(ctx, domain.Account{
		HouseholdID:        householdID,
		Nickname:           "DBS Everyday",
		Type:               domain.AccountCash,
		OwnerMembershipID:  membershipID,
		OpeningBalance:     domain.Money{Amount: 824_055, Currency: "SGD"},
		OpeningBalanceAsOf: time.Date(2026, 7, 26, 0, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	if _, err := db.Pool().Exec(ctx, `DELETE FROM memberships WHERE id = $1`, membershipID); err != nil {
		t.Fatalf("delete membership: %v", err)
	}

	view, err := repo.Get(ctx, householdID, created.ID)
	if err != nil {
		t.Fatalf("Get after member removal: %v", err)
	}
	if view.Account.OwnerMembershipID != "" {
		t.Errorf("OwnerMembershipID = %q, want \"\" -- the account should have fallen back to shared",
			view.Account.OwnerMembershipID)
	}
	if view.Account.Nickname != "DBS Everyday" {
		t.Error("the account itself was deleted; removing a member must not take their accounts with it")
	}
}

// TestGetInAnotherHouseholdIsNotFound: an account in someone else's household
// must be indistinguishable from one that does not exist. A caller who can
// tell the two apart can enumerate account ids across tenants.
func TestGetInAnotherHouseholdIsNotFound(t *testing.T) {
	db, householdID, _ := newAccountFixture(t)
	repo := postgres.NewAccountRepo(db)
	ctx := context.Background()

	created, err := repo.Create(ctx, domain.Account{
		HouseholdID:        householdID,
		Nickname:           "BCA Tahapan",
		Type:               domain.AccountCash,
		OpeningBalance:     domain.Money{Amount: 8_540_000_000, Currency: "IDR"},
		OpeningBalanceAsOf: time.Date(2026, 7, 26, 0, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	other := insertSecondHousehold(t, db)
	if _, err := repo.Get(ctx, other, created.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("err = %v, want domain.ErrNotFound", err)
	}

	// List is scoped by household_id exactly the same way Get is; nothing
	// above exercises that half, since every other fixture household only
	// ever holds its own accounts.
	if list, err := repo.List(ctx, other, false); err != nil || len(list) != 0 {
		t.Errorf("List(other household) = %+v (err %v), want an empty list", list, err)
	}
}

func TestSetArchivedHidesAndRestores(t *testing.T) {
	db, householdID, _ := newAccountFixture(t)
	repo := postgres.NewAccountRepo(db)
	ctx := context.Background()

	created, err := repo.Create(ctx, domain.Account{
		HouseholdID:        householdID,
		Nickname:           "Old card",
		Type:               domain.AccountCreditCard,
		OpeningBalance:     domain.Money{Amount: 12_000, Currency: "SGD"},
		OpeningBalanceAsOf: time.Date(2026, 7, 26, 0, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	at := time.Date(2026, 7, 28, 9, 0, 0, 0, time.UTC)
	if _, err := repo.SetArchived(ctx, householdID, created.ID, true, at); err != nil {
		t.Fatalf("SetArchived: %v", err)
	}

	live, err := repo.List(ctx, householdID, false)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(live) != 0 {
		t.Errorf("live = %d accounts, want 0", len(live))
	}

	all, err := repo.List(ctx, householdID, true)
	if err != nil {
		t.Fatalf("List including archived: %v", err)
	}
	if len(all) != 1 || !all[0].Account.IsArchived() {
		t.Fatalf("all = %+v, want one archived account", all)
	}

	if _, err := repo.SetArchived(ctx, householdID, created.ID, false, at); err != nil {
		t.Fatalf("restore: %v", err)
	}
	live, err = repo.List(ctx, householdID, false)
	if err != nil {
		t.Fatalf("List after restore: %v", err)
	}
	if len(live) != 1 {
		t.Errorf("live after restore = %d, want 1", len(live))
	}
}

func TestMembershipBelongsToHousehold(t *testing.T) {
	db, householdID, membershipID := newAccountFixture(t)
	repo := postgres.NewAccountRepo(db)
	ctx := context.Background()

	ok, err := repo.MembershipBelongsToHousehold(ctx, householdID, membershipID)
	if err != nil || !ok {
		t.Fatalf("own membership: ok = %v, err = %v", ok, err)
	}

	other := insertSecondHousehold(t, db)
	ok, err = repo.MembershipBelongsToHousehold(ctx, other, membershipID)
	if err != nil {
		t.Fatalf("other household: %v", err)
	}
	if ok {
		t.Error("a membership resolved against the wrong household")
	}
}

// TestOpeningBalanceAsOfKeepsItsCalendarDayRegardlessOfZone guards dateOnly's
// promise (see its doc comment in convert.go): a caller who means the 26th
// in their own zone, but is already past midnight UTC, must still see the
// 26th come back, not the 25th.
func TestOpeningBalanceAsOfKeepsItsCalendarDayRegardlessOfZone(t *testing.T) {
	db, householdID, _ := newAccountFixture(t)
	repo := postgres.NewAccountRepo(db)
	ctx := context.Background()

	sgt := time.FixedZone("SGT", 8*3600)
	asOf := time.Date(2026, 7, 26, 7, 0, 0, 0, sgt) // 2026-07-25 23:00 UTC

	created, err := repo.Create(ctx, domain.Account{
		HouseholdID:        householdID,
		Nickname:           "UOB One",
		Type:               domain.AccountCash,
		OpeningBalance:     domain.Money{Amount: 100_000, Currency: "SGD"},
		OpeningBalanceAsOf: asOf,
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	view, err := repo.Get(ctx, householdID, created.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	got := view.Account.OpeningBalanceAsOf
	if got.Year() != 2026 || got.Month() != time.July || got.Day() != 26 {
		t.Fatalf("OpeningBalanceAsOf = %v, want 2026-07-26 -- the day the caller meant in their own zone", got)
	}
}

// newAccountFixture builds the household and membership every account test
// needs: household_id for the accounts table's foreign key, and a real
// membership (owned by fixtureOwnerName) to use as an account's owner --
// through the repos, not raw SQL, since the owner must be a real membership
// row, the same one MembershipBelongsToHousehold and the foreign-key check
// use.
func newAccountFixture(t *testing.T) (db *postgres.DB, householdID, membershipID string) {
	t.Helper()
	db = openTestDB(t)
	ctx := context.Background()

	households := postgres.NewHouseholdRepo(db)
	users := postgres.NewUserRepo(db)
	members := postgres.NewMembershipRepo(db)

	h, err := households.Create(ctx, domain.Household{
		Timezone: "UTC",
		Name:     "Test", FamilyName: "Household",
		PrimaryCurrency: "SGD", SecondaryCurrency: "IDR", ShowSecondaryCurrency: true,
	})
	if err != nil {
		t.Fatalf("create household: %v", err)
	}
	u, err := users.Create(ctx, "christine@hearth.family", "hash", fixtureOwnerName)
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	m, err := members.Create(ctx, domain.Membership{
		HouseholdID: h.ID, UserID: u.ID, Role: domain.RoleOwner,
		Capabilities: domain.AllCapabilities(),
	})
	if err != nil {
		t.Fatalf("create membership: %v", err)
	}
	return db, h.ID, m.ID
}

// insertSecondHousehold inserts a household distinct from newAccountFixture's,
// for the tests proving a household cannot reach another's accounts or
// memberships. It wraps insertTestHousehold (schema_test.go) rather than
// duplicating a household-inserting helper.
func insertSecondHousehold(t *testing.T, db *postgres.DB) string {
	t.Helper()
	return insertTestHousehold(t, db)
}

// TestAccountBalanceSumsItsTransactions pins AccountView.Balance's contract:
// the opening balance plus every transaction dated on or after
// opening_balance_as_of.
func TestAccountBalanceSumsItsTransactions(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	accounts := postgres.NewAccountRepo(db)
	transactions := postgres.NewTransactionRepo(db)
	householdID := insertTestHousehold(t, db)

	var accountID string
	err := db.Pool().QueryRow(ctx,
		`INSERT INTO accounts (household_id, nickname, type, opening_balance_minor,
		                       opening_balance_currency, opening_balance_as_of)
		 VALUES ($1, 'DBS Everyday', 'cash', 100000, 'SGD', DATE '2026-07-10') RETURNING id`,
		householdID).Scan(&accountID)
	if err != nil {
		t.Fatalf("insert account: %v", err)
	}

	mustCreate := func(kind domain.TransactionKind, day int, minor int64, from, to string) {
		t.Helper()
		if _, err := transactions.Create(ctx, domain.Transaction{
			HouseholdID: householdID, Kind: kind, OccurredOn: july(day),
			Description: "Row", FromAccountID: from, ToAccountID: to,
			Amount: domain.Money{Amount: minor, Currency: "SGD"},
		}); err != nil {
			t.Fatalf("create: %v", err)
		}
	}

	mustCreate(domain.TransactionExpense, 12, 5000, accountID, "")
	mustCreate(domain.TransactionIncome, 14, 20000, "", accountID)
	// Dated ON the opening date: the opening balance is the figure at the
	// START of that day, so this counts.
	mustCreate(domain.TransactionExpense, 10, 7777, accountID, "")
	// Dated before it: already inside the opening figure, still excluded.
	mustCreate(domain.TransactionExpense, 3, 9999, accountID, "")

	views, err := accounts.List(ctx, householdID, false)
	if err != nil {
		t.Fatalf("list accounts: %v", err)
	}
	if len(views) != 1 {
		t.Fatalf("got %d accounts, want 1", len(views))
	}
	// 100000 - 5000 + 20000 - 7777
	if got := views[0].Balance.Amount; got != 107223 {
		t.Fatalf("balance = %d, want 107223 (opening 100000, -5000, +20000, -7777 on the "+
			"opening date itself, and nothing from the one dated before it)", got)
	}
	// Get must agree with List. Two queries computing one figure is where they
	// drift.
	view, err := accounts.Get(ctx, householdID, accountID)
	if err != nil {
		t.Fatalf("get account: %v", err)
	}
	if view.Balance.Amount != views[0].Balance.Amount {
		t.Fatalf("Get says %d and List says %d", view.Balance.Amount, views[0].Balance.Amount)
	}

	// ListAccountsIncludingArchived carries its own copy of the same
	// expression -- three queries computing one figure, not two -- so it gets
	// its own assertion instead of trusting the copy-paste.
	all, err := accounts.List(ctx, householdID, true)
	if err != nil {
		t.Fatalf("list accounts including archived: %v", err)
	}
	if len(all) != 1 || all[0].Balance.Amount != views[0].Balance.Amount {
		t.Fatalf("ListAccountsIncludingArchived balance = %+v, want %d to match List and Get",
			all, views[0].Balance.Amount)
	}
}

// insertTestAccountAsOf is insertTestAccount with the two columns the
// balance sum actually reads: the opening figure and the day it was true --
// the transfer tests below need those to differ per account.
func insertTestAccountAsOf(
	t *testing.T, db *postgres.DB, householdID, nickname, currency string,
	openingMinor int64, asOf time.Time,
) string {
	t.Helper()
	var id string
	err := db.Pool().QueryRow(context.Background(),
		`INSERT INTO accounts (household_id, nickname, type, opening_balance_minor,
		                       opening_balance_currency, opening_balance_as_of)
		 VALUES ($1, $2, 'cash', $3, $4, $5) RETURNING id`,
		householdID, nickname, openingMinor, currency, asOf).Scan(&id)
	if err != nil {
		t.Fatalf("insert account %s: %v", nickname, err)
	}
	return id
}

// balancesByNickname is what both transfer tests below assert against: two
// accounts, read in one List call, so the pair's total is read from a single
// consistent view rather than two.
func balancesByNickname(t *testing.T, views []usecase.AccountView) map[string]int64 {
	t.Helper()
	out := map[string]int64{}
	for _, v := range views {
		out[v.Account.Nickname] = v.Balance.Amount
	}
	return out
}

// A same-currency transfer must leave the two accounts' total exactly where
// it was: the money did not leave the household, it changed hands inside it.
// That is what one row carrying both sides of a transfer, rather than two
// rows that could disagree, protects.
//
// It is asserted here against Postgres, not against
// domain.Transaction.BalanceEffect, because the arithmetic that ships is the
// SQL in queries/account.sql (see BalanceEffect's own doc comment) -- a
// domain-only test could pass while the balance every screen shows went wrong.
func TestASameCurrencyTransferLeavesTheTwoAccountsTotalUnchanged(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	accounts := postgres.NewAccountRepo(db)
	transactions := postgres.NewTransactionRepo(db)
	householdID := insertTestHousehold(t, db)

	dbs := insertTestAccountAsOf(t, db, householdID, "DBS", "SGD", 100000, july(1))
	ocbc := insertTestAccountAsOf(t, db, householdID, "OCBC", "SGD", 50000, july(1))
	const openingTotal = 150000

	if _, err := transactions.Create(ctx, domain.Transaction{
		HouseholdID: householdID, Kind: domain.TransactionTransfer,
		OccurredOn: july(20), Description: "Move to savings",
		FromAccountID: dbs, ToAccountID: ocbc,
		Amount: domain.Money{Amount: 30000, Currency: "SGD"},
	}); err != nil {
		t.Fatalf("create transfer: %v", err)
	}

	views, err := accounts.List(ctx, householdID, false)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	got := balancesByNickname(t, views)
	// Equal and opposite, stated as two figures rather than only as a sum:
	// a sum alone would still hold if both sides moved the same way by
	// amounts that happened to cancel against the openings.
	if got["DBS"] != 70000 || got["OCBC"] != 80000 {
		t.Fatalf("balances = %v, want DBS 70000 and OCBC 80000 (S$300 moved from one to the other)", got)
	}
	if total := got["DBS"] + got["OCBC"]; total != openingTotal {
		t.Fatalf("the pair totals %d after the transfer, want %d -- a transfer must not "+
			"change what the household is worth", total, openingTotal)
	}
}

// A transfer dated before one account's opening date and after the other's
// moves exactly one of the two balances.
//
// The comparison lives in queries/account.sql as
// `t.occurred_on >= a.opening_balance_as_of`, evaluated once per *account*,
// so "before the opening date" can be true for one side and false for the
// other -- no single transaction-level flag could say so. Hoist the
// comparison to the transaction and one of the two balances below goes
// wrong, which is what this test catches.
//
// The pair's total does change here: OCBC's opening figure (an assertion
// about 31 July) already accounts for the transfer, so counting it again
// would double it.
func TestATransferStraddlingOneOpeningDateMovesOnlyThatSideOfIt(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	accounts := postgres.NewAccountRepo(db)
	transactions := postgres.NewTransactionRepo(db)
	householdID := insertTestHousehold(t, db)

	// The transfer on the 20th is after DBS's opening date and before OCBC's.
	dbs := insertTestAccountAsOf(t, db, householdID, "DBS", "SGD", 100000, july(1))
	ocbc := insertTestAccountAsOf(t, db, householdID, "OCBC", "SGD", 50000, july(31))

	created, err := transactions.Create(ctx, domain.Transaction{
		HouseholdID: householdID, Kind: domain.TransactionTransfer,
		OccurredOn: july(20), Description: "Move to savings",
		FromAccountID: dbs, ToAccountID: ocbc,
		Amount: domain.Money{Amount: 30000, Currency: "SGD"},
	})
	if err != nil {
		t.Fatalf("create transfer: %v", err)
	}

	// The ledger's own note about this row must say the same thing the two
	// balances do, separately per side -- one flag for both sides would have
	// to be true or false, libelling one of the two accounts either way.
	view, err := transactions.Get(ctx, householdID, created.ID)
	if err != nil {
		t.Fatalf("get transfer: %v", err)
	}
	if view.BeforeFromAccountOpening == nil || *view.BeforeFromAccountOpening {
		t.Fatalf("beforeFromAccountOpening = %v, want non-nil false -- 20 July is after DBS's 1 July opening",
			view.BeforeFromAccountOpening)
	}
	if view.BeforeToAccountOpening == nil || !*view.BeforeToAccountOpening {
		t.Fatalf("beforeToAccountOpening = %v, want non-nil true -- 20 July predates OCBC's 31 July opening",
			view.BeforeToAccountOpening)
	}

	views, err := accounts.List(ctx, householdID, false)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	got := balancesByNickname(t, views)
	if got["DBS"] != 70000 {
		t.Fatalf("DBS balance = %d, want 70000 -- the transfer is after its opening date "+
			"and must come off it", got["DBS"])
	}
	if got["OCBC"] != 50000 {
		t.Fatalf("OCBC balance = %d, want its opening 50000 unchanged -- the transfer predates "+
			"the day that figure was asserted true, so it is already in it", got["OCBC"])
	}
}

// The defect this prevents: crediting the destination with the amount that
// left rather than what arrived would add Singapore dollars to a rupiah
// balance -- the account ends up wrong by a factor of ten thousand.
func TestACrossCurrencyTransferCreditsTheDestinationInItsOwnCurrency(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	accounts := postgres.NewAccountRepo(db)
	transactions := postgres.NewTransactionRepo(db)
	householdID := insertTestHousehold(t, db)

	dbs := insertTestAccount(t, db, householdID, "DBS", "SGD")
	bca := insertTestAccount(t, db, householdID, "BCA Tahapan", "IDR")

	received := domain.Money{Amount: 620000000, Currency: "IDR"}
	if _, err := transactions.Create(ctx, domain.Transaction{
		HouseholdID: householdID, Kind: domain.TransactionTransfer,
		OccurredOn: july(20), Description: "Transfer to BCA",
		FromAccountID: dbs, ToAccountID: bca,
		Amount: domain.Money{Amount: 50000, Currency: "SGD"}, ReceivedAmount: &received,
	}); err != nil {
		t.Fatalf("create transfer: %v", err)
	}

	views, err := accounts.List(ctx, householdID, false)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	byName := map[string]int64{}
	for _, v := range views {
		byName[v.Account.Nickname] = v.Balance.Amount
	}
	if byName["DBS"] != -50000 {
		t.Fatalf("DBS balance = %d, want -50000", byName["DBS"])
	}
	if byName["BCA Tahapan"] != 620000000 {
		t.Fatalf("BCA balance = %d, want 620000000 (the received amount, not the sent one)",
			byName["BCA Tahapan"])
	}
}

// TestMonthlyMovementsSplitsTheBalanceExpressionByMonth is the trend's whole
// correctness argument: the chart walks backwards from AccountView.Balance
// by subtracting these deltas, so this query and ListAccounts's
// balance_minor expression must filter the same rows the same way -- change
// >= to > here and the oldest bars drift from the headline figure silently.
func TestMonthlyMovementsSplitsTheBalanceExpressionByMonth(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	accounts := postgres.NewAccountRepo(db)
	transactions := postgres.NewTransactionRepo(db)
	householdID := insertTestHousehold(t, db)

	// Opened on 1 June. The 1 June expense is ON the opening date, so it
	// counts (the start-of-day rule); the 31 May one is before it and does
	// not, because that history is already inside the opening figure.
	dbs := insertTestAccountAsOf(t, db, householdID, "DBS", "SGD", 100_000,
		time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC))

	for _, tx := range []struct {
		on     time.Time
		amount int64
	}{
		{time.Date(2026, 5, 31, 0, 0, 0, 0, time.UTC), 5_000},
		{time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC), 1_000},
		{july(4), 2_000},
		{july(20), 3_000},
	} {
		if _, err := transactions.Create(ctx, domain.Transaction{
			HouseholdID: householdID, Kind: domain.TransactionExpense,
			OccurredOn: tx.on, Description: "Groceries",
			FromAccountID: dbs,
			Amount:        domain.Money{Amount: tx.amount, Currency: "SGD"},
		}); err != nil {
			t.Fatalf("create transaction on %s: %v", tx.on, err)
		}
	}

	got, err := accounts.MonthlyMovements(ctx, householdID,
		time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("MonthlyMovements: %v", err)
	}

	byMonth := map[string]int64{}
	for _, m := range got {
		if m.AccountID != dbs {
			t.Fatalf("movement for account %s, want only %s", m.AccountID, dbs)
		}
		if m.Delta.Currency != "SGD" {
			t.Errorf("delta currency = %q, want SGD (the account's own)", m.Delta.Currency)
		}
		byMonth[m.Month.Format("2006-01")] = m.Delta.Amount
	}

	// June: only the 1st counts, and an expense leaves the account.
	if byMonth["2026-06"] != -1_000 {
		t.Errorf("June = %d, want -1000 (the 31 May expense is before the opening date)", byMonth["2026-06"])
	}
	if byMonth["2026-07"] != -5_000 {
		t.Errorf("July = %d, want -5000 (2000 + 3000)", byMonth["2026-07"])
	}
	if _, ok := byMonth["2026-05"]; ok {
		t.Errorf("May is present: %v -- a transaction before the opening date must not appear at all", byMonth)
	}

	// The invariant the trend rests on: opening balance plus every delta in
	// the window equals the balance the Finances screen prints.
	views, err := accounts.List(ctx, householdID, false)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	var walked int64 = 100_000
	for _, delta := range byMonth {
		walked += delta
	}
	if got := balancesByNickname(t, views)["DBS"]; got != walked {
		t.Fatalf("balance = %d but opening plus the deltas is %d -- the two "+
			"expressions disagree, which is exactly what makes the chart lie", got, walked)
	}
}

// TestMonthlyMovementsCreditsTheReceivingSideInItsOwnCurrency covers what a
// single-account test cannot: a cross-currency transfer credits the
// destination with received_amount_minor, what actually landed -- using
// amount_minor there would credit an IDR account a figure of SGD.
//
// BCA opens on the day the transfer lands, deliberately: only then can this
// test tell the incoming side's >= from a > mutation, since a later opening
// date would let both comparisons admit the row identically.
func TestMonthlyMovementsCreditsTheReceivingSideInItsOwnCurrency(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	accounts := postgres.NewAccountRepo(db)
	transactions := postgres.NewTransactionRepo(db)
	householdID := insertTestHousehold(t, db)

	dbs := insertTestAccountAsOf(t, db, householdID, "DBS", "SGD", 100_000, july(1))
	bca := insertTestAccountAsOf(t, db, householdID, "BCA", "IDR", 0, july(10))

	if _, err := transactions.Create(ctx, domain.Transaction{
		HouseholdID: householdID, Kind: domain.TransactionTransfer,
		OccurredOn: july(10), Description: "To Jakarta",
		FromAccountID: dbs, ToAccountID: bca,
		Amount:         domain.Money{Amount: 10_000, Currency: "SGD"},
		ReceivedAmount: &domain.Money{Amount: 124_100_000, Currency: "IDR"},
	}); err != nil {
		t.Fatalf("create transfer: %v", err)
	}

	got, err := accounts.MonthlyMovements(ctx, householdID, july(1))
	if err != nil {
		t.Fatalf("MonthlyMovements: %v", err)
	}
	byAccount := map[string]usecase.AccountMonthMovement{}
	for _, m := range got {
		byAccount[m.AccountID] = m
	}
	if byAccount[dbs].Delta.Amount != -10_000 || byAccount[dbs].Delta.Currency != "SGD" {
		t.Errorf("DBS = %+v, want -10000 SGD", byAccount[dbs].Delta)
	}
	if byAccount[bca].Delta.Amount != 124_100_000 || byAccount[bca].Delta.Currency != "IDR" {
		t.Errorf("BCA = %+v, want 124100000 IDR -- what landed, in the account's own currency", byAccount[bca].Delta)
	}
}
