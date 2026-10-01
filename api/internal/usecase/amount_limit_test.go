package usecase_test

import (
	"context"
	"errors"
	"math"
	"testing"
	"time"

	"github.com/andreasoentoro/hearth/api/internal/domain"
	"github.com/andreasoentoro/hearth/api/internal/usecase"
)

// Every write path that takes an amount from a person refuses one past
// domain.MaxAmountMinor. They are gathered in this one file, rather than
// spread one per service, so that the list of paths can be read in one place:
// a new path that takes an amount gets a test added here.
//
// The rule exists because stored amounts are summed on every read. One
// expense of int64 max was accepted, and every money page of that household
// then failed.

const onePastTheCeiling = domain.MaxAmountMinor + 1

func TestTransactionCreateRefusesAnAmountPastTheCeiling(t *testing.T) {
	svc, repo := transactionFixture(t)
	ctx := context.Background()

	// The two figures the defect was found with, and the first one refused.
	for _, amount := range []int64{onePastTheCeiling, 5_000_000_000_000_000_000, math.MaxInt64} {
		tooLarge := expenseInput()
		tooLarge.AmountMinor = amount
		if _, err := svc.Create(ctx, tooLarge, transactionToday); !errors.Is(err, domain.ErrAmountTooLarge) {
			t.Fatalf("amount %d = %v, want ErrAmountTooLarge", amount, err)
		}
	}
	if len(repo.transactions) != 0 {
		t.Fatalf("%d transactions were stored, want none", len(repo.transactions))
	}

	// The ceiling itself is allowed: the refusal starts one minor unit later.
	atCeiling := expenseInput()
	atCeiling.AmountMinor = domain.MaxAmountMinor
	if _, err := svc.Create(ctx, atCeiling, transactionToday); err != nil {
		t.Fatalf("amount at the ceiling: %v", err)
	}
}

func TestTransactionCreateRefusesAReceivedAmountPastTheCeiling(t *testing.T) {
	svc, repo := transactionFixture(t)

	received := onePastTheCeiling
	transfer := usecase.NewTransaction{
		HouseholdID: "house-1", Kind: "transfer",
		OccurredOn:  time.Date(2026, 7, 16, 0, 0, 0, 0, time.UTC),
		Description: "To BCA", FromAccountID: "dbs", ToAccountID: "bca",
		AmountMinor: 50000, ReceivedAmountMinor: &received,
	}
	if _, err := svc.Create(context.Background(), transfer, transactionToday); !errors.Is(err, domain.ErrAmountTooLarge) {
		t.Fatalf("received amount past the ceiling = %v, want ErrAmountTooLarge", err)
	}
	if len(repo.transactions) != 0 {
		t.Fatalf("%d transactions were stored, want none", len(repo.transactions))
	}
}

// Update validates the merged row through the same function Create does, so
// an edit cannot raise a stored amount past what Create would have refused.
func TestTransactionUpdateRefusesAnAmountPastTheCeiling(t *testing.T) {
	svc, repo := transactionFixture(t)
	ctx := context.Background()
	created, err := svc.Create(ctx, expenseInput(), transactionToday)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	tooLarge := onePastTheCeiling
	_, err = svc.Update(ctx, "house-1", created.ID, usecase.TransactionUpdate{AmountMinor: &tooLarge}, transactionToday)
	if !errors.Is(err, domain.ErrAmountTooLarge) {
		t.Fatalf("Update = %v, want ErrAmountTooLarge", err)
	}
	if got := repo.transactions[0].Amount.Amount; got != expenseInput().AmountMinor {
		t.Fatalf("stored amount = %d, want it left at %d", got, expenseInput().AmountMinor)
	}
}

// An opening balance may be negative (an overdrawn cash account), so the
// ceiling applies on both sides of zero.
func TestAccountCreateRefusesAnOpeningBalancePastTheCeilingOnEitherSide(t *testing.T) {
	svc, repo := newAccountService(t)

	for _, balance := range []int64{onePastTheCeiling, -onePastTheCeiling, math.MinInt64} {
		in := validNewAccount()
		in.OpeningBalanceMinor = balance
		if _, err := svc.Create(context.Background(), in, accountToday); !errors.Is(err, domain.ErrAmountTooLarge) {
			t.Fatalf("opening balance %d = %v, want ErrAmountTooLarge", balance, err)
		}
	}
	if len(repo.accounts) != 0 {
		t.Fatalf("%d accounts were stored, want none", len(repo.accounts))
	}
}

func TestAccountUpdateRefusesAnOpeningBalancePastTheCeiling(t *testing.T) {
	svc, _ := newAccountService(t)
	created, err := svc.Create(context.Background(), validNewAccount(), accountToday)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	tooLarge := onePastTheCeiling
	_, err = svc.Update(context.Background(), "h-1", created.ID, usecase.AccountUpdate{OpeningBalanceMinor: &tooLarge}, accountToday)
	if !errors.Is(err, domain.ErrAmountTooLarge) {
		t.Fatalf("Update = %v, want ErrAmountTooLarge", err)
	}
}

func TestBillCreateRefusesAnAmountPastTheCeiling(t *testing.T) {
	repo := &fakeBillRepo{}
	svc := newBillService(t, repo)

	_, err := svc.Create(context.Background(), usecase.NewBill{
		HouseholdID: "h1", Name: "Rent", AmountMinor: onePastTheCeiling,
		Cadence: domain.CadenceMonthly, NextDue: day("2026-08-20"),
		PayFromAccountID: "acct-1",
	}, day("2026-08-09"))
	if !errors.Is(err, domain.ErrAmountTooLarge) {
		t.Fatalf("Create = %v, want ErrAmountTooLarge", err)
	}
	if len(repo.records) != 0 {
		t.Fatalf("%d bills were stored, want none", len(repo.records))
	}
}

func TestBillUpdateRefusesAnAmountPastTheCeiling(t *testing.T) {
	repo := &fakeBillRepo{}
	repo.add(bill("SP utilities", "2026-08-08", 14230))
	svc := newBillService(t, repo)

	tooLarge := onePastTheCeiling
	_, err := svc.Update(context.Background(), "h1", "bill-1", usecase.BillPatch{AmountMinor: &tooLarge}, day("2026-08-09"))
	if !errors.Is(err, domain.ErrAmountTooLarge) {
		t.Fatalf("Update = %v, want ErrAmountTooLarge", err)
	}
}

// Marking a bill paid may name what was actually paid, and that figure
// becomes a stored payment, so it gets the ceiling too.
func TestBillMarkPaidRefusesAnAmountPastTheCeiling(t *testing.T) {
	repo := &fakeBillRepo{}
	repo.add(bill("SP utilities", "2026-08-08", 14230))
	svc := newBillService(t, repo)

	_, err := svc.MarkPaid(context.Background(), usecase.MarkPayment{
		HouseholdID: "h1", BillID: "bill-1", AmountMinor: int64Ptr(onePastTheCeiling), PaidOn: day("2026-08-08"),
	}, afterEveryPayment)
	if !errors.Is(err, domain.ErrAmountTooLarge) {
		t.Fatalf("MarkPaid = %v, want ErrAmountTooLarge", err)
	}
}

func TestBudgetSaveRefusesACapOrAnExpectedIncomePastTheCeiling(t *testing.T) {
	f := newBudgetFixture(t)
	ctx := context.Background()

	_, err := f.svc.Save(ctx, "house-1", julyMonth(), nil, []usecase.BudgetLineInput{
		{CategoryID: "cat-groceries", CapMinor: onePastTheCeiling},
	})
	if !errors.Is(err, domain.ErrAmountTooLarge) {
		t.Fatalf("cap past the ceiling = %v, want ErrAmountTooLarge", err)
	}

	tooLarge := onePastTheCeiling
	_, err = f.svc.Save(ctx, "house-1", julyMonth(), &tooLarge, []usecase.BudgetLineInput{
		{CategoryID: "cat-groceries", CapMinor: 1000},
	})
	if !errors.Is(err, domain.ErrAmountTooLarge) {
		t.Fatalf("expected income past the ceiling = %v, want ErrAmountTooLarge", err)
	}
}

func TestGoalCreateRefusesEachFigurePastTheCeiling(t *testing.T) {
	f := newGoalFixture(t)
	ctx := context.Background()
	createdOn := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	base := func(name string) usecase.NewGoal {
		return usecase.NewGoal{
			HouseholdID: "house-1", Name: name, TargetMinor: 100000,
			Currency: "SGD", PlannedMonthlyMinor: 10000,
		}
	}

	target := base("Target too large")
	target.TargetMinor = onePastTheCeiling
	if _, err := f.svc.Create(ctx, target, createdOn); !errors.Is(err, domain.ErrAmountTooLarge) {
		t.Fatalf("target past the ceiling = %v, want ErrAmountTooLarge", err)
	}

	planned := base("Planned too large")
	planned.PlannedMonthlyMinor = onePastTheCeiling
	if _, err := f.svc.Create(ctx, planned, createdOn); !errors.Is(err, domain.ErrAmountTooLarge) {
		t.Fatalf("planned monthly past the ceiling = %v, want ErrAmountTooLarge", err)
	}

	// A starting balance may be negative (a goal can start in deficit), so
	// the ceiling applies on both sides of zero.
	for _, starting := range []int64{onePastTheCeiling, -onePastTheCeiling} {
		in := base("Starting balance too large")
		in.StartingBalanceMinor = starting
		if _, err := f.svc.Create(ctx, in, createdOn); !errors.Is(err, domain.ErrAmountTooLarge) {
			t.Fatalf("starting balance %d = %v, want ErrAmountTooLarge", starting, err)
		}
	}
}

func TestGoalUpdateRefusesATargetOrPlannedMonthlyPastTheCeiling(t *testing.T) {
	f := newGoalFixture(t)
	ctx := context.Background()
	goal, err := f.svc.Create(ctx, usecase.NewGoal{
		HouseholdID: "house-1", Name: "Holiday", TargetMinor: 100000,
		Currency: "SGD", PlannedMonthlyMinor: 10000,
	}, time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	tooLarge := onePastTheCeiling
	if _, err := f.svc.Update(ctx, "house-1", goal.ID, usecase.GoalUpdate{TargetMinor: &tooLarge}); !errors.Is(err, domain.ErrAmountTooLarge) {
		t.Fatalf("target past the ceiling = %v, want ErrAmountTooLarge", err)
	}
	if _, err := f.svc.Update(ctx, "house-1", goal.ID, usecase.GoalUpdate{PlannedMonthlyMinor: &tooLarge}); !errors.Is(err, domain.ErrAmountTooLarge) {
		t.Fatalf("planned monthly past the ceiling = %v, want ErrAmountTooLarge", err)
	}
}

// A contribution may be negative (money taken back out of a goal), so the
// ceiling applies on both sides of zero.
func TestGoalAddContributionRefusesAnAmountPastTheCeilingOnEitherSide(t *testing.T) {
	f := newGoalFixture(t)
	ctx := context.Background()
	createdOn := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	goal, err := f.svc.Create(ctx, usecase.NewGoal{
		HouseholdID: "house-1", Name: "Holiday", TargetMinor: 100000,
		Currency: "SGD", PlannedMonthlyMinor: 10000,
	}, createdOn)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	for _, amount := range []int64{onePastTheCeiling, -onePastTheCeiling} {
		_, err := f.svc.AddContribution(ctx, usecase.NewContribution{
			HouseholdID: "house-1", GoalID: goal.ID, AmountMinor: amount, OccurredOn: createdOn,
		})
		if !errors.Is(err, domain.ErrAmountTooLarge) {
			t.Fatalf("contribution %d = %v, want ErrAmountTooLarge", amount, err)
		}
	}
}

// Telegram's /spend and free text reach the ledger through the same
// TransactionService the web form does, so the chat cannot be a way around
// the ceiling. The amount here is one ParseAmount can read (it refuses only
// past 18 digits), so the refusal has to come from the ceiling, and with its
// own error: the bot answers ErrInvalidAmount with "that amount could not be
// read", which would be the wrong thing to say about a readable number.
func TestTelegramSpendRefusesAnAmountPastTheCeiling(t *testing.T) {
	svc, repo := telegramCommandFixture(t)

	tooLarge := spend(78)
	tooLarge.AmountText = "1000000000000.01" // one cent past S$1 trillion
	_, err := svc.LogSpend(context.Background(), tooLarge)
	if !errors.Is(err, domain.ErrAmountTooLarge) {
		t.Fatalf("LogSpend = %v, want ErrAmountTooLarge", err)
	}
	if errors.Is(err, domain.ErrInvalidAmount) {
		t.Fatalf("LogSpend = %v, must not also read as an unreadable amount", err)
	}
	if len(repo.transactions) != 0 {
		t.Fatalf("%d transactions were stored, want none", len(repo.transactions))
	}
}
