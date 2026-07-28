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
	svc := usecase.NewAccountService(usecase.AccountDeps{
		Accounts:   repo,
		Households: newHouseholdDouble(),
		FX:         staticTestRates{},
		Clock:      &fixedClock{now: fixedNow},
	})
	return svc, repo
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

// TestCreateRefusesACurrencyTheMoneyPathRendersWrong covers JPY: it is a real
// ISO 4217 code, so ParseCurrency would accept it, but Money.String hard-codes
// two decimal places and would render every JPY amount a hundred times too
// small. The same gate a household's primary currency goes through.
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
	in.OpeningBalanceAsOf = fixedNow.AddDate(0, 0, 1)

	_, err := svc.Create(context.Background(), in)
	if !errors.Is(err, domain.ErrOpeningBalanceInFuture) {
		t.Fatalf("err = %v, want ErrOpeningBalanceInFuture", err)
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
// once there are two households in the database -- which, since self-serve
// sign-up shipped, is every deployment.
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

// TestUpdateCanClearTheOwnerToShared documents the one subtlety in the patch
// shape: a nil pointer means "leave it alone", and a pointer to "" means "make
// this shared". Without the second, an account assigned to the wrong person
// could never be un-assigned.
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

// TestUpdateRefusesANegativeBalanceWhenTheTypeBecomesADebt is the case a
// per-field patch makes reachable: neither change is invalid alone, and the
// pair is. Validation therefore runs against the merged account, never against
// the incoming fields.
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
