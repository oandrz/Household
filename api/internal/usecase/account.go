package usecase

import (
	"context"
	"strings"
	"time"

	"github.com/andreasoentoro/hearth/api/internal/domain"
)

// NewAccount is the create input. It is a struct rather than nine parameters
// because five of them are the same two types and a caller swapping two would
// compile.
//
// OwnerMembershipID follows the "" <-> SQL NULL convention: "" means shared.
type NewAccount struct {
	HouseholdID             string
	Nickname                string
	Type                    string
	OwnerMembershipID       string
	OpeningBalanceMinor     int64
	OpeningBalanceCurrency  string
	OpeningBalanceAsOf      time.Time
	CountTowardNetWorth     bool
	VisibleToLimitedMembers bool
}

// AccountUpdate is a real patch: a nil pointer means "leave this field alone".
//
// OwnerMembershipID is a *string rather than a **string, and the two states a
// caller needs are both reachable: nil leaves the owner unchanged, and a
// pointer to "" makes the account shared. Without the second, an account
// assigned to the wrong person could never be un-assigned.
type AccountUpdate struct {
	Nickname                *string
	Type                    *string
	OwnerMembershipID       *string
	OpeningBalanceMinor     *int64
	OpeningBalanceCurrency  *string
	OpeningBalanceAsOf      *time.Time
	CountTowardNetWorth     *bool
	VisibleToLimitedMembers *bool
}

// AccountDeps mirrors HouseholdDeps: every port AccountService needs, gathered
// into one named argument.
type AccountDeps struct {
	Accounts   AccountRepository
	Households HouseholdRepository
	FX         FXRateProvider
	Clock      Clock
}

// AccountService covers the Finances screen: the accounts themselves and the
// net worth summary computed from them.
//
// It takes no actor parameter, by the rule this whole codebase follows:
// services enforce what is *valid*, middleware enforces who is *asking*. The
// money capability and the owner check live in the router.
type AccountService struct {
	d AccountDeps
}

func NewAccountService(d AccountDeps) *AccountService {
	return &AccountService{d: d}
}

func (s *AccountService) List(ctx context.Context, householdID string, includeArchived bool) ([]AccountView, error) {
	return s.d.Accounts.List(ctx, householdID, includeArchived)
}

func (s *AccountService) Get(ctx context.Context, householdID, accountID string) (AccountView, error) {
	return s.d.Accounts.Get(ctx, householdID, accountID)
}

func (s *AccountService) Create(ctx context.Context, in NewAccount) (domain.Account, error) {
	account := domain.Account{
		HouseholdID:             in.HouseholdID,
		Nickname:                in.Nickname,
		OwnerMembershipID:       in.OwnerMembershipID,
		OpeningBalanceAsOf:      in.OpeningBalanceAsOf,
		CountTowardNetWorth:     in.CountTowardNetWorth,
		VisibleToLimitedMembers: in.VisibleToLimitedMembers,
		OpeningBalance:          domain.Money{Amount: in.OpeningBalanceMinor, Currency: in.OpeningBalanceCurrency},
		Type:                    domain.AccountType(in.Type),
	}
	if err := s.validate(ctx, &account); err != nil {
		return domain.Account{}, err
	}
	return s.d.Accounts.Create(ctx, account)
}

// Update merges the patch onto the stored account and then validates the
// *result*, never the incoming fields. That ordering is the point: switching a
// type to "loan" and leaving a negative balance alone are each legal in
// isolation and illegal together, so validating the patch would let the pair
// through.
func (s *AccountService) Update(ctx context.Context, householdID, accountID string, patch AccountUpdate) (domain.Account, error) {
	view, err := s.d.Accounts.Get(ctx, householdID, accountID)
	if err != nil {
		return domain.Account{}, err
	}
	account := view.Account

	if patch.Nickname != nil {
		account.Nickname = *patch.Nickname
	}
	if patch.Type != nil {
		account.Type = domain.AccountType(*patch.Type)
	}
	if patch.OwnerMembershipID != nil {
		account.OwnerMembershipID = *patch.OwnerMembershipID
	}
	if patch.OpeningBalanceMinor != nil {
		account.OpeningBalance.Amount = *patch.OpeningBalanceMinor
	}
	if patch.OpeningBalanceCurrency != nil {
		account.OpeningBalance.Currency = *patch.OpeningBalanceCurrency
	}
	if patch.OpeningBalanceAsOf != nil {
		account.OpeningBalanceAsOf = *patch.OpeningBalanceAsOf
	}
	if patch.CountTowardNetWorth != nil {
		account.CountTowardNetWorth = *patch.CountTowardNetWorth
	}
	if patch.VisibleToLimitedMembers != nil {
		account.VisibleToLimitedMembers = *patch.VisibleToLimitedMembers
	}

	if err := s.validate(ctx, &account); err != nil {
		return domain.Account{}, err
	}
	return s.d.Accounts.Update(ctx, account)
}

func (s *AccountService) SetArchived(ctx context.Context, householdID, accountID string, archived bool) (domain.Account, error) {
	return s.d.Accounts.SetArchived(ctx, householdID, accountID, archived, s.d.Clock.Now())
}

// validate normalises and checks an assembled account in place. It is shared by
// Create and Update so the two cannot drift -- the class of defect this project
// has hit four times is a rule fixed at one call site while its sibling kept
// the bug.
func (s *AccountService) validate(ctx context.Context, a *domain.Account) error {
	a.Nickname = strings.TrimSpace(a.Nickname)
	if a.Nickname == "" {
		return domain.ErrAccountNicknameRequired
	}

	accountType, err := domain.ParseAccountType(string(a.Type))
	if err != nil {
		return err
	}
	a.Type = accountType

	// ParseSelectableCurrency, not ParseCurrency: Money.String hard-codes two
	// decimal places, so a JPY or KWD account would render every amount
	// wrong. The identical gate a household's primary currency goes through.
	code, err := domain.ParseSelectableCurrency(a.OpeningBalance.Currency)
	if err != nil {
		return err
	}
	a.OpeningBalance.Currency = code

	// A whole day of tolerance, deliberately. This product stores no timezone
	// for a household, so the server cannot know what "today" means to the
	// person filling in the form: at 17:00 UTC it is already tomorrow in
	// Singapore, and a household there entering today's balance would be
	// refused for eight hours out of every twenty-four. Real zones span UTC-12
	// to UTC+14, so one day of slack covers every one of them.
	//
	// The asymmetry is what makes this the right trade: accepting a balance
	// dated a day early costs nothing -- it is a figure the owner typed and can
	// edit -- while refusing a genuine "today" is a wall with no way past it
	// and no explanation that would make sense to the person hitting it. The
	// check exists to catch a typo like 2062, not to police the date line.
	if a.OpeningBalanceAsOf.After(s.d.Clock.Now().AddDate(0, 0, 1)) {
		return domain.ErrOpeningBalanceInFuture
	}

	if a.Type.IsLiability() && a.OpeningBalance.Amount < 0 {
		return domain.ErrLiabilityBalanceNegative
	}

	if a.OwnerMembershipID != "" {
		ok, err := s.d.Accounts.MembershipBelongsToHousehold(ctx, a.HouseholdID, a.OwnerMembershipID)
		if err != nil {
			return err
		}
		if !ok {
			return domain.ErrAccountOwnerNotInHousehold
		}
	}
	return nil
}
