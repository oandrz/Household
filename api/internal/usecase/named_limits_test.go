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

// namedThingCase is one kind of named thing: how to build a fresh service
// whose usage double the test controls, and how to create one with a given
// name. Each case runs against a fresh service so a create in one step
// cannot change what the next one sees.
type namedThingCase struct {
	kind   domain.LimitKind
	create func(t *testing.T, usage *usageDouble, name string) error
}

func namedThingCases() map[string]namedThingCase {
	ctx := context.Background()
	return map[string]namedThingCase{
		"account": {domain.LimitAccounts, func(t *testing.T, usage *usageDouble, name string) error {
			svc, _ := newAccountServiceWithUsage(t, usage)
			in := validNewAccount()
			in.Nickname = name
			_, err := svc.Create(ctx, in, accountToday)
			return err
		}},
		"category": {domain.LimitCategories, func(t *testing.T, usage *usageDouble, name string) error {
			svc := usecase.NewCategoryService(&fakeCategoryRepo{}, usage)
			_, err := svc.Create(ctx, "house-1", name)
			return err
		}},
		"goal": {domain.LimitGoals, func(t *testing.T, usage *usageDouble, name string) error {
			f := newGoalFixture(t)
			f.usage.adopt(usage)
			_, err := f.svc.Create(ctx, usecase.NewGoal{
				HouseholdID: "house-1", Name: name, TargetMinor: 100000, Currency: "SGD", PlannedMonthlyMinor: 10000,
			}, time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC))
			return err
		}},
		"bill": {domain.LimitBills, func(t *testing.T, usage *usageDouble, name string) error {
			svc := newBillService(t, &fakeBillRepo{}, withUsage(usage))
			_, err := svc.Create(ctx, usecase.NewBill{
				HouseholdID: "h1", Name: name, AmountMinor: 250000,
				Cadence: domain.CadenceMonthly, NextDue: day("2026-08-20"), PayFromAccountID: "acct-1",
			}, day("2026-08-09"))
			return err
		}},
		"holding": {domain.LimitHoldings, func(t *testing.T, usage *usageDouble, name string) error {
			f := newHoldingFixture(t, "SGD")
			f.usage.adopt(usage)
			_, err := f.svc.Create(ctx, domain.Holding{
				HouseholdID: f.householdID, AccountID: f.accountID, Name: name,
				Instrument: domain.InstrumentStock, Unit: "share", Currency: "SGD",
			})
			return err
		}},
		"space": {domain.LimitSpaces, func(t *testing.T, usage *usageDouble, name string) error {
			f := newFixture(t)
			f.householdUsage.adopt(usage)
			_, err := f.householdSvc.CreateSpace(ctx, f.householdID, name, domain.VisibilityEveryone)
			return err
		}},
		"agreement section": {domain.LimitAgreementSections, func(t *testing.T, usage *usageDouble, name string) error {
			members := seedMembers(t, 2)
			repo := newAgreementRepoDouble()
			repo.setMembers(members)
			svc := usecase.NewAgreementService(repo, members, usage)
			_, _, err := svc.CreateSection(ctx, "hh", name, time.Date(2026, 9, 5, 20, 0, 0, 0, time.UTC))
			return err
		}},
	}
}

// Named things are capped at MaxNamedThings each. The usage double's count
// stands in for archived and live rows alike: the Postgres count includes
// archived ones (usage_repo_test.go), since these are archived, not deleted.
func TestNamedThingsAreCappedPerHousehold(t *testing.T) {
	for name, c := range namedThingCases() {
		t.Run(name, func(t *testing.T) {
			below := newUsageDouble()
			below.set(c.kind, domain.MaxNamedThings-1)
			if err := c.create(t, below, "Below the cap"); err != nil {
				t.Fatalf("one below the cap: %v", err)
			}
			at := newUsageDouble()
			at.set(c.kind, domain.MaxNamedThings)
			err := c.create(t, at, "At the cap")
			var limit *domain.LimitReachedError
			if !errors.As(err, &limit) || limit.Kind != c.kind || limit.Limit != domain.MaxNamedThings {
				t.Fatalf("at the cap: err = %v, want the %s cap", err, c.kind)
			}
		})
	}
}

// Names are limited to 80 characters, counted as characters. Agreement
// sections keep their own, shorter limit and are not part of this rule.
func TestNamesOverEightyCharactersAreRefused(t *testing.T) {
	for name, c := range namedThingCases() {
		if c.kind == domain.LimitAgreementSections {
			continue
		}
		t.Run(name, func(t *testing.T) {
			if err := c.create(t, newUsageDouble(), strings.Repeat("中", domain.MaxNameLen)); err != nil {
				t.Fatalf("80 characters refused: %v", err)
			}
			err := c.create(t, newUsageDouble(), strings.Repeat("a", domain.MaxNameLen+1))
			if !errors.Is(err, domain.ErrTextTooLong) {
				t.Fatalf("81 characters: err = %v, want ErrTextTooLong", err)
			}
		})
	}
}

// The edit paths apply the same name limit as create.
func TestRenamingPastEightyCharactersIsRefused(t *testing.T) {
	ctx := context.Background()
	long := strings.Repeat("a", domain.MaxNameLen+1)

	t.Run("account", func(t *testing.T) {
		svc, _ := newAccountService(t)
		created, err := svc.Create(ctx, validNewAccount(), accountToday)
		if err != nil {
			t.Fatal(err)
		}
		_, err = svc.Update(ctx, created.HouseholdID, created.ID, usecase.AccountUpdate{Nickname: &long}, accountToday)
		if !errors.Is(err, domain.ErrTextTooLong) {
			t.Fatalf("err = %v, want ErrTextTooLong", err)
		}
	})
	t.Run("category", func(t *testing.T) {
		repo := &fakeCategoryRepo{}
		svc := usecase.NewCategoryService(repo, newUsageDouble())
		created, err := svc.Create(ctx, "house-1", "Groceries")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := svc.Rename(ctx, "house-1", created.ID, long); !errors.Is(err, domain.ErrTextTooLong) {
			t.Fatalf("err = %v, want ErrTextTooLong", err)
		}
	})
	t.Run("goal", func(t *testing.T) {
		f := newGoalFixture(t)
		created, err := f.svc.Create(ctx, usecase.NewGoal{
			HouseholdID: "house-1", Name: "Car", TargetMinor: 100000, Currency: "SGD", PlannedMonthlyMinor: 10000,
		}, time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := f.svc.Update(ctx, "house-1", created.ID, usecase.GoalUpdate{Name: &long}); !errors.Is(err, domain.ErrTextTooLong) {
			t.Fatalf("err = %v, want ErrTextTooLong", err)
		}
	})
	t.Run("bill", func(t *testing.T) {
		repo := &fakeBillRepo{}
		svc := newBillService(t, repo)
		created, err := svc.Create(ctx, usecase.NewBill{
			HouseholdID: "h1", Name: "Rent", AmountMinor: 250000,
			Cadence: domain.CadenceMonthly, NextDue: day("2026-08-20"), PayFromAccountID: "acct-1",
		}, day("2026-08-09"))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := svc.Update(ctx, "h1", created.Bill.ID, usecase.BillPatch{Name: &long}, day("2026-08-09")); !errors.Is(err, domain.ErrTextTooLong) {
			t.Fatalf("err = %v, want ErrTextTooLong", err)
		}
	})
	t.Run("holding name and unit", func(t *testing.T) {
		f := newHoldingFixture(t, "SGD")
		h := f.create(t, "Gold", "SGD")
		renamed := h
		renamed.Name = long
		if _, err := f.svc.Update(ctx, renamed); !errors.Is(err, domain.ErrTextTooLong) {
			t.Fatalf("name: err = %v, want ErrTextTooLong", err)
		}
		relabelled := h
		relabelled.Unit = strings.Repeat("g", domain.MaxHoldingUnitLen+1)
		if _, err := f.svc.Update(ctx, relabelled); !errors.Is(err, domain.ErrTextTooLong) {
			t.Fatalf("unit: err = %v, want ErrTextTooLong", err)
		}
	})
}

// A holding's unit is limited to 20 characters on create too.
func TestAHoldingUnitOverTwentyCharactersIsRefused(t *testing.T) {
	f := newHoldingFixture(t, "SGD")
	_, err := f.svc.Create(context.Background(), domain.Holding{
		HouseholdID: f.householdID, AccountID: f.accountID, Name: "Gold",
		Instrument: domain.InstrumentStock, Unit: strings.Repeat("g", domain.MaxHoldingUnitLen+1), Currency: "SGD",
	})
	if !errors.Is(err, domain.ErrTextTooLong) {
		t.Fatalf("err = %v, want ErrTextTooLong", err)
	}
}
