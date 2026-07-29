package usecase_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/andreasoentoro/hearth/api/internal/domain"
	"github.com/andreasoentoro/hearth/api/internal/usecase"
)

// transactionFixture wires a service whose accounts span two households --
// "house-1", the one every test acts as, and "other-house", which exists
// solely so a test can plant an account that is real, just not the caller's,
// rather than leaning on an id nobody registered to mean the same thing.
func transactionFixture(t *testing.T) (*usecase.TransactionService, *fakeTransactionRepo) {
	t.Helper()
	return newTransactionFixture(t, nil)
}

// transactionFixtureWithAccount is transactionFixture plus one extra account
// under "house-1", for a test that needs a currency staticTestRates cannot
// convert (e.g. USD) without inventing a second, differently-wired fixture.
func transactionFixtureWithAccount(t *testing.T, accountID, currency string) (*usecase.TransactionService, *fakeTransactionRepo) {
	t.Helper()
	return newTransactionFixture(t, map[string]fakeAccountRecord{
		accountID: {householdID: "house-1", currency: currency},
	})
}

func newTransactionFixture(t *testing.T, extraAccounts map[string]fakeAccountRecord) (*usecase.TransactionService, *fakeTransactionRepo) {
	t.Helper()
	repo := &fakeTransactionRepo{}
	households := newHouseholdDouble()
	households.put(domain.Household{ID: "house-1", PrimaryCurrency: "SGD"})

	accounts := map[string]fakeAccountRecord{
		"dbs":           {householdID: "house-1", currency: "SGD"},
		"ocbc":          {householdID: "house-1", currency: "SGD"},
		"bca":           {householdID: "house-1", currency: "IDR"},
		"someone-elses": {householdID: "other-house", currency: "SGD"},
	}
	for id, record := range extraAccounts {
		accounts[id] = record
	}

	svc := usecase.NewTransactionService(usecase.TransactionDeps{
		Transactions: repo,
		Categories: &fakeCategoryLookup{kinds: map[string]domain.CategoryKind{
			"cat-groceries": domain.CategoryExpense,
			"cat-income":    domain.CategoryIncome,
		}},
		Accounts: &fakeAccountLookup{
			accounts: accounts,
			memberships: map[string]string{
				"m-1":             "house-1",
				"someone-elses-m": "other-house",
			},
		},
		Households: households,
		FX:         staticTestRates{},
		Clock:      &fixedClock{now: time.Date(2026, 7, 20, 12, 0, 0, 0, time.UTC)},
	})
	return svc, repo
}

func expenseInput() usecase.NewTransaction {
	return usecase.NewTransaction{
		HouseholdID:   "house-1",
		Kind:          "expense",
		OccurredOn:    time.Date(2026, 7, 18, 0, 0, 0, 0, time.UTC),
		Description:   "Cold Storage",
		CategoryID:    "cat-groceries",
		FromAccountID: "dbs",
		AmountMinor:   5230,
	}
}

// The service derives the currency from the account. A request cannot name
// one -- NewTransaction has no currency field at all, which is what stops a
// handler accepting a value it never persists.
//
// The third case is the one that actually distinguishes "derives it from the
// account" from "derives it from whichever account happens to be set as
// FromAccountID": an income has no FromAccountID at all, so it is the only
// shape that exercises the toCurrency branch of that derivation. Dropping
// that branch entirely would leave every expense test here green while every
// income silently recorded an empty currency.
func TestCreateTakesTheAccountsCurrency(t *testing.T) {
	svc, _ := transactionFixture(t)

	created, err := svc.Create(context.Background(), expenseInput())
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if created.Amount.Currency != "SGD" {
		t.Fatalf("currency = %q, want the account's SGD", created.Amount.Currency)
	}

	idrExpense := expenseInput()
	idrExpense.FromAccountID = "bca"
	created, err = svc.Create(context.Background(), idrExpense)
	if err != nil {
		t.Fatalf("create on an IDR account: %v", err)
	}
	if created.Amount.Currency != "IDR" {
		t.Fatalf("currency = %q, want the account's IDR", created.Amount.Currency)
	}

	income := usecase.NewTransaction{
		HouseholdID: "house-1", Kind: "income",
		OccurredOn:  time.Date(2026, 7, 18, 0, 0, 0, 0, time.UTC),
		Description: "Interest", ToAccountID: "bca",
		CategoryID: "cat-income", AmountMinor: 15000,
	}
	created, err = svc.Create(context.Background(), income)
	if err != nil {
		t.Fatalf("create income on an IDR account: %v", err)
	}
	if created.Amount.Currency != "IDR" {
		t.Fatalf("income currency = %q, want the destination account's IDR", created.Amount.Currency)
	}
}

func TestCreateRefusesTheWrongAccountsForItsKind(t *testing.T) {
	svc, _ := transactionFixture(t)
	ctx := context.Background()

	cases := map[string]func(usecase.NewTransaction) usecase.NewTransaction{
		"an expense with a destination": func(in usecase.NewTransaction) usecase.NewTransaction {
			in.ToAccountID = "ocbc"
			return in
		},
		"an expense with no source": func(in usecase.NewTransaction) usecase.NewTransaction {
			in.FromAccountID = ""
			return in
		},
		"a transfer with one leg": func(in usecase.NewTransaction) usecase.NewTransaction {
			in.Kind, in.CategoryID = "transfer", ""
			return in
		},
		"a transfer to and from the same account": func(in usecase.NewTransaction) usecase.NewTransaction {
			in.Kind, in.CategoryID = "transfer", ""
			in.ToAccountID = in.FromAccountID
			return in
		},
		// This account genuinely exists -- fakeAccountLookup knows its
		// currency -- just under "other-house", not "house-1". If the service
		// (or the port) ever stopped scoping the lookup by household, this is
		// the case that would start passing where it should not, while an
		// id nobody registered at all could not tell the difference.
		"an account in another household": func(in usecase.NewTransaction) usecase.NewTransaction {
			in.FromAccountID = "someone-elses"
			return in
		},
		"paid by a membership in another household": func(in usecase.NewTransaction) usecase.NewTransaction {
			in.PaidByMembershipID = "someone-elses-m"
			return in
		},
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := svc.Create(ctx, mutate(expenseInput()))
			if !errors.Is(err, domain.ErrTransactionAccountsInvalid) {
				t.Fatalf("create = %v, want ErrTransactionAccountsInvalid", err)
			}
		})
	}
}

// Decision 3: required across currencies so what arrived is recorded rather
// than guessed at a rate we do not have; permitted within one currency so a
// transfer fee is recordable; refused on anything that is not a transfer,
// where it would have nothing to mean.
func TestTheReceivedAmountFollowsTheCurrencies(t *testing.T) {
	svc, _ := transactionFixture(t)
	ctx := context.Background()

	crossCurrency := usecase.NewTransaction{
		HouseholdID: "house-1", Kind: "transfer",
		OccurredOn:  time.Date(2026, 6, 29, 0, 0, 0, 0, time.UTC),
		Description: "To BCA", FromAccountID: "dbs", ToAccountID: "bca",
		AmountMinor: 50000,
	}
	if _, err := svc.Create(ctx, crossCurrency); !errors.Is(err, domain.ErrReceivedAmountRequired) {
		t.Fatalf("cross-currency transfer with no received amount = %v, want ErrReceivedAmountRequired", err)
	}

	received := int64(620000000)
	crossCurrency.ReceivedAmountMinor = &received
	created, err := svc.Create(ctx, crossCurrency)
	if err != nil {
		t.Fatalf("cross-currency transfer: %v", err)
	}
	if created.ReceivedAmount == nil || created.ReceivedAmount.Currency != "IDR" {
		t.Fatalf("received amount = %v, want 620000000 IDR", created.ReceivedAmount)
	}

	// Same currency, with a fee. Accepted.
	fee := int64(49800)
	sameCurrency := crossCurrency
	sameCurrency.ToAccountID = "ocbc"
	sameCurrency.ReceivedAmountMinor = &fee
	if _, err := svc.Create(ctx, sameCurrency); err != nil {
		t.Fatalf("same-currency transfer with a fee: %v", err)
	}

	// An expense cannot carry one.
	expense := expenseInput()
	expense.ReceivedAmountMinor = &fee
	if _, err := svc.Create(ctx, expense); !errors.Is(err, domain.ErrReceivedAmountNotAllowed) {
		t.Fatalf("expense with a received amount = %v, want ErrReceivedAmountNotAllowed", err)
	}
}

// ClearReceivedAmount is the only path that removes a received amount -- a
// nil ReceivedAmountMinor on its own leaves the stored figure untouched,
// because nil already means "leave this alone". No other test in this file
// reaches the clearing branch, so this is the one that would notice if it
// silently stopped clearing.
func TestUpdateClearsTheReceivedAmount(t *testing.T) {
	svc, _ := transactionFixture(t)
	ctx := context.Background()

	received := int64(620000000)
	created, err := svc.Create(ctx, usecase.NewTransaction{
		HouseholdID: "house-1", Kind: "transfer",
		OccurredOn:  time.Date(2026, 6, 29, 0, 0, 0, 0, time.UTC),
		Description: "To BCA", FromAccountID: "dbs", ToAccountID: "bca",
		AmountMinor: 50000, ReceivedAmountMinor: &received,
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if created.ReceivedAmount == nil {
		t.Fatal("create did not record the received amount")
	}

	// The transfer no longer crosses currencies once it lands in ocbc, so
	// nothing requires a received amount any more -- clearing it is exactly
	// what a caller making this edit would want.
	toAccount := "ocbc"
	updated, err := svc.Update(ctx, "house-1", created.ID, usecase.TransactionUpdate{
		ToAccountID:         &toAccount,
		ClearReceivedAmount: true,
	})
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if updated.ReceivedAmount != nil {
		t.Fatalf("received amount = %v, want nil after clearing", updated.ReceivedAmount)
	}
}

func TestCreateRefusesACategoryOfTheWrongKind(t *testing.T) {
	svc, _ := transactionFixture(t)

	income := usecase.NewTransaction{
		HouseholdID: "house-1", Kind: "income",
		OccurredOn:  time.Date(2026, 7, 16, 0, 0, 0, 0, time.UTC),
		Description: "Bonus", ToAccountID: "dbs",
		CategoryID: "cat-groceries", AmountMinor: 120000,
	}
	if _, err := svc.Create(context.Background(), income); !errors.Is(err, domain.ErrCategoryKindMismatch) {
		t.Fatalf("income categorised as Groceries = %v, want ErrCategoryKindMismatch", err)
	}

	transfer := usecase.NewTransaction{
		HouseholdID: "house-1", Kind: "transfer",
		OccurredOn:  time.Date(2026, 7, 16, 0, 0, 0, 0, time.UTC),
		Description: "To savings", FromAccountID: "dbs", ToAccountID: "ocbc",
		CategoryID: "cat-groceries", AmountMinor: 50000,
	}
	if _, err := svc.Create(context.Background(), transfer); !errors.Is(err, domain.ErrCategoryKindMismatch) {
		t.Fatalf("transfer with a category = %v, want ErrCategoryKindMismatch", err)
	}
}

func TestCreateRefusesAnEmptyDescriptionAndANonPositiveAmount(t *testing.T) {
	svc, _ := transactionFixture(t)
	ctx := context.Background()

	blank := expenseInput()
	blank.Description = "   "
	if _, err := svc.Create(ctx, blank); !errors.Is(err, domain.ErrTransactionDescriptionRequired) {
		t.Fatalf("blank description = %v, want ErrTransactionDescriptionRequired", err)
	}

	for _, amount := range []int64{0, -100} {
		bad := expenseInput()
		bad.AmountMinor = amount
		if _, err := svc.Create(ctx, bad); !errors.Is(err, domain.ErrTransactionAmountNotPositive) {
			t.Fatalf("amount %d = %v, want ErrTransactionAmountNotPositive", amount, err)
		}
	}
}

// Update validates the merged result, never the incoming fields -- switching
// the kind to transfer and leaving a category alone are each legal on their
// own and illegal together. The patch below touches only Kind and
// ToAccountID: FromAccountID and CategoryID come from the stored expense
// (dbs, cat-groceries), unchanged. Validating the patch alone would see a
// transfer with only one leg named and no category at all, and fail for that
// unrelated reason -- so this asserts the specific sentinel the merge is
// supposed to produce, not merely that some error occurred.
func TestUpdateValidatesTheMergedResult(t *testing.T) {
	svc, _ := transactionFixture(t)
	ctx := context.Background()

	created, err := svc.Create(ctx, expenseInput())
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	kind := "transfer"
	toAccount := "ocbc"
	_, err = svc.Update(ctx, "house-1", created.ID, usecase.TransactionUpdate{
		Kind:        &kind,
		ToAccountID: &toAccount,
	})
	if !errors.Is(err, domain.ErrCategoryKindMismatch) {
		t.Fatalf("switching an expense to a transfer kept its category = %v, want ErrCategoryKindMismatch", err)
	}
}

// Update reads the stored transaction, merges a patch onto its own copy, and
// validates that copy -- a rejected patch must leave the stored row exactly
// as it was. ReceivedAmount is a pointer, so this is the one field a naive
// merge (copying the struct but not what it points to) could still write
// through to the repository's own value even when validation fails
// afterwards and nothing is meant to persist. This same-currency-to
// cross-currency patch is rejected for an unrelated reason (the category),
// but only after validateReceivedAmount has already re-stamped the received
// amount's currency for the *new* destination account -- exactly the write
// that must land on Update's copy, never on the row Get returned.
func TestARejectedUpdateDoesNotMutateTheStoredReceivedAmount(t *testing.T) {
	svc, _ := transactionFixture(t)
	ctx := context.Background()

	received := int64(620000000)
	created, err := svc.Create(ctx, usecase.NewTransaction{
		HouseholdID: "house-1", Kind: "transfer",
		OccurredOn:  time.Date(2026, 6, 29, 0, 0, 0, 0, time.UTC),
		Description: "To BCA", FromAccountID: "dbs", ToAccountID: "bca",
		AmountMinor: 50000, ReceivedAmountMinor: &received,
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if created.ReceivedAmount == nil || created.ReceivedAmount.Currency != "IDR" {
		t.Fatalf("received amount = %v, want 620000000 IDR", created.ReceivedAmount)
	}

	toAccount := "ocbc" // SGD, unlike bca's IDR -- gives validateReceivedAmount a currency to overwrite
	category := "cat-groceries"
	_, err = svc.Update(ctx, "house-1", created.ID, usecase.TransactionUpdate{
		ToAccountID: &toAccount,
		CategoryID:  &category,
	})
	if !errors.Is(err, domain.ErrCategoryKindMismatch) {
		t.Fatalf("update = %v, want ErrCategoryKindMismatch (so nothing should have been persisted)", err)
	}

	stored, err := svc.Get(ctx, "house-1", created.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if stored.Transaction.ReceivedAmount == nil || stored.Transaction.ReceivedAmount.Currency != "IDR" {
		t.Fatalf("stored received amount = %v, want untouched 620000000 IDR", stored.Transaction.ReceivedAmount)
	}
}
