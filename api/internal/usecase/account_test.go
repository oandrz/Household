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
	created, err := svc.Create(context.Background(), investmentAccountInput())
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	_ = repo

	cash := "cash"
	_, err = svc.Update(context.Background(), "h-1", created.ID, usecase.AccountUpdate{Type: &cash})
	if !errors.Is(err, domain.ErrAccountHasHoldings) {
		t.Fatalf("error = %v, want ErrAccountHasHoldings", err)
	}
}

// The guard is about the TYPE changing, not about touching the account at all:
// renaming a brokerage that holds something must still work.
func TestAnAccountWithHoldingsCanStillBeRenamed(t *testing.T) {
	svc, _ := newAccountServiceWithHoldings(t, 1)
	created, err := svc.Create(context.Background(), investmentAccountInput())
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	name := "Moomoo SG"
	updated, err := svc.Update(context.Background(), "h-1", created.ID, usecase.AccountUpdate{Nickname: &name})
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
	created, err := svc.Create(context.Background(), investmentAccountInput())
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	cash := "cash"
	updated, err := svc.Update(context.Background(), "h-1", created.ID, usecase.AccountUpdate{Type: &cash})
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

	_, err := svc.Create(context.Background(), in)
	if !errors.Is(err, domain.ErrAccountNicknameRequired) {
		t.Fatalf("err = %v, want ErrAccountNicknameRequired", err)
	}
}

func TestCreateTrimsTheNickname(t *testing.T) {
	svc, _ := newAccountService(t)
	in := validNewAccount()
	in.Nickname = "  DBS Everyday  "

	got, err := svc.Create(context.Background(), in)
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

	_, err := svc.Create(context.Background(), in)
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

		if _, err := svc.Create(context.Background(), in); !errors.Is(err, domain.ErrInvalidMoney) {
			t.Errorf("%s: err = %v, want ErrInvalidMoney", code, err)
		}
	}
}

func TestCreateRefusesAFutureOpeningBalanceDate(t *testing.T) {
	svc, _ := newAccountService(t)
	in := validNewAccount()
	in.OpeningBalanceAsOf = fixedNow.AddDate(0, 0, 7)

	_, err := svc.Create(context.Background(), in)
	if !errors.Is(err, domain.ErrOpeningBalanceInFuture) {
		t.Fatalf("err = %v, want ErrOpeningBalanceInFuture", err)
	}
}

// TestCreateAcceptsTodayFromAnyTimezone: households store no timezone, so
// from 16:00 UTC it is already tomorrow in Singapore (UTC+8), and without
// slack a household there could not enter today's balance for eight hours
// a day. The clock reads 09:00 UTC on the 28th and the input is the 29th,
// 15 hours ahead: inside the one day of slack that covers every real zone
// (UTC-12 to UTC+14).
func TestCreateAcceptsTodayFromAnyTimezone(t *testing.T) {
	svc, _ := newAccountService(t)
	in := validNewAccount()
	in.OpeningBalanceAsOf = time.Date(2026, 7, 29, 0, 0, 0, 0, time.UTC)

	if _, err := svc.Create(context.Background(), in); err != nil {
		t.Fatalf("Create: %v -- a household in UTC+8 cannot enter today's balance", err)
	}
}

func TestCreateRefusesANegativeDebt(t *testing.T) {
	svc, _ := newAccountService(t)
	in := validNewAccount()
	in.Type = "loan"
	in.OpeningBalanceMinor = -1_450_000

	_, err := svc.Create(context.Background(), in)
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

	if _, err := svc.Create(context.Background(), in); err != nil {
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

	_, err := svc.Create(context.Background(), in)
	if !errors.Is(err, domain.ErrAccountOwnerNotInHousehold) {
		t.Fatalf("err = %v, want ErrAccountOwnerNotInHousehold", err)
	}
}

func TestCreateAcceptsAnEmptyOwnerAsShared(t *testing.T) {
	svc, _ := newAccountService(t)
	in := validNewAccount()
	in.OwnerMembershipID = ""

	got, err := svc.Create(context.Background(), in)
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
	created, err := svc.Create(context.Background(), validNewAccount())
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	nickname := "DBS Salary"
	got, err := svc.Update(context.Background(), "h-1", created.ID, usecase.AccountUpdate{
		Nickname: &nickname,
	})
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
	created, err := svc.Create(context.Background(), in)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	shared := ""
	got, err := svc.Update(context.Background(), "h-1", created.ID, usecase.AccountUpdate{
		OwnerMembershipID: &shared,
	})
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
	created, err := svc.Create(context.Background(), in)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	loan := "loan"
	_, err = svc.Update(context.Background(), "h-1", created.ID, usecase.AccountUpdate{Type: &loan})
	if !errors.Is(err, domain.ErrLiabilityBalanceNegative) {
		t.Fatalf("err = %v, want ErrLiabilityBalanceNegative", err)
	}
}

func TestListExcludesArchivedAccountsByDefault(t *testing.T) {
	svc, _ := newAccountService(t)
	created, err := svc.Create(context.Background(), validNewAccount())
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
