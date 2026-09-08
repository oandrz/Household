package usecase_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/andreasoentoro/hearth/api/internal/domain"
	"github.com/andreasoentoro/hearth/api/internal/usecase"
)

// telegramCommandFixture wires a TelegramCommandService over the same
// transaction fixture the ledger tests use, plus an account list and a
// category list with matching ids, so a chat command resolves names to the
// very ids TransactionService then validates.
func telegramCommandFixture(t *testing.T, accounts ...domain.Account) (*usecase.TelegramCommandService, *fakeTransactionRepo) {
	t.Helper()
	txnSvc, txnRepo := transactionFixtureWithAccount(t, "jpy", "JPY")

	accountRepo := newFakeAccountRepo()
	if len(accounts) == 0 {
		accounts = []domain.Account{{ID: "dbs", HouseholdID: "house-1", Nickname: "DBS Savings", Type: domain.AccountCash, OpeningBalance: domain.Money{Currency: "SGD"}}}
	}
	for _, a := range accounts {
		accountRepo.accounts[a.ID] = a
	}
	households := newHouseholdDouble()
	households.put(domain.Household{ID: "house-1", PrimaryCurrency: "SGD"})
	accountSvc := usecase.NewAccountService(usecase.AccountDeps{
		Accounts: accountRepo, Households: households, FX: staticTestRates{}, Clock: &fixedClock{now: time.Date(2026, 9, 8, 9, 0, 0, 0, time.UTC)},
	})
	categorySvc := usecase.NewCategoryService(&fakeCategoryRepo{categories: []domain.Category{
		{ID: "cat-groceries", HouseholdID: "house-1", Name: "Groceries", Kind: domain.CategoryExpense},
		{ID: "cat-income", HouseholdID: "house-1", Name: "Salary", Kind: domain.CategoryIncome},
	}})
	svc := usecase.NewTelegramCommandService(usecase.TelegramCommandDeps{
		Accounts: accountSvc, Categories: categorySvc, Transactions: txnSvc,
		Clock: &fixedClock{now: time.Date(2026, 9, 8, 9, 30, 0, 0, time.UTC)},
	})
	return svc, txnRepo
}

func spend(update int64) usecase.TelegramSpend {
	return usecase.TelegramSpend{HouseholdID: "house-1", MembershipID: "m-1", UpdateID: update,
		Kind: domain.TransactionExpense, AmountText: "84.50", Description: "groceries", CategoryName: "groceries"}
}

func TestLogSpendUsesTheOnlyCashAccountAndTheUpdateIDAsTheKey(t *testing.T) {
	svc, repo := telegramCommandFixture(t)
	res, err := svc.LogSpend(context.Background(), spend(77))
	if err != nil {
		t.Fatal(err)
	}
	if len(repo.transactions) != 1 {
		t.Fatalf("stored %d rows", len(repo.transactions))
	}
	row := repo.transactions[0]
	if row.FromAccountID != "dbs" || row.Amount.Amount != 8450 || row.Amount.Currency != "SGD" || row.CategoryID != "cat-groceries" || row.PaidByMembershipID != "m-1" {
		t.Fatalf("row %+v", row)
	}
	if row.IdempotencyKey != "telegram-update-77" {
		t.Fatalf("key %q", row.IdempotencyKey)
	}
	if !row.OccurredOn.Equal(time.Date(2026, 9, 8, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("occurredOn %v, want today at midnight UTC", row.OccurredOn)
	}
	if res.AccountName != "DBS Savings" || res.MinorUnits != 2 || res.Replayed {
		t.Fatalf("result %+v", res)
	}
}

func TestARedeliveredUpdateIsAReplayNotASecondRow(t *testing.T) {
	svc, repo := telegramCommandFixture(t)
	if _, err := svc.LogSpend(context.Background(), spend(77)); err != nil {
		t.Fatal(err)
	}
	res, err := svc.LogSpend(context.Background(), spend(77))
	if err != nil || !res.Replayed {
		t.Fatalf("err=%v replayed=%v", err, res.Replayed)
	}
	if len(repo.transactions) != 1 {
		t.Fatalf("stored %d rows", len(repo.transactions))
	}
}

func TestLogSpendRefusesToGuessBetweenTwoCashAccounts(t *testing.T) {
	svc, repo := telegramCommandFixture(t,
		domain.Account{ID: "dbs", HouseholdID: "house-1", Nickname: "DBS Savings", Type: domain.AccountCash, OpeningBalance: domain.Money{Currency: "SGD"}},
		domain.Account{ID: "ocbc", HouseholdID: "house-1", Nickname: "OCBC", Type: domain.AccountCash, OpeningBalance: domain.Money{Currency: "SGD"}},
	)
	_, err := svc.LogSpend(context.Background(), spend(1))
	re, ok := usecase.IsResolutionError(err)
	if !ok || !re.Ambiguous || re.What != "account" || len(re.Candidates) != 2 {
		t.Fatalf("want an ambiguous account refusal, got %v", err)
	}
	if len(repo.transactions) != 0 {
		t.Fatal("a refusal must write nothing")
	}

	in := spend(2)
	in.AccountName = "ocbc" // case-insensitive
	if _, err := svc.LogSpend(context.Background(), in); err != nil {
		t.Fatalf("named account: %v", err)
	}
	if repo.transactions[0].FromAccountID != "ocbc" {
		t.Fatalf("resolved to %q", repo.transactions[0].FromAccountID)
	}
}

func TestLogSpendRefusesAnUnknownCategoryAndListsOnlyTheRightKind(t *testing.T) {
	svc, _ := telegramCommandFixture(t)
	in := spend(1)
	in.CategoryName = "Fun"
	_, err := svc.LogSpend(context.Background(), in)
	re, ok := usecase.IsResolutionError(err)
	if !ok || re.What != "category" || re.Typed != "Fun" {
		t.Fatalf("got %v", err)
	}
	if len(re.Candidates) != 1 || re.Candidates[0] != "Groceries" {
		t.Fatalf("an expense must only be offered expense categories, got %v", re.Candidates)
	}
}

func TestLogSpendParsesTheAmountInTheAccountsCurrency(t *testing.T) {
	svc, repo := telegramCommandFixture(t,
		domain.Account{ID: "jpy", HouseholdID: "house-1", Nickname: "Yen", Type: domain.AccountCash, OpeningBalance: domain.Money{Currency: "JPY"}},
	)
	in := spend(1)
	in.CategoryName = ""
	in.AmountText = "12.5"
	if _, err := svc.LogSpend(context.Background(), in); !errors.Is(err, domain.ErrInvalidMoney) {
		t.Fatalf("12.5 yen must be refused, got %v", err)
	}
	in.UpdateID, in.AmountText = 2, "1200"
	res, err := svc.LogSpend(context.Background(), in)
	if err != nil {
		t.Fatal(err)
	}
	if repo.transactions[0].Amount.Amount != 1200 || res.MinorUnits != 0 {
		t.Fatalf("yen stored as %d minor units with %d places", repo.transactions[0].Amount.Amount, res.MinorUnits)
	}
}

func TestIncomeGoesToTheAccountNotFromIt(t *testing.T) {
	svc, repo := telegramCommandFixture(t)
	in := spend(1)
	in.Kind, in.CategoryName, in.AmountText = domain.TransactionIncome, "salary", "6500"
	if _, err := svc.LogSpend(context.Background(), in); err != nil {
		t.Fatal(err)
	}
	row := repo.transactions[0]
	if row.ToAccountID != "dbs" || row.FromAccountID != "" || row.CategoryID != "cat-income" || row.Amount.Amount != 650000 {
		t.Fatalf("row %+v", row)
	}
}
