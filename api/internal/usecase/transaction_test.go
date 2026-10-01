package usecase_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/andreasoentoro/hearth/api/internal/domain"
	"github.com/andreasoentoro/hearth/api/internal/usecase"
)

// transactionFixture wires a service whose accounts span two households --
// "house-1", the one every test acts as, and "other-house", which lets a
// test plant an account that is real, just not the caller's, rather than
// leaning on an id nobody registered at all.
func transactionFixture(t *testing.T) (*usecase.TransactionService, *fakeTransactionRepo) {
	t.Helper()
	return newTransactionFixture(t, nil)
}

// transactionFixtureWithAccount is transactionFixture plus one extra account
// under "house-1", for a test that needs a currency the FX double cannot
// convert (e.g. USD) without inventing a second, differently-wired fixture.
func transactionFixtureWithAccount(t *testing.T, accountID, currency string) (*usecase.TransactionService, *fakeTransactionRepo) {
	t.Helper()
	return newTransactionFixture(t, map[string]fakeAccountRecord{
		accountID: {householdID: "house-1", currency: currency},
	})
}

func newTransactionFixture(t *testing.T, extraAccounts map[string]fakeAccountRecord) (*usecase.TransactionService, *fakeTransactionRepo) {
	t.Helper()
	return newTransactionFixtureWithFX(t, extraAccounts, newFXDouble())
}

// newTransactionFixtureWithFX is newTransactionFixture with the FX double
// chosen by the test, the same shape as bill_test.go's newBillServiceWithFX.
func newTransactionFixtureWithFX(t *testing.T, extraAccounts map[string]fakeAccountRecord, fx usecase.FXRateProvider) (*usecase.TransactionService, *fakeTransactionRepo) {
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
		FX:         fx,
	})
	return svc, repo
}

// transactionToday is the household's calendar day every test here writes
// on, unless it says otherwise. The fixtures are dated two days before it.
var transactionToday = time.Date(2026, 7, 20, 0, 0, 0, 0, time.UTC)

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
// one -- NewTransaction has no currency field at all, which stops a handler
// accepting a value it never persists. The income case here is the one that
// actually proves it: it has no FromAccountID, so it is the only shape
// exercising the toCurrency branch. Drop that branch and every expense test
// here would still pass while income silently recorded an empty currency.
func TestCreateTakesTheAccountsCurrency(t *testing.T) {
	svc, _ := transactionFixture(t)

	created, err := svc.Create(context.Background(), expenseInput(), transactionToday)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if created.Amount.Currency != "SGD" {
		t.Fatalf("currency = %q, want the account's SGD", created.Amount.Currency)
	}

	idrExpense := expenseInput()
	idrExpense.FromAccountID = "bca"
	created, err = svc.Create(context.Background(), idrExpense, transactionToday)
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
	created, err = svc.Create(context.Background(), income, transactionToday)
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
		// currency -- just under "other-house", not "house-1". If the lookup
		// ever stopped scoping by household, this is the case that would start
		// passing where it should not; an unregistered id could not tell the
		// difference.
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
			_, err := svc.Create(ctx, mutate(expenseInput()), transactionToday)
			if !errors.Is(err, domain.ErrTransactionAccountsInvalid) {
				t.Fatalf("create = %v, want ErrTransactionAccountsInvalid", err)
			}
		})
	}
}

// Required across currencies, so what arrived is recorded rather than
// guessed at a rate we don't have. Permitted within one currency, so a
// transfer fee is recordable. Refused on anything that is not a transfer,
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
	if _, err := svc.Create(ctx, crossCurrency, transactionToday); !errors.Is(err, domain.ErrReceivedAmountRequired) {
		t.Fatalf("cross-currency transfer with no received amount = %v, want ErrReceivedAmountRequired", err)
	}

	received := int64(620000000)
	crossCurrency.ReceivedAmountMinor = &received
	created, err := svc.Create(ctx, crossCurrency, transactionToday)
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
	if _, err := svc.Create(ctx, sameCurrency, transactionToday); err != nil {
		t.Fatalf("same-currency transfer with a fee: %v", err)
	}

	// An expense cannot carry one.
	expense := expenseInput()
	expense.ReceivedAmountMinor = &fee
	if _, err := svc.Create(ctx, expense, transactionToday); !errors.Is(err, domain.ErrReceivedAmountNotAllowed) {
		t.Fatalf("expense with a received amount = %v, want ErrReceivedAmountNotAllowed", err)
	}
}

// ClearReceivedAmount is the only path that removes a received amount: a
// nil ReceivedAmountMinor alone leaves the figure untouched, since nil
// already means "leave this alone". No other test here reaches the
// clearing branch, so this is the one that would notice if it stopped.
func TestUpdateClearsTheReceivedAmount(t *testing.T) {
	svc, _ := transactionFixture(t)
	ctx := context.Background()

	received := int64(620000000)
	created, err := svc.Create(ctx, usecase.NewTransaction{
		HouseholdID: "house-1", Kind: "transfer",
		OccurredOn:  time.Date(2026, 6, 29, 0, 0, 0, 0, time.UTC),
		Description: "To BCA", FromAccountID: "dbs", ToAccountID: "bca",
		AmountMinor: 50000, ReceivedAmountMinor: &received,
	}, transactionToday)
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
	}, transactionToday)
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
	if _, err := svc.Create(context.Background(), income, transactionToday); !errors.Is(err, domain.ErrCategoryKindMismatch) {
		t.Fatalf("income categorised as Groceries = %v, want ErrCategoryKindMismatch", err)
	}

	transfer := usecase.NewTransaction{
		HouseholdID: "house-1", Kind: "transfer",
		OccurredOn:  time.Date(2026, 7, 16, 0, 0, 0, 0, time.UTC),
		Description: "To savings", FromAccountID: "dbs", ToAccountID: "ocbc",
		CategoryID: "cat-groceries", AmountMinor: 50000,
	}
	if _, err := svc.Create(context.Background(), transfer, transactionToday); !errors.Is(err, domain.ErrCategoryKindMismatch) {
		t.Fatalf("transfer with a category = %v, want ErrCategoryKindMismatch", err)
	}
}

func TestCreateRefusesAnEmptyDescriptionAndANonPositiveAmount(t *testing.T) {
	svc, _ := transactionFixture(t)
	ctx := context.Background()

	blank := expenseInput()
	blank.Description = "   "
	if _, err := svc.Create(ctx, blank, transactionToday); !errors.Is(err, domain.ErrTransactionDescriptionRequired) {
		t.Fatalf("blank description = %v, want ErrTransactionDescriptionRequired", err)
	}

	for _, amount := range []int64{0, -100} {
		bad := expenseInput()
		bad.AmountMinor = amount
		if _, err := svc.Create(ctx, bad, transactionToday); !errors.Is(err, domain.ErrTransactionAmountNotPositive) {
			t.Fatalf("amount %d = %v, want ErrTransactionAmountNotPositive", amount, err)
		}
	}
}

// Update validates the merged result, not the incoming fields alone --
// switching kind to transfer and leaving category alone are each legal
// alone, illegal together. The patch touches only Kind/ToAccountID;
// FromAccountID/CategoryID come from the stored expense unchanged.
// Validating the patch alone would fail for an unrelated reason (one leg,
// no category), so this pins the specific sentinel the merge must produce.
func TestUpdateValidatesTheMergedResult(t *testing.T) {
	svc, _ := transactionFixture(t)
	ctx := context.Background()

	created, err := svc.Create(ctx, expenseInput(), transactionToday)
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	kind := "transfer"
	toAccount := "ocbc"
	_, err = svc.Update(ctx, "house-1", created.ID, usecase.TransactionUpdate{
		Kind:        &kind,
		ToAccountID: &toAccount,
	}, transactionToday)
	if !errors.Is(err, domain.ErrCategoryKindMismatch) {
		t.Fatalf("switching an expense to a transfer kept its category = %v, want ErrCategoryKindMismatch", err)
	}
}

// Update merges a patch onto its own copy of the stored transaction and
// validates that copy -- a rejected patch must leave the stored row
// untouched. ReceivedAmount is a pointer, so a naive merge could still
// write through to the repository's value despite the failed validation.
// This patch fails for an unrelated reason (the category), but only after
// validateReceivedAmount re-stamps the currency for the new destination --
// exactly the write that must land on the copy, not on the row Get
// returned.
func TestARejectedUpdateDoesNotMutateTheStoredReceivedAmount(t *testing.T) {
	svc, _ := transactionFixture(t)
	ctx := context.Background()

	received := int64(620000000)
	created, err := svc.Create(ctx, usecase.NewTransaction{
		HouseholdID: "house-1", Kind: "transfer",
		OccurredOn:  time.Date(2026, 6, 29, 0, 0, 0, 0, time.UTC),
		Description: "To BCA", FromAccountID: "dbs", ToAccountID: "bca",
		AmountMinor: 50000, ReceivedAmountMinor: &received,
	}, transactionToday)
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
	}, transactionToday)
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

// Idempotency: the same key twice hands back the first row and writes
// nothing; the same key for a different transaction is refused; no key
// means two rows, exactly as before the key existed.

func TestCreateOrReplayReturnsTheStoredRowForARepeatedKey(t *testing.T) {
	svc, repo := transactionFixture(t)
	in := expenseInput()
	in.IdempotencyKey = "import-2026-09-08-row-7"

	first, replayed, err := svc.CreateOrReplay(context.Background(), in, transactionToday)
	if err != nil || replayed {
		t.Fatalf("first create: err=%v replayed=%v", err, replayed)
	}
	second, replayed, err := svc.CreateOrReplay(context.Background(), in, transactionToday)
	if err != nil {
		t.Fatalf("second create with the same key: %v", err)
	}
	if !replayed {
		t.Fatalf("the second call must report a replay")
	}
	if second.ID != first.ID {
		t.Fatalf("replay returned %q, want the stored row %q", second.ID, first.ID)
	}
	if len(repo.transactions) != 1 {
		t.Fatalf("stored %d rows, want exactly one", len(repo.transactions))
	}
}

func TestCreateOrReplayRefusesAKeyReusedForADifferentTransaction(t *testing.T) {
	svc, repo := transactionFixture(t)
	in := expenseInput()
	in.IdempotencyKey = "k1"
	if _, _, err := svc.CreateOrReplay(context.Background(), in, transactionToday); err != nil {
		t.Fatal(err)
	}

	changed := in
	changed.AmountMinor = in.AmountMinor + 1
	_, _, err := svc.CreateOrReplay(context.Background(), changed, transactionToday)
	if !errors.Is(err, domain.ErrIdempotencyKeyReused) {
		t.Fatalf("err = %v, want ErrIdempotencyKeyReused", err)
	}
	if len(repo.transactions) != 1 {
		t.Fatalf("a refused reuse must write nothing; stored %d", len(repo.transactions))
	}
}

func TestCreateOrReplayWithoutAKeyWritesEveryTime(t *testing.T) {
	svc, repo := transactionFixture(t)
	for i := 0; i < 2; i++ {
		if _, replayed, err := svc.CreateOrReplay(context.Background(), expenseInput(), transactionToday); err != nil || replayed {
			t.Fatalf("call %d: err=%v replayed=%v", i, err, replayed)
		}
	}
	if len(repo.transactions) != 2 {
		t.Fatalf("stored %d rows, want two: no key means no dedupe", len(repo.transactions))
	}
}

func TestCreateOrReplayRefusesAMalformedKey(t *testing.T) {
	svc, repo := transactionFixture(t)
	for _, key := range []string{"has space", "tab\tkey", strings.Repeat("x", 129), "ünïcode"} {
		in := expenseInput()
		in.IdempotencyKey = key
		if _, _, err := svc.CreateOrReplay(context.Background(), in, transactionToday); !errors.Is(err, domain.ErrIdempotencyKeyInvalid) {
			t.Errorf("key %q: err = %v, want ErrIdempotencyKeyInvalid", key, err)
		}
	}
	if len(repo.transactions) != 0 {
		t.Fatalf("a refused key must write nothing")
	}
}

func TestIdempotencyKeysAreScopedToTheHousehold(t *testing.T) {
	_, repo := transactionFixture(t)
	a := domain.Transaction{HouseholdID: "house-1", IdempotencyKey: "shared"}
	b := domain.Transaction{HouseholdID: "house-2", IdempotencyKey: "shared"}
	if _, err := repo.Create(context.Background(), a); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.Create(context.Background(), b); err != nil {
		t.Fatalf("another household's identical key must not collide: %v", err)
	}
}

// A transaction is a recorded fact, so it may not be dated after the
// household's today: a 2099 expense would lower today's balance, because a
// balance sums every row with no upper bound on the date. Today itself is
// allowed. Every kind is checked, and a refused create writes nothing.
func TestCreateRefusesATransactionDatedAfterTheHouseholdsToday(t *testing.T) {
	tomorrow := transactionToday.AddDate(0, 0, 1)

	inputs := map[string]usecase.NewTransaction{
		"an expense": expenseInput(),
		"an income": {
			HouseholdID: "house-1", Kind: "income", Description: "Salary",
			ToAccountID: "dbs", CategoryID: "cat-income", AmountMinor: 500000,
		},
		"a transfer": {
			HouseholdID: "house-1", Kind: "transfer", Description: "To savings",
			FromAccountID: "dbs", ToAccountID: "ocbc", AmountMinor: 10000,
		},
	}
	for name, in := range inputs {
		t.Run(name, func(t *testing.T) {
			svc, repo := transactionFixture(t)
			ctx := context.Background()

			in.OccurredOn = tomorrow
			if _, err := svc.Create(ctx, in, transactionToday); !errors.Is(err, domain.ErrDateInFuture) {
				t.Fatalf("dated tomorrow: err = %v, want ErrDateInFuture", err)
			}
			if len(repo.transactions) != 0 {
				t.Fatalf("%d row(s) written by a refused create", len(repo.transactions))
			}

			in.OccurredOn = transactionToday
			if _, err := svc.Create(ctx, in, transactionToday); err != nil {
				t.Fatalf("dated today: %v", err)
			}
		})
	}
}

// The keyed create is the path hearthctl's import and the Telegram bot use.
// It is the same rule: a key does not get a future date past it.
func TestCreateOrReplayRefusesAFutureDateBeforeAnythingIsWritten(t *testing.T) {
	svc, repo := transactionFixture(t)

	in := expenseInput()
	in.IdempotencyKey = "import-row-17"
	in.OccurredOn = transactionToday.AddDate(0, 0, 1)

	_, replayed, err := svc.CreateOrReplay(context.Background(), in, transactionToday)
	if !errors.Is(err, domain.ErrDateInFuture) {
		t.Fatalf("err = %v, want ErrDateInFuture", err)
	}
	if replayed {
		t.Fatal("a refused create was reported as a replay")
	}
	if len(repo.transactions) != 0 {
		t.Fatalf("%d row(s) written by a refused create", len(repo.transactions))
	}
}

// An edit that moves a transaction to a later day than today is refused, and
// the stored row keeps the date it had.
func TestUpdateRefusesMovingATransactionIntoTheFuture(t *testing.T) {
	svc, repo := transactionFixture(t)
	ctx := context.Background()

	created, err := svc.Create(ctx, expenseInput(), transactionToday)
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	tomorrow := transactionToday.AddDate(0, 0, 1)
	_, err = svc.Update(ctx, "house-1", created.ID, usecase.TransactionUpdate{OccurredOn: &tomorrow}, transactionToday)
	if !errors.Is(err, domain.ErrDateInFuture) {
		t.Fatalf("moved to tomorrow: err = %v, want ErrDateInFuture", err)
	}
	if got := repo.transactions[0].OccurredOn; !got.Equal(expenseInput().OccurredOn) {
		t.Fatalf("stored date = %v after a refused edit, want it unchanged", got)
	}

	today := transactionToday
	if _, err := svc.Update(ctx, "house-1", created.ID, usecase.TransactionUpdate{OccurredOn: &today}, transactionToday); err != nil {
		t.Fatalf("moved to today: %v", err)
	}
}

// A row already stored with a date after today stays as it is, and can still
// be edited: the date is checked only when the edit changes it. Such a row
// exists without anyone having typed a future date after this rule: it was
// saved before the rule, or the household's zone was moved west afterwards.
//
// The edit form sends every field back, the unchanged date included, so
// "the patch carries a date" must not be read as "the date changed".
func TestUpdateChecksTheDateOnlyWhenTheEditChangesIt(t *testing.T) {
	svc, repo := transactionFixture(t)
	ctx := context.Background()

	// Written on a day when this date was not in the future.
	stored := expenseInput()
	stored.OccurredOn = time.Date(2026, 8, 30, 0, 0, 0, 0, time.UTC)
	created, err := svc.Create(ctx, stored, stored.OccurredOn)
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	description := "Cold Storage, Great World"
	if _, err := svc.Update(ctx, "house-1", created.ID,
		usecase.TransactionUpdate{Description: &description}, transactionToday); err != nil {
		t.Fatalf("an edit that does not name the date: %v", err)
	}

	sameDate := stored.OccurredOn
	amount := int64(6100)
	if _, err := svc.Update(ctx, "house-1", created.ID,
		usecase.TransactionUpdate{OccurredOn: &sameDate, AmountMinor: &amount}, transactionToday); err != nil {
		t.Fatalf("an edit that sends the same date back: %v", err)
	}
	if got := repo.transactions[0]; got.Description != description || got.Amount.Amount != amount {
		t.Fatalf("stored = %q %d, want both edits saved", got.Description, got.Amount.Amount)
	}

	otherFutureDate := stored.OccurredOn.AddDate(0, 0, 1)
	_, err = svc.Update(ctx, "house-1", created.ID,
		usecase.TransactionUpdate{OccurredOn: &otherFutureDate}, transactionToday)
	if !errors.Is(err, domain.ErrDateInFuture) {
		t.Fatalf("an edit to a different future date: err = %v, want ErrDateInFuture", err)
	}
}
