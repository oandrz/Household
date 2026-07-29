package postgres_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/andreasoentoro/hearth/api/internal/adapter/postgres"
	"github.com/andreasoentoro/hearth/api/internal/domain"
)

// fixtureOwnerName is the display name newAccountFixture's membership is
// created with. TestAccountWithARealOwnerCarriesTheirDisplayName asserts
// against this constant rather than newAccountFixture handing back a fourth
// value, since every other fixture caller in this file only wants the
// household and membership ids.
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
// TestSharedAccountRoundTripsAsAnEmptyOwner. AccountView.OwnerName was
// introduced with the port and had never had a value or a test -- the
// in-memory double just leaves it empty -- so the LEFT JOIN in GetAccount and
// ListAccounts is the first place it can go wrong: a join that silently
// selected the wrong column, or joined through the wrong table, would still
// compile and still return some string. Only an assertion on the actual
// value catches that, which is why this checks both Get and List rather than
// just one of the three query converters.
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
// promise (see its doc comment in convert.go): "the balance was true on the
// 26th" must not depend on the zone the request arrived from. A caller who
// means the 26th in their own zone but is already past midnight UTC must
// still see the 26th come back -- not the 25th.
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
// needs: a valid household_id to satisfy the accounts table's foreign key,
// and a real membership (owned by fixtureOwnerName) to assign as an
// account's owner. It goes through the household/user/membership repos
// rather than raw SQL, mirroring membership_repo_test.go's fixture, because
// an account's owner must be a real membership row -- the same row
// MembershipBelongsToHousehold and the owner_membership_id foreign key check.
func newAccountFixture(t *testing.T) (db *postgres.DB, householdID, membershipID string) {
	t.Helper()
	db = openTestDB(t)
	ctx := context.Background()

	households := postgres.NewHouseholdRepo(db)
	users := postgres.NewUserRepo(db)
	members := postgres.NewMembershipRepo(db)

	h, err := households.Create(ctx, domain.Household{
		Name: "Test", FamilyName: "Household",
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
// for the tests that prove a household cannot reach another's accounts or
// memberships. insertTestHousehold (schema_test.go) already inserts the
// minimum valid household row; this just names that call's intent here
// rather than writing a third household-inserting helper.
func insertSecondHousehold(t *testing.T, db *postgres.DB) string {
	t.Helper()
	return insertTestHousehold(t, db)
}

// The doc comment on AccountRepository.List has promised this since Accounts
// shipped: Balance is the opening balance plus every transaction dated after
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
	// Dated ON the opening date: already reflected in the figure someone
	// asserted was true that day, so it must not be counted again.
	mustCreate(domain.TransactionExpense, 10, 7777, accountID, "")
	// Dated before it: same reasoning.
	mustCreate(domain.TransactionExpense, 3, 9999, accountID, "")

	views, err := accounts.List(ctx, householdID, false)
	if err != nil {
		t.Fatalf("list accounts: %v", err)
	}
	if len(views) != 1 {
		t.Fatalf("got %d accounts, want 1", len(views))
	}
	// 100000 - 5000 + 20000
	if got := views[0].Balance.Amount; got != 115000 {
		t.Fatalf("balance = %d, want 115000 (opening 100000, -5000, +20000, "+
			"and nothing from the two dated on or before the opening date)", got)
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
	// its own assertion rather than trusting that copy-pasting the SQL also
	// copy-pasted correctly.
	all, err := accounts.List(ctx, householdID, true)
	if err != nil {
		t.Fatalf("list accounts including archived: %v", err)
	}
	if len(all) != 1 || all[0].Balance.Amount != views[0].Balance.Amount {
		t.Fatalf("ListAccountsIncludingArchived balance = %+v, want %d to match List and Get",
			all, views[0].Balance.Amount)
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
