package postgres_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/andreasoentoro/hearth/api/internal/adapter/postgres"
	"github.com/andreasoentoro/hearth/api/internal/domain"
)

func july(day int) time.Time {
	return time.Date(2026, 7, day, 0, 0, 0, 0, time.UTC)
}

// insertTestAccount gives the transaction tests something to attach to
// without going through AccountRepo -- a repository test that builds its
// fixtures through another repository fails for two reasons at once.
func insertTestAccount(t *testing.T, db *postgres.DB, householdID, nickname, currency string) string {
	t.Helper()
	var id string
	err := db.Pool().QueryRow(context.Background(),
		`INSERT INTO accounts (household_id, nickname, type, opening_balance_minor,
		                       opening_balance_currency, opening_balance_as_of)
		 VALUES ($1, $2, 'cash', 0, $3, DATE '2026-07-01') RETURNING id`,
		householdID, nickname, currency).Scan(&id)
	if err != nil {
		t.Fatalf("insert account %s: %v", nickname, err)
	}
	return id
}

// insertTestMembership gives the transaction tests a payer to attach without
// going through MembershipRepo, for the same reason insertTestAccount avoids
// AccountRepo.
func insertTestMembership(t *testing.T, db *postgres.DB, householdID, displayName string) string {
	t.Helper()
	ctx := context.Background()
	var userID string
	if err := db.Pool().QueryRow(ctx,
		`INSERT INTO users (display_name, avatar_initial) VALUES ($1, left($1, 1)) RETURNING id`,
		displayName).Scan(&userID); err != nil {
		t.Fatalf("insert user: %v", err)
	}
	var membershipID string
	if err := db.Pool().QueryRow(ctx,
		`INSERT INTO memberships (household_id, user_id, role, capabilities)
		 VALUES ($1, $2, 'owner', ARRAY['calendar','chores','money','marriage']) RETURNING id`,
		householdID, userID).Scan(&membershipID); err != nil {
		t.Fatalf("insert membership: %v", err)
	}
	return membershipID
}

// insertTestCategory gives the Update test a category to attach and then
// clear, for the same reason insertTestAccount avoids AccountRepo.
func insertTestCategory(t *testing.T, db *postgres.DB, householdID, name string) string {
	t.Helper()
	var id string
	err := db.Pool().QueryRow(context.Background(),
		`INSERT INTO categories (household_id, name, kind, sort_order) VALUES ($1, $2, 'expense', 1) RETURNING id`,
		householdID, name).Scan(&id)
	if err != nil {
		t.Fatalf("insert category %s: %v", name, err)
	}
	return id
}

func TestTransactionRoundTrips(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	repo := postgres.NewTransactionRepo(db)
	householdID := insertTestHousehold(t, db)
	dbs := insertTestAccount(t, db, householdID, "DBS Everyday", "SGD")

	created, err := repo.Create(ctx, domain.Transaction{
		HouseholdID:   householdID,
		Kind:          domain.TransactionExpense,
		OccurredOn:    july(18),
		Description:   "Cold Storage",
		FromAccountID: dbs,
		Amount:        domain.Money{Amount: 5230, Currency: "SGD"},
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if created.ID == "" {
		t.Fatal("create returned no id")
	}

	view, err := repo.Get(ctx, householdID, created.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if view.Transaction.Description != "Cold Storage" {
		t.Fatalf("description = %q", view.Transaction.Description)
	}
	if view.FromAccountName != "DBS Everyday" {
		t.Fatalf("fromAccountName = %q, want the account's nickname", view.FromAccountName)
	}
	// An absent side is "" -- the "" <-> NULL convention -- never the zero uuid,
	// which would read as a real account that happens not to exist.
	if view.Transaction.ToAccountID != "" {
		t.Fatalf("toAccountId = %q, want \"\" for an expense", view.Transaction.ToAccountID)
	}
	// A date, compared as a date. Storing an instant would make this assertion
	// depend on the server's zone.
	if !view.Transaction.OccurredOn.Equal(july(18)) {
		t.Fatalf("occurredOn = %v, want %v", view.Transaction.OccurredOn, july(18))
	}
	// An expense has no destination account, so "does this predate the
	// destination's opening date" has no answer at all -- nil, not a false
	// that would misreport an account this transaction never touches.
	if view.BeforeToAccountOpening != nil {
		t.Fatalf("beforeToAccountOpening = %v, want nil for an expense with no destination",
			*view.BeforeToAccountOpening)
	}
	// DBS opened 1 July; this transaction is dated 18 July, after the opening,
	// so it does move the balance forward from the opening figure.
	if view.BeforeFromAccountOpening == nil || *view.BeforeFromAccountOpening {
		t.Fatalf("beforeFromAccountOpening = %v, want non-nil false for 18 July against a 1 July opening",
			view.BeforeFromAccountOpening)
	}

	// A transaction dated exactly on the opening date is already reflected in
	// the balance someone asserted was true that day -- the <= boundary, not
	// a strict <, which is why before is true here and not on the 18th above.
	onOpening, err := repo.Create(ctx, domain.Transaction{
		HouseholdID:   householdID,
		Kind:          domain.TransactionExpense,
		OccurredOn:    july(1),
		Description:   "Opening day spend",
		FromAccountID: dbs,
		Amount:        domain.Money{Amount: 100, Currency: "SGD"},
	})
	if err != nil {
		t.Fatalf("create on opening date: %v", err)
	}
	onOpeningView, err := repo.Get(ctx, householdID, onOpening.ID)
	if err != nil {
		t.Fatalf("get on opening date: %v", err)
	}
	if onOpeningView.BeforeFromAccountOpening == nil || !*onOpeningView.BeforeFromAccountOpening {
		t.Fatalf("beforeFromAccountOpening = %v, want non-nil true for a transaction on the opening date itself",
			onOpeningView.BeforeFromAccountOpening)
	}
}

// An id from another household must be indistinguishable from one that does
// not exist. A 404 that differs from a 403 tells a caller which ids are real.
func TestGetRefusesATransactionInAnotherHousehold(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	repo := postgres.NewTransactionRepo(db)

	mine, theirs := insertTestHousehold(t, db), insertTestHousehold(t, db)
	theirAccount := insertTestAccount(t, db, theirs, "Their DBS", "SGD")
	created, err := repo.Create(ctx, domain.Transaction{
		HouseholdID: theirs, Kind: domain.TransactionExpense, OccurredOn: july(18),
		Description: "Theirs", FromAccountID: theirAccount,
		Amount: domain.Money{Amount: 100, Currency: "SGD"},
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	if _, err := repo.Get(ctx, mine, created.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("get across households = %v, want ErrNotFound", err)
	}
	if err := repo.Delete(ctx, mine, created.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("delete across households = %v, want ErrNotFound", err)
	}
}

func TestDeleteRemovesTheRow(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	repo := postgres.NewTransactionRepo(db)
	householdID := insertTestHousehold(t, db)
	dbs := insertTestAccount(t, db, householdID, "DBS", "SGD")

	created, err := repo.Create(ctx, domain.Transaction{
		HouseholdID: householdID, Kind: domain.TransactionExpense, OccurredOn: july(18),
		Description: "Typo", FromAccountID: dbs,
		Amount: domain.Money{Amount: 100, Currency: "SGD"},
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := repo.Delete(ctx, householdID, created.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := repo.Get(ctx, householdID, created.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("get after delete = %v, want ErrNotFound", err)
	}
	if err := repo.Delete(ctx, householdID, created.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("second delete = %v, want ErrNotFound", err)
	}
}

// Both are database behaviour, so only a database can prove them. The
// membership case is why the column is ON DELETE SET NULL. The household
// delete at the end proves households.id cascades away its transactions
// end-to-end; it does not by itself prove the account columns are CASCADE
// and not RESTRICT -- transactions.household_id's own cascade removes this
// row before the account's cascade ever runs, so a RESTRICT on
// from_account_id/to_account_id would never be reached from this path.
// TestDeletingAnAccountTakesItsTransactionsWithIt is the test for that
// property, isolated from this race.
func TestDeletingAMemberKeepsTheirTransactionsAndDeletingAHouseholdTakesThemAway(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	repo := postgres.NewTransactionRepo(db)
	householdID := insertTestHousehold(t, db)
	dbs := insertTestAccount(t, db, householdID, "DBS", "SGD")
	membershipID := insertTestMembership(t, db, householdID, "Christine")

	created, err := repo.Create(ctx, domain.Transaction{
		HouseholdID: householdID, Kind: domain.TransactionExpense, OccurredOn: july(18),
		Description: "Cold Storage", FromAccountID: dbs, PaidByMembershipID: membershipID,
		Amount: domain.Money{Amount: 5230, Currency: "SGD"},
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	if _, err := db.Pool().Exec(ctx, `DELETE FROM memberships WHERE id = $1`, membershipID); err != nil {
		t.Fatalf("delete membership: %v", err)
	}
	view, err := repo.Get(ctx, householdID, created.ID)
	if err != nil {
		t.Fatalf("get after member removal: %v", err)
	}
	if view.Transaction.PaidByMembershipID != "" {
		t.Fatalf("paidBy = %q after the member was removed, want \"\"",
			view.Transaction.PaidByMembershipID)
	}

	// A RESTRICT anywhere in the household's cascade would surface here as
	// a foreign key violation.
	if _, err := db.Pool().Exec(ctx, `DELETE FROM households WHERE id = $1`, householdID); err != nil {
		t.Fatalf("delete household: %v", err)
	}
}

// The account columns are CASCADE and not RESTRICT so that deleting a
// household -- which cascades to its accounts -- never fails with a foreign
// key violation from a transaction still pointing at one. Deleting the
// account directly, rather than via a household delete, is what isolates
// this: a household delete also cascades the transaction away through its
// own household_id reference, which can remove the row before the account's
// side is ever checked and would let a RESTRICT there pass unnoticed -- see
// TestDeletingAMemberKeepsTheirTransactionsAndDeletingAHouseholdTakesThemAway's
// own comment. Both from_account_id and to_account_id are covered since
// they are declared identically but nothing enforces that they stay that way.
func TestDeletingAnAccountTakesItsTransactionsWithIt(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	repo := postgres.NewTransactionRepo(db)
	householdID := insertTestHousehold(t, db)

	fromSide := insertTestAccount(t, db, householdID, "Spends from this", "SGD")
	expense, err := repo.Create(ctx, domain.Transaction{
		HouseholdID: householdID, Kind: domain.TransactionExpense, OccurredOn: july(18),
		Description: "Expense", FromAccountID: fromSide,
		Amount: domain.Money{Amount: 100, Currency: "SGD"},
	})
	if err != nil {
		t.Fatalf("create expense: %v", err)
	}
	if _, err := db.Pool().Exec(ctx, `DELETE FROM accounts WHERE id = $1`, fromSide); err != nil {
		t.Fatalf("delete from_account_id's account: %v", err)
	}
	if _, err := repo.Get(ctx, householdID, expense.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("get after from_account_id's account was deleted = %v, want ErrNotFound", err)
	}

	toSide := insertTestAccount(t, db, householdID, "Pays into this", "SGD")
	income, err := repo.Create(ctx, domain.Transaction{
		HouseholdID: householdID, Kind: domain.TransactionIncome, OccurredOn: july(18),
		Description: "Income", ToAccountID: toSide,
		Amount: domain.Money{Amount: 100, Currency: "SGD"},
	})
	if err != nil {
		t.Fatalf("create income: %v", err)
	}
	if _, err := db.Pool().Exec(ctx, `DELETE FROM accounts WHERE id = $1`, toSide); err != nil {
		t.Fatalf("delete to_account_id's account: %v", err)
	}
	if _, err := repo.Get(ctx, householdID, income.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("get after to_account_id's account was deleted = %v, want ErrNotFound", err)
	}
}

// Update's own param block has thirteen fields, hand-mapped from the domain
// type field by field -- exactly the shape a copy-paste slip (writing one
// field's source value into an adjacent field's slot) survives compilation
// and every other test in this file. This changes more than one field of
// different kinds in the same update, on purpose: a plain value (kind,
// description, amount), both directions of the "" <-> NULL convention on two
// different optional ids in the same call (to_account_id and
// paid_by_membership_id go from unset to set; category_id goes from set to
// unset), and the received-amount pair together (nil to a real pair) --
// received_amount_is_a_transfer_thing is why this update also turns the
// transaction into a transfer, the only kind that permits one.
func TestUpdateChangesEveryMutableFieldAtOnce(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	repo := postgres.NewTransactionRepo(db)
	householdID := insertTestHousehold(t, db)
	dbs := insertTestAccount(t, db, householdID, "DBS", "SGD")
	ocbc := insertTestAccount(t, db, householdID, "OCBC", "SGD")
	groceries := insertTestCategory(t, db, householdID, "Groceries")
	membershipID := insertTestMembership(t, db, householdID, "Christine")

	created, err := repo.Create(ctx, domain.Transaction{
		HouseholdID:   householdID,
		Kind:          domain.TransactionExpense,
		OccurredOn:    july(18),
		Description:   "Cold Storage",
		CategoryID:    groceries,
		FromAccountID: dbs,
		Amount:        domain.Money{Amount: 5230, Currency: "SGD"},
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	received := domain.Money{Amount: 620000000, Currency: "IDR"}
	updated, err := repo.Update(ctx, domain.Transaction{
		ID:                 created.ID,
		HouseholdID:        householdID,
		Kind:               domain.TransactionTransfer,
		OccurredOn:         july(19),
		Description:        "Moved to OCBC",
		CategoryID:         "",           // set -> unset: a transfer carries no category
		PaidByMembershipID: membershipID, // unset -> set
		FromAccountID:      dbs,          // unchanged
		ToAccountID:        ocbc,         // unset -> set
		Amount:             domain.Money{Amount: 50000, Currency: "SGD"},
		ReceivedAmount:     &received, // nil -> set
	})
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if updated.ID != created.ID {
		t.Fatalf("update changed the id: %q, want %q", updated.ID, created.ID)
	}

	view, err := repo.Get(ctx, householdID, created.ID)
	if err != nil {
		t.Fatalf("get after update: %v", err)
	}
	got := view.Transaction
	if got.Kind != domain.TransactionTransfer {
		t.Fatalf("kind = %q, want transfer", got.Kind)
	}
	if got.Description != "Moved to OCBC" {
		t.Fatalf("description = %q, want %q", got.Description, "Moved to OCBC")
	}
	if !got.OccurredOn.Equal(july(19)) {
		t.Fatalf("occurredOn = %v, want %v", got.OccurredOn, july(19))
	}
	if got.Amount != (domain.Money{Amount: 50000, Currency: "SGD"}) {
		t.Fatalf("amount = %+v, want 50000 SGD", got.Amount)
	}
	if got.CategoryID != "" {
		t.Fatalf("categoryId = %q, want \"\" -- a transfer carries no category", got.CategoryID)
	}
	if got.PaidByMembershipID != membershipID {
		t.Fatalf("paidByMembershipId = %q, want %q", got.PaidByMembershipID, membershipID)
	}
	if got.FromAccountID != dbs {
		t.Fatalf("fromAccountId = %q, want %q", got.FromAccountID, dbs)
	}
	if got.ToAccountID != ocbc {
		t.Fatalf("toAccountId = %q, want %q", got.ToAccountID, ocbc)
	}
	if got.ReceivedAmount == nil || *got.ReceivedAmount != received {
		t.Fatalf("receivedAmount = %+v, want %+v", got.ReceivedAmount, received)
	}
}
