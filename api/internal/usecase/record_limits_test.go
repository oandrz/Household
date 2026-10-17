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

// recordCase is one kind of record: how to add one to a fresh service whose
// usage double carries the counts the test chose.
type recordCase struct {
	kind domain.LimitKind
	add  func(t *testing.T, usage *usageDouble) error
}

func recordCases() map[string]recordCase {
	ctx := context.Background()
	return map[string]recordCase{
		"goal contribution": {domain.LimitGoalContributions, func(t *testing.T, usage *usageDouble) error {
			f := newGoalFixture(t)
			goal := mustCreateGoal(t, f)
			f.usage.adopt(usage)
			_, err := f.svc.AddContribution(ctx, usecase.NewContribution{
				HouseholdID: "house-1", GoalID: goal.ID, AmountMinor: 5000,
				OccurredOn: time.Date(2026, 8, 2, 0, 0, 0, 0, time.UTC),
			}, time.Date(2026, 8, 9, 0, 0, 0, 0, time.UTC))
			return err
		}},
		"bill payment": {domain.LimitBillPayments, func(t *testing.T, usage *usageDouble) error {
			return markBillPaid(t, usage)
		}},
		"holding event": {domain.LimitHoldingEvents, func(t *testing.T, usage *usageDouble) error {
			f := newHoldingFixture(t, "SGD")
			h := f.create(t, "Gold", "SGD")
			f.usage.adopt(usage)
			_, err := f.svc.RecordEvent(ctx, domain.HoldingEvent{
				HouseholdID: f.householdID, HoldingID: h.ID, Kind: domain.HoldingAcquisition,
				Quantity: qty(t, 10), Amount: money(t, 1000, "SGD"), OccurredOn: holdingDay(5),
			}, reportToday)
			return err
		}},
		"holding valuation": {domain.LimitHoldingValuations, func(t *testing.T, usage *usageDouble) error {
			f := newHoldingFixture(t, "SGD")
			h := f.create(t, "Gold", "SGD")
			f.buy(t, h.ID, holdingDay(1), 10, 1000)
			f.usage.adopt(usage)
			_, err := f.svc.RecordValuation(ctx, domain.Valuation{
				HouseholdID: f.householdID, HoldingID: h.ID, UnitPrice: money(t, 120, "SGD"), AsOf: holdingDay(5),
			}, reportToday)
			return err
		}},
		"holding income": {domain.LimitHoldingIncome, func(t *testing.T, usage *usageDouble) error {
			f := newHoldingFixture(t, "SGD")
			h := f.create(t, "Gold", "SGD")
			f.usage.adopt(usage)
			_, err := f.svc.RecordIncome(ctx, domain.HoldingIncome{
				HouseholdID: f.householdID, HoldingID: h.ID, Kind: domain.IncomeReceived,
				Amount: money(t, 500, "SGD"), ReceivedOn: holdingDay(5),
			}, reportToday)
			return err
		}},
		"retro action": {domain.LimitRetroActions, func(t *testing.T, usage *usageDouble) error {
			retros := newRetroRepoDouble()
			r := retros.seed(aug2026(), 0, "", false)
			svc := usecase.NewRetroService(retros, newRetroActionRepoDouble(),
				retroHouseholds(time.Date(2020, 1, 15, 9, 0, 0, 0, time.UTC), "UTC"), usage)
			_, err := svc.AddAction(ctx, usecase.RetroActionInput{HouseholdID: "hh", RetroID: r.ID, Body: "book the getaway"})
			return err
		}},
		"agreement proposal": {domain.LimitAgreementProposals, func(t *testing.T, usage *usageDouble) error {
			members := seedMembers(t, 2)
			repo := newAgreementRepoDouble()
			repo.setMembers(members)
			section := repo.seedSection("Money")
			svc := usecase.NewAgreementService(repo, members, usage)
			_, _, err := svc.Propose(ctx, "hh", "m1", domain.AgreementProposal{
				Kind: "add", SectionID: section.ID, Body: "One night a week is ours"},
				time.Date(2026, 9, 5, 20, 0, 0, 0, time.UTC))
			return err
		}},
		"budget rollover": {domain.LimitGoalContributions, func(t *testing.T, usage *usageDouble) error {
			f := newBudgetFixture(t)
			july := julyMonth()
			if _, err := f.svc.Save(ctx, "house-1", july, nil, []usecase.BudgetLineInput{
				{CategoryID: "cat-groceries", CapMinor: 520000},
			}, budgetSaveToday); err != nil {
				t.Fatalf("save: %v", err)
			}
			f.addExpense("tx-groceries", "cat-groceries", "", july.AddDate(0, 0, 5), 342000, "SGD")
			goal := f.seedGoal(t, "house-1", "Emergency fund", "SGD", 1000000, false)
			f.usage.adopt(usage)
			_, err := f.svc.RollOver(ctx, "house-1", july, goal.ID, time.Date(2026, 8, 5, 0, 0, 0, 0, time.UTC))
			return err
		}},
	}
}

func TestRecordsAreCappedPerHousehold(t *testing.T) {
	for name, c := range recordCases() {
		t.Run(name, func(t *testing.T) {
			below := newUsageDouble()
			below.set(c.kind, domain.MaxRecords-1)
			if err := c.add(t, below); err != nil {
				t.Fatalf("one below the cap: %v", err)
			}
			at := newUsageDouble()
			at.set(c.kind, domain.MaxRecords)
			err := c.add(t, at)
			var limit *domain.LimitReachedError
			if !errors.As(err, &limit) || limit.Kind != c.kind {
				t.Fatalf("at the cap: err = %v, want the %s cap", err, c.kind)
			}
		})
	}
}

// A bill payment writes a ledger row as well as a payment, so it meets both
// transaction caps too, not only the bill-payment cap.
func TestABillPaymentMeetsTheTransactionCaps(t *testing.T) {
	t.Run("total", func(t *testing.T) {
		usage := newUsageDouble()
		usage.set(domain.LimitTransactions, domain.MaxTransactions)
		if err := markBillPaid(t, usage); !errors.Is(err, domain.ErrLimitReached) {
			t.Fatalf("err = %v, want ErrLimitReached", err)
		}
	})
	t.Run("the paid-on month", func(t *testing.T) {
		usage := newUsageDouble()
		usage.setMonth(day("2026-08-08"), domain.MaxTransactionsPerMonth)
		if err := markBillPaid(t, usage); !errors.Is(err, domain.ErrLimitReached) {
			t.Fatalf("err = %v, want ErrLimitReached", err)
		}
	})
}

// Valuations are unique per holding per day: recording one for a day that
// already has one replaces it, so it adds no row and is allowed at the cap.
func TestAValuationReplacingTheSameDayIsAllowedAtTheCap(t *testing.T) {
	ctx := context.Background()
	f := newHoldingFixture(t, "SGD")
	h := f.create(t, "Gold", "SGD")
	f.buy(t, h.ID, holdingDay(1), 10, 1000)
	f.price(t, h.ID, holdingDay(5), 120)
	f.usage.set(domain.LimitHoldingValuations, domain.MaxRecords)

	if _, err := f.svc.RecordValuation(ctx, domain.Valuation{
		HouseholdID: f.householdID, HoldingID: h.ID, UnitPrice: money(t, 125, "SGD"), AsOf: holdingDay(5),
	}, reportToday); err != nil {
		t.Fatalf("replacing the same day at the cap: %v", err)
	}
	_, err := f.svc.RecordValuation(ctx, domain.Valuation{
		HouseholdID: f.householdID, HoldingID: h.ID, UnitPrice: money(t, 125, "SGD"), AsOf: holdingDay(6),
	}, reportToday)
	if !errors.Is(err, domain.ErrLimitReached) {
		t.Fatalf("a new day at the cap: err = %v, want ErrLimitReached", err)
	}
}

// Notes on records are limited to 500 characters.
func TestRecordNotesOverFiveHundredCharactersAreRefused(t *testing.T) {
	ctx := context.Background()
	long := strings.Repeat("a", domain.MaxNoteLen+1)

	t.Run("goal contribution", func(t *testing.T) {
		f := newGoalFixture(t)
		goal := mustCreateGoal(t, f)
		_, err := f.svc.AddContribution(ctx, usecase.NewContribution{
			HouseholdID: "house-1", GoalID: goal.ID, AmountMinor: 5000, Note: long,
			OccurredOn: time.Date(2026, 8, 2, 0, 0, 0, 0, time.UTC),
		}, time.Date(2026, 8, 9, 0, 0, 0, 0, time.UTC))
		if !errors.Is(err, domain.ErrTextTooLong) {
			t.Fatalf("err = %v, want ErrTextTooLong", err)
		}
	})
	t.Run("holding event", func(t *testing.T) {
		f := newHoldingFixture(t, "SGD")
		h := f.create(t, "Gold", "SGD")
		_, err := f.svc.RecordEvent(ctx, domain.HoldingEvent{
			HouseholdID: f.householdID, HoldingID: h.ID, Kind: domain.HoldingAcquisition,
			Quantity: qty(t, 10), Amount: money(t, 1000, "SGD"), OccurredOn: holdingDay(5), Note: long,
		}, reportToday)
		if !errors.Is(err, domain.ErrTextTooLong) {
			t.Fatalf("err = %v, want ErrTextTooLong", err)
		}
	})
	t.Run("holding valuation", func(t *testing.T) {
		f := newHoldingFixture(t, "SGD")
		h := f.create(t, "Gold", "SGD")
		f.buy(t, h.ID, holdingDay(1), 10, 1000)
		_, err := f.svc.RecordValuation(ctx, domain.Valuation{
			HouseholdID: f.householdID, HoldingID: h.ID, UnitPrice: money(t, 120, "SGD"), AsOf: holdingDay(5), Note: long,
		}, reportToday)
		if !errors.Is(err, domain.ErrTextTooLong) {
			t.Fatalf("err = %v, want ErrTextTooLong", err)
		}
	})
	t.Run("holding income", func(t *testing.T) {
		f := newHoldingFixture(t, "SGD")
		h := f.create(t, "Gold", "SGD")
		_, err := f.svc.RecordIncome(ctx, domain.HoldingIncome{
			HouseholdID: f.householdID, HoldingID: h.ID, Kind: domain.IncomeReceived,
			Amount: money(t, 500, "SGD"), ReceivedOn: holdingDay(5), Note: long,
		}, reportToday)
		if !errors.Is(err, domain.ErrTextTooLong) {
			t.Fatalf("err = %v, want ErrTextTooLong", err)
		}
	})
	t.Run("retro action", func(t *testing.T) {
		retros := newRetroRepoDouble()
		r := retros.seed(aug2026(), 0, "", false)
		svc := newRetroService(retros, newRetroActionRepoDouble())
		_, err := svc.AddAction(ctx, usecase.RetroActionInput{HouseholdID: "hh", RetroID: r.ID, Body: long})
		if !errors.Is(err, domain.ErrTextTooLong) {
			t.Fatalf("err = %v, want ErrTextTooLong", err)
		}
	})
}

func mustCreateGoal(t *testing.T, f *goalFixture) domain.Goal {
	t.Helper()
	goal, err := f.svc.Create(context.Background(), usecase.NewGoal{
		HouseholdID: "house-1", Name: "Car", TargetMinor: 1000000, Currency: "SGD", PlannedMonthlyMinor: 10000,
	}, time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("create goal: %v", err)
	}
	return goal
}

// markBillPaid pays a seeded bill on 8 August against a service whose usage
// double is usage.
func markBillPaid(t *testing.T, usage *usageDouble) error {
	t.Helper()
	repo := &fakeBillRepo{}
	svc := newBillService(t, repo, withUsage(usage))
	repo.add(bill("SP utilities", "2026-08-08", 14230))
	_, err := svc.MarkPaid(context.Background(), usecase.MarkPayment{
		HouseholdID: "h1", BillID: "bill-1", PaidOn: day("2026-08-08"),
	}, day("2026-08-08"))
	return err
}
