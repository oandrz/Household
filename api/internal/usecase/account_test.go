package usecase_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/andreasoentoro/hearth/api/internal/domain"
	"github.com/andreasoentoro/hearth/api/internal/usecase"
)

// fixedNow is the clock every test in this file runs against, so "in the
// future" is a fact about the input rather than about when the suite ran.
var fixedNow = time.Date(2026, 7, 28, 9, 0, 0, 0, time.UTC)

// accountToday is the household's calendar day these tests pass to the
// account service: the day fixedNow falls on, as midnight UTC, the shape
// domain.TodayIn produces.
var accountToday = time.Date(2026, 7, 28, 0, 0, 0, 0, time.UTC)

func newAccountService(t *testing.T) (*usecase.AccountService, *fakeAccountRepo) {
	t.Helper()
	repo := newFakeAccountRepo()
	repo.memberships["m-1"] = "h-1"

	// Populated with a primary currency: AccountService.Summary reads
	// household.PrimaryCurrency to convert balances into it.
	households := newHouseholdDouble()
	households.put(domain.Household{
		ID: "h-1", Name: "Andreas & Christine", FamilyName: "Oentoro",
		PrimaryCurrency: "SGD", ShowSecondaryCurrency: true, SecondaryCurrency: "IDR", FXRateMode: "auto",
	})

	svc := usecase.NewAccountService(usecase.AccountDeps{
		Accounts:   repo,
		Households: households,
		FX:         newFXDouble(),
		Clock:      &fixedClock{now: fixedNow},
		Holdings:   holdingCounterDouble{},
	})
	return svc, repo
}

// newAccountServiceWithHoldings is newAccountService, but the account already
// holds something, for the one rule that depends on it.
func newAccountServiceWithHoldings(t *testing.T, count int64) (*usecase.AccountService, *fakeAccountRepo) {
	t.Helper()
	svc, repo := newAccountService(t)
	_ = svc
	households := newHouseholdDouble()
	households.put(domain.Household{
		ID: "h-1", Name: "Andreas & Christine", FamilyName: "Oentoro",
		PrimaryCurrency: "SGD", ShowSecondaryCurrency: true, SecondaryCurrency: "IDR", FXRateMode: "auto",
	})
	return usecase.NewAccountService(usecase.AccountDeps{
		Accounts:   repo,
		Households: households,
		FX:         newFXDouble(),
		Clock:      &fixedClock{now: fixedNow},
		Holdings:   holdingCounterDouble{n: count},
	}), repo
}

// Account type is patchable; without this guard an owner could turn a
// brokerage still holding gold into a cash account, anchoring the holding to
// a type that refuses to accept it. The counter port exists for this check.
func TestAccountTypeCannotChangeWhileTheAccountHoldsInvestments(t *testing.T) {
	svc, repo := newAccountServiceWithHoldings(t, 1)
	created, err := svc.Create(context.Background(), investmentAccountInput(), accountToday)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	_ = repo

	cash := "cash"
	_, err = svc.Update(context.Background(), "h-1", created.ID, usecase.AccountUpdate{Type: &cash}, accountToday)
	if !errors.Is(err, domain.ErrAccountHasHoldings) {
		t.Fatalf("error = %v, want ErrAccountHasHoldings", err)
	}
}

// The guard is about the TYPE changing, not about touching the account at all:
// renaming a brokerage that holds something must still work.
func TestAnAccountWithHoldingsCanStillBeRenamed(t *testing.T) {
	svc, _ := newAccountServiceWithHoldings(t, 1)
	created, err := svc.Create(context.Background(), investmentAccountInput(), accountToday)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	name := "Moomoo SG"
	updated, err := svc.Update(context.Background(), "h-1", created.ID, usecase.AccountUpdate{Nickname: &name}, accountToday)
	if err != nil {
		t.Fatalf("Update nickname: %v", err)
	}
	if updated.Nickname != "Moomoo SG" {
		t.Fatalf("Nickname = %q, want Moomoo SG", updated.Nickname)
	}
}

// And an account with NO holdings changes type freely, as it always has.
func TestAccountTypeStillChangesWhenNothingIsHeld(t *testing.T) {
	svc, _ := newAccountServiceWithHoldings(t, 0)
	created, err := svc.Create(context.Background(), investmentAccountInput(), accountToday)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	cash := "cash"
	updated, err := svc.Update(context.Background(), "h-1", created.ID, usecase.AccountUpdate{Type: &cash}, accountToday)
	if err != nil {
		t.Fatalf("Update type: %v", err)
	}
	if updated.Type != domain.AccountCash {
		t.Fatalf("Type = %q, want cash", updated.Type)
	}
}

func investmentAccountInput() usecase.NewAccount {
	in := validNewAccount()
	in.Nickname = "Brokerage"
	in.Type = "investment"
	return in
}

// holdingCounterDouble stands in for the holdings table, reporting n live
// holdings.
type holdingCounterDouble struct{ n int64 }

func (d holdingCounterDouble) CountLiveForAccount(_ context.Context, _, _ string) (int64, error) {
	return d.n, nil
}

func (d holdingCounterDouble) CountForHousehold(_ context.Context, _ string) (int64, error) {
	return d.n, nil
}

func validNewAccount() usecase.NewAccount {
	return usecase.NewAccount{
		HouseholdID:            "h-1",
		Nickname:               "DBS Everyday",
		Type:                   "cash",
		OpeningBalanceMinor:    824_055,
		OpeningBalanceCurrency: "SGD",
		OpeningBalanceAsOf:     fixedNow.AddDate(0, 0, -2),
		CountTowardNetWorth:    true,
	}
}

func TestCreateRefusesABlankNickname(t *testing.T) {
	svc, _ := newAccountService(t)
	in := validNewAccount()
	in.Nickname = "   "

	_, err := svc.Create(context.Background(), in, accountToday)
	if !errors.Is(err, domain.ErrAccountNicknameRequired) {
		t.Fatalf("err = %v, want ErrAccountNicknameRequired", err)
	}
}

func TestCreateTrimsTheNickname(t *testing.T) {
	svc, _ := newAccountService(t)
	in := validNewAccount()
	in.Nickname = "  DBS Everyday  "

	got, err := svc.Create(context.Background(), in, accountToday)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if got.Nickname != "DBS Everyday" {
		t.Errorf("Nickname = %q, want %q", got.Nickname, "DBS Everyday")
	}
}

func TestCreateRefusesAnUnknownType(t *testing.T) {
	svc, _ := newAccountService(t)
	in := validNewAccount()
	in.Type = "crypto"

	_, err := svc.Create(context.Background(), in, accountToday)
	if !errors.Is(err, domain.ErrUnknownAccountType) {
		t.Fatalf("err = %v, want ErrUnknownAccountType", err)
	}
}

// TestCreateRefusesACurrencyTheMoneyPathRendersWrong covers JPY: a real ISO
// 4217 code ParseCurrency accepts, but Money.String hard-codes two decimals
// and would render it a hundred times too small -- the same gate a
// household's primary currency goes through.
func TestCreateRefusesACurrencyTheMoneyPathRendersWrong(t *testing.T) {
	svc, _ := newAccountService(t)
	for _, code := range []string{"ZZZ", "JPY", "KWD"} {
		in := validNewAccount()
		in.OpeningBalanceCurrency = code

		if _, err := svc.Create(context.Background(), in, accountToday); !errors.Is(err, domain.ErrInvalidMoney) {
			t.Errorf("%s: err = %v, want ErrInvalidMoney", code, err)
		}
	}
}

func TestCreateRefusesAFutureOpeningBalanceDate(t *testing.T) {
	svc, _ := newAccountService(t)
	in := validNewAccount()
	in.OpeningBalanceAsOf = accountToday.AddDate(0, 0, 7)

	_, err := svc.Create(context.Background(), in, accountToday)
	if !errors.Is(err, domain.ErrOpeningBalanceInFuture) {
		t.Fatalf("err = %v, want ErrOpeningBalanceInFuture", err)
	}
}

// The opening balance is a fact, so it may be dated up to the household's
// today and no later. "Today" is passed in as the household's calendar day,
// worked out from its time zone at the edge.
//
// This replaces TestCreateAcceptsTodayFromAnyTimezone. That test pinned a day
// of slack past the server's clock, which existed only because a household
// stored no time zone and the server could not know its today. With the zone
// stored the slack is gone, and tomorrow is refused.
func TestCreateAcceptsTheHouseholdsTodayAndRefusesItsTomorrow(t *testing.T) {
	svc, _ := newAccountService(t)

	today := validNewAccount()
	today.OpeningBalanceAsOf = accountToday
	if _, err := svc.Create(context.Background(), today, accountToday); err != nil {
		t.Fatalf("Create dated today: %v", err)
	}

	tomorrow := validNewAccount()
	tomorrow.Nickname = "OCBC 360"
	tomorrow.OpeningBalanceAsOf = accountToday.AddDate(0, 0, 1)
	if _, err := svc.Create(context.Background(), tomorrow, accountToday); !errors.Is(err, domain.ErrOpeningBalanceInFuture) {
		t.Fatalf("Create dated tomorrow: err = %v, want ErrOpeningBalanceInFuture", err)
	}
}

// The service reads no clock for this rule. With its own clock late in the
// evening and the household's day still passed in as the 28th, the 29th is
// refused all the same: the answer comes from the argument.
func TestTheOpeningBalanceRuleReadsTheDayItIsGivenNotTheClock(t *testing.T) {
	repo := newFakeAccountRepo()
	households := newHouseholdDouble()
	households.put(domain.Household{ID: "h-1", PrimaryCurrency: "SGD"})
	svc := usecase.NewAccountService(usecase.AccountDeps{
		Accounts: repo, Households: households, FX: newFXDouble(),
		// A whole week ahead of the day passed in below.
		Clock:    &fixedClock{now: accountToday.AddDate(0, 0, 7)},
		Holdings: holdingCounterDouble{},
	})

	in := validNewAccount()
	in.OpeningBalanceAsOf = accountToday.AddDate(0, 0, 1)
	if _, err := svc.Create(context.Background(), in, accountToday); !errors.Is(err, domain.ErrOpeningBalanceInFuture) {
		t.Fatalf("err = %v, want ErrOpeningBalanceInFuture", err)
	}
}

func TestUpdateRefusesMovingTheOpeningBalanceIntoTheFuture(t *testing.T) {
	svc, _ := newAccountService(t)
	created, err := svc.Create(context.Background(), validNewAccount(), accountToday)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	tomorrow := accountToday.AddDate(0, 0, 1)
	_, err = svc.Update(context.Background(), "h-1", created.ID,
		usecase.AccountUpdate{OpeningBalanceAsOf: &tomorrow}, accountToday)
	if !errors.Is(err, domain.ErrOpeningBalanceInFuture) {
		t.Fatalf("err = %v, want ErrOpeningBalanceInFuture", err)
	}
}

// A stored date is checked only when a patch changes it. An account can be
// dated after the household's today without anyone typing a future date: an
// owner moves the household to a zone further west, and yesterday's "today"
// is now tomorrow. Renaming that account must still work, or the owner meets
// "That date is in the future" on a field they did not touch.
func TestUpdateLeavesAStoredOpeningDateAloneWhenThePatchDoesNotChangeIt(t *testing.T) {
	svc, _ := newAccountService(t)
	in := validNewAccount()
	in.OpeningBalanceAsOf = accountToday
	created, err := svc.Create(context.Background(), in, accountToday)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	// The household's day is now the day BEFORE the stored date.
	yesterday := accountToday.AddDate(0, 0, -1)
	renamed := "DBS Multiplier"

	t.Run("a patch that does not mention the date", func(t *testing.T) {
		got, err := svc.Update(context.Background(), "h-1", created.ID,
			usecase.AccountUpdate{Nickname: &renamed}, yesterday)
		if err != nil {
			t.Fatalf("Update: %v", err)
		}
		if got.Nickname != renamed || !got.OpeningBalanceAsOf.Equal(accountToday) {
			t.Fatalf("account = %+v, want it renamed with its date untouched", got)
		}
	})

	// The edit form sends every field back, changed or not.
	t.Run("a patch that sends the same date back", func(t *testing.T) {
		same := accountToday
		if _, err := svc.Update(context.Background(), "h-1", created.ID,
			usecase.AccountUpdate{Nickname: &renamed, OpeningBalanceAsOf: &same}, yesterday); err != nil {
			t.Fatalf("Update: %v", err)
		}
	})
}

func TestCreateRefusesANegativeDebt(t *testing.T) {
	svc, _ := newAccountService(t)
	in := validNewAccount()
	in.Type = "loan"
	in.OpeningBalanceMinor = -1_450_000

	_, err := svc.Create(context.Background(), in, accountToday)
	if !errors.Is(err, domain.ErrLiabilityBalanceNegative) {
		t.Fatalf("err = %v, want ErrLiabilityBalanceNegative", err)
	}
}

// TestCreateAllowsANegativeAsset is the other half of the rule: an overdrawn
// current account is an ordinary thing, and only debts are constrained.
func TestCreateAllowsANegativeAsset(t *testing.T) {
	svc, _ := newAccountService(t)
	in := validNewAccount()
	in.OpeningBalanceMinor = -12_000

	if _, err := svc.Create(context.Background(), in, accountToday); err != nil {
		t.Fatalf("Create: %v", err)
	}
}

// TestCreateRefusesAnOwnerFromAnotherHousehold is the check that only shows up
// once there are two households in the database -- which, with self-serve
// sign-up, is every deployment.
func TestCreateRefusesAnOwnerFromAnotherHousehold(t *testing.T) {
	svc, repo := newAccountService(t)
	repo.memberships["m-other"] = "h-2"

	in := validNewAccount()
	in.OwnerMembershipID = "m-other"

	_, err := svc.Create(context.Background(), in, accountToday)
	if !errors.Is(err, domain.ErrAccountOwnerNotInHousehold) {
		t.Fatalf("err = %v, want ErrAccountOwnerNotInHousehold", err)
	}
}

func TestCreateAcceptsAnEmptyOwnerAsShared(t *testing.T) {
	svc, _ := newAccountService(t)
	in := validNewAccount()
	in.OwnerMembershipID = ""

	got, err := svc.Create(context.Background(), in, accountToday)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if got.OwnerMembershipID != "" {
		t.Errorf("OwnerMembershipID = %q, want \"\" (shared)", got.OwnerMembershipID)
	}
}

// TestUpdateIsARealPatch mirrors TestUpdateHouseholdIsARealPatch: a field the
// caller did not name keeps its stored value rather than being reset to a
// zero.
func TestUpdateIsARealPatch(t *testing.T) {
	svc, _ := newAccountService(t)
	created, err := svc.Create(context.Background(), validNewAccount(), accountToday)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	nickname := "DBS Salary"
	got, err := svc.Update(context.Background(), "h-1", created.ID, usecase.AccountUpdate{
		Nickname: &nickname,
	}, accountToday)
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if got.Nickname != "DBS Salary" {
		t.Errorf("Nickname = %q, want %q", got.Nickname, "DBS Salary")
	}
	if got.Type != domain.AccountCash {
		t.Errorf("Type = %q, want cash -- an unnamed field was reset", got.Type)
	}
	if got.OpeningBalance.Amount != 824_055 {
		t.Errorf("OpeningBalance = %d, want 824055 -- an unnamed field was reset", got.OpeningBalance.Amount)
	}
}

// TestUpdateCanClearTheOwnerToShared: without a pointer-to-"" meaning
// "shared" (see AccountUpdate), a wrongly assigned account could never be
// un-assigned.
func TestUpdateCanClearTheOwnerToShared(t *testing.T) {
	svc, _ := newAccountService(t)
	in := validNewAccount()
	in.OwnerMembershipID = "m-1"
	created, err := svc.Create(context.Background(), in, accountToday)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	shared := ""
	got, err := svc.Update(context.Background(), "h-1", created.ID, usecase.AccountUpdate{
		OwnerMembershipID: &shared,
	}, accountToday)
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if got.OwnerMembershipID != "" {
		t.Errorf("OwnerMembershipID = %q, want \"\"", got.OwnerMembershipID)
	}
}

// TestUpdateRefusesANegativeBalanceWhenTheTypeBecomesADebt is only reachable
// via a per-field patch: neither change is invalid alone, but validation
// runs against the merged account, so the pair together is.
func TestUpdateRefusesANegativeBalanceWhenTheTypeBecomesADebt(t *testing.T) {
	svc, _ := newAccountService(t)
	in := validNewAccount()
	in.OpeningBalanceMinor = -12_000 // legal for cash
	created, err := svc.Create(context.Background(), in, accountToday)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	loan := "loan"
	_, err = svc.Update(context.Background(), "h-1", created.ID, usecase.AccountUpdate{Type: &loan}, accountToday)
	if !errors.Is(err, domain.ErrLiabilityBalanceNegative) {
		t.Fatalf("err = %v, want ErrLiabilityBalanceNegative", err)
	}
}

func TestListExcludesArchivedAccountsByDefault(t *testing.T) {
	svc, _ := newAccountService(t)
	created, err := svc.Create(context.Background(), validNewAccount(), accountToday)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if _, err := svc.SetArchived(context.Background(), "h-1", created.ID, true); err != nil {
		t.Fatalf("SetArchived: %v", err)
	}

	live, err := svc.List(context.Background(), "h-1", false)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(live) != 0 {
		t.Errorf("live accounts = %d, want 0", len(live))
	}

	all, err := svc.List(context.Background(), "h-1", true)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(all) != 1 {
		t.Errorf("all accounts = %d, want 1", len(all))
	}
}
