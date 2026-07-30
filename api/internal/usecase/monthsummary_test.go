package usecase_test

import (
	"context"
	"testing"
	"time"

	"github.com/andreasoentoro/hearth/api/internal/domain"
	"github.com/andreasoentoro/hearth/api/internal/usecase"
)

func TestSpentCountsExpensesOnly(t *testing.T) {
	svc, _ := transactionFixture(t)
	ctx := context.Background()
	july := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)

	mustCreate(t, svc, usecase.NewTransaction{
		HouseholdID: "house-1", Kind: "expense", OccurredOn: july.AddDate(0, 0, 17),
		Description: "Cold Storage", CategoryID: "cat-groceries",
		FromAccountID: "dbs", AmountMinor: 5230,
	})
	mustCreate(t, svc, usecase.NewTransaction{
		HouseholdID: "house-1", Kind: "income", OccurredOn: july.AddDate(0, 0, 15),
		Description: "Bonus", CategoryID: "cat-income",
		ToAccountID: "dbs", AmountMinor: 120000,
	})
	mustCreate(t, svc, usecase.NewTransaction{
		HouseholdID: "house-1", Kind: "transfer", OccurredOn: july.AddDate(0, 0, 10),
		Description: "To savings", FromAccountID: "dbs", ToAccountID: "ocbc",
		AmountMinor: 50000,
	})

	got, err := svc.MonthSummary(ctx, "house-1", july)
	if err != nil {
		t.Fatalf("month summary: %v", err)
	}
	// The count is what the ledger shows: all three kinds.
	if got.Count != 3 {
		t.Fatalf("count = %d, want 3 (every kind)", got.Count)
	}
	// Spent is expenses only. Income is not spending, and a transfer is the
	// same money arriving somewhere else -- counting either would tell a
	// household it spent money it still has. The transfer's amount (50000) and
	// the income's (120000) both differ from the expense's (5230), so this
	// assertion would catch either leaking into Spent.
	if got.Spent.Amount != 5230 {
		t.Fatalf("spent = %d, want 5230 (the expense alone)", got.Spent.Amount)
	}
}

// domain.Money.Add refuses to add two currencies, deliberately. Summing first
// and converting after fails on the second transaction of a mixed-currency
// household -- LEARNING.md pattern 12, the same order AccountService.Summary
// uses.
func TestSpentConvertsEachTransactionBeforeSumming(t *testing.T) {
	svc, _ := transactionFixture(t)
	ctx := context.Background()
	july := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)

	mustCreate(t, svc, usecase.NewTransaction{
		HouseholdID: "house-1", Kind: "expense", OccurredOn: july.AddDate(0, 0, 17),
		Description: "Cold Storage", CategoryID: "cat-groceries",
		FromAccountID: "dbs", AmountMinor: 5230,
	})
	// An IDR expense. staticTestFX knows SGD<->IDR, so this converts.
	mustCreate(t, svc, usecase.NewTransaction{
		HouseholdID: "house-1", Kind: "expense", OccurredOn: july.AddDate(0, 0, 12),
		Description: "Warung", CategoryID: "cat-groceries",
		FromAccountID: "bca", AmountMinor: 5000000,
	})

	got, err := svc.MonthSummary(ctx, "house-1", july)
	if err != nil {
		t.Fatalf("month summary: %v", err)
	}
	if got.Currency != "SGD" {
		t.Fatalf("currency = %q, want the household's SGD", got.Currency)
	}
	if got.Spent.Amount <= 5230 {
		t.Fatalf("spent = %d, want more than the SGD expense alone -- the IDR one did not convert",
			got.Spent.Amount)
	}
	if len(got.ExcludedNoRate) != 0 {
		t.Fatalf("excluded %d transactions that had a rate", len(got.ExcludedNoRate))
	}
}

// A quietly short total looks identical to a correct one. Net worth already
// follows this rule; the ledger follows the same one.
func TestATransactionWithNoRateIsExcludedAndNamed(t *testing.T) {
	svc, _ := transactionFixtureWithAccount(t, "usd-card", "USD")
	ctx := context.Background()
	july := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)

	created := mustCreate(t, svc, usecase.NewTransaction{
		HouseholdID: "house-1", Kind: "expense", OccurredOn: july.AddDate(0, 0, 8),
		Description: "Steam", CategoryID: "cat-groceries",
		FromAccountID: "usd-card", AmountMinor: 3999,
	})

	got, err := svc.MonthSummary(ctx, "house-1", july)
	if err != nil {
		t.Fatalf("month summary: %v", err)
	}
	// Count counts every transaction the ledger shows, convertible or not --
	// an implementation that only counted convertible rows would still read 0
	// here, indistinguishable from "nothing happened this month" without this
	// assertion.
	if got.Count != 1 {
		t.Fatalf("count = %d, want 1 -- the ledger shows this row even though spend excludes it", got.Count)
	}
	if got.Spent.Amount != 0 {
		t.Fatalf("spent = %d, want 0 -- nothing convertible was spent", got.Spent.Amount)
	}
	// "Excluded and named" means both: which transaction, and which currency
	// -- Currency alone doesn't say which of several USD rows this was, so
	// the screen needs TransactionID to point at the actual entry.
	if len(got.ExcludedNoRate) != 1 ||
		got.ExcludedNoRate[0].Currency != "USD" ||
		got.ExcludedNoRate[0].TransactionID != created.ID {
		t.Fatalf("excluded = %v, want one USD transaction named %q", got.ExcludedNoRate, created.ID)
	}
}

// Decision 6's split, asserted in one test because it is the thing that will
// get "simplified" later: the balance ignores a transaction dated before the
// account's opening date, and spend does not. The money was spent.
//
// The transaction's view is marked via repo.markBeforeFromAccountOpening --
// the same BeforeFromAccountOpening flag the real postgres repository
// computes from a join to the account's OpeningBalanceAsOf (see
// transaction_repo.go) -- so the assertion below fails the moment
// MonthSummary starts respecting that flag, rather than passing regardless of
// whether it does.
func TestSpendCountsATransactionDatedBeforeTheAccountsOpeningBalance(t *testing.T) {
	svc, repo := transactionFixture(t)
	ctx := context.Background()
	july := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)

	// "log yesterday's lunch on an account I added today": the account's
	// opening balance was asserted as of some later date, and this expense
	// predates it -- the ledger's BeforeFromAccountOpening flag would be true
	// for exactly this row.
	created := mustCreate(t, svc, usecase.NewTransaction{
		HouseholdID: "house-1", Kind: "expense", OccurredOn: july.AddDate(0, 0, 1),
		Description: "Kopitiam", CategoryID: "cat-groceries",
		FromAccountID: "dbs", AmountMinor: 840,
	})
	repo.markBeforeFromAccountOpening(created.ID)

	got, err := svc.MonthSummary(ctx, "house-1", july)
	if err != nil {
		t.Fatalf("month summary: %v", err)
	}
	if got.Spent.Amount != 840 {
		t.Fatalf("spent = %d, want 840 -- a transaction the balance ignores was still spent",
			got.Spent.Amount)
	}
}

func mustCreate(t *testing.T, svc *usecase.TransactionService, in usecase.NewTransaction) domain.Transaction {
	t.Helper()
	created, err := svc.Create(context.Background(), in)
	if err != nil {
		t.Fatalf("create %q: %v", in.Description, err)
	}
	return created
}
