package usecase_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/andreasoentoro/hearth/api/internal/domain"
	"github.com/andreasoentoro/hearth/api/internal/usecase"
)

// telegramCommandFixture wires a TelegramCommandService over the ledger
// tests' transaction fixture, with matching account/category ids, so a
// chat command resolves names to the ids TransactionService then validates.
func telegramCommandFixture(t *testing.T, accounts ...domain.Account) (*usecase.TelegramCommandService, *fakeTransactionRepo) {
	t.Helper()
	return telegramCommandFixtureAt(t, time.Date(2026, 9, 8, 9, 30, 0, 0, time.UTC), "Asia/Singapore", accounts...)
}

// telegramCommandFixtureAt is telegramCommandFixture with the two things a
// chat command's date depends on made explicit: the instant the message
// arrives, and the zone the household keeps its calendar in.
func telegramCommandFixtureAt(t *testing.T, now time.Time, timezone string, accounts ...domain.Account) (*usecase.TelegramCommandService, *fakeTransactionRepo) {
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
	households.put(domain.Household{ID: "house-1", PrimaryCurrency: "SGD", Timezone: timezone})
	accountSvc := usecase.NewAccountService(usecase.AccountDeps{
		Accounts: accountRepo, Households: households, FX: newFXDouble(), Clock: &fixedClock{now: now},
		Holdings: holdingCounterDouble{},
	})
	categorySvc := usecase.NewCategoryService(&fakeCategoryRepo{categories: []domain.Category{
		{ID: "cat-groceries", HouseholdID: "house-1", Name: "Groceries", Kind: domain.CategoryExpense},
		{ID: "cat-income", HouseholdID: "house-1", Name: "Salary", Kind: domain.CategoryIncome},
	}})
	svc := usecase.NewTelegramCommandService(usecase.TelegramCommandDeps{
		Accounts: accountSvc, Categories: categorySvc, Transactions: txnSvc,
		Households: households, Clock: &fixedClock{now: now},
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
	if _, err := svc.LogSpend(context.Background(), in); !errors.Is(err, domain.ErrInvalidAmount) {
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

// A chat has no browser to say what day it is. /spend is dated the
// household's today, worked out from its stored zone, exactly as a web
// request's is. At 23:00 UTC on 30 September that is already 1 October in
// Singapore, and the row has to land in October's ledger and budget.
func TestLogSpendIsDatedTheHouseholdsTodayNotTheServers(t *testing.T) {
	arrives := time.Date(2026, 9, 30, 23, 0, 0, 0, time.UTC)
	cases := []struct {
		timezone string
		want     time.Time
	}{
		{"Asia/Singapore", time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)},
		// The control: the same message for a household on UTC is still
		// September's.
		{"UTC", time.Date(2026, 9, 30, 0, 0, 0, 0, time.UTC)},
		{"America/Los_Angeles", time.Date(2026, 9, 30, 0, 0, 0, 0, time.UTC)},
	}
	for _, tc := range cases {
		t.Run(tc.timezone, func(t *testing.T) {
			svc, repo := telegramCommandFixtureAt(t, arrives, tc.timezone)
			if _, err := svc.LogSpend(context.Background(), spend(77)); err != nil {
				t.Fatalf("LogSpend: %v", err)
			}
			if got := repo.transactions[0].OccurredOn; !got.Equal(tc.want) {
				t.Errorf("occurredOn = %s, want %s", got.Format(time.DateOnly), tc.want.Format(time.DateOnly))
			}
		})
	}
}

// A household whose zone cannot be loaded has no "today". The spend is
// refused and nothing is written, rather than dated by the server's clock.
func TestLogSpendWritesNothingWhenTheHouseholdsZoneCannotBeLoaded(t *testing.T) {
	svc, repo := telegramCommandFixtureAt(t, time.Date(2026, 9, 30, 23, 0, 0, 0, time.UTC), "Mars/Olympus_Mons")

	if _, err := svc.LogSpend(context.Background(), spend(77)); !errors.Is(err, domain.ErrInvalidTimezone) {
		t.Fatalf("err = %v, want domain.ErrInvalidTimezone", err)
	}
	if len(repo.transactions) != 0 {
		t.Fatalf("%d rows were stored, want none", len(repo.transactions))
	}
}
